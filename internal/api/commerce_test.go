package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vps-control/internal/live"
)

func commerceReady(t *testing.T, f subscriptionFixture) {
	t.Helper()
	_, e := f.h.s.DB.Exec(context.Background(), `INSERT INTO telemetry_status(tenant_id,host_id,engine,state) VALUES($1,$2,'sing-box','collecting') ON CONFLICT(tenant_id,host_id,engine) DO UPDATE SET state='collecting',reported_at=now()`, tenant, f.host["id"])
	if e != nil {
		t.Fatal(e)
	}
}
func TestCommercePermanentOrderIdentityAndExpiryEnforcement(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	commerceReady(t, f)
	b := map[string]any{"external_id": "portal:" + ID(), "order_id": ID(), "name": "paid-customer", "months": 1, "quota_bytes": 100000000000, "group_ids": []string{f.group}, "protocols": []string{"vmess"}, "max_devices": 3, "billing_mode": "both", "multiplier_milli": 1000}
	first := obj018(f.h.call("POST", "commerce/entitlements", b, 200, nil))
	replay := obj018(f.h.call("POST", "commerce/entitlements", b, 200, nil))
	if first["subscription_id"] != replay["subscription_id"] || first["expires_at"] != replay["expires_at"] {
		t.Fatal("duplicate order changed entitlement")
	}
	b["quota_bytes"] = 200000000000
	f.h.call("POST", "commerce/entitlements", b, 409, nil)
	b["quota_bytes"] = 100000000000
	f.ack()
	commerceReady(t, f)
	user := first["user_id"].(string)
	subscription := first["subscription_id"].(string)
	share := obj018(f.h.call("GET", "subscriptions/"+subscription+"/share?client=mihomo", nil, 200, nil))
	link := share["url"].(string)
	if len(proxies019(t, f.share(link, nil, 200))) != 1 {
		t.Fatal("commercial node missing")
	}
	b["order_id"] = ID()
	renew := obj018(f.h.call("POST", "commerce/entitlements", b, 200, nil))
	if renew["subscription_id"] != subscription {
		t.Fatal("renewal changed stable subscription")
	}
	oldExpiry, _ := time.Parse(time.RFC3339Nano, first["expires_at"].(string))
	newExpiry, _ := time.Parse(time.RFC3339Nano, renew["expires_at"].(string))
	if !newExpiry.After(oldExpiry) {
		t.Fatal("renewal did not extend")
	}
	f.ack()
	var sealed []byte
	if e := f.h.s.DB.QueryRow(context.Background(), "SELECT sealed FROM subscription_identities WHERE subscription_id=$1", subscription).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	plain, e := live.Open(f.h.s.MasterKey, sealed)
	if e != nil {
		t.Fatal(e)
	}
	var identity live.Account
	json.Unmarshal(plain, &identity)
	_, e = f.h.s.DB.Exec(context.Background(), "UPDATE proxy_users SET expires_at=now()-interval '1 second' WHERE id=$1", user)
	if e != nil {
		t.Fatal(e)
	}
	f.share(link, nil, 404)
	tx, e := f.h.s.DB.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	spec := live.Spec{ManagedUsers: true}
	e = f.h.s.materializeAccounts(httptest.NewRequest("GET", "/", nil), tx, f.node, &spec)
	if e != nil {
		t.Fatal(e)
	}
	for _, account := range spec.Accounts {
		if account.UUID == identity.UUID {
			t.Fatal("expired commercial UUID remains deployed")
		}
	}
}

func TestCommerceRejectsSharedGroupAndFiltersFutureSharedNodes(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	commerceReady(t, f)
	b := map[string]any{"external_id": "portal:" + ID(), "order_id": ID(), "name": "restricted-customer", "months": 1, "quota_bytes": 100000000000, "group_ids": []string{f.group}, "protocols": []string{"vmess"}, "max_devices": 0, "billing_mode": "both", "multiplier_milli": 1000}
	first := obj018(f.h.call("POST", "commerce/entitlements", b, 200, nil))
	f.ack()
	snell := obj018(f.h.call("POST", "live", map[string]any{"name": "shared-do-not-sell", "host_id": f.host["id"], "kind": "snell", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 28135}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+snell+"/toggle", map[string]any{"enabled": true}, 200, nil)
	var version int
	f.h.s.DB.QueryRow(context.Background(), "SELECT version FROM user_groups WHERE id=$1", f.group).Scan(&version)
	f.h.call("PUT", "user-groups/"+f.group, map[string]any{"name": "changed-group", "version": version, "user_ids": []string{f.user, first["user_id"].(string)}, "service_ids": []string{f.node, snell}}, 200, nil)
	f.ack()
	commerceReady(t, f)
	v := obj018(f.h.call("POST", "commerce/validate", commerceSelection{[]string{f.group}, []string{"vmess"}}, 200, nil))
	if v["ready"] != false {
		t.Fatal("shared-key group was approved for sale")
	}
	b["order_id"] = ID()
	f.h.call("POST", "commerce/entitlements", b, 409, nil)
	share := obj018(f.h.call("GET", "subscriptions/"+first["subscription_id"].(string)+"/share?client=mihomo", nil, 200, nil))
	w := f.share(share["url"].(string), nil, 200)
	if strings.Contains(w.Body.String(), "shared-do-not-sell") || len(proxies019(t, w)) != 1 {
		t.Fatal("shared-key node leaked into old stable commercial link")
	}
	normal := f.share(f.url, nil, 200)
	if len(proxies019(t, normal)) != 2 {
		t.Fatal("ordinary self-use shared-node behavior regressed")
	}
}

func TestCommerceCalendarMonthKeepsOriginalDay(t *testing.T) {
	a := time.Date(2027, 1, 31, 12, 0, 0, 0, billingLocation)
	b := calendarMonths(a, 1, 31)
	c := calendarMonths(b, 1, 31)
	if b.Month() != time.February || b.Day() != 28 || c.Month() != time.March || c.Day() != 31 {
		t.Fatal(a, b, c)
	}
}
