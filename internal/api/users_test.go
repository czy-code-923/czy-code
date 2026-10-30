package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"vps-control/internal/live"
	"vps-control/internal/store"
)

func TestHostKernelUsersIntegration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dbURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "users_" + ID()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(dbURL)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &Server{DB: db, AdminToken: strings.Repeat("a", 32), MasterKey: bytes.Repeat([]byte{7}, 32)}
	admin, public := s.Handler(), s.ShareHandler()
	call := func(method, path string, body any, status int, agent map[string]any) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		if agent != nil {
			r.Header.Set("Authorization", "Bearer "+agent["agent_token"].(string))
			r.Header.Set("X-Host-ID", agent["id"].(string))
		}
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	object := func(raw []byte) map[string]any {
		t.Helper()
		var m map[string]any
		if e := json.Unmarshal(raw, &m); e != nil {
			var value any
			if e = yaml.Unmarshal(raw, &value); e != nil {
				t.Fatal(e)
			}
			normalized, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if e = json.Unmarshal(normalized, &m); e != nil {
				t.Fatal(e)
			}
		}
		return m
	}
	h1 := object(call("POST", "hosts", map[string]any{"name": "Host A", "provider": "fixture", "address": "192.0.2.1"}, 201, nil))
	h2 := object(call("POST", "hosts", map[string]any{"name": "Host B", "provider": "fixture", "address": "2001:db8::2"}, 201, nil))
	for _, h := range []map[string]any{h1, h2} {
		call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.8.0", "platform": "linux/amd64"}, 200, h)
		call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true, "capabilities": []map[string]any{{"engine": "sing-box", "available": true, "version": "sing-box 1.14.3"}, {"engine": "xray", "available": true, "version": "Xray 26.10.21"}}}, 200, h)
	}
	call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true}, 200, nil)
	a := object(call("POST", "live", map[string]any{"name": "A managed VMess", "host_id": h1["id"], "kind": "vmess", "engine": "sing-box", "address": "192.0.2.1", "listen": "127.0.0.1", "port": 24000, "tls_mode": "none", "managed_users": true}, 201, nil))
	b := object(call("POST", "live", map[string]any{"name": "B REALITY", "host_id": h2["id"], "kind": "vless", "engine": "xray", "address": "2001:db8::2", "listen": "::1", "port": 24443, "tls_mode": "reality", "flow": "xtls-rprx-vision", "server_name": "www.microsoft.com", "reality": map[string]any{"target": "www.microsoft.com", "port": 443, "fingerprint": "chrome"}, "managed_users": true}, 201, nil))
	for _, v := range []map[string]any{a, b} {
		call("POST", "live/"+v["id"].(string)+"/toggle", map[string]bool{"enabled": true}, 200, nil)
	}
	user := object(call("POST", "proxy-users", map[string]any{"name": "Alice", "enabled": true, "expires_at": time.Now().Add(24 * time.Hour)}, 200, nil))
	group := object(call("POST", "user-groups", map[string]any{"name": "Alice A", "user_ids": []any{user["id"]}, "service_ids": []any{a["id"]}}, 200, nil))
	profile := object(call("POST", "client-profiles", map[string]any{"name": "Mihomo auto", "format": "mihomo", "local_port": 1088, "selection": "urltest", "test_url": "https://www.gstatic.com/generate_204"}, 200, nil))
	sub := object(call("POST", "subscriptions", map[string]any{"name": "Alice subscription", "user_id": user["id"], "profile_id": profile["id"], "days": 30}, 201, nil))
	call("POST", "subscriptions", map[string]any{"name": "unsafe shared", "service_ids": []any{a["id"]}, "days": 30}, 400, nil)
	raw := call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h1)
	var bundles []live.Work
	if e = json.Unmarshal(raw, &bundles); e != nil {
		t.Fatal(e)
	}
	if len(bundles) != 1 || len(bundles[0].Members) != 1 || bundles[0].Members[0].ID != a["id"] || bundles[0].Spec.Engine != "sing-box" {
		t.Fatal("cross-host or engine deployment leaked")
	}
	member := bundles[0].Members[0]
	if len(member.Spec.Accounts) != 1 || member.Spec.Accounts[0].Name == user["id"] || member.Spec.Accounts[0].UUID == member.Spec.UUID {
		t.Fatal("user credentials not materialized independently")
	}
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": member.ID, "version": member.Version, "state": "running"}}, "bundle_mode": true, "kernel_reports": []live.KernelReport{{Engine: "sing-box", Checksum: bundles[0].Revision(), State: "running"}}}, 200, h1)
	kernel := object(call("GET", "hosts/"+h1["id"].(string)+"/kernel-config?engine=sing-box", nil, 200, nil))
	preview, _ := json.Marshal(kernel)
	if strings.Contains(string(preview), member.Spec.Accounts[0].UUID) || strings.Contains(string(preview), member.Spec.Password) {
		t.Fatal("kernel preview leaks credentials")
	}
	share := func(suffix string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest("GET", sub["url"].(string)+suffix, nil)
		w := httptest.NewRecorder()
		public.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("share got %d want %d: %s", w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	mh := object(share("", 200))
	if mh["mixed-port"] != float64(1088) {
		t.Fatal("client profile ignored")
	}
	out := object(share("?format=sing-box", 200))
	outs := out["outbounds"].([]any)
	if outs[0].(map[string]any)["uuid"] != member.Spec.Accounts[0].UUID {
		t.Fatal("subscription does not use user credential")
	}
	detail := object(call("GET", "live/"+b["id"].(string), nil, 200, nil))
	detailRaw, _ := json.Marshal(detail)
	if strings.Contains(string(detailRaw), "PRIVATE KEY") {
		t.Fatal("REALITY private key exposed")
	}
	spec := detail["spec"].(map[string]any)
	if spec["reality"].(map[string]any)["private_key"] != nil {
		t.Fatal("REALITY key exposed in detail")
	}
	call("PUT", "user-groups/"+group["id"].(string), map[string]any{"name": "Alice A", "user_ids": []any{}, "service_ids": []any{a["id"]}, "version": 1}, 200, nil)
	share("", 503)
	raw = call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h1)
	bundles = nil
	bundles = nil
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("revoked member remains in server inbound")
	}
	call("PUT", "proxy-users/"+user["id"].(string), map[string]any{"name": "Alice", "enabled": false, "expires_at": time.Now().Add(time.Hour)}, 200, nil)
	share("", 404)
	// CRUD lifecycle: links remain recoverable, metadata edits preserve tokens, user deletion revokes access.
	uid, gid, sid := user["id"].(string), group["id"].(string), sub["id"].(string)
	recovered := object(call("GET", "subscriptions/"+sid+"/link", nil, 200, nil))
	if recovered["url"] != sub["url"] {
		t.Fatal("recoverable link changed token")
	}
	call("PUT", "subscriptions/"+sid, map[string]any{"name": "renamed", "expires_at": time.Now().Add(time.Hour), "profile_id": profile["id"]}, 200, nil)
	call("PUT", "proxy-users/"+uid, map[string]any{"name": "Alice", "enabled": true, "expires_at": time.Now().Add(time.Hour), "group_ids": []string{gid}}, 200, nil)
	raw = call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h1)
	bundles = nil
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 1 {
		t.Fatal("user editor did not assign group")
	}
	call("DELETE", "client-profiles/"+profile["id"].(string), nil, 200, nil)
	call("DELETE", "proxy-users/"+uid, nil, 200, nil)
	share("", 404)
	raw = call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h1)
	bundles = nil
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("deleted account remains in kernel")
	}
	call("DELETE", "user-groups/"+gid, nil, 200, nil)
	// Stale stop acknowledgement must never release a port. Current acknowledgement finalizes deletion.
	aid := a["id"].(string)
	call("DELETE", "live/"+aid, nil, 202, nil)
	call("POST", "live/"+aid+"/toggle", map[string]bool{"enabled": true}, 409, nil)
	call("DELETE", "hosts/"+h1["id"].(string), nil, 409, nil)
	detail = object(call("GET", "live/"+aid, nil, 200, nil))
	version := int(detail["version"].(float64))
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": aid, "version": version - 1, "state": "stopped"}}, "bundle_mode": true}, 200, h1)
	call("GET", "live/"+aid, nil, 200, nil)
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": aid, "version": version, "state": "stopped"}}, "bundle_mode": true}, 200, h1)
	call("GET", "live/"+aid, nil, 404, nil)
	var ports int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM live_ports WHERE tenant_id=$1 AND service_id=$2`, tenant, aid).Scan(&ports); err != nil || ports != 0 {
		t.Fatal("deleted inbound retained port", err)
	}
	call("DELETE", "hosts/"+h1["id"].(string), nil, 409, nil)
	call("POST", "agent/heartbeat", map[string]any{"version": "0.12.0", "cleanup_capable": true}, 200, h1)
	retirement := object(call("POST", "hosts/"+h1["id"].(string)+"/retirement", map[string]any{}, 202, nil))
	receipt := map[string]any{"retire_id": retirement["retire_id"]}
	call("POST", "agent/retirement/begin", receipt, 200, h1)
	call("POST", "agent/retirement/ack", receipt, 200, h1)
	call("POST", "agent/retirement/ack", receipt, 200, h1)
	call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.9.0", "platform": "linux/amd64"}, 401, h1)
	call("POST", "hosts/"+h1["id"].(string)+"/enrollment", map[string]any{}, 404, nil)
	remaining := call("GET", "hosts", nil, 200, nil)
	if strings.Contains(string(remaining), h1["id"].(string)) {
		t.Fatal("retired host visible")
	}

	// Quotas require actual metering-capable hosts, use absolute counters, and revoke kernel identities.
	quotaUser := object(call("POST", "proxy-users", map[string]any{"name": "Metered", "enabled": true, "expires_at": time.Now().Add(time.Hour), "quota_bytes": 1000, "reset_strategy": "daily"}, 200, nil))
	qid := quotaUser["id"].(string)
	accessGroup := map[string]any{"name": "Metered group", "user_ids": []string{qid}, "service_ids": []any{b["id"]}}
	call("POST", "user-groups", accessGroup, 409, nil)
	call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.10.0", "platform": "linux/amd64"}, 200, h2)
	call("POST", "user-groups", accessGroup, 200, nil)
	collecting := []live.MeterStatus{{Engine: "xray", State: "collecting"}}
	call("POST", "agent/telemetry", live.TelemetryBatch{Status: collecting}, 200, h2)
	raw = call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h2)
	bundles = nil
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 1 {
		t.Fatal("meter-ready account not admitted")
	}
	counter := live.TrafficCounter{ID: Hash("xray/user/" + qid)[:32], Kind: "user", Subject: qid, Up: 300, Down: 400}
	observation := live.AccessObservation{ID: strings.Repeat("d", 32), User: qid, Service: b["id"].(string), Source: "2001:db8::99", Destination: "example.com", Port: 443, Network: "tcp"}
	batch := live.TelemetryBatch{Counters: []live.TrafficCounter{counter}, Access: []live.AccessObservation{observation}, Status: collecting}
	call("POST", "agent/telemetry", batch, 200, h2)
	call("POST", "agent/telemetry", batch, 200, h2)
	var used, lifetime int64
	if e = db.QueryRow(ctx, `SELECT used_bytes,lifetime_bytes FROM proxy_users WHERE tenant_id=$1 AND id=$2`, tenant, qid).Scan(&used, &lifetime); e != nil || used != 700 || lifetime != 700 {
		t.Fatal("retry double-billed", used, lifetime, e)
	}
	baseline := call("POST", "agent/telemetry/baseline", nil, 200, h2)
	if !strings.Contains(string(baseline), counter.ID) {
		t.Fatal("baseline missing")
	}
	records := call("GET", "access-records?user_id="+qid, nil, 200, nil)
	if strings.Count(string(records), "example.com") != 1 || !strings.Contains(string(records), "2001:db8::99") {
		t.Fatal("access records duplicated or IPv6 lost", string(records))
	}
	batch.Counters[0].Up = 500
	batch.Counters[0].Down = 800
	call("POST", "agent/telemetry", batch, 200, h2)
	raw = call("POST", "agent/live/sync", map[string]any{"reports": []any{}, "bundle_mode": true}, 200, h2)
	bundles = nil
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("quota-exhausted identity still admitted")
	}
	call("POST", "proxy-users/"+qid+"/reset-traffic", map[string]any{}, 200, nil)
	call("POST", "agent/telemetry", batch, 200, h2)
	if e = db.QueryRow(ctx, `SELECT used_bytes,lifetime_bytes FROM proxy_users WHERE tenant_id=$1 AND id=$2`, tenant, qid).Scan(&used, &lifetime); e != nil || used != 0 || lifetime != 1300 {
		t.Fatal("manual reset reapplied old counters", used, lifetime, e)
	}
	batch.Counters[0].Down = 900
	call("POST", "agent/telemetry", batch, 200, h2)
	if _, e = db.Exec(ctx, `UPDATE proxy_users SET last_reset_at=now()-interval '2 days' WHERE tenant_id=$1 AND id=$2`, tenant, qid); e != nil {
		t.Fatal(e)
	}
	if e = s.Reap(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(ctx, `SELECT used_bytes FROM proxy_users WHERE tenant_id=$1 AND id=$2`, tenant, qid).Scan(&used); e != nil || used != 0 {
		t.Fatal("automatic quota reset", used, e)
	}
	call("GET", "dashboard", nil, 200, nil)
	batch.Access[0].Destination = "https://example.com/private?token=secret"
	call("POST", "agent/telemetry", batch, 400, h2)
	// Cleanup is narrowly scoped: ordinary, enabled, or granted inbounds survive.
	fixture := object(call("POST", "live", map[string]any{"name": "Acceptance unused", "host_id": h2["id"], "kind": "vmess", "engine": "sing-box", "address": "2001:db8::2", "listen": "::1", "port": 25000, "tls_mode": "none"}, 201, nil))
	fid := fixture["id"].(string)
	previewCleanup := call("GET", "maintenance/trials", nil, 200, nil)
	if !strings.Contains(string(previewCleanup), fid) {
		t.Fatal("cleanup preview missing fixture")
	}
	call("POST", "maintenance/trials", map[string]any{"ids": []any{b["id"]}}, 409, nil)
	call("POST", "maintenance/trials", map[string]any{"ids": []string{fid}}, 200, nil)
	call("GET", "live/"+fid, nil, 404, nil)
	call("GET", "live/"+b["id"].(string), nil, 200, nil)
}
