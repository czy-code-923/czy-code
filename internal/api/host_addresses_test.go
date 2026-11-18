package api

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"vps-control/internal/live"
)

func TestReportedPrimaryPreservesStableAndManualNetworkBoundaries(t *testing.T) {
	for _, tc := range []struct {
		old  string
		ips  []string
		want string
	}{
		{"outbound-agent", []string{"172.17.0.1", "2001:db8::1"}, "2001:db8::1"},
		{"192.0.2.42", []string{"172.17.0.1", "192.0.2.62"}, "192.0.2.62"},
		{"192.0.2.42", []string{"192.0.2.62", "192.0.2.42"}, "192.0.2.42"},
		{"192.0.2.42", []string{"172.17.0.1", "2001:db8::1"}, "192.0.2.42"},
		{"2001:db8::1", []string{"2001:db8::2"}, "2001:db8::2"},
		{"192.0.2.42", nil, "192.0.2.42"},
	} {
		if got := reportedPrimary(tc.old, tc.ips); got != tc.want {
			t.Fatalf("%s: %s != %s", tc.old, got, tc.want)
		}
	}
}

func TestReconcileHostSpecRetainsCustomAndAmbiguousAddresses(t *testing.T) {
	for _, address := range []string{"edge.example.com", "198.51.100.10"} {
		s := live.Spec{Kind: "vmess", Address: address}
		if reconcileHostSpec(&s, "192.0.2.42", "192.0.2.62", []string{"192.0.2.62"}) {
			t.Fatal("changed independent address", address)
		}
	}
	s := live.Spec{Kind: "vmess", Address: "192.0.2.42", EndpointAddresses: []string{"192.0.2.42", "2001:db8::1"}, ServerName: "kept.example.com", Password: "keep", UUID: "keep-uuid"}
	if !reconcileHostSpec(&s, "192.0.2.42", "192.0.2.62", []string{"192.0.2.62", "2001:db8::2"}) || !reflect.DeepEqual(s.EndpointAddresses, []string{"192.0.2.62", "2001:db8::2"}) || s.ServerName != "kept.example.com" || s.Password != "keep" || s.UUID != "keep-uuid" {
		t.Fatal("dual stack address or credentials", s.Address, s.EndpointAddresses)
	}
	if reconcileHostSpec(&s, "192.0.2.62", "192.0.2.62", []string{"192.0.2.62", "2001:db8::2"}) {
		t.Fatal("repeat heartbeat changed config")
	}
	s = live.Spec{Kind: "vmess", Address: "192.0.2.1", EndpointAddresses: []string{"192.0.2.1"}}
	if reconcileHostSpec(&s, "192.0.2.9", "192.0.2.9", []string{"192.0.2.2", "192.0.2.3"}) {
		t.Fatal("ambiguous address was guessed")
	}
}

