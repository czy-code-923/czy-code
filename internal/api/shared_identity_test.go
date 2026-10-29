package api

import (
	"strings"
	"testing"
	"vps-control/internal/live"
)

func TestIndependentSSIdentityAndRevocation(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.23.0", "platform": "linux/amd64"}, 200, f.host)
	id := obj018(f.h.call("POST", "live", map[string]any{"name": "private-ss", "host_id": f.host["id"], "kind": "shadowsocks", "engine": "sing-box", "address": "192.0.2.10", "listen": "127.0.0.1", "port": 26323, "managed_users": true}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+id+"/toggle", map[string]any{"enabled": true}, 200, nil)
	f.changeGroup([]string{f.node, id})
	work := f.ack()
	var server live.Spec
	for _, b := range work {
		for _, m := range b.Members {
			if m.ID == id {
				server = m.Spec
			}
		}
	}
	if len(server.Accounts) != 1 || server.Accounts[0].Name != f.user {
		t.Fatal("independent SS identity missing")
	}
	expected := server
	expected.ApplyAccount(server.Accounts[0])
	_, mh, _, _ := expected.Client("private-ss")
	got := proxies019(t, f.share(f.url, nil, 200))
	if len(got) != 2 {
		t.Fatal("other inbound was lost")
	}
	for _, p := range got {
		if p["name"] == "private-ss" && (p["password"] != mh["password"] || len(strings.Split(p["password"].(string), ":")) != 2) {
			t.Fatal("SS 2022 identity chain mismatch")
		}
	}
	f.changeGroup([]string{f.node})
	work = f.ack()
	decoy := ""
	for _, b := range work {
		for _, m := range b.Members {
			if m.ID == id {
				if len(m.Spec.Accounts) != 1 || m.Spec.Accounts[0].Name != "_starhub_unassigned" {
					t.Fatal("empty list reopened shared-key access")
				}
				decoy = m.Spec.Accounts[0].Password
			}
		}
	}
	if decoy == "" || strings.Contains(server.Password, decoy) {
		t.Fatal("unsafe unassigned credential")
	}
	if len(proxies019(t, f.share(f.url, nil, 200))) != 1 {
		t.Fatal("revoked node survived or other inbound disappeared")
	}
	for _, b := range f.ack() {
		for _, m := range b.Members {
			if m.ID == id && m.Spec.Accounts[0].Password != decoy {
				t.Fatal("unassigned identity changes every poll")
			}
		}
	}
}
