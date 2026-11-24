package api

import (
	"encoding/json"
	"net/http"
)

// The authenticated Agent can inspect only its own recovery outcome. No
// credentials, node configuration or other hosts are returned.
func (s *Server) agentStatus(w http.ResponseWriter, r *http.Request, host string) error {
	var raw json.RawMessage
	err := s.DB.QueryRow(r.Context(), `SELECT jsonb_build_object(
 'server_time',now(),'online',COALESCE(h.last_seen>now()-interval '90 seconds',false),
 'last_seen',h.last_seen,'paused',h.connection_paused,
 'traffic_fresh',COALESCE(t.last_sample_at>now()-interval '35 seconds',false),
 'last_sample_at',t.last_sample_at,
 'enabled_services',(SELECT count(*) FROM live_services l WHERE l.tenant_id=h.tenant_id AND l.host_id=h.id AND l.enabled),
 'last_node_report_at',(SELECT min(reported_at) FROM live_services l WHERE l.tenant_id=h.tenant_id AND l.host_id=h.id AND l.enabled),
 'failed_services',(SELECT count(*) FROM live_services l WHERE l.tenant_id=h.tenant_id AND l.host_id=h.id AND l.enabled AND l.state='failed'),
 'pending_services',(SELECT count(*) FROM live_services l WHERE l.tenant_id=h.tenant_id AND l.host_id=h.id AND l.enabled AND (l.state<>'running' OR l.reported_version IS DISTINCT FROM l.version OR l.reported_at IS NULL OR l.reported_at<now()-interval '35 seconds')))
 FROM hosts h LEFT JOIN host_traffic t ON (t.tenant_id,t.host_id)=(h.tenant_id,h.id)
 WHERE h.tenant_id=$1 AND h.id=$2 AND h.deleted_at IS NULL`, tenant, host).Scan(&raw)
	if err != nil {
		return err
	}
	send(w, 200, raw)
	return nil
}
