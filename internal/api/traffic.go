package api

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"vps-control/internal/live"
	"vps-control/internal/manifest"
)

// Absolute durable Agent counters make retries and reconnects idempotent.
func (s *Server) telemetry(w http.ResponseWriter, r *http.Request, host string) error {
	if r.Method == "GET" || strings.HasSuffix(r.URL.Path, "/baseline") {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT counter_id id,subject_kind kind,subject_id,uplink,downlink FROM traffic_cursors WHERE tenant_id=$1 AND host_id=$2 ORDER BY counter_id LIMIT 2000) x`, tenant, host).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	var b live.TelemetryBatch
	if e := decodeBounded(w, r, &b, 512<<10); e != nil {
		return e
	}
	if len(b.Counters) > 2000 || len(b.Access) > 100 || len(b.Status) > 2 {
		return bad("telemetry batch exceeds bounded capacity")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if e = resetPeriods(r.Context(), tx); e != nil {
		return e
	}
	for _, v := range b.Status {
		if v.Engine != "sing-box" && v.Engine != "xray" || v.State != "collecting" && v.State != "unavailable" {
			return bad("invalid telemetry status")
		}
		if len(v.Error) > 120 {
			return bad("telemetry error too long")
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO telemetry_status(tenant_id,host_id,engine,state,error) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,host_id,engine) DO UPDATE SET state=excluded.state,error=excluded.error,reported_at=now()`, tenant, host, v.Engine, v.State, v.Error); e != nil {
			return e
		}
	}
	retired := []string{}
	seen := map[string]bool{}
	for _, v := range b.Counters {
		if (v.ID != Hash("sing-box/" + v.Kind + "/" + v.Subject)[:32] && v.ID != Hash("xray/" + v.Kind + "/" + v.Subject)[:32]) || !manifest.ValidID(v.ID) || !manifest.ValidID(v.Subject) || (v.Kind != "user" && v.Kind != "service") || v.Up < 0 || v.Down < 0 || v.Up > 9000000000000000 || v.Down > 9000000000000000 || seen[v.ID] {
			return bad("invalid traffic counter")
		}
		seen[v.ID] = true
		var allowed bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM traffic_subjects WHERE tenant_id=$1 AND host_id=$2 AND subject_kind=$3 AND subject_id=$4) AND CASE WHEN $3='user' THEN EXISTS(SELECT 1 FROM proxy_users WHERE tenant_id=$1 AND id=$4) ELSE EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND id=$4 AND host_id=$2) END`, tenant, host, v.Kind, v.Subject).Scan(&allowed); e != nil {
			return e
		}
		if !allowed {
			retired = append(retired, v.ID)
			continue
		}
		var up, down int64
		var kind, subject string
		e = tx.QueryRow(r.Context(), `SELECT uplink,downlink,subject_kind,subject_id FROM traffic_cursors WHERE tenant_id=$1 AND host_id=$2 AND counter_id=$3 FOR UPDATE`, tenant, host, v.ID).Scan(&up, &down, &kind, &subject)
		if e != nil && e != pgx.ErrNoRows {
			return e
		}
		if e == nil && (kind != v.Kind || subject != v.Subject) {
			return bad("counter identity changed")
		}
		if v.Up < up || v.Down < down {
			continue
		}
		du, dd := v.Up-up, v.Down-down
		if du+dd == 0 {
			continue
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO traffic_cursors(tenant_id,host_id,counter_id,subject_kind,subject_id,uplink,downlink) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,host_id,counter_id) DO UPDATE SET uplink=excluded.uplink,downlink=excluded.downlink,updated_at=now()`, tenant, host, v.ID, v.Kind, v.Subject, v.Up, v.Down); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO traffic_daily(tenant_id,day,host_id,subject_kind,subject_id,uplink,downlink) VALUES($1,(now() AT TIME ZONE 'Asia/Shanghai')::date,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,day,host_id,subject_kind,subject_id) DO UPDATE SET uplink=traffic_daily.uplink+excluded.uplink,downlink=traffic_daily.downlink+excluded.downlink`, tenant, host, v.Kind, v.Subject, du, dd); e != nil {
			return e
		}
		if v.Kind == "user" {
			var before, limit int64
			if e = tx.QueryRow(r.Context(), `SELECT used_bytes,quota_bytes FROM proxy_users WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, v.Subject).Scan(&before, &limit); e != nil {
				return e
			}
			if _, e = tx.Exec(r.Context(), `UPDATE proxy_users SET used_bytes=used_bytes+$3,lifetime_bytes=lifetime_bytes+$3,upload_bytes=upload_bytes+$4,download_bytes=download_bytes+$5,last_active_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, v.Subject, du+dd, du, dd); e != nil {
				return e
			}
			if limit > 0 && before < limit && before+du+dd >= limit {
				if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id IN (SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=$1 AND gm.user_id=$2)`, tenant, v.Subject); e != nil {
					return e
				}
				if e = audit(r.Context(), tx, "control", "user.quota-exhausted", v.Subject); e != nil {
					return e
				}
			}
		}
	}
	for _, v := range b.Access {
		ip, err := netip.ParseAddr(v.Source)
		if !manifest.ValidID(v.ID) || !manifest.ValidID(v.Service) || (v.User != "" && !manifest.ValidID(v.User)) || err != nil || ip.Zone() != "" || ip.IsUnspecified() || !live.Address(v.Destination) || v.Port < 1 || v.Port > 65535 || (v.Network != "tcp" && v.Network != "udp") {
			return bad("invalid access observation")
		}
		// Only the host owning the inbound can report its traffic metadata.
		if _, e = tx.Exec(r.Context(), `INSERT INTO access_observations(tenant_id,host_id,event_id,user_id,service_id,source_ip,destination,destination_port,network) SELECT $1,$2,$3,$4,$5,$6::inet,$7,$8,$9 WHERE EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND id=$5 AND host_id=$2) AND ($4='' OR EXISTS(SELECT 1 FROM traffic_subjects WHERE tenant_id=$1 AND host_id=$2 AND subject_kind='user' AND subject_id=$4)) ON CONFLICT DO NOTHING`, tenant, host, v.ID, v.User, v.Service, ip.String(), strings.ToLower(v.Destination), v.Port, v.Network); e != nil {
			return e
		}
	}
	// Bound destination/IP retention and daily aggregates. Counter cursors are compact and never time-reset.
	if len(b.Access) > 0 {
		for _, q := range []string{`DELETE FROM access_observations WHERE tenant_id=$1 AND (host_id,event_id) IN (SELECT host_id,event_id FROM access_observations WHERE tenant_id=$1 ORDER BY observed_at DESC OFFSET 20000)`} {
			if _, e = tx.Exec(r.Context(), q, tenant); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"accepted": true, "retired_counter_ids": retired})
	return nil
}

