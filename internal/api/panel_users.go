package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"vps-control/internal/manifest"
)

// Only explicit public account fields are selected; credential ciphertext never
// enters API response serialization, including paginated integration responses.
const proxyUserColumns = `id,name,external_id,version,billing_mode,multiplier_milli,billed_upload_bytes,billed_download_bytes,max_devices,device_limit_mode,` + deviceLimitSQL + ` effective_max_devices,CASE WHEN (u.device_limit_mode='compatible' OR ` + deviceLimitSQL + `=0) THEN (SELECT count(*) FROM subscription_clients c WHERE c.tenant_id=u.tenant_id AND c.user_id=u.id AND c.enabled AND c.last_subscription_at>now()-interval '7 days') ELSE (SELECT count(*) FROM user_devices d WHERE d.tenant_id=u.tenant_id AND d.user_id=u.id AND d.enabled) END device_count,enabled,expires_at,created_at,quota_bytes,used_bytes,lifetime_bytes,upload_bytes,download_bytes,reset_strategy,reset_anchor_at,reset_day,CASE reset_strategy WHEN 'rolling_monthly' THEN next_reset_at WHEN 'daily' THEN (date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai')+interval '1 day') AT TIME ZONE 'Asia/Shanghai' WHEN 'monthly' THEN (date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai')+interval '1 month') AT TIME ZONE 'Asia/Shanghai' END next_reset_at,last_reset_at,last_active_at,CASE WHEN quota_bytes=0 THEN NULL ELSE GREATEST(quota_bytes-used_bytes,0) END remaining_bytes,(quota_bytes>0 AND used_bytes>=quota_bytes) limited,COALESCE((SELECT jsonb_agg(group_id) FROM user_group_members m WHERE m.tenant_id=u.tenant_id AND m.user_id=u.id),'[]') group_ids,(enabled AND expires_at>now() AND (quota_bytes=0 OR used_bytes<quota_bytes)) active`

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) error {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		v, e := strconv.Atoi(value)
		if e != nil || v < 1 || v > 200 {
			return bad("limit must be 1-200")
		}
		limit = v
	}
	after := r.URL.Query().Get("after")
	if after != "" && !manifest.ValidID(after) {
		return bad("invalid user cursor")
	}
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT `+proxyUserColumns+` FROM proxy_users u WHERE tenant_id=$1 AND ($2='' OR id>$2) ORDER BY id LIMIT $3) x`, tenant, after, limit+1).Scan(&raw)
	if e != nil {
		return e
	}
	var items []map[string]any
	if e = json.Unmarshal(raw, &items); e != nil {
		return e
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1]["id"].(string)
	}
	send(w, 200, map[string]any{"items": items, "next_cursor": next, "limit": limit})
	return nil
}
