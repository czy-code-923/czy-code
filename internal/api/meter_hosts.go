package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/live"
)

func (s *Server) checkMeterHosts(r *http.Request, tx pgx.Tx, user, group string) error {
	rows, e := tx.Query(r.Context(), `SELECT DISTINCT h.agent_version FROM proxy_users u JOIN user_group_members m ON (u.tenant_id,u.id)=(m.tenant_id,m.user_id) JOIN user_group_services g ON (g.tenant_id,g.group_id)=(m.tenant_id,m.group_id) JOIN live_services l ON (l.tenant_id,l.id)=(g.tenant_id,g.service_id) JOIN hosts h ON (h.tenant_id,h.id)=(l.tenant_id,l.host_id) WHERE u.tenant_id=$1 AND u.quota_bytes>0 AND l.managed_users AND ($2='' OR u.id=$2) AND ($3='' OR m.group_id=$3)`, tenant, user, group)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			return e
		}
		if !live.MeterCapable(v) {
			return problem{409, "流量配额要求授权主机升级至 Agent 0.10+，以保证真实统计和超额撤销"}
		}
	}
	return rows.Err()
}
