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
	var subscriptionIdentity string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT id FROM subscription_identities WHERE tenant_id=$1 AND subscription_id=$2 AND device_id IS NULL`, tenant, f.subscription).Scan(&subscriptionIdentity); err != nil {
		t.Fatal(err)
	}
	other := obj018(f.h.call("POST", "proxy-users", map[string]any{"name": "other-user", "enabled": true, "expires_at": time.Now().Add(time.Hour)}, 200, nil))["id"].(string)
	f.h.call("POST", "user-groups", map[string]any{"name": "other-group", "user_ids": []string{other}, "service_ids": []string{f.node}}, 200, nil)
	before := f.ack()
	member := findMember(t, before, f.node)
	if !hasAccount(member, subscriptionIdentity) || !hasAccount(member, other) {
		t.Fatal("initial users missing")
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE proxy_users SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, f.user); err != nil {
		t.Fatal(err)
	}
	f.share(f.url, nil, 404)
	after := f.ack()
	member = findMember(t, after, f.node)
	if hasAccount(member, subscriptionIdentity) || !hasAccount(member, other) {
		t.Fatal("expired identity retained or unrelated user removed")
	}
	if before[0].Revision() == after[0].Revision() {
		t.Fatal("expiry did not change desired kernel revision")
	}
}

func TestSubscriptionExpiryRevokesOnlyItsOwnCredential(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	other := obj018(f.h.call("POST", "subscriptions", map[string]any{"name": "second", "days": 1, "user_id": f.user}, 201, nil))
	secondID := other["id"].(string)
	var first, second string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT id FROM subscription_identities WHERE tenant_id=$1 AND subscription_id=$2 AND device_id IS NULL`, tenant, f.subscription).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT id FROM subscription_identities WHERE tenant_id=$1 AND subscription_id=$2 AND device_id IS NULL`, tenant, secondID).Scan(&second); err != nil {
		t.Fatal(err)
	}
	before := f.ack()
	if first == second || !hasAccount(findMember(t, before, f.node), first) || !hasAccount(findMember(t, before, f.node), second) {
		t.Fatal("subscription identities are not distinct and deployed")
	}
	counter := live.TrafficCounter{ID: Hash("sing-box/user/" + first)[:32], Kind: "user", Subject: first, Up: 40, Down: 60}
	f.h.call("POST", "agent/telemetry", live.TelemetryBatch{Counters: []live.TrafficCounter{counter}}, 200, f.host)
	f.h.call("POST", "agent/telemetry", live.TelemetryBatch{Counters: []live.TrafficCounter{counter}}, 200, f.host)
	u := obj018(f.h.call("GET", "proxy-users/"+f.user, nil, 200, nil))
	if u["used_bytes"] != float64(100) {
		t.Fatalf("independent subscription traffic was not billed exactly once: %v", u["used_bytes"])
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, f.subscription); err != nil {
		t.Fatal(err)
	}
	f.share(f.url, nil, 404)
	after := f.ack()
	if hasAccount(findMember(t, after, f.node), first) || !hasAccount(findMember(t, after, f.node), second) {
		t.Fatal("expired identity retained or unrelated identity removed")
	}
	if before[0].Revision() == after[0].Revision() {
		t.Fatal("expiry did not change kernel revision")
	}
}

func TestSeparateSubscriptionsForSameHWID(t *testing.T) {
	f := newSubscriptionFixture(t, 1)
	second := obj018(f.h.call("POST", "subscriptions", map[string]any{"name": "second", "days": 1, "user_id": f.user}, 201, nil))
	f.ack()
	headers := map[string]string{"X-Hwid": "same-fixture-device-123456", "User-Agent": "Happ/fixture"}
	f.share(f.url, headers, 503)
	f.ack()
	firstProxy := proxies019(t, f.share(f.url, headers, 200))[0]
	f.share(second["url"].(string)+"?format=mihomo", headers, 503)
	f.ack()
	secondProxy := proxies019(t, f.share(second["url"].(string)+"?format=mihomo", headers, 200))[0]
	if firstProxy["uuid"] == secondProxy["uuid"] {
		t.Fatal("different subscriptions reused a device credential")
	}
	var count int
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM user_devices WHERE tenant_id=$1 AND user_id=$2`, tenant, f.user).Scan(&count); err != nil || count != 1 {
		t.Fatalf("one device consumed multiple slots: %d %v", count, err)
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, f.subscription); err != nil {
		t.Fatal(err)
	}
	f.share(f.url, headers, 404)
	work := f.ack()
	member := findMember(t, work, f.node)
	for _, account := range member.Spec.Accounts {
		if account.UUID == firstProxy["uuid"] {
			t.Fatal("expired device subscription credential remained deployed")
		}
	}
	if got := proxies019(t, f.share(second["url"].(string)+"?format=mihomo", headers, 200))[0]; got["uuid"] != secondProxy["uuid"] {
		t.Fatal("unexpired device subscription changed")
	}
}

