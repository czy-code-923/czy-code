package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"slices"
	"strings"
	"vps-control/internal/manifest"
)

type commercePolicyRequest struct {
	External     string   `json:"external_id"`
	User         string   `json:"user_id"`
	Subscription string   `json:"subscription_id"`
	Source       string   `json:"source_id"`
	Revision     int64    `json:"revision"`
	Groups       []string `json:"group_ids"`
	Protocols    []string `json:"protocols"`
}

// A policy update changes authorization only. It never grants time, resets
// accounting, enables a disabled user or revives a deleted identity/link.
func (s *Server) commercePolicy(w http.ResponseWriter, r *http.Request) error {
	var b commercePolicyRequest
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.External != "portal:"+r.PathValue("id") || !manifest.ValidID(r.PathValue("id")) || !manifest.ValidID(b.User) || !manifest.ValidID(b.Subscription) || !manifest.ValidID(b.Source) || b.Revision < 1 {
		return bad("invalid commercial policy identity")
	}
	if e := validCommerceSelection(commerceSelection{b.Groups, b.Protocols}); e != nil {
		return e
	}
	slices.Sort(b.Groups)
	slices.Sort(b.Protocols)
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var id string
	e = tx.QueryRow(r.Context(), `SELECT id FROM proxy_users WHERE tenant_id=$1 AND id=$2 AND external_id=$3 AND commercial_only FOR UPDATE`, tenant, b.User, b.External).Scan(&id)
	if e == pgx.ErrNoRows {
		return problem{404, "commercial identity no longer exists"}
	}
	if e != nil {
		return e
	}
	var valid bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND user_id=$3 AND independent_identity AND revoked_at IS NULL)`, tenant, b.Subscription, id).Scan(&valid); e != nil {
		return e
	}
	if !valid {
		return problem{409, "subscription revoked or replaced"}
	}
	fingerprint := Hash(string(mustJSON(b)))
	var source, oldHash string
	var revision int64
	e = tx.QueryRow(r.Context(), "SELECT source_id,revision,request_hash FROM commerce_account_policies WHERE tenant_id=$1 AND external_id=$2", tenant, b.External).Scan(&source, &revision, &oldHash)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == nil {
		if source != b.Source || b.Revision < revision || (b.Revision == revision && fingerprint != oldHash) {
			return problem{409, "stale or conflicting commercial policy"}
		}
		if b.Revision == revision {
			send(w, 200, map[string]any{"saved": true, "revision": revision})
			return nil
		}
	}
	var count int
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM user_groups WHERE tenant_id=$1 AND id=ANY($2)", tenant, b.Groups).Scan(&count); e != nil {
		return e
	}
	if count != len(b.Groups) {
		return problem{409, "policy group no longer exists"}
	}
	var shared bool
	var protocols []byte
	if e = tx.QueryRow(r.Context(), `SELECT COALESCE(bool_or(NOT l.managed_users),false),COALESCE(jsonb_agg(DISTINCT l.kind) FILTER(WHERE l.managed_users),'[]') FROM live_services l JOIN user_group_services gs ON(gs.tenant_id,gs.service_id)=(l.tenant_id,l.id) WHERE l.tenant_id=$1 AND gs.group_id=ANY($2) AND l.delete_requested_at IS NULL`, tenant, b.Groups).Scan(&shared, &protocols); e != nil {
		return e
	}
	var available []string
	json.Unmarshal(protocols, &available)
	if shared {
		return problem{409, "commercial policy cannot include shared-key nodes"}
	}
	for _, p := range b.Protocols {
		if !slices.Contains(available, p) {
			return problem{409, "selected protocol missing from commercial groups: " + p}
		}
	}
	// Invalidate both old and new inbounds so removed memberships lose credentials.
	if e = bumpDeviceUser(r.Context(), tx, id); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE user_groups SET version=version+1 WHERE tenant_id=$1 AND (id=ANY($2) OR id IN(SELECT group_id FROM user_group_members WHERE tenant_id=$1 AND user_id=$3))`, tenant, b.Groups, id); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM user_group_members WHERE tenant_id=$1 AND user_id=$2", tenant, id); e != nil {
		return e
	}
	for _, group := range b.Groups {
		if _, e = tx.Exec(r.Context(), "INSERT INTO user_group_members(tenant_id,group_id,user_id) VALUES($1,$2,$3)", tenant, group, id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET commercial_protocols=$3,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, id, mustJSON(b.Protocols)); e != nil {
		return e
	}
	if e = checkDevicePolicies(r.Context(), tx); e != nil {
		return e
	}
	if e = s.checkMeterHosts(r, tx, id, ""); e != nil {
		return e
	}
	if e = bumpDeviceUser(r.Context(), tx, id); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO commerce_account_policies(tenant_id,external_id,source_id,revision,request_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,external_id) DO UPDATE SET revision=excluded.revision,request_hash=excluded.request_hash,updated_at=now()`, tenant, b.External, b.Source, b.Revision, fingerprint); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "integration", "commerce.policy", strings.TrimPrefix(b.External, "portal:")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"saved": true, "revision": b.Revision})
	return nil
}
