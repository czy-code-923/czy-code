package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
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
			t.Fatal(e)
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
	if len(member.Spec.Accounts) != 1 || member.Spec.Accounts[0].Name != user["id"] || member.Spec.Accounts[0].UUID == member.Spec.UUID {
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
	json.Unmarshal(raw, &bundles)
	if len(bundles[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("revoked member remains in server inbound")
	}
	call("PUT", "proxy-users/"+user["id"].(string), map[string]any{"name": "Alice", "enabled": false, "expires_at": time.Now().Add(time.Hour)}, 200, nil)
	share("", 404)
}
