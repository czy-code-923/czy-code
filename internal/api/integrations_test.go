package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vps-control/internal/store"
)

func TestMonthlyCycleCalendar(t *testing.T) {
	for _, c := range []struct{ anchor, after, want string }{
		{"2026-01-31T12:30:00+08:00", "2026-01-31T12:30:00+08:00", "2026-02-28T12:30:00+08:00"},
		{"2026-01-31T12:30:00+08:00", "2026-02-28T12:30:00+08:00", "2026-03-31T12:30:00+08:00"},
		{"2028-01-31T12:30:00+08:00", "2028-02-10T12:30:00+08:00", "2028-02-29T12:30:00+08:00"},
		{"2025-12-31T12:30:00+08:00", "2027-02-28T12:30:00+08:00", "2027-03-31T12:30:00+08:00"},
	} {
		a, _ := time.Parse(time.RFC3339, c.anchor)
		after, _ := time.Parse(time.RFC3339, c.after)
		if got := NextMonthlyReset(a, after).Format(time.RFC3339); got != c.want {
			t.Fatal(got, c.want)
		}
	}
}

func TestIntegrationPermissionsAndSubscriptions(t *testing.T) {
	rawURL := os.Getenv("TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, rawURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "integration_" + ID()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(rawURL)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := &Server{DB: db, AdminToken: strings.Repeat("a", 32), MasterKey: bytes.Repeat([]byte{9}, 32)}
	handler := s.Handler()
	request := func(method, path string, body any, token, origin string, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	call := func(method, path string, b any, want int) *httptest.ResponseRecorder {
		return request(method, path, b, s.AdminToken, "", want)
	}
	object := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var m map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	sql := func(query string, args ...any) {
		t.Helper()
		if _, e = db.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true, "title": "星枢测试", "api_origins": []string{"https://panel.example.com"}}, 200)
	request("OPTIONS", "proxy-users", nil, "", "https://panel.example.com", 204)
	request("OPTIONS", "proxy-users", nil, "", "https://panel.example.com.evil.test", 403)
	request("GET", "proxy-users", nil, "", "https://panel.example.com", 401)
	call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true, "api_origins": []string{"https://*.example.com"}}, 400)
	if object(call("GET", "branding", nil, 200))["title"] != "星枢测试" {
		t.Fatal("branding")
	}
	keyBody := map[string]any{"name": "read only", "scopes": []string{"users:read", "subscriptions:read"}, "expires_at": time.Now().Add(time.Hour), "rate_limit": 120}
	key := object(call("POST", "api-keys", keyBody, 201))
	token, kid := key["token"].(string), key["id"].(string)
	var stored string
	if e = db.QueryRow(ctx, `SELECT token_hash FROM api_keys WHERE id=$1`, kid).Scan(&stored); e != nil || stored == token || stored != Hash(token) {
		t.Fatal("key storage")
	}
	request("GET", "proxy-users", nil, token, "", 200)
	request("GET", "openapi", nil, token, "", 200)
	request("POST", "proxy-users", map[string]any{}, token, "", 403)
	request("GET", "access-records", nil, token, "", 403)
	request("GET", "api-keys", nil, token, "", 403)
	request("GET", "snapshot", nil, token, "", 403)
	request("POST", "site/gateway-status", map[string]any{}, token, "", 403)
	request("POST", "hosts/"+ID()+"/rotate-agent-token", nil, token, "", 403)
	if strings.Contains(call("GET", "api-keys", nil, 200).Body.String(), token) {
		t.Fatal("listed plaintext key")
	}
	call("DELETE", "api-keys/"+kid, nil, 200)
	request("GET", "proxy-users", nil, token, "", 401)
	keyBody["version"] = 1
	call("PUT", "api-keys/"+kid, keyBody, 409)
	keyBody["scopes"] = []string{"*"}
	call("POST", "api-keys", keyBody, 400)
	delete(keyBody, "version")
	keyBody["scopes"] = []string{"users:read"}
	keyBody["rate_limit"] = 1
	key = object(call("POST", "api-keys", keyBody, 201))
	token = key["token"].(string)
	request("GET", "proxy-users", nil, token, "", 200)
	request("GET", "proxy-users", nil, token, "", 429)
	// One host/core batch: validation rollback, port uniqueness and retry idempotence.
	h := object(call("POST", "hosts", map[string]any{"name": "fixture", "provider": "test", "address": "192.0.2.1"}, 201))
	hid := h["id"].(string)
	sql(`UPDATE hosts SET agent_version='0.10.0',last_seen=now() WHERE id=$1`, hid)
	first := map[string]any{"name": "shared", "host_id": hid, "kind": "shadowsocks", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.1", "port": 0}
	second := map[string]any{"name": "invalid", "host_id": hid, "kind": "vmess", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.1", "port": 0, "tls_mode": "invalid", "managed_users": true}
	batch := map[string]any{"items": []any{first, second}, "start_port": 25000, "request_key": ID()}
	call("POST", "live/batch", batch, 400)
	var n int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM live_services`).Scan(&n); e != nil || n != 0 {
		t.Fatal("batch partially saved")
	}
	second["tls_mode"] = "none"
	created := object(call("POST", "live/batch", batch, 201))
	items := created["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["port"] == items[1].(map[string]any)["port"] {
		t.Fatal("ports not unique")
	}
	retry := object(call("POST", "live/batch", batch, 201))
	if fmt.Sprint(retry["items"]) != fmt.Sprint(items) {
		t.Fatal("retry duplicate batch")
	}
	batch["start_port"] = 26000
	call("POST", "live/batch", batch, 409)
	sid := items[0].(map[string]any)["id"].(string)
	sql(`UPDATE live_services SET enabled=true,state='running',reported_version=version,reported_at=now() WHERE id=$1`, sid)
	sub := object(call("POST", "subscriptions", map[string]any{"name": "共享测试", "service_ids": []string{sid}, "days": 30}, 201))
	subid := sub["id"].(string)
	sql(`INSERT INTO traffic_cursors(tenant_id,host_id,counter_id,subject_kind,subject_id,uplink,downlink) VALUES('self',$1,$2,'service',$3,1024,4096)`, hid, ID(), sid)
	download := func(format, ua string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", sub["url"].(string)+format, nil)
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		s.ShareHandler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w
	}
	w := download("", "ClashVerge/2")
	header := w.Header().Get("Subscription-Userinfo")
	if !strings.Contains(header, "upload=1024; download=4096; total=0;") || !strings.Contains(w.Header().Get("Content-Type"), "yaml") {
		t.Fatal("Clash metadata or format", header)
	}
	var mh map[string]any
	if e = yaml.Unmarshal(w.Body.Bytes(), &mh); e != nil {
		t.Fatal(e)
	}
	// Manual YAML source round trips, is merged, and cannot replace generated credentials.
	profileBody := map[string]any{"name": "manual", "format": "mihomo", "local_port": 1088, "selection": "select", "test_url": "https://www.gstatic.com/generate_204", "extra_text": "# comment\nipv6: true\nrules:\n  - MATCH,PROXY\n"}
	profile := object(call("POST", "client-profiles", profileBody, 200))
	pid := profile["id"].(string)
	profileBody["version"] = 0
	call("PUT", "client-profiles/"+pid, profileBody, 409)
	profileBody["version"] = 1
	call("PUT", "client-profiles/"+pid, profileBody, 200)
	profileBody["extra_text"] = "proxies: []"
	call("POST", "client-profiles", profileBody, 400)
	sql(`UPDATE subscriptions SET profile_id=$2 WHERE id=$1`, subid, pid)
	w = download("?format=mihomo", "")
	yaml.Unmarshal(w.Body.Bytes(), &mh)
	if mh["ipv6"] != true {
		t.Fatal("manual config ignored")
	}
	if binary := os.Getenv("TEST_MIHOMO_BINARY"); binary != "" {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.yaml")
		os.WriteFile(file, w.Body.Bytes(), 0600)
		output, err := exec.Command(binary, "-t", "-d", dir, "-f", file).CombinedOutput()
		if err != nil {
			t.Fatal("Mihomo rejected generated config", string(output))
		}
	}
	preview := call("GET", "subscriptions/"+subid+"/config?format=mihomo", nil, 200)
	check := object(call("GET", "subscriptions/"+subid+"/check?client=mihomo", nil, 200))
	if check["ready"] != true || strings.Contains(fmt.Sprint(check), sub["url"].(string)) {
		t.Fatal("readiness or private link leak", check)
	}
	sql(`UPDATE live_services SET enabled=false,state='stopped' WHERE id=$1`, sid)
	check = object(call("GET", "subscriptions/"+subid+"/check?client=mihomo", nil, 200))
	if check["ready"] != false || check["status"] != float64(503) || check["stage"] != "nodes" {
		t.Fatal("stopped subscription incorrectly ready", check)
	}
	sql(`UPDATE live_services SET enabled=true,state='running' WHERE id=$1`, sid)
	spec, e := s.loadSpec(httptest.NewRequest("GET", "/", nil), sid)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(preview.Body.String(), spec.Password) {
		t.Fatal("preview leaked proxy credential")
	}
	// User cycle scheduling, identity binding, accurate quota headers and quota-exhausted refresh.
	user := object(call("POST", "proxy-users", map[string]any{"name": "monthly", "enabled": true, "expires_at": time.Now().Add(24 * time.Hour), "quota_bytes": 1000000, "reset_strategy": "rolling_monthly"}, 200))
	uid := user["id"].(string)
	call("POST", "user-groups", map[string]any{"name": "allowed", "user_ids": []string{uid}, "service_ids": []string{items[1].(map[string]any)["id"].(string)}}, 200)
	var anchor, next time.Time
	if e = db.QueryRow(ctx, `SELECT reset_anchor_at,next_reset_at FROM proxy_users WHERE id=$1`, uid).Scan(&anchor, &next); e != nil || !next.Equal(NextMonthlyReset(anchor, anchor)) {
		t.Fatal("initial rolling cycle", e)
	}
	sql(`UPDATE proxy_users SET used_bytes=700,lifetime_bytes=1700,upload_bytes=300,download_bytes=400,next_reset_at=now()-interval '1 second' WHERE id=$1`, uid)
	if e = s.Reap(ctx); e != nil {
		t.Fatal(e)
	}
	var used, lifetime int64
	if e = db.QueryRow(ctx, `SELECT used_bytes,lifetime_bytes,next_reset_at FROM proxy_users WHERE id=$1`, uid).Scan(&used, &lifetime, &next); e != nil || used != 0 || lifetime != 1700 || !next.After(time.Now()) {
		t.Fatal("automatic reset", used, lifetime, e)
	}
	editBody := map[string]any{"name": "bound user", "profile_id": pid, "expires_at": time.Now().Add(time.Hour), "user_id": uid, "version": 1}
	call("PUT", "subscriptions/"+subid, editBody, 200)
	call("PUT", "subscriptions/"+subid, editBody, 409)
	sql(`UPDATE proxy_users SET used_bytes=700,upload_bytes=300,download_bytes=400 WHERE id=$1`, uid)
	usage := object(call("GET", "subscriptions/"+subid+"/usage", nil, 200))["usage"].(map[string]any)
	if usage["remaining_bytes"] != float64(999300) || usage["mode"] != "user" {
		t.Fatal("bound quota", usage)
	}
	sql(`UPDATE proxy_users SET used_bytes=1000001,download_bytes=999701 WHERE id=$1`, uid)
	check = object(call("GET", "subscriptions/"+subid+"/check?client=mihomo", nil, 200))
	if check["ready"] != false || check["stage"] != "quota" {
		t.Fatal("exhausted user ready", check)
	}
	w = download("?format=mihomo", "")
	if !strings.Contains(w.Header().Get("Subscription-Userinfo"), "total=1000000;") || !strings.Contains(w.Body.String(), "MATCH,REJECT") {
		t.Fatal("quota refresh blocked metadata")
	}
	call("POST", "proxy-users/"+uid+"/reset-traffic", map[string]any{"restart_cycle": true}, 200)
	if object(call("GET", "subscriptions/"+subid+"/link", nil, 200))["url"] != sub["url"] {
		t.Fatal("editing rotated subscription token")
	}
}

func TestClientTemplateSafety(t *testing.T) {
	for _, c := range []struct {
		format, raw string
		valid       bool
	}{
		{"mihomo", `{"ipv6":true,"rules":["MATCH,PROXY"]}`, true},
		{"mihomo", `{"external-controller":"0.0.0.0:9090"}`, false},
		{"sing-box", `{"inbounds":[{"type":"mixed","listen":"0.0.0.0","listen_port":1080}]}`, false},
		{"sing-box", `{"experimental":{"clash_api":{"external_controller":"0.0.0.0:9090"}}}`, false},
		{"sing-box", `{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9090"}}}`, true},
		{"xray", `{"outbounds":[]}`, false},
	} {
		if e := validateClientOptions(c.format, json.RawMessage(c.raw)); (e == nil) != c.valid {
			t.Fatal(c, e)
		}
	}
	if _, e := parseClientText("a: ["); e == nil {
		t.Fatal("invalid YAML accepted")
	}
}
