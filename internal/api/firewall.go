package api

import (
	"encoding/json"
	"net/http"
	"vps-control/internal/firewall"
	"vps-control/internal/manifest"
)

func (s *Server) createFirewall(w http.ResponseWriter, r *http.Request) error {
	var p firewall.Policy
	if e := decode(w, r, &p); e != nil {
		return e
	}
	if e := p.Validate(); e != nil {
		return bad(e.Error())
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	host := r.PathValue("id")
	var locked string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, host).Scan(&locked); e != nil {
		return e
	}
	var version int
	if e = tx.QueryRow(ctx, "SELECT COALESCE(max(version),0)+1 FROM firewall_policies WHERE tenant_id=$1 AND host_id=$2", tenant, host).Scan(&version); e != nil {
		return e
	}
	raw, _ := json.Marshal(p)
	raw, _ = manifest.Canonical(raw)
	id := ID()
	sum := manifest.Digest(raw)
	if _, e = tx.Exec(ctx, "INSERT INTO firewall_policies(tenant_id,id,host_id,name,version,spec,checksum) VALUES($1,$2,$3,$4,$5,$6,$7)", tenant, id, host, p.Name, version, raw, sum); e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "firewall.version.create", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 201, map[string]any{"id": id, "version": version, "checksum": sum, "state": "draft"})
	return nil
}
func (s *Server) firewallPreview(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	var host, sum string
	if e := s.DB.QueryRow(r.Context(), "SELECT host_id,spec,checksum FROM firewall_policies WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id")).Scan(&host, &raw, &sum); e != nil {
		return e
	}
	canonical, e := manifest.Canonical(raw)
	if e != nil || manifest.Digest(canonical) != sum {
		return problem{409, "stored firewall checksum mismatch"}
	}
	var p firewall.Policy
	if e = json.Unmarshal(raw, &p); e != nil {
		return e
	}
	export := firewall.Export{HostID: host, Policy: p}
	script, e := firewall.Compile(export)
	if e != nil {
		return problem{409, "invalid stored policy"}
	}
	send(w, 200, map[string]any{"mode": "preview", "export": export, "nft": script, "checksum": sum, "warnings": []string{"Not applied. Does not control provider security groups, Docker FORWARD, NAT or outbound traffic.", "Existing firewall rules can still deny traffic accepted by this table.", "SSH guards and loopback web management protection are mandatory."}})
	return nil
}
func (s *Server) archiveFirewall(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Archived *bool `json:"archived"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Archived == nil {
		return bad("archived is required")
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE firewall_policies SET archived_at=CASE WHEN $3 THEN now() ELSE NULL END WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id"), *b.Archived)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return problem{404, "policy not found"}
	}
	action := "firewall.restored"
	if *b.Archived {
		action = "firewall.archived"
	}
	if e = audit(ctx, tx, "admin", action, r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"archived": *b.Archived})
	return nil
}
func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE tasks SET state='cancelled',result='cancelled before claim',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND state='queued'", tenant, r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return problem{409, "only queued tasks can be cancelled"}
	}
	if e = audit(ctx, tx, "admin", "task.cancel", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"state": "cancelled"})
	return nil
}