func (s *Server) resetTraffic(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		RestartCycle bool `json:"restart_cycle"`
	}
	if e := decode(w, r, &input); e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	tag, e := tx.Exec(r.Context(), `UPDATE proxy_users SET used_bytes=0,upload_bytes=0,download_bytes=0,last_reset_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id IN (SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=$1 AND gm.user_id=$2)`, tenant, r.PathValue("id")); e != nil {
		return e
	}
	if input.RestartCycle {
		if e = configureCycle(r.Context(), tx, r.PathValue("id"), nil, true); e != nil {
			return e
		}
	}
	if e = audit(r.Context(), tx, "admin", "user.traffic-reset", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"reset": true})
	return nil
}

func (s *Server) accessRecords(w http.ResponseWriter, r *http.Request) error {
	user, host := r.URL.Query().Get("user_id"), r.URL.Query().Get("host_id")
	if user != "" && !manifest.ValidID(user) || host != "" && !manifest.ValidID(host) {
		return bad("invalid record filter")
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 200 {
			return bad("record limit must be 1-200")
		}
		limit = v
	}
	var data json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT a.event_id,a.host_id,a.user_id,a.service_id,host(a.source_ip) source_ip,a.destination,a.destination_port,a.network,a.observed_at,u.name user_name,l.name service_name,h.name host_name FROM access_observations a JOIN hosts h ON (h.tenant_id,h.id)=(a.tenant_id,a.host_id) LEFT JOIN proxy_users u ON (u.tenant_id,u.id)=(a.tenant_id,a.user_id) LEFT JOIN live_services l ON (l.tenant_id,l.id)=(a.tenant_id,a.service_id) WHERE a.tenant_id=$1 AND ($2='' OR a.user_id=$2) AND ($3='' OR a.host_id=$3) AND a.observed_at>now()-interval '7 days' ORDER BY a.observed_at DESC LIMIT $4) x`, tenant, user, host, limit).Scan(&data)
	if e != nil {
		return e
	}
	send(w, 200, data)
	return nil
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	user := r.URL.Query().Get("user_id")
	if user != "" && !manifest.ValidID(user) {
		return bad("invalid user filter")
	}
	var data json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT jsonb_build_object(
 'server_time',now(),
 'remaining_bytes',(SELECT COALESCE(sum(GREATEST(quota_bytes-used_bytes,0)),0) FROM proxy_users WHERE tenant_id=$1 AND quota_bytes>0),
 'finite_used_bytes',(SELECT COALESCE(sum(used_bytes),0) FROM proxy_users WHERE tenant_id=$1 AND quota_bytes>0),
 'unlimited_users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1 AND quota_bytes=0),
 'active_users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1 AND enabled AND expires_at>now() AND (quota_bytes=0 OR used_bytes<quota_bytes)),
 'enabled_inbounds',(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND enabled),
 'today_uplink',(SELECT COALESCE(sum(uplink),0) FROM traffic_daily WHERE tenant_id=$1 AND subject_kind='service' AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'today_downlink',(SELECT COALESCE(sum(downlink),0) FROM traffic_daily WHERE tenant_id=$1 AND subject_kind='service' AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'user_today_bytes',(SELECT COALESCE(sum(uplink+downlink),0) FROM traffic_daily WHERE tenant_id=$1 AND subject_kind='user' AND ($2='' OR subject_id=$2) AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'user_daily',(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT to_char(d.day,'YYYY-MM-DD') AS day,COALESCE(sum(t.uplink),0) uplink,COALESCE(sum(t.downlink),0) downlink FROM generate_series((now() AT TIME ZONE 'Asia/Shanghai')::date-13,(now() AT TIME ZONE 'Asia/Shanghai')::date,interval '1 day') d(day) LEFT JOIN traffic_daily t ON t.tenant_id=$1 AND t.subject_kind='user' AND ($2='' OR t.subject_id=$2) AND t.day=d.day::date GROUP BY d.day ORDER BY d.day) x),
 'hosts',(SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL),
 'online_hosts',(SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL AND last_seen>now()-interval '90 seconds'),
 'users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1),
 'online_users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1 AND enabled AND expires_at>now() AND (quota_bytes=0 OR used_bytes<quota_bytes) AND last_active_at>now()-interval '60 seconds'),
 'running_inbounds',(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND enabled AND version=reported_version AND state='running' AND reported_at>now()-interval '45 seconds'),
 'limited_users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1 AND quota_bytes>0 AND used_bytes>=quota_bytes),
 'allocated_bytes',(SELECT COALESCE(sum(quota_bytes),0) FROM proxy_users WHERE tenant_id=$1),
 'used_bytes',(SELECT COALESCE(sum(used_bytes),0) FROM proxy_users WHERE tenant_id=$1),
 'today_bytes',(SELECT COALESCE(sum(uplink+downlink),0) FROM traffic_daily WHERE tenant_id=$1 AND subject_kind='service' AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'inbound_usage',(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT l.id,l.name,l.kind,l.engine,h.name host_name,COALESCE(sum(c.uplink),0) uplink,COALESCE(sum(c.downlink),0) downlink FROM live_services l JOIN hosts h ON (h.tenant_id,h.id)=(l.tenant_id,l.host_id) LEFT JOIN traffic_cursors c ON c.tenant_id=l.tenant_id AND c.host_id=l.host_id AND c.subject_kind='service' AND c.subject_id=l.id WHERE l.tenant_id=$1 AND l.kind NOT IN ('frps','frpc') GROUP BY l.id,l.name,l.kind,l.engine,h.name ORDER BY COALESCE(sum(c.uplink+c.downlink),0) DESC LIMIT 64) x),
 'daily',(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT to_char(d.day,'YYYY-MM-DD') AS day,COALESCE(sum(t.uplink),0) uplink,COALESCE(sum(t.downlink),0) downlink FROM generate_series((now() AT TIME ZONE 'Asia/Shanghai')::date-13,(now() AT TIME ZONE 'Asia/Shanghai')::date,interval '1 day') d(day) LEFT JOIN traffic_daily t ON t.tenant_id=$1 AND t.subject_kind='service' AND t.day=d.day::date GROUP BY d.day ORDER BY d.day) x),
 'top_users',(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,quota_bytes,used_bytes,lifetime_bytes,reset_strategy,last_active_at FROM proxy_users WHERE tenant_id=$1 ORDER BY used_bytes DESC LIMIT 10) x),
 'telemetry',(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT t.*,h.name host_name,(t.state='collecting' AND t.reported_at>now()-interval '45 seconds') fresh FROM telemetry_status t JOIN hosts h ON (t.tenant_id,t.host_id)=(h.tenant_id,h.id) WHERE t.tenant_id=$1 AND h.deleted_at IS NULL) x)
 )`, tenant, user).Scan(&data)
	if e != nil {
		return e
	}
	send(w, 200, data)
	return nil
}
