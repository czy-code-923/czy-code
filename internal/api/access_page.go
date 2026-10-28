package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"vps-control/internal/manifest"
)

func (s *Server) accessPage(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	user, host, device := q.Get("user_id"), q.Get("host_id"), q.Get("device_id")
	for _, id := range []string{user, host, device} {
		if id != "" && !manifest.ValidID(id) {
			return bad("invalid metadata filter")
		}
	}
	page, size := 1, 50
	for name, target := range map[string]*int{"page": &page, "limit": &size} {
		if q.Get(name) != "" {
			n, e := strconv.Atoi(q.Get(name))
			if e != nil {
				return bad("invalid pagination")
			}
			*target = n
		}
	}
	if page < 1 || page > 20000 || size < 1 || size > 200 {
		return bad("page/limit outside supported range")
	}
	offset := (page - 1) * size
	if offset >= 20000 {
		return bad("metadata retention is bounded to 20000 rows")
	}
	sort := "a.observed_at"
	if q.Get("sort") == "traffic" {
		sort = "a.uplink+a.downlink"
	} else if q.Get("sort") != "" && q.Get("sort") != "time" {
		return bad("sort must be time or traffic")
	}
	order := "DESC"
	if q.Get("order") == "asc" {
		order = "ASC"
	} else if q.Get("order") != "" && q.Get("order") != "desc" {
		return bad("order must be asc or desc")
	}
	search := q.Get("search")
	if len(search) > 255 {
		return bad("metadata search too long")
	}
	where := `a.tenant_id=$1 AND ($2='' OR a.user_id=$2) AND ($3='' OR a.host_id=$3) AND ($4='' OR a.device_id=$4) AND a.observed_at>now()-interval '7 days' AND ($5='' OR a.destination ILIKE '%'||$5||'%' OR host(a.source_ip) ILIKE '%'||$5||'%')`
	var total int
	if e := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM access_observations a WHERE `+where, tenant, user, host, device, search).Scan(&total); e != nil {
		return e
	}
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT a.event_id,a.host_id,a.user_id,a.device_id,a.service_id,host(a.source_ip) source_ip,a.destination,a.destination_port,a.network,a.observed_at,a.last_seen_at,a.uplink,a.downlink,(a.uplink+a.downlink) total_bytes,a.closed,u.name user_name,l.name service_name,h.name host_name,d.name device_name FROM access_observations a JOIN hosts h ON(h.tenant_id,h.id)=(a.tenant_id,a.host_id) LEFT JOIN proxy_users u ON(u.tenant_id,u.id)=(a.tenant_id,a.user_id) LEFT JOIN live_services l ON(l.tenant_id,l.id)=(a.tenant_id,a.service_id) LEFT JOIN user_devices d ON(d.tenant_id,d.id)=(a.tenant_id,a.device_id) WHERE `+where+` ORDER BY `+sort+` `+order+` NULLS LAST,a.observed_at `+order+`,a.host_id,a.event_id LIMIT $6 OFFSET $7)x`, tenant, user, host, device, search, size, offset).Scan(&raw)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"items": raw, "total": total, "page": page, "limit": size, "pages": max(1, (total+size-1)/size), "capture_limit_per_batch": 100, "retention_days": 7, "retention_rows": 20000, "metadata_only": true})
	return nil
}
