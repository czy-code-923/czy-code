package api

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"regexp"
)

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request, host string) error {
	var b struct {
		Mode string `json:"mode"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Mode == "" {
		b.Mode = "simulation"
	}
	if b.Mode != "simulation" && b.Mode != "staging" {
		return bad("invalid agent mode")
	}
	_, e := s.DB.Exec(r.Context(), "UPDATE hosts SET last_seen=now(),agent_mode=$3 WHERE tenant_id=$1 AND id=$2", tenant, host, b.Mode)
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"mode": b.Mode})
	return nil
}
func (s *Server) claim(w http.ResponseWriter, r *http.Request, host string) error {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = reap(ctx, tx); e != nil {
		return e
	}
	var id, config, instance, mode, operation string
	e = tx.QueryRow(ctx, `SELECT t.id,t.config_id,t.instance_id,t.mode,t.operation FROM tasks t
 JOIN instances i ON (i.tenant_id,i.id)=(t.tenant_id,t.instance_id)
 JOIN hosts h ON (h.tenant_id,h.id)=(i.tenant_id,i.host_id)
 WHERE t.tenant_id=$1 AND i.host_id=$2 AND t.state='queued' AND t.attempts<3
 AND (t.mode='simulation' OR h.agent_mode='staging')
 ORDER BY t.created_at,t.id FOR UPDATE OF t SKIP LOCKED LIMIT 1`, tenant, host).Scan(&id, &config, &instance, &mode, &operation)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		w.WriteHeader(204)
		return nil
	}
	if e != nil {
		return e
	}
	lease := ID()
	_, e = tx.Exec(ctx, "UPDATE tasks SET state='running',attempts=attempts+1,lease_token=$3,lease_until=now()+interval '30 seconds',updated_at=now() WHERE tenant_id=$1 AND id=$2", tenant, id, lease)
	if e != nil {
		return e
	}
	var spec json.RawMessage
	var checksum string
	if e = tx.QueryRow(ctx, "SELECT spec,checksum FROM configs WHERE tenant_id=$1 AND id=$2", tenant, config).Scan(&spec, &checksum); e != nil {
		return e
	}
	if e = audit(ctx, tx, "agent:"+host, "deploy.claim", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": id, "instance_id": instance, "config_id": config, "lease_token": lease, "spec": spec, "checksum": checksum, "mode": mode, "operation": operation})
	return nil
}
func (s *Server) renew(w http.ResponseWriter, r *http.Request, host string) error {
	var b struct {
		Lease string `json:"lease_token"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	tag, e := s.DB.Exec(r.Context(), `UPDATE tasks t SET lease_until=now()+interval '30 seconds',updated_at=now()
 FROM instances i WHERE (i.tenant_id,i.id)=(t.tenant_id,t.instance_id) AND t.tenant_id=$1 AND t.id=$2 AND i.host_id=$3
 AND t.state='running' AND t.lease_token=$4 AND t.lease_until>now()`, tenant, r.PathValue("id"), host, b.Lease)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return problem{409, "lease stale or task not owned"}
	}
	send(w, 200, map[string]string{"state": "running"})
	return nil
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Server) complete(w http.ResponseWriter, r *http.Request, host string) error {
	var b struct {
		Lease            string `json:"lease_token"`
		Success          *bool  `json:"success"`
		ArtifactChecksum string `json:"artifact_checksum"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Success == nil {
		return bad("success is required")
	}
	if b.ArtifactChecksum != "" && !digestPattern.MatchString(b.ArtifactChecksum) {
		return bad("invalid artifact checksum")
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Keep lock order consistent with enqueue: instance, then task.
	var locked string
	e = tx.QueryRow(ctx, `SELECT i.id FROM instances i JOIN tasks t ON (t.tenant_id,t.instance_id)=(i.tenant_id,i.id)
 WHERE t.tenant_id=$1 AND t.id=$2 AND i.host_id=$3 FOR UPDATE OF i`, tenant, r.PathValue("id"), host).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		return problem{409, "task not owned"}
	}
	if e != nil {
		return e
	}
	var instance, config, mode, state, checksum string
	var lease, completed *string
	var live bool
	e = tx.QueryRow(ctx, `SELECT t.instance_id,t.config_id,t.mode,t.state,t.artifact_checksum,t.lease_token,t.completed_lease,COALESCE(t.lease_until>now(),false)
 FROM tasks t JOIN instances i ON (i.tenant_id,i.id)=(t.tenant_id,t.instance_id)
 WHERE t.tenant_id=$1 AND t.id=$2 AND i.host_id=$3 FOR UPDATE OF t`, tenant, r.PathValue("id"), host).Scan(&instance, &config, &mode, &state, &checksum, &lease, &completed, &live)
	if errors.Is(e, pgx.ErrNoRows) {
		return problem{409, "task not owned"}
	}
	if e != nil {
		return e
	}
	final := "failed"
	if *b.Success {
		final = "succeeded"
	}
	if state == final && completed != nil && *completed == b.Lease && checksum == b.ArtifactChecksum {
		send(w, 200, map[string]string{"state": state})
		return nil
	}
	if state != "running" || lease == nil || *lease != b.Lease || !live {
		return problem{409, "lease stale or task already completed"}
	}
	if mode == "staging" && *b.Success && !digestPattern.MatchString(b.ArtifactChecksum) {
		return bad("staging success requires artifact checksum")
	}
	if mode == "simulation" && b.ArtifactChecksum != "" {
		return bad("simulation has no staged artifact")
	}
	result := "manifest or native validation/staging failed"
	if *b.Success {
		result = "simulation completed; no host changes"
		if mode == "staging" {
			result = "native configuration validated and staged; service not started"
		}
	}
	_, e = tx.Exec(ctx, "UPDATE tasks SET state=$3,result=$4,artifact_checksum=$5,completed_lease=$6,lease_token=NULL,lease_until=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id"), final, result, b.ArtifactChecksum, b.Lease)
	if e != nil {
		return e
	}
	if *b.Success {
		column, status := "applied_config_id", "simulated"
		if mode == "staging" {
			column, status = "staged_config_id", "staged"
		}
		_, e = tx.Exec(ctx, "UPDATE instances SET state=$4,"+column+"=$3 WHERE tenant_id=$1 AND id=$2", tenant, instance, config, status)
		if e != nil {
			return e
		}
	}
	if e = audit(ctx, tx, "agent:"+host, "deploy."+final, r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"state": final})
	return nil
}
