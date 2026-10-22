package api

import "net/http"

func (s *Server) rotate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	token := ID() + ID()
	tag, e := tx.Exec(ctx, "UPDATE hosts SET agent_hash=$3 WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id"), Hash(token))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return problem{404, "host not found"}
	}
	if e = audit(ctx, tx, "admin", "agent.token.rotate", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"agent_token": token})
	return nil
}
