package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"vps-control/internal/manifest"

	"github.com/jackc/pgx/v5"
)

type commerceRenewalRequest struct {
	External     string `json:"external_id"`
	Order        string `json:"order_id"`
	User         string `json:"user_id"`
	Subscription string `json:"subscription_id"`
	Months       int    `json:"months"`
}

// Renewal is deliberately separate from a grant: a deleted identity or revoked
// link must never be recreated, and extending time must not rewrite permissions,
// credentials, quota, usage, billing settings or the monthly reset anchor.
func (s *Server) commerceRenewal(w http.ResponseWriter, r *http.Request) error {
	var b commerceRenewalRequest
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if !strings.HasPrefix(b.External, "portal:") || !manifest.ValidID(strings.TrimPrefix(b.External, "portal:")) || !manifest.ValidID(b.Order) || !manifest.ValidID(b.User) || !manifest.ValidID(b.Subscription) || b.Months < 1 || b.Months > 12 {
		return bad("invalid commercial renewal")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	requestHash := Hash(string(mustJSON(b)))
	var previous json.RawMessage
	var oldHash string
	e = tx.QueryRow(r.Context(), "SELECT request_hash,response FROM commerce_entitlements WHERE tenant_id=$1 AND order_id=$2", tenant, b.Order).Scan(&oldHash, &previous)
	if e == nil {
		if oldHash != requestHash {
			return problem{409, "renewal order ID was used with different fields"}
		}
		send(w, 200, previous)
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	var expires time.Time
	var day int
	var enabled bool
	e = tx.QueryRow(r.Context(), `SELECT expires_at,enabled FROM proxy_users WHERE tenant_id=$1 AND id=$2 AND external_id=$3 AND commercial_only FOR UPDATE`, tenant, b.User, b.External).Scan(&expires, &enabled)
	if e == pgx.ErrNoRows {
		return problem{409, "renewal identity no longer exists; do not recreate it"}
	}
	if e != nil {
		return e
	}
	if !enabled {
		return problem{409, "renewal identity is disabled; administrator review required"}
	}
	var valid bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND user_id=$3 AND independent_identity AND device_id IS NULL AND revoked_at IS NULL)`, tenant, b.Subscription, b.User).Scan(&valid); e != nil {
		return e
	}
	if !valid {
		return problem{409, "renewal subscription was revoked or replaced"}
	}
	now := time.Now()
	day = expires.In(billingLocation).Day()
	if expires.Before(now) {
		expires = now
		day = now.In(billingLocation).Day()
	}
	if day < 1 || day > 31 {
		day = expires.In(billingLocation).Day()
	}
	expires = calendarMonths(expires, b.Months, day)
	if expires.After(now.AddDate(10, 0, 0)) {
		return bad("maximum commercial entitlement horizon is ten years")
	}
	if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET expires_at=$3,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, b.User, expires); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "UPDATE subscriptions SET expires_at=$3,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, b.Subscription, expires); e != nil {
		return e
	}
	if e = bumpDeviceUser(r.Context(), tx, b.User); e != nil {
		return e
	}
	response := json.RawMessage(mustJSON(map[string]any{"user_id": b.User, "subscription_id": b.Subscription, "operation_id": b.Order, "expires_at": expires}))
	if _, e = tx.Exec(r.Context(), "INSERT INTO commerce_entitlements(tenant_id,order_id,external_id,request_hash,response) VALUES($1,$2,$3,$4,$5)", tenant, b.Order, b.External, requestHash, response); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "integration", "commerce.renew", b.Order); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, response)
	return nil
}
