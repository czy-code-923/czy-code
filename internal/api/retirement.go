package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/live"
)

func (s *Server) retirement(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	id := r.PathValue("id")
	var state, version string
	var capable bool
	var seen bool
	var rid *string
	if e = tx.QueryRow(r.Context(), `SELECT retire_state,agent_version,cleanup_capable,last_seen IS NOT NULL,retire_id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, id).Scan(&state, &version, &capable, &seen, &rid); e != nil {
		return e
	}
	var count, deps int
	if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM live_services WHERE tenant_id=$1 AND host_id=$2`, tenant, id).Scan(&count); e != nil {
		return e
	}
	if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM live_services l WHERE l.tenant_id=$1 AND l.host_id<>$2 AND (l.next_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2) OR l.parent_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2))`, tenant, id).Scan(&deps); e != nil {
		return e
	}
	var legacy int
	if e = tx.QueryRow(r.Context(), `SELECT count(DISTINCT a.route_id) FROM route_hops a JOIN instances i ON (a.tenant_id,a.instance_id)=(i.tenant_id,i.id) WHERE a.tenant_id=$1 AND i.host_id=$2 AND EXISTS(SELECT 1 FROM route_hops b JOIN instances j ON (b.tenant_id,b.instance_id)=(j.tenant_id,j.id) WHERE b.tenant_id=a.tenant_id AND b.route_id=a.route_id AND j.host_id<>$2)`, tenant, id).Scan(&legacy); e != nil {
		return e
	}
	deps += legacy
	if r.Method == "GET" {
		send(w, 200, map[string]any{"state": state, "service_count": count, "external_dependencies": deps, "remote_cleanup": seen, "cleanup_capable": capable && live.NetworkCapable(version), "notice": "停止并删除本主机的 Agent、托管内核和运行配置；保留控制面、数据库、审计及其他软件。离线时等待回执。跨主机依赖需要先解除。"})
		return nil
	}
	if deps > 0 {
		return problem{409, "其他主机仍依赖此主机的链式代理或 FRP，请先修改依赖配置"}
	}
	if seen && (!capable || !live.NetworkCapable(version)) {
		return problem{409, "远端 Agent 尚未安装受控卸载器，请升级到 0.12.0 后重试"}
	}
	if state == "" {
		newID := ID()
		rid = &newID
		if _, e = tx.Exec(r.Context(), `UPDATE hosts SET retire_id=$3,retire_state='requested',retire_requested_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, newID); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE live_services SET enabled=false,version=version+1,state='pending' WHERE tenant_id=$1 AND host_id=$2`, tenant, id); e != nil {
			return e
		}
		if e = audit(r.Context(), tx, "admin", "host.uninstall.request", id); e != nil {
			return e
		}
	}
	if !seen {
		if count > 0 {
			return problem{409, "主机从未连接但仍有配置，请删除配置后重试"}
		}
		if e = purgeHost(r, tx, id); e != nil {
			return e
		}
		state = "complete"
	} else {
		state = "requested"
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	status := 202
	if state == "complete" && r.Method == "DELETE" {
		status = 200
	}
	send(w, status, map[string]any{"state": state, "retire_id": rid, "deleted": state == "complete"})
	return nil
}
func (s *Server) retirementAgent(w http.ResponseWriter, r *http.Request, host string) error {
	var b struct {
		ID string `json:"retire_id"`
	}
	if e := decode(w, r, &b); e != nil {
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
	var id, state string
	if e = tx.QueryRow(r.Context(), `SELECT COALESCE(retire_id,''),retire_state FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, host).Scan(&id, &state); e != nil {
		return e
	}
	if r.PathValue("action") != "begin" && r.PathValue("action") != "ack" {
		return problem{404, "unknown action"}
	}
	if state == "complete" && b.ID == id && r.PathValue("action") == "ack" {
		send(w, 200, map[string]bool{"accepted": true})
		return nil
	}
	if b.ID == "" || b.ID != id || state == "" {
		return problem{409, "no matching uninstall request"}
	}
	if r.PathValue("action") == "ack" {
		var hash string
		if e = tx.QueryRow(r.Context(), `SELECT agent_hash FROM hosts WHERE tenant_id=$1 AND id=$2`, tenant, host).Scan(&hash); e != nil {
			return e
		}
		if e = purgeHost(r, tx, host); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO host_retirement_receipts(tenant_id,host_id,retire_id,result,agent_hash) VALUES($1,$2,$3,'remote-cleaned',$4) ON CONFLICT DO NOTHING`, tenant, host, id, hash); e != nil {
			return e
		}
	} else {
		if _, e = tx.Exec(r.Context(), `UPDATE hosts SET retire_state='cleaning' WHERE tenant_id=$1 AND id=$2`, tenant, host); e != nil {
			return e
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"accepted": true})
	return nil
}
func purgeHost(r *http.Request, tx pgx.Tx, host string) error {
	// Remove only the selected host's data; shared logical nodes remain if referenced elsewhere.
	var deps bool
	if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM live_services l WHERE l.tenant_id=$1 AND l.host_id<>$2 AND (l.next_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2) OR l.parent_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2)))`, tenant, host).Scan(&deps); e != nil {
		return e
	}
	if deps {
		return problem{409, "host still has external dependencies"}
	}
	var legacy int
	if e := tx.QueryRow(r.Context(), `SELECT count(DISTINCT a.route_id) FROM route_hops a JOIN instances i ON (a.tenant_id,a.instance_id)=(i.tenant_id,i.id) WHERE a.tenant_id=$1 AND i.host_id=$2 AND EXISTS(SELECT 1 FROM route_hops b JOIN instances j ON (b.tenant_id,b.instance_id)=(j.tenant_id,j.id) WHERE b.tenant_id=a.tenant_id AND b.route_id=a.route_id AND j.host_id<>$2)`, tenant, host).Scan(&legacy); e != nil {
		return e
	}
	if legacy > 0 {
		return problem{409, "historical route still depends on this host"}
	}
	var serviceIDs []string
	if e := tx.QueryRow(r.Context(), `SELECT ARRAY(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2)`, tenant, host).Scan(&serviceIDs); e != nil {
		return e
	}
	removed := map[string]bool{}
	for _, id := range serviceIDs {
		removed[id] = true
	}
	if e := prunePolicyNodes(r, tx, removed); e != nil {
		return e
	}
	var nodeIDs, routeIDs []string
	if e := tx.QueryRow(r.Context(), `SELECT ARRAY(SELECT node_id FROM live_services WHERE tenant_id=$1 AND host_id=$2 AND node_id IS NOT NULL UNION SELECT node_id FROM instances WHERE tenant_id=$1 AND host_id=$2)`, tenant, host).Scan(&nodeIDs); e != nil {
		return e
	}
	if e := tx.QueryRow(r.Context(), `SELECT ARRAY(SELECT DISTINCT route_id FROM route_hops a JOIN instances i ON (a.tenant_id,a.instance_id)=(i.tenant_id,i.id) WHERE i.tenant_id=$1 AND i.host_id=$2)`, tenant, host).Scan(&routeIDs); e != nil {
		return e
	}
	for _, q := range []string{`DELETE FROM route_hops WHERE tenant_id=$1 AND route_id=ANY($2)`, `DELETE FROM route_plans WHERE tenant_id=$1 AND id=ANY($2)`} {
		if _, e := tx.Exec(r.Context(), q, tenant, routeIDs); e != nil {
			return e
		}
	}
	for _, q := range []string{
		`UPDATE live_services SET next_id=NULL,parent_id=NULL WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM frp_assignments WHERE tenant_id=$1 AND client_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2)`,
		`DELETE FROM subscription_services WHERE tenant_id=$1 AND service_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2)`,
		`UPDATE user_groups SET version=version+1 WHERE tenant_id=$1 AND id IN(SELECT group_id FROM user_group_services WHERE tenant_id=$1 AND service_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2))`,
		`DELETE FROM user_group_services WHERE tenant_id=$1 AND service_id IN(SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2)`,
		`DELETE FROM live_ports WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM live_services WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM tasks WHERE tenant_id=$1 AND instance_id IN(SELECT id FROM instances WHERE tenant_id=$1 AND host_id=$2)`,
		`DELETE FROM route_hops WHERE tenant_id=$1 AND route_id IN(SELECT route_id FROM route_hops WHERE tenant_id=$1 AND instance_id IN(SELECT id FROM instances WHERE tenant_id=$1 AND host_id=$2))`,
		`UPDATE instances SET staged_config_id=NULL,applied_config_id=NULL WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM configs WHERE tenant_id=$1 AND instance_id IN(SELECT id FROM instances WHERE tenant_id=$1 AND host_id=$2)`,
		`DELETE FROM port_reservations WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM tunnels WHERE tenant_id=$1 AND instance_id IN(SELECT id FROM instances WHERE tenant_id=$1 AND host_id=$2)`,
		`DELETE FROM port_reservations WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM instances WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM firewall_policies WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM agent_enrollments WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM traffic_cursors WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM traffic_daily WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM traffic_subjects WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM access_observations WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM telemetry_status WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM engine_updates WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM kernel_revisions WHERE tenant_id=$1 AND host_id=$2`,
		`DELETE FROM host_kernels WHERE tenant_id=$1 AND host_id=$2`,
		`UPDATE hosts SET deleted_at=now(),retire_state='complete',retire_result='remote-cleaned',agent_hash='',reported_addresses='[]',address='removed',name='removed',provider='' WHERE tenant_id=$1 AND id=$2`,
	} {
		if _, e := tx.Exec(r.Context(), q, tenant, host); e != nil {
			return e
		}
	}
	if _, e := tx.Exec(r.Context(), `DELETE FROM nodes n WHERE n.tenant_id=$1 AND n.id=ANY($2) AND NOT EXISTS(SELECT 1 FROM live_services l WHERE (l.tenant_id,l.node_id)=(n.tenant_id,n.id)) AND NOT EXISTS(SELECT 1 FROM instances i WHERE (i.tenant_id,i.node_id)=(n.tenant_id,n.id))`, tenant, nodeIDs); e != nil {
		return e
	}
	return audit(r.Context(), tx, "control", "host.uninstalled", host)
}
