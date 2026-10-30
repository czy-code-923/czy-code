package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vps-control/internal/live"

	"gopkg.in/yaml.v3"
)

type subscriptionFixture struct {
	h                                    api018
	host                                 map[string]any
	user, group, node, url, subscription string
}

func newSubscriptionFixture(t *testing.T, limit int) subscriptionFixture {
	t.Helper()
	s := testControlDB(t)
	h := api018{t, s}
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "subscription-host", "address": "192.0.2.10"}, 201, nil))
	h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.19.0", "platform": "linux/amd64"}, 200, host)
	h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true, "capabilities": []live.Capability{{Engine: "sing-box", Available: true, Version: "sing-box version 1.14.4"}}}, 200, host)
	h.call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true}, 200, nil)
	f := subscriptionFixture{h: h, host: host}
	f.node = f.addNode("original", 26019)
	u := obj018(h.call("POST", "proxy-users", map[string]any{"name": "existing-user", "enabled": true, "expires_at": time.Now().Add(24 * time.Hour), "max_devices": limit, "device_limit_mode": "hwid"}, 200, nil))
	f.user = u["id"].(string)
	g := obj018(h.call("POST", "user-groups", map[string]any{"name": "existing-group", "user_ids": []string{f.user}, "service_ids": []string{f.node}}, 200, nil))
	f.group = g["id"].(string)
	f.ack()
	sub := obj018(h.call("POST", "subscriptions", map[string]any{"name": "stable-link", "days": 1, "user_id": f.user}, 201, nil))
	f.url = sub["url"].(string) + "?format=mihomo"
	f.subscription = sub["id"].(string)
	f.ack() // Provision the new subscription identity before using its link.
	return f
}
func (f subscriptionFixture) addNode(name string, port int) string {
	f.h.t.Helper()
	node := obj018(f.h.call("POST", "live", map[string]any{"name": name, "host_id": f.host["id"], "kind": "vmess", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": port, "tls_mode": "none", "managed_users": true}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+node+"/toggle", map[string]any{"enabled": true}, 200, nil)
	return node
}
func (f subscriptionFixture) ack() []live.Work {
	f.h.t.Helper()
	var work []live.Work
	json.Unmarshal(f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, f.host), &work)
	reports := []live.Report{}
	kernels := []live.KernelReport{}
	for _, bundle := range work {
		for _, m := range bundle.Members {
			state := "stopped"
			if m.Enabled {
				state = "running"
			}
			reports = append(reports, live.Report{ID: m.ID, Version: m.Version, State: state})
		}
		kernels = append(kernels, live.KernelReport{Engine: bundle.Spec.Engine, Checksum: bundle.Revision(), State: "running"})
	}
	f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true, "reports": reports, "kernel_reports": kernels}, 200, f.host)
	return work
}
func (f subscriptionFixture) share(raw string, headers map[string]string, status int) *httptest.ResponseRecorder {
	f.h.t.Helper()
	r := httptest.NewRequest("GET", raw, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.h.s.ShareHandler().ServeHTTP(w, r)
	if w.Code != status {
		f.h.t.Fatalf("subscription status %d want %d: %s", w.Code, status, w.Body)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		f.h.t.Fatal("subscription can be cached across group changes")
	}
	return w
}
func proxies019(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var config struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(w.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	return config.Proxies
}
func (f subscriptionFixture) changeGroup(nodes []string) {
	f.h.t.Helper()
	var groups []map[string]any
	json.Unmarshal(f.h.call("GET", "user-groups", nil, 200, nil), &groups)
	var version any
	for _, g := range groups {
		if g["id"] == f.group {
			version = g["version"]
		}
	}
	f.h.call("PUT", "user-groups/"+f.group, map[string]any{"version": version, "name": "existing-group", "user_ids": []string{f.user}, "service_ids": nodes}, 200, nil)
}
func TestExistingSubscriptionRefreshesGroupsWithoutRotatingIdentity(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	first := proxies019(t, f.share(f.url, nil, 200))
	if len(first) != 1 {
		t.Fatal("initial nodes", len(first))
	}
	uuid := first[0]["uuid"]
	var identityID string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT id FROM subscription_identities WHERE tenant_id=$1 AND subscription_id=$2 AND device_id IS NULL`, tenant, f.subscription).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	device := obj018(f.h.call("POST", "proxy-users/"+f.user+"/devices", map[string]any{"name": "legacy-device", "client_id": "mihomo"}, 201, nil))
	f.ack()
	durl := device["url"].(string)
	dfirst := proxies019(t, f.share(durl, nil, 200))
	duuid := dfirst[0]["uuid"]
	next := f.addNode("new-node", 26020)
	f.changeGroup([]string{f.node, next})
	var listed []map[string]any
	json.Unmarshal(f.h.call("GET", "subscriptions", nil, 200, nil), &listed)
	for _, row := range listed {
		if row["id"] == f.subscription && len(row["service_ids"].([]any)) != 2 {
			t.Fatal("subscription list retained creation-time group snapshot", row["service_ids"])
		}
	}
	pending := f.share(f.url, nil, 503)
	if pending.Header().Get("Retry-After") == "" {
		t.Fatal("no pending retry guidance")
	}
	f.share(durl, nil, 503)
	work := f.ack()
	accounts := map[string]map[string]string{}
	for _, b := range work {
		for _, m := range b.Members {
			accounts[m.ID] = map[string]string{}
			for _, a := range m.Spec.Accounts {
				accounts[m.ID][a.Name] = a.UUID
			}
		}
	}
	current := proxies019(t, f.share(f.url, nil, 200))
	dcurrent := proxies019(t, f.share(durl, nil, 200))
	if len(current) != 2 || len(dcurrent) != 2 {
		t.Fatal("new group nodes not reflected in stable URLs", len(current), len(dcurrent))
	}
	for _, p := range current {
		if p["uuid"] != uuid {
			t.Fatal("existing user identity rotated")
		}
	}
	for _, p := range dcurrent {
		if p["uuid"] != duuid {
			t.Fatal("device identity rotated")
		}
	}
	for _, node := range []string{f.node, next} {
		if accounts[node][identityID] != uuid || accounts[node][device["id"].(string)] != duuid {
			t.Fatal("client export differs from deployed credentials")
		}
	}
	f.changeGroup([]string{next})
	f.ack()
	if got := proxies019(t, f.share(f.url, nil, 200)); len(got) != 1 || got[0]["port"] != 26020 {
		t.Fatal("removed group node remains in stable link")
	}
	f.h.call("DELETE", "live/"+f.node, nil, 202, nil)
	f.ack()
	if got := proxies019(t, f.share(durl, nil, 200)); len(got) != 1 {
		t.Fatal("device link after deletion", len(got))
	}
	// Move the same user to a different existing group using the user API.
	other := obj018(f.h.call("POST", "user-groups", map[string]any{"name": "other-group", "user_ids": []string{}, "service_ids": []string{next}}, 200, nil))["id"].(string)
	f.h.call("PUT", "proxy-users/"+f.user, map[string]any{"name": "existing-user", "enabled": true, "expires_at": time.Now().Add(24 * time.Hour), "group_ids": []string{other}}, 200, nil)
	f.ack()
	if got := proxies019(t, f.share(f.url, nil, 200)); len(got) != 1 || got[0]["uuid"] != uuid {
		t.Fatal("user group change broke stable user identity")
	}
}

func TestSubscriptionDomainChangeKeepsAppliedHostUntilGatewayReportsNewOne(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	set := func(domain string) {
		f.h.call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true, "share_domain": domain, "share_https": true}, 200, nil)
	}
	report := func(domain, state string) {
		f.h.call("POST", "site/gateway-status", map[string]any{"role": "subscription", "domain": domain, "https": true, "state": state}, 200, nil)
	}
	set("old.example.com")
	report("old.example.com", "active")
	old := strings.Replace(f.url, "vps.example.com", "old.example.com", 1)
	f.share(old, nil, 200)
	set("new.example.com")
	site := obj018(f.h.call("GET", "site", nil, 200, nil))
	if site["share_gateway_state"] != "pending" || site["share_gateway_domain"] != "old.example.com" {
		t.Fatal("missing pending state", site)
	}
	// A delayed report for the old settings must not overwrite the new state.
	report("old.example.com", "active")
	site = obj018(f.h.call("GET", "site", nil, 200, nil))
	if site["share_gateway_state"] != "pending" {
		t.Fatal("stale gateway report accepted", site)
	}
	f.share(old, nil, 200)
	report("new.example.com", "error")
	site = obj018(f.h.call("GET", "site", nil, 200, nil))
	if site["share_gateway_state"] != "error" || site["share_gateway_domain"] != "old.example.com" {
		t.Fatal("failed update lost live gateway", site)
	}
	f.share(old, nil, 200)
	report("new.example.com", "active")
	newURL := strings.Replace(f.url, "vps.example.com", "new.example.com", 1)
	f.share(newURL, nil, 200)
	f.share(old, nil, 404)
	link := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/link", nil, 200, nil))["url"].(string)
	if !strings.HasPrefix(link, "https://new.example.com/") {
		t.Fatal("link not updated", link)
	}
}

func TestStableSubscriptionRejectsPartialSnapshotFromNewGroup(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	device := obj018(f.h.call("POST", "proxy-users/"+f.user+"/devices", map[string]any{"name": "restricted-device", "client_id": "mihomo", "all_nodes": false, "service_ids": []string{f.node}}, 201, nil))
	f.ack()
	original := proxies019(t, f.share(f.url, nil, 200))[0]["uuid"]
	second := f.addNode("second-group-node", 26022)
	f.h.call("POST", "user-groups", map[string]any{"name": "newly-added-group", "user_ids": []string{f.user}, "service_ids": []string{second}}, 200, nil)
	var state string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT state FROM live_services WHERE id=$1`, f.node).Scan(&state); err != nil || state != "running" {
		t.Fatal("fixture must retain an independently running old group", state, err)
	}
	// Previously this request succeeded with only the old group's node. A client
	// treated that incomplete response as a successful update and retained it.
	pending := f.share(f.url, nil, 503)
	if pending.Header().Get("X-Starhub-Pending-Nodes") != "1" {
		t.Fatal("pending group is not visible in status")
	}
	if got := proxies019(t, f.share(device["url"].(string), nil, 200)); len(got) != 1 {
		t.Fatal("unrelated pending node blocked a restricted device's complete snapshot")
	}
	f.ack()
	got := proxies019(t, f.share(f.url, nil, 200))
	if len(got) != 2 || got[0]["uuid"] != original || got[1]["uuid"] != original {
		t.Fatal("stable subscription did not refresh after the new group's real deployment")
	}
}
