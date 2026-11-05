package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"math/big"
	"net/http"
	"strings"
	"time"
	"vps-control/internal/manifest"
)

func binaryQuota(n int64) int64 {
	v := new(big.Int).Mul(big.NewInt(n), big.NewInt(1073741824))
	v.Quo(v, big.NewInt(1000000000))
	return v.Int64()
}
func weightedBytes(n, weight, total int64) int64 {
	v := new(big.Int).Mul(big.NewInt(n), big.NewInt(weight))
	v.Quo(v, big.NewInt(total))
	return v.Int64()
}

func (s *Server) commerceMigrateOrders(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Legacy string                 `json:"legacy_external_id"`
		Orders []commerceGrantRequest `json:"orders"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if !strings.HasPrefix(b.Legacy, "portal:") || !manifest.ValidID(strings.TrimPrefix(b.Legacy, "portal:")) || len(b.Orders) < 1 || len(b.Orders) > 100 {
		return bad("invalid migration request")
	}
	seen := map[string]bool{}
	var sum int64
	for _, o := range b.Orders {
		if e := validCommerceGrant(o); e != nil {
			return e
		}
		if o.External != "portal:"+o.Order || o.External == b.Legacy || seen[o.Order] {
			return bad("migration orders must have distinct order identities")
		}
		seen[o.Order] = true
		sum += o.Quota
		if sum > 9000000000000000 {
			return bad("migration quota exceeds safe total")
		}
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
	var previousHash string
	var previous json.RawMessage
	e = tx.QueryRow(r.Context(), "SELECT request_hash,response FROM commerce_order_migrations WHERE tenant_id=$1 AND legacy_external_id=$2", tenant, b.Legacy).Scan(&previousHash, &previous)
	if e == nil {
		if previousHash != requestHash {
			return problem{409, "migration was already applied with different order details"}
		}
		send(w, 200, previous)
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	var old string
	var quota, used int64
	var expiry time.Time
	var enabled bool
	var resetDay int
	var nextReset *time.Time
	if e = tx.QueryRow(r.Context(), `SELECT id,quota_bytes,used_bytes,expires_at,enabled,reset_day,next_reset_at FROM proxy_users WHERE tenant_id=$1 AND external_id=$2 AND commercial_only FOR UPDATE`, tenant, b.Legacy).Scan(&old, &quota, &used, &expiry, &enabled, &resetDay, &nextReset); e != nil {
		return e
	}
	if quota <= 0 || !expiry.After(time.Now()) {
		return problem{409, "legacy entitlement is expired or has no finite quota"}
	}
	for _, o := range b.Orders {
		reason, err := s.commerceNodes(r.Context(), tx, commerceSelection{o.Groups, o.Protocols})
		if err != nil {
			return err
		}
		if reason != "" {
			return problem{409, reason}
		}
	}
	remaining := min(sum, binaryQuota(max(int64(0), quota-used)))
	left := remaining
	results := []map[string]any{}
	for i, original := range b.Orders {
		allocation := weightedBytes(remaining, original.Quota, sum)
		if i == len(b.Orders)-1 {
			allocation = left
		}
		left -= allocation
		grant := original
		grant.Order = Hash("portal-split/" + b.Legacy + "/" + original.Order)[:32]
		grant.SeedExpiry = expiry
		grant.SeedUsed = grant.Quota - allocation
		grant.SeedResetDay = resetDay
		grant.SeedNextReset = nextReset
		result, err := s.commerceGrantTx(r, tx, grant)
		if err != nil {
			return err
		}
		var fields map[string]any
		if e = json.Unmarshal(result, &fields); e != nil {
			return e
		}
		fields["order_id"] = original.Order
		fields["quota_bytes"] = grant.Quota
		fields["used_bytes"] = grant.SeedUsed
		fields["remaining_bytes"] = allocation
		fields["external_id"] = original.External
		if !enabled {
			if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET enabled=false,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, fields["user_id"]); e != nil {
				return e
			}
			if e = bumpDeviceUser(r.Context(), tx, fields["user_id"].(string)); e != nil {
				return e
			}
		}
		results = append(results, fields)
	}
	if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET enabled=false,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, old); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "UPDATE subscriptions SET revoked_at=now(),version=version+1 WHERE tenant_id=$1 AND user_id=$2 AND revoked_at IS NULL", tenant, old); e != nil {
		return e
	}
	if e = bumpDeviceUser(r.Context(), tx, old); e != nil {
		return e
	}
	if e = checkDevicePolicies(r.Context(), tx); e != nil {
		return e
	}
	response := map[string]any{"legacy_user_id": old, "expires_at": expiry, "remaining_bytes": remaining, "orders": results, "credentials_revoked": true}
	if _, e = tx.Exec(r.Context(), "INSERT INTO commerce_order_migrations(tenant_id,legacy_external_id,request_hash,response) VALUES($1,$2,$3,$4)", tenant, b.Legacy, requestHash, mustJSON(response)); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "integration", "commerce.split", old); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, response)
	return nil
}
func (s *Server) commerceCapacity(w http.ResponseWriter, r *http.Request) error {
	var used int
	if e := s.DB.QueryRow(r.Context(), "SELECT count(*) FROM proxy_users WHERE tenant_id=$1", tenant).Scan(&used); e != nil {
		return e
	}
	send(w, 200, map[string]any{"identity_limit": 500, "identities": used, "remaining_identities": max(0, 500-used), "metering": "per_proxy_identity"})
	return nil
}
