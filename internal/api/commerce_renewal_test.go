package api

import (
	"context"
	"testing"
	"time"
)

func TestCommerceRenewalOnlyExtendsExistingIdentity(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	commerceReady(t, f)
	grant := map[string]any{"external_id": "portal:" + ID(), "order_id": ID(), "name": "renew-original", "months": 1, "quota_bytes": int64(100000000000), "group_ids": []string{f.group}, "protocols": []string{"vmess"}, "max_devices": 3, "billing_mode": "both", "multiplier_milli": 1200}
	first := obj018(f.h.call("POST", "commerce/entitlements", grant, 200, nil))
	user := first["user_id"].(string)
	sub := first["subscription_id"].(string)
	ctx := context.Background()
	if _, e := f.h.s.DB.Exec(ctx, "UPDATE proxy_users SET used_bytes=1234,upload_bytes=321,download_bytes=913,reset_day=1 WHERE tenant_id=$1 AND id=$2", tenant, user); e != nil {
		t.Fatal(e)
	}
	var before, after string
	query := `SELECT (to_jsonb(u)-'expires_at'-'version')::text FROM proxy_users u WHERE tenant_id=$1 AND id=$2`
	if e := f.h.s.DB.QueryRow(ctx, query, tenant, user).Scan(&before); e != nil {
		t.Fatal(e)
	}
	var tokenBefore, tokenAfter string
	f.h.s.DB.QueryRow(ctx, "SELECT token_hash FROM subscriptions WHERE tenant_id=$1 AND id=$2", tenant, sub).Scan(&tokenBefore)
	b := commerceRenewalRequest{External: grant["external_id"].(string), Order: ID(), User: user, Subscription: sub, Months: 2}
	renewed := obj018(f.h.call("POST", "commerce/renewals", b, 200, nil))
	replay := obj018(f.h.call("POST", "commerce/renewals", b, 200, nil))
	if renewed["user_id"] != user || renewed["subscription_id"] != sub || replay["expires_at"] != renewed["expires_at"] {
		t.Fatal("renewal replaced identity or replay extended twice", renewed, replay)
	}
	oldExpiry, _ := time.Parse(time.RFC3339Nano, first["expires_at"].(string))
	newExpiry, _ := time.Parse(time.RFC3339Nano, renewed["expires_at"].(string))
	if !newExpiry.After(oldExpiry.AddDate(0, 1, 0)) {
		t.Fatal("renewal failed to extend two months")
	}
	if newExpiry.In(billingLocation).Day() != oldExpiry.In(billingLocation).Day() {
		t.Fatal("traffic reset day changed renewal expiry")
	}
	f.h.s.DB.QueryRow(ctx, query, tenant, user).Scan(&after)
	f.h.s.DB.QueryRow(ctx, "SELECT token_hash FROM subscriptions WHERE tenant_id=$1 AND id=$2", tenant, sub).Scan(&tokenAfter)
	if before != after || tokenBefore != tokenAfter {
		t.Fatal("time-only renewal changed accounting or credentials")
	}
	b.Months = 3
	f.h.call("POST", "commerce/renewals", b, 409, nil)
	b.Order = ID()
	b.User = ID()
	f.h.call("POST", "commerce/renewals", b, 409, nil)
	b.User = user
	if _, e := f.h.s.DB.Exec(ctx, "UPDATE subscriptions SET revoked_at=now() WHERE tenant_id=$1 AND id=$2", tenant, sub); e != nil {
		t.Fatal(e)
	}
	f.h.call("POST", "commerce/renewals", b, 409, nil)
	if _, e := f.h.s.DB.Exec(ctx, "DELETE FROM subscriptions WHERE tenant_id=$1 AND id=$2", tenant, sub); e != nil {
		t.Fatal(e)
	}
	// Removing all credentials for this identity leaves no grant path: renewal
	// cannot recreate a deleted account even if the caller retains old IDs.
	if _, e := f.h.s.DB.Exec(ctx, "DELETE FROM proxy_users WHERE tenant_id=$1 AND id=$2", tenant, user); e != nil {
		t.Fatal(e)
	}
	f.h.call("POST", "commerce/renewals", b, 409, nil)
	var count int
	f.h.s.DB.QueryRow(ctx, "SELECT count(*) FROM proxy_users WHERE tenant_id=$1 AND external_id=$2", tenant, b.External).Scan(&count)
	if count != 0 {
		t.Fatal("renewal recreated deleted user")
	}
}
