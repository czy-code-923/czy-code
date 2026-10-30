package api

import (
	"context"
	"testing"
	"time"
	"vps-control/internal/live"
)

func findMember(t *testing.T, bundles []live.Work, id string) live.Work {
	t.Helper()
	for _, bundle := range bundles {
		for _, member := range bundle.Members {
			if member.ID == id {
				return member
			}
		}
	}
	t.Fatal("missing inbound")
	return live.Work{}
}

func hasAccount(w live.Work, id string) bool {
	for _, account := range w.Spec.Accounts {
		if account.Name == id {
			return true
		}
	}
	return false
}

// User expiry is re-evaluated on every Agent sync, independently of API writes.
func TestUserExpiryRemovesOnlyExpiredAccountFromKernel(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	other := obj018(f.h.call("POST", "proxy-users", map[string]any{"name": "other-user", "enabled": true, "expires_at": time.Now().Add(time.Hour)}, 200, nil))["id"].(string)
	f.h.call("POST", "user-groups", map[string]any{"name": "other-group", "user_ids": []string{other}, "service_ids": []string{f.node}}, 200, nil)
	before := f.ack()
	member := findMember(t, before, f.node)
	if !hasAccount(member, f.user) || !hasAccount(member, other) {
		t.Fatal("initial users missing")
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE proxy_users SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, f.user); err != nil {
		t.Fatal(err)
	}
	f.share(f.url, nil, 404)
	after := f.ack()
	member = findMember(t, after, f.node)
	if hasAccount(member, f.user) || !hasAccount(member, other) {
		t.Fatal("expired identity retained or unrelated user removed")
	}
	if before[0].Revision() == after[0].Revision() {
		t.Fatal("expiry did not change desired kernel revision")
	}
}

// Subscriptions currently reuse a user identity; expiry of one link cannot
// independently revoke that identity while other links use it.
func TestSubscriptionExpiryCurrentlyDoesNotRevokeUserCredential(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, f.subscription); err != nil {
		t.Fatal(err)
	}
	f.share(f.url, nil, 404)
	if !hasAccount(findMember(t, f.ack(), f.node), f.user) {
		t.Fatal("test expects current shared user identity to remain deployed")
	}
}
