package api

import (
	"encoding/json"
	"net/http"
)

// Trial cleanup is deliberately restricted to the named acceptance fixtures produced
// by earlier verification. Active services, user/group grants and normal subscriptions are protected.
func (s *Server) trials(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT l.id,l.name,l.host_id,l.kind,l.state,l.version,l.reported_version,EXISTS(SELECT 1 FROM user_group_services g WHERE g.tenant_id=l.tenant_id AND g.service_id=l.id) OR EXISTS(SELECT 1 FROM subscription_services ss JOIN subscriptions sub ON (sub.tenant_id,sub.id)=(ss.tenant_id,ss.subscription_id) WHERE ss.tenant_id=l.tenant_id AND ss.service_id=l.id AND (sub.user_id IS NOT NULL OR sub.name NOT LIKE 'Acceptance%')) protected FROM live_services l WHERE l.tenant_id=$1 AND l.name LIKE 'Acceptance%' AND NOT l.enabled ORDER BY l.created_at LIMIT 64) x`, tenant).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	var b struct {
		IDs []string `json:"ids"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(b.IDs) < 1 || len(b.IDs) > 64 {
		return bad("select 1-64 acceptance fixtures")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, id := range b.IDs {
		if seen[id] {
			return bad("duplicate fixture")
		}
		seen[id] = true
		var found string
		if e = tx.QueryRow(r.Context(), `SELECT id FROM live_services WHERE tenant_id=$1 AND id=$2 AND name LIKE 'Acceptance%' AND NOT enabled FOR UPDATE`, tenant, id).Scan(&found); e != nil {
			return problem{409, "仅允许清理停用的 Acceptance 验收配置"}
		}
	}
	var protected bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND (next_id=ANY($2) OR parent_id=ANY($2)) AND NOT(id=ANY($2))) OR EXISTS(SELECT 1 FROM user_group_services WHERE tenant_id=$1 AND service_id=ANY($2)) OR EXISTS(SELECT 1 FROM subscription_services ss JOIN subscriptions sub ON (sub.tenant_id,sub.id)=(ss.tenant_id,ss.subscription_id) WHERE ss.tenant_id=$1 AND ss.service_id=ANY($2) AND (sub.user_id IS NOT NULL OR sub.name NOT LIKE 'Acceptance%'))`, tenant, b.IDs).Scan(&protected); e != nil {
		return e
	}
	if protected {
		return problem{409, "验收配置仍被实际授权、订阅或其他配置引用，未执行清理"}
	}
	var subs []string
	rows, e := tx.Query(r.Context(), `SELECT id FROM subscriptions sub WHERE tenant_id=$1 AND user_id IS NULL AND name LIKE 'Acceptance%' AND EXISTS(SELECT 1 FROM subscription_services s WHERE s.tenant_id=$1 AND s.subscription_id=sub.id AND s.service_id=ANY($2)) AND NOT EXISTS(SELECT 1 FROM subscription_services s WHERE s.tenant_id=$1 AND s.subscription_id=sub.id AND NOT(s.service_id=ANY($2)))`, tenant, b.IDs)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		subs = append(subs, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if len(subs) > 0 {
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscription_services WHERE tenant_id=$1 AND subscription_id=ANY($2)`, tenant, subs); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscriptions WHERE tenant_id=$1 AND id=ANY($2)`, tenant, subs); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET next_id=NULL,parent_id=NULL WHERE tenant_id=$1 AND id=ANY($2)`, tenant, b.IDs); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM frp_assignments WHERE tenant_id=$1 AND client_id=ANY($2) AND parent_id=ANY($2)`, tenant, b.IDs); e != nil {
		return e
	}
	removed, pending := 0, 0
	for _, id := range b.IDs {
		var done bool
		if e = tx.QueryRow(r.Context(), `SELECT (state='stopped' AND version=reported_version) OR (version=1 AND reported_version=0) FROM live_services WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&done); e != nil {
			return e
		}
		if done {
			if e = purgeLive(r, tx, id); e != nil {
				return e
			}
			removed++
		} else {
			if _, e = tx.Exec(r.Context(), `UPDATE live_services SET enabled=false,version=version+1,state='pending',delete_requested_at=COALESCE(delete_requested_at,now()) WHERE tenant_id=$1 AND id=$2`, tenant, id); e != nil {
				return e
			}
			pending++
		}
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM nodes n WHERE tenant_id=$1 AND name LIKE 'Acceptance%' AND NOT EXISTS(SELECT 1 FROM live_services l WHERE (l.tenant_id,l.node_id)=(n.tenant_id,n.id)) AND NOT EXISTS(SELECT 1 FROM instances i WHERE (i.tenant_id,i.node_id)=(n.tenant_id,n.id))`, tenant); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "maintenance.acceptance-cleanup", tenant); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]int{"removed": removed, "pending_stop": pending, "trial_subscriptions_removed": len(subs)})
	return nil
}
