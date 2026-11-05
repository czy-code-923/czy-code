package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCommercePolicyMovesGroupsPreservesBillingAndStableSubscription(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	commerceReady(t, f)
	ctx := context.Background()
	external := "portal:" + ID()
	grant := obj018(f.h.call("POST", "commerce/entitlements", map[string]any{"external_id": external, "order_id": ID(), "name": "policy-fixture", "months": 1, "quota_bytes": 100000000000, "group_ids": []string{f.group}, "protocols": []string{"vmess"}, "max_devices": 3, "billing_mode": "both", "multiplier_milli": 1000}, 200, nil))
	f.ack()
	user, sub := grant["user_id"].(string), grant["subscription_id"].(string)
	f.h.s.DB.Exec(ctx, "UPDATE proxy_users SET used_bytes=1234 WHERE id=$1", user)
	var before []byte
	query := `SELECT jsonb_build_array(sealed,expires_at,quota_bytes,used_bytes,reset_day,next_reset_at,max_devices,billing_mode,multiplier_milli,enabled) FROM proxy_users WHERE id=$1`
	if e := f.h.s.DB.QueryRow(ctx, query, user).Scan(&before); e != nil {
		t.Fatal(e)
	}
	node := f.addNode("policy-new-node", 26029)
	group := obj018(f.h.call("POST", "user-groups", map[string]any{"name": "replacement", "user_ids": []string{}, "service_ids": []string{node}}, 200, nil))["id"].(string)
	var oldGroupVersion, newGroupVersion int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM user_groups WHERE id=$1", f.group).Scan(&oldGroupVersion)
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM user_groups WHERE id=$1", group).Scan(&newGroupVersion)
	body := commercePolicyRequest{External: external, User: user, Subscription: sub, Source: ID(), Revision: 2, Groups: []string{group}, Protocols: []string{"vmess"}}
	path := "commerce/accounts/" + external[len("portal:"):] + "/policy"
	f.h.call("PUT", path, body, 200, nil)
	var oldGroupAfter, newGroupAfter int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM user_groups WHERE id=$1", f.group).Scan(&oldGroupAfter)
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM user_groups WHERE id=$1", group).Scan(&newGroupAfter)
	if oldGroupAfter != oldGroupVersion+1 || newGroupAfter != newGroupVersion+1 {
		t.Fatal("group edits can overwrite synchronized membership")
	}
	var after []byte
	f.h.s.DB.QueryRow(ctx, query, user).Scan(&after)
	if string(before) != string(after) {
		t.Fatal("policy changed credentials, time or accounting")
	}
	var old, new bool
	f.h.s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_group_members WHERE user_id=$1 AND group_id=$2),EXISTS(SELECT 1 FROM user_group_members WHERE user_id=$1 AND group_id=$3)", user, f.group, group).Scan(&old, &new)
	if old || !new {
		t.Fatal("group policy not replaced")
	}
	var version int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM proxy_users WHERE id=$1", user).Scan(&version)
	f.h.call("PUT", path, body, 200, nil)
	var again int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM proxy_users WHERE id=$1", user).Scan(&again)
	if again != version {
		t.Fatal("policy replay invalidated inbounds again")
	}
	f.ack()
	commerceReady(t, f)
	link := obj018(f.h.call("GET", "subscriptions/"+sub+"/share?client=mihomo", nil, 200, nil))["url"].(string)
	proxies := proxies019(t, f.share(link, nil, 200))
	if len(proxies) != 1 || proxies[0]["name"] != "policy-new-node" {
		b, _ := json.Marshal(proxies)
		t.Fatal(string(b))
	}
	body.Revision = 1
	f.h.call("PUT", path, body, 409, nil)
	body.Revision = 3
	body.Protocols = []string{"vless"}
	f.h.call("PUT", path, body, 409, nil)
	body.Protocols = []string{"vmess"}
	body.User = ID()
	f.h.call("PUT", path, body, 404, nil)
	// A policy write cannot revive a missing or revoked link.
	body.User = user
	f.h.s.DB.Exec(ctx, "UPDATE subscriptions SET revoked_at=$2 WHERE id=$1", sub, time.Now())
	f.h.call("PUT", path, body, 409, nil)
}
