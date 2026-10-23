package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/live"
)

// Replace endpoint membership without rotating the URL or extending its lifetime.
func (s *Server) subscriptionServices(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		ServiceIDs []string `json:"service_ids"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(b.ServiceIDs) < 1 || len(b.ServiceIDs) > 64 {
		return bad("select 1-64 services")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var id string
	if e = tx.QueryRow(r.Context(), "SELECT id FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now() FOR UPDATE", tenant, r.PathValue("id")).Scan(&id); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, service := range b.ServiceIDs {
		if seen[service] {
			return bad("duplicate service")
		}
		seen[service] = true
		var kind string
		e = tx.QueryRow(r.Context(), "SELECT kind FROM live_services WHERE tenant_id=$1 AND id=$2", tenant, service).Scan(&kind)
		if e == pgx.ErrNoRows {
			return bad("unknown service")
		}
		if e != nil {
			return e
		}
		if !live.Proxy(kind) {
			return bad("only proxy services may be shared")
		}
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM subscription_services WHERE tenant_id=$1 AND subscription_id=$2", tenant, id); e != nil {
		return e
	}
	for _, service := range b.ServiceIDs {
		if _, e = tx.Exec(r.Context(), "INSERT INTO subscription_services VALUES($1,$2,$3)", tenant, id, service); e != nil {
			return e
		}
	}
	if e = audit(r.Context(), tx, "admin", "subscription.services.replace", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"updated": true})
	return nil
}