func TestHostIPChangeUpdatesExistingSubscriptionWithoutIdentityRotation(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	host := f.host["id"].(string)
	ctx := context.Background()
	before := proxies019(t, f.share(f.url, nil, 200))[0]
	f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.25.9", "addresses": []string{"192.0.2.10", "2001:db8::1"}}, 200, f.host)
	// A legacy selected endpoint can already be stale when upgrading the control plane.
	var sealed []byte
	if err := f.h.s.DB.QueryRow(ctx, "SELECT sealed FROM live_services WHERE id=$1", f.node).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := live.Open(f.h.s.MasterKey, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var spec live.Spec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	spec.EndpointAddresses = []string{"192.0.2.10"}
	raw, _ = json.Marshal(spec)
	sealed, err = live.Seal(f.h.s.MasterKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.s.DB.Exec(ctx, "UPDATE live_services SET sealed=$2 WHERE id=$1", f.node, sealed); err != nil {
		t.Fatal(err)
	}
	f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.25.9", "addresses": []string{"172.17.0.1", "192.0.2.62", "2001:db8::2"}}, 200, f.host)
	var address string
	if err = f.h.s.DB.QueryRow(ctx, "SELECT address FROM hosts WHERE id=$1", host).Scan(&address); err != nil || address != "192.0.2.62" {
		t.Fatal(address, err)
	}
	f.ack()
	after := proxies019(t, f.share(f.url, nil, 200))[0]
	if after["server"] != "192.0.2.62" || before["uuid"] != after["uuid"] {
		t.Fatal("subscription address/identity changed incorrectly")
	}
	for _, client := range []string{"mihomo", "clash-verge", "flclash", "sing-box", "xray", "shadowrocket", "v2rayng"} {
		w := f.share(strings.Split(f.url, "?")[0]+"?client="+client, nil, 200)
		if w.Header().Get("X-Starhub-Excluded-Nodes") != "0" {
			t.Fatal("compatible node excluded", client)
		}
	}
	var version int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM live_services WHERE id=$1", f.node).Scan(&version)
	f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.25.9"}, 200, f.host)
	var unchanged int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM live_services WHERE id=$1", f.node).Scan(&unchanged)
	if unchanged != version {
		t.Fatal("empty heartbeat restarted service")
	}
	// A manual inventory edit updates exact old references before Agent reports it.
	f.h.call("PATCH", "hosts/"+host, map[string]any{"address": "192.0.2.63", "address_mode": "manual"}, 200, nil)
	f.ack()
	after = proxies019(t, f.share(f.url, nil, 200))[0]
	if after["server"] != "192.0.2.63" || after["uuid"] != before["uuid"] {
		t.Fatal("manual edit did not sync published address")
	}
	f.h.call("POST", "agent/heartbeat", map[string]any{"addresses": []string{"192.0.2.64"}}, 200, f.host)
	f.h.s.DB.QueryRow(ctx, "SELECT address FROM hosts WHERE id=$1", host).Scan(&address)
	if address != "192.0.2.63" {
		t.Fatal("manual address overwritten", address)
	}
	f.h.call("PATCH", "hosts/"+host, map[string]any{"address_mode": "invalid"}, 400, nil)
	f.h.call("PATCH", "hosts/"+host, map[string]any{"address_mode": "auto"}, 200, nil)
	f.h.s.DB.QueryRow(ctx, "SELECT address FROM hosts WHERE id=$1", host).Scan(&address)
	if address != "192.0.2.64" {
		t.Fatal("auto mode did not resume", address)
	}
	f.h.call("PATCH", "hosts/"+host, map[string]any{"address": "", "address_mode": "auto"}, 200, nil)
	f.ack()
	if proxies019(t, f.share(f.url, nil, 200))[0]["server"] != "192.0.2.64" {
		t.Fatal("clearing auto address broke node")
	}
	f.h.call("PATCH", "hosts/"+host, map[string]any{"address": "inventory.example.com"}, 200, nil)
	f.h.call("POST", "agent/heartbeat", map[string]any{"addresses": []string{"192.0.2.65"}}, 200, f.host)
	f.h.s.DB.QueryRow(ctx, "SELECT address FROM hosts WHERE id=$1", host).Scan(&address)
	if address != "inventory.example.com" {
		t.Fatal("legacy manual inventory domain overwritten", address)
	}
}

func TestIPChangeNotifiesCrossHostChainAndPreservesCustomDomain(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	h := obj018(f.h.call("POST", "hosts", map[string]any{"name": "upstream", "address": "198.51.100.1"}, 201, nil))
	f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.25.9", "addresses": []string{"198.51.100.1"}}, 200, h)
	parent := obj018(f.h.call("POST", "live", map[string]any{"name": "chain", "host_id": h["id"], "kind": "vmess", "engine": "sing-box", "listen": "0.0.0.0", "port": 26021, "address": "198.51.100.1", "tls_mode": "none", "next_id": f.node}, 201, nil))["id"].(string)
	custom := obj018(f.h.call("POST", "live", map[string]any{"name": "domain", "host_id": f.host["id"], "kind": "vmess", "engine": "sing-box", "listen": "0.0.0.0", "port": 26022, "address": "proxy.example.com", "tls_mode": "none"}, 201, nil))["id"].(string)
	ctx := context.Background()
	var before, after int
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM live_services WHERE id=$1", parent).Scan(&before)
	f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.25.9", "addresses": []string{"192.0.2.11"}}, 200, f.host)
	f.h.s.DB.QueryRow(ctx, "SELECT version FROM live_services WHERE id=$1", parent).Scan(&after)
	if after <= before {
		t.Fatal("upstream chain was not notified")
	}
	var work []live.Work
	if err := json.Unmarshal(f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, h), &work); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, bundle := range work {
		for _, m := range bundle.Members {
			if m.ID == parent {
				found = m.Spec.Outbound != nil && m.Spec.Outbound.Address == "192.0.2.11"
			}
		}
	}
	if !found {
		t.Fatal("upstream retained old outbound IP")
	}
	var address string
	f.h.s.DB.QueryRow(ctx, "SELECT address FROM live_services WHERE id=$1", custom).Scan(&address)
	if address != "proxy.example.com" {
		t.Fatal("custom domain overwritten", address)
	}
}
