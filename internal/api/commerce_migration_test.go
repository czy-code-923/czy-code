package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCommerceSplitIsAtomicPreservesRemainingAndRevokesLegacy(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	commerceReady(t, f)
	legacy := "portal:" + ID()
	base := map[string]any{"external_id": legacy, "order_id": ID(), "name": "legacy customer", "months": 2, "quota_bytes": int64(20000000000), "group_ids": []string{f.group}, "protocols": []string{"vmess"}, "max_devices": 3, "billing_mode": "both", "multiplier_milli": 1000}
	old := obj018(f.h.call("POST", "commerce/entitlements", base, 200, nil))
	f.ack()
	commerceReady(t, f)
	ctx := context.Background()
	_, e := f.h.s.DB.Exec(ctx, "UPDATE proxy_users SET used_bytes=10000000000 WHERE id=$1", old["user_id"])
	if e != nil {
		t.Fatal(e)
	}
	makeOrder := func() map[string]any {
		o := map[string]any{}
		for k, v := range base {
			o[k] = v
		}
		id := ID()
		o["order_id"] = id
		o["external_id"] = "portal:" + id
		o["name"] = "split-" + id
		o["quota_bytes"] = int64(20 * 1073741824)
		return o
	}
	a, b := makeOrder(), makeOrder()
	bad := makeOrder()
	bad["group_ids"] = []string{ID()}
	f.h.call("POST", "commerce/migrate-orders", map[string]any{"legacy_external_id": legacy, "orders": []any{a, bad}}, 409, nil)
	var enabled bool
	f.h.s.DB.QueryRow(ctx, "SELECT enabled FROM proxy_users WHERE id=$1", old["user_id"]).Scan(&enabled)
	if !enabled {
		t.Fatal("failed migration revoked legacy user")
	}
	request := map[string]any{"legacy_external_id": legacy, "orders": []any{a, b}}
	first := obj018(f.h.call("POST", "commerce/migrate-orders", request, 200, nil))
	replay := obj018(f.h.call("POST", "commerce/migrate-orders", request, 200, nil))
	if string(mustJSON(first)) != string(mustJSON(replay)) {
		t.Fatal("migration replay changed allocation")
	}
	rows := first["orders"].([]any)
	if len(rows) != 2 || first["remaining_bytes"] != float64(10*1073741824) {
		t.Fatal(first)
	}
	ids := map[string]bool{}
	var remaining int64
	for _, v := range rows {
		row := v.(map[string]any)
		id := row["user_id"].(string)
		if ids[id] {
			t.Fatal("merged identities")
		}
		ids[id] = true
		remaining += int64(row["remaining_bytes"].(float64))
		var quota, used int64
		var expiry time.Time
		f.h.s.DB.QueryRow(ctx, "SELECT quota_bytes,used_bytes,expires_at FROM proxy_users WHERE id=$1", id).Scan(&quota, &used, &expiry)
		if quota != 20*1073741824 || quota-used != 5*1073741824 || func() bool {
			original, _ := time.Parse(time.RFC3339Nano, old["expires_at"].(string))
			return expiry.Sub(original).Abs() > time.Microsecond
		}() {
			t.Fatal(quota, used, expiry, old["expires_at"])
		}
	}
	if remaining != 10*1073741824 {
		t.Fatal("remaining traffic was inflated", remaining)
	}
	f.h.s.DB.QueryRow(ctx, "SELECT enabled FROM proxy_users WHERE id=$1", old["user_id"]).Scan(&enabled)
	if enabled {
		t.Fatal("legacy UUID not disabled")
	}
	var revoked bool
	f.h.s.DB.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM subscriptions WHERE id=$1", old["subscription_id"]).Scan(&revoked)
	if !revoked {
		t.Fatal("legacy link still valid")
	}
	var response json.RawMessage
	f.h.s.DB.QueryRow(ctx, "SELECT response FROM commerce_order_migrations WHERE legacy_external_id=$1", legacy).Scan(&response)
	if len(response) == 0 {
		t.Fatal("no durable migration record")
	}
}