func TestLegacyMigrationPreservesOtherLegacyLinks(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	second := obj018(f.h.call("POST", "subscriptions", map[string]any{"name": "legacy-two", "days": 1, "user_id": f.user}, 201, nil))["id"].(string)
	_, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET independent_identity=false WHERE tenant_id=$1 AND id IN($2,$3)`, tenant, f.subscription, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.s.DB.Exec(context.Background(), `UPDATE proxy_users SET legacy_credentials=true WHERE tenant_id=$1 AND id=$2`, tenant, f.user); err != nil {
		t.Fatal(err)
	}
	before := f.ack()
	if !hasAccount(findMember(t, before, f.node), f.user) {
		t.Fatal("legacy account not available before migration")
	}
	first := obj018(f.h.call("POST", "subscriptions/"+f.subscription+"/independent-identity", map[string]any{}, 200, nil))
	if first["legacy_identity_still_active"] != true {
		t.Fatal("other legacy link not preserved")
	}
	work := f.ack()
	if !hasAccount(findMember(t, work, f.node), f.user) {
		t.Fatal("other legacy link lost access")
	}
	last := obj018(f.h.call("POST", "subscriptions/"+second+"/independent-identity", map[string]any{}, 200, nil))
	if last["legacy_identity_still_active"] != false {
		t.Fatal("legacy user identity should be revoked")
	}
	work = f.ack()
	if hasAccount(findMember(t, work, f.node), f.user) {
		t.Fatal("legacy shared identity survived migration")
	}
}

func TestUserCredentialRotationIncludesIndependentSubscriptions(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	before := proxies019(t, f.share(f.url, nil, 200))[0]["uuid"]
	f.h.call("PUT", "proxy-users/"+f.user, map[string]any{"name": "existing-user", "enabled": true, "expires_at": time.Now().Add(time.Hour), "rotate_credentials": true}, 200, nil)
	f.ack()
	after := proxies019(t, f.share(f.url, nil, 200))[0]["uuid"]
	if before == after {
		t.Fatal("user credential rotation left subscription UUID unchanged")
	}
}

func TestLegacyDeviceSubscriptionMigration(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	device := obj018(f.h.call("POST", "proxy-users/"+f.user+"/devices", map[string]any{"name": "old-device", "client_id": "mihomo"}, 201, nil))
	did := device["id"].(string)
	var subID string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT id FROM subscriptions WHERE tenant_id=$1 AND device_id=$2`, tenant, did).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	f.ack()
	before := proxies019(t, f.share(device["url"].(string), nil, 200))[0]["uuid"]
	f.h.call("POST", "subscriptions/"+subID+"/independent-identity", map[string]any{}, 200, nil)
	f.ack()
	after := proxies019(t, f.share(device["url"].(string), nil, 200))[0]["uuid"]
	if before == after {
		t.Fatal("migrated device subscription retained old UUID")
	}
	var expiry time.Time
	var version int
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT expires_at,version FROM subscriptions WHERE tenant_id=$1 AND id=$2`, tenant, subID).Scan(&expiry, &version); err != nil {
		t.Fatal(err)
	}
	f.h.call("PUT", "subscriptions/"+subID, map[string]any{"name": "renamed-device", "profile_id": "", "expires_at": expiry, "version": version}, 200, nil)
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, subID); err != nil {
		t.Fatal(err)
	}
	f.share(device["url"].(string), nil, 404)
	member := findMember(t, f.ack(), f.node)
	for _, account := range member.Spec.Accounts {
		if account.UUID == after || account.UUID == before {
			t.Fatal("expired or legacy device credential still deployed")
		}
	}
}

func TestLegacyCommonIdentityRetiresWhenLastOtherLinkExpires(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	second := obj018(f.h.call("POST", "subscriptions", map[string]any{"name": "old", "days": 1, "user_id": f.user}, 201, nil))["id"].(string)
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET independent_identity=false WHERE tenant_id=$1 AND id=$2`, tenant, second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE proxy_users SET legacy_credentials=true WHERE tenant_id=$1 AND id=$2`, tenant, f.user); err != nil {
		t.Fatal(err)
	}
	if !hasAccount(findMember(t, f.ack(), f.node), f.user) {
		t.Fatal("active legacy identity was removed")
	}
	if _, err := f.h.s.DB.Exec(context.Background(), `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, second); err != nil {
		t.Fatal(err)
	}
	if err := f.h.s.Reap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hasAccount(findMember(t, f.ack(), f.node), f.user) {
		t.Fatal("expired final legacy common identity survived")
	}
}
