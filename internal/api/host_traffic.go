package api

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"slices"
	"strings"
	"time"
	"vps-control/internal/hostmeter"
	"vps-control/internal/manifest"
)

type hostTrafficInput struct {
	InitialUsed *int64   `json:"initial_used_bytes,omitempty"`
	Version     *int     `json:"version"`
	Quota       int64    `json:"quota_bytes"`
	Mode        string   `json:"mode"`
	ResetDay    int      `json:"reset_day"`
	AutoReset   bool     `json:"auto_reset"`
	Interfaces  []string `json:"interfaces"`
}

func nextHostReset(now time.Time, day int) time.Time {
	n := now.In(billingLocation)
	anchor := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, billingLocation)
	return nextMonthlyResetDay(anchor, now, day)
}
func initHostTraffic(ctx context.Context, tx pgx.Tx, id string, in *hostTrafficInput) error {
	now := time.Now().In(billingLocation)
	if _, e := tx.Exec(ctx, `INSERT INTO host_traffic(tenant_id,host_id,reset_day,next_reset_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenant, id, now.Day(), nextHostReset(now, now.Day())); e != nil {
		return e
	}
	if in != nil {
		if in.InitialUsed != nil {
			if *in.InitialUsed < 0 || *in.InitialUsed > 9000000000000000 {
				return bad("invalid initial provider usage")
			}
			if _, e := tx.Exec(ctx, `UPDATE host_traffic SET adjustment_bytes=$3,calibrated_at=now() WHERE tenant_id=$1 AND host_id=$2`, tenant, id, *in.InitialUsed); e != nil {
				return e
			}
		}
		return saveHostTraffic(ctx, tx, id, *in, true)
	}
	return nil
}
func saveHostTraffic(ctx context.Context, tx pgx.Tx, id string, in hostTrafficInput, fresh bool) error {
	if !fresh && in.InitialUsed != nil {
		return bad("use the calibration endpoint for an existing host")
	}
	if in.Quota < 0 || in.Quota > 9000000000000000 || !slices.Contains([]string{"both", "rx", "tx", "max"}, in.Mode) || in.ResetDay < 1 || in.ResetDay > 31 || len(in.Interfaces) > 16 {
		return bad("invalid host quota, accounting mode, reset day or interfaces")
	}
	seen := map[string]bool{}
	for _, name := range in.Interfaces {
		if name == "" || len(name) > 128 || strings.ContainsAny(name, "/\\\x00\r\n") || seen[name] {
			return bad("invalid or duplicate network interface")
		}
		seen[name] = true
	}
	var version, day int
	var auto bool
	if e := tx.QueryRow(ctx, `SELECT version,reset_day,auto_reset FROM host_traffic WHERE tenant_id=$1 AND host_id=$2 FOR UPDATE`, tenant, id).Scan(&version, &day, &auto); e != nil {
		return e
	}
	if !fresh && (in.Version == nil || *in.Version != version) {
		return problem{409, "主机流量配置版本已变化，请重新读取"}
	}
	raw, _ := json.Marshal(in.Interfaces)
	if in.Interfaces == nil {
		raw = []byte(`[]`)
	}
	_, e := tx.Exec(ctx, `UPDATE host_traffic SET quota_bytes=$3,mode=$4,reset_day=$5,auto_reset=$6,interfaces=$7,version=version+1,updated_at=now(),next_reset_at=CASE WHEN NOT $6 THEN NULL WHEN next_reset_at IS NULL OR reset_day<>$5 OR NOT auto_reset THEN $8 ELSE next_reset_at END WHERE tenant_id=$1 AND host_id=$2`, tenant, id, in.Quota, in.Mode, in.ResetDay, in.AutoReset, raw, nextHostReset(time.Now(), in.ResetDay))
	return e
}

const hostTrafficJSON = `to_jsonb(t)||jsonb_build_object('remaining_bytes',CASE WHEN t.quota_bytes=0 THEN NULL ELSE GREATEST(t.quota_bytes-t.used_bytes,0) END,'fresh',COALESCE(t.last_sample_at>now()-interval '35 seconds',false),'limited',t.quota_bytes>0 AND t.used_bytes>=t.quota_bytes,'interfaces_available',(SELECT COALESCE(jsonb_agg(jsonb_build_object('name',c.name,'uplink',c.is_uplink,'rx',c.rx,'tx',c.tx,'sampled_at',c.sampled_at) ORDER BY c.name),'[]') FROM host_interface_cursors c WHERE c.tenant_id=t.tenant_id AND c.host_id=t.host_id))`

func (s *Server) hostTraffic(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !manifest.ValidID(id) {
		return bad("invalid host")
	}
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT `+hostTrafficJSON+` FROM host_traffic t WHERE t.tenant_id=$1 AND t.host_id=$2`, tenant, id).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var permitted bool
	if e = tx.QueryRow(r.Context(), `SELECT retire_state='' FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, id).Scan(&permitted); e != nil {
		return e
	}
	if !permitted {
		return problem{409, "host is retiring"}
	}
	action := "host.traffic-settings"
	if r.Method == "PUT" {
		var in hostTrafficInput
		if e = decode(w, r, &in); e != nil {
			return e
		}
		if e = saveHostTraffic(r.Context(), tx, id, in, false); e != nil {
			return e
		}
	} else {
		var in struct {
			Version int   `json:"version"`
			Used    int64 `json:"used_bytes"`
			Restart bool  `json:"restart_cycle"`
		}
		if e = decode(w, r, &in); e != nil {
			return e
		}
		if in.Used < 0 || in.Used > 9000000000000000 {
			return bad("invalid host usage calibration")
		}
		var v, day int
		if e = tx.QueryRow(r.Context(), `SELECT version,reset_day FROM host_traffic WHERE tenant_id=$1 AND host_id=$2 FOR UPDATE`, tenant, id).Scan(&v, &day); e != nil {
			return e
		}
		if v != in.Version {
			return problem{409, "主机流量版本已变化"}
		}
		_, e = tx.Exec(r.Context(), `UPDATE host_traffic SET period_rx=0,period_tx=0,adjustment_bytes=$3,acknowledged_gaps=gap_count,quality=CASE WHEN last_sample_at>now()-interval '35 seconds' THEN 'collecting' ELSE 'waiting' END,calibrated_at=now(),last_reset_at=now(),version=version+1,next_reset_at=CASE WHEN $4 AND auto_reset THEN $5 ELSE next_reset_at END WHERE tenant_id=$1 AND host_id=$2`, tenant, id, in.Used, in.Restart, nextHostReset(time.Now(), day))
		if e != nil {
			return e
		}
		action = "host.traffic-calibrate"
	}
	if e = audit(r.Context(), tx, "admin", action, id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"saved": true})
	return nil
}
func resetHostPeriods(ctx context.Context, tx pgx.Tx) error {
	rows, e := tx.Query(ctx, `SELECT host_id,reset_day FROM host_traffic WHERE tenant_id=$1 AND auto_reset AND next_reset_at<=now() FOR UPDATE`, tenant)
	if e != nil {
		return e
	}
	type reset struct {
		id  string
		day int
	}
	due := []reset{}
	for rows.Next() {
		var v reset
		if e = rows.Scan(&v.id, &v.day); e != nil {
			rows.Close()
			return e
		}
		due = append(due, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, v := range due {
		_, e = tx.Exec(ctx, `UPDATE host_traffic SET period_rx=0,period_tx=0,adjustment_bytes=0,last_reset_at=now(),next_reset_at=$3,version=version+1 WHERE tenant_id=$1 AND host_id=$2`, tenant, v.id, nextHostReset(time.Now(), v.day))
		if e != nil {
			return e
		}
		if e = audit(ctx, tx, "control", "host.traffic-period-reset", v.id); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) hostTrafficReport(w http.ResponseWriter, r *http.Request, host string) error {
	var b hostmeter.Report
	if e := decodeBounded(w, r, &b, 32<<10); e != nil {
		return e
	}
	now := time.Now()
	if b.State != "collecting" && b.State != "unavailable" || len(b.Interfaces) > 64 || !manifest.ValidID(b.Epoch) || b.At.After(now.Add(time.Minute)) || b.At.Before(now.Add(-24*time.Hour)) || b.Started.After(b.At) || b.Started.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) || b.Gaps < 0 {
		return bad("invalid host interface report")
	}
	seen := map[string]bool{}
	for _, v := range b.Interfaces {
		if v.Name == "" || len(v.Name) > 128 || strings.ContainsAny(v.Name, "/\\\x00\r\n") || v.RX < 0 || v.TX < 0 || v.RX > 9000000000000000 || v.TX > 9000000000000000 || seen[v.Name] {
			return bad("invalid interface counter")
		}
		seen[v.Name] = true
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if e = resetHostPeriods(r.Context(), tx); e != nil {
		return e
	}
	var epoch string
	var started, last *time.Time
	var previousGaps, totalGaps, ack int64
	var namesRaw []byte
	if e = tx.QueryRow(r.Context(), `SELECT epoch,epoch_started_at,last_sample_at,agent_gaps,gap_count,acknowledged_gaps,interfaces FROM host_traffic WHERE tenant_id=$1 AND host_id=$2 FOR UPDATE`, tenant, host).Scan(&epoch, &started, &last, &previousGaps, &totalGaps, &ack, &namesRaw); e != nil {
		return e
	}
	if last != nil && !b.At.After(*last) || epoch != "" && epoch != b.Epoch && started != nil && !b.Started.After(*started) {
		send(w, 200, map[string]bool{"accepted": true})
		return nil
	}
	var names []string
	if e = json.Unmarshal(namesRaw, &names); e != nil {
		return e
	}
	newEpoch := epoch != "" && epoch != b.Epoch
	if newEpoch {
		totalGaps++
		previousGaps = 0
		if _, e = tx.Exec(r.Context(), `DELETE FROM host_interface_cursors WHERE tenant_id=$1 AND host_id=$2`, tenant, host); e != nil {
			return e
		}
	}
	if b.Gaps > previousGaps {
		totalGaps += b.Gaps - previousGaps
	}
	if last != nil && b.At.Sub(*last) > 35*time.Second {
		totalGaps++
	}
	var rx, txBytes int64
	for _, v := range b.Interfaces {
		var oldRX, oldTX int64
		err := tx.QueryRow(r.Context(), `SELECT rx,tx FROM host_interface_cursors WHERE tenant_id=$1 AND host_id=$2 AND name=$3 AND epoch=$4`, tenant, host, v.Name, b.Epoch).Scan(&oldRX, &oldTX)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if v.RX < oldRX || v.TX < oldTX {
			return problem{409, "host counter moved backwards; preserve the durable Agent ledger"}
		}
		selected := v.Uplink
		if len(names) > 0 {
			selected = slices.Contains(names, v.Name)
		}
		if selected {
			rx += v.RX - oldRX
			txBytes += v.TX - oldTX
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO host_interface_cursors(tenant_id,host_id,name,rx,tx,is_uplink,epoch,sampled_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,host_id,name) DO UPDATE SET rx=excluded.rx,tx=excluded.tx,is_uplink=excluded.is_uplink,epoch=excluded.epoch,sampled_at=excluded.sampled_at`, tenant, host, v.Name, v.RX, v.TX, v.Uplink, b.Epoch, b.At)
		if e != nil {
			return e
		}
	}
	quality := "collecting"
	if totalGaps > ack {
		quality = "gap"
	}
	if b.State == "unavailable" {
		quality = "unavailable"
	}
	seconds := float64(10)
	if last != nil {
		seconds = max(1, b.At.Sub(*last).Seconds())
	}
	_, e = tx.Exec(r.Context(), `UPDATE host_traffic SET period_rx=period_rx+$3,period_tx=period_tx+$4,lifetime_rx=lifetime_rx+$3,lifetime_tx=lifetime_tx+$4,epoch=$5,epoch_started_at=$6,started_at=COALESCE(started_at,$6),last_sample_at=$7,quality=$8,gap_count=$9,agent_gaps=$10,rx_rate=$11,tx_rate=$12 WHERE tenant_id=$1 AND host_id=$2`, tenant, host, rx, txBytes, b.Epoch, b.Started, b.At, quality, totalGaps, b.Gaps, int64(float64(rx)/seconds), int64(float64(txBytes)/seconds))
	if e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"accepted": true})
	return nil
}
