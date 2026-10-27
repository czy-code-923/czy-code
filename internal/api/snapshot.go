package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// One database round trip. Config/native bodies are fetched only when explicitly previewed.
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) error {
	parts := []string{}
	for _, c := range []struct {
		kind, query string
		limit       int
	}{
		{"hosts", "SELECT id,name,provider,address,last_seen,agent_mode,agent_version,platform,reported_addresses,created_at FROM hosts", 200},
		{"nodes", "SELECT id,name,engine,created_at FROM nodes", 200},
		{"instances", "SELECT id,name,host_id,node_id,listen_port,state,applied_config_id,staged_config_id,created_at FROM instances", 200},
		{"tunnels", "SELECT id,name,instance_id,local_host,local_port,remote_port,created_at FROM tunnels", 200},
		{"routes", "SELECT id,name,hops,state,created_at FROM route_plans", 200},
		{"configs", "SELECT id,instance_id,version,checksum,created_at FROM configs", 200},
		{"tasks", "SELECT id,instance_id,config_id,mode,operation,state,attempts,result,artifact_checksum,created_at,updated_at FROM tasks", 200},
		{"audit", "SELECT sequence,actor,action,resource_id,created_at FROM audit", 100},
		{"firewalls", "SELECT id,host_id,name,version,checksum,archived_at,created_at FROM firewall_policies", 200},
	} {
		parts = append(parts, "'"+c.kind+"', (SELECT COALESCE(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM ("+c.query+" WHERE tenant_id=$1"+func() string {
			if c.kind == "hosts" {
				return " AND deleted_at IS NULL"
			}
			return ""
		}()+" ORDER BY created_at DESC,"+func() string {
			if c.kind == "audit" {
				return "sequence"
			}
			return "id"
		}()+" DESC LIMIT "+func() string {
			if c.limit == 100 {
				return "100"
			}
			return "200"
		}()+") x)")
	}
	query := `SELECT jsonb_build_object('collections',jsonb_build_object(` + strings.Join(parts, ",") + `),'summary',jsonb_build_object(
 'hosts',(SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL),
 'online_hosts',(SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL AND last_seen>now()-interval '90 seconds'),
 'instances',(SELECT count(*) FROM instances WHERE tenant_id=$1),
 'live_services',(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND kind NOT IN ('frps','frpc')),
 'running_services',(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND kind NOT IN ('frps','frpc') AND enabled AND state='running' AND reported_version=version AND reported_at>now()-interval '45 seconds'),
 'proxy_users',(SELECT count(*) FROM proxy_users WHERE tenant_id=$1),
 'active_tasks',(SELECT count(*) FROM tasks WHERE tenant_id=$1 AND state IN ('queued','running')),
 'completed_tasks',(SELECT count(*) FROM tasks WHERE tenant_id=$1 AND state='succeeded'),
 'firewall_versions',(SELECT count(*) FROM firewall_policies WHERE tenant_id=$1 AND archived_at IS NULL)))`
	var raw json.RawMessage
	if e := s.DB.QueryRow(r.Context(), query, tenant).Scan(&raw); e != nil {
		return e
	}
	etag := `"` + Hash(string(raw)) + `"`
	w.Header().Set("ETag", etag)
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(304)
		return nil
	}
	send(w, 200, raw)
	return nil
}

func matchesETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		v := strings.TrimSpace(part)
		if v == "*" || strings.TrimPrefix(v, "W/") == etag {
			return true
		}
	}
	return false
}
