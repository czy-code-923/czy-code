package api

import "net/http"

// Pause only the platform's managed proxy synchronization. Heartbeats and
// accounting stay available so an outbound-only Agent can later resume.
func (s *Server) hostConnection(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Paused *bool `json:"paused"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if b.Paused == nil {
		return bad("paused is required")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(r.Context(), `UPDATE hosts SET connection_paused=$3 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state=''`, tenant, r.PathValue("id"), *b.Paused)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return problem{404, "host unavailable"}
	}
	if err = audit(r.Context(), tx, "admin", "host.connection", r.PathValue("id")); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"paused": *b.Paused, "notice": "Agent 保留心跳和流量回传；暂停后托管代理会停止，恢复后由在线 Agent 自动同步。离线 Agent 需先恢复网络或服务。"})
	return nil
}
