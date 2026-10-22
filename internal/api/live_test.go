package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"vps-control/internal/store"
)

func TestLiveIntegration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dbURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "live_" + ID()
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
	s := &Server{DB: db, AdminToken: strings.Repeat("a", 32), MasterKey: bytes.Repeat([]byte{1}, 32)}
	admin := s.Handler()
	public := s.ShareHandler()
	call := func(method, path string, body any, status int, agent map[string]any) map[string]any {
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
			t.Fatalf("%s %s %d want %d %s", method, path, w.Code, status, w.Body.String())
		}
		var b map[string]any
		json.Unmarshal(w.Body.Bytes(), &b)
		return b
	}
	share := func(link string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, link, nil)
		w := httptest.NewRecorder()
		public.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("share got %d want %d %s", w.Code, status, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("caching enabled")
		}
	}
	h := call("POST", "hosts", map[string]any{"name": "NAT device", "provider": "local", "address": "inventory.invalid"}, 201, nil)
	other := call("POST", "hosts", map[string]any{"name": "public", "provider": "vps", "address": "inventory.invalid"}, 201, nil)
	call("PUT", "site", map[string]any{"domain": "vps.example.com/path", "https": true}, 400, nil)
	call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true}, 200, nil)
	create := func(kind string, port int, extra map[string]any) map[string]any {
		b := map[string]any{"name": "test", "host_id": h["id"], "kind": kind, "address": "2001:db8::1", "port": port, "listen": "::1"}
		for k, v := range extra {
			b[k] = v
		}
		return call("POST", "live", b, 201, nil)
	}
	a := create("shadowsocks", 22000, nil)
	upstream := create("shadowsocks", 22001, map[string]any{"next_id": a["id"]})
	call("POST", "live", map[string]any{"name": "collision", "host_id": h["id"], "kind": "shadowsocks", "address": "::1", "port": 22000, "listen": "::1"}, 409, nil)
	node := call("POST", "nodes", map[string]any{"name": "node", "engine": "sing-box"}, 201, nil)
	call("POST", "instances", map[string]any{"name": "collision", "host_id": h["id"], "node_id": node["id"], "listen_port": 22000}, 409, nil)
	server := create("frps", 22002, map[string]any{"tunnels": []map[string]any{{"name": "demo", "local_host": "::1", "local_port": 3000, "remote_port": 23000}}})
	client := call("POST", "live", map[string]any{"name": "client", "host_id": other["id"], "kind": "frpc", "parent_id": server["id"]}, 201, nil)
	_ = client
	call("POST", "subscriptions", map[string]any{"name": "bad", "days": 30, "service_ids": []any{server["id"]}}, 400, nil)
	sub := call("POST", "subscriptions", map[string]any{"name": "personal", "days": 30, "service_ids": []any{a["id"], upstream["id"]}}, 201, nil)
	link := sub["url"].(string)
	share(link, 503)
	call("POST", "live/"+a["id"].(string)+"/toggle", map[string]any{"enabled": true}, 200, nil)
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": a["id"], "version": 2, "state": "running"}}}, 200, other)
	share(link, 503)
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": a["id"], "version": 1, "state": "running"}}}, 200, h)
	share(link, 503)
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": a["id"], "version": 2, "state": "running"}}}, 200, h)
	share(link, 200)
	share(link+"?format=clash", 200)
	share(link+"?format=base64", 200)
	share(strings.Replace(link, "vps.example.com", "evil.example.com", 1), 404)
	rotated := call("POST", "subscriptions/"+sub["id"].(string)+"/rotate", map[string]any{}, 200, nil)
	share(link, 404)
	link = rotated["url"].(string)
	share(link, 200)
	if _, e = db.Exec(ctx, "UPDATE live_services SET reported_at=now()-interval '2 minutes' WHERE id=$1", a["id"]); e != nil {
		t.Fatal(e)
	}
	share(link, 503)
	call("POST", "agent/live/sync", map[string]any{"reports": []map[string]any{{"id": a["id"], "version": 2, "state": "running"}}}, 200, h)
	call("POST", "live/"+a["id"].(string)+"/toggle", map[string]any{"enabled": false}, 200, nil)
	share(link, 503)
	call("POST", "subscriptions/"+sub["id"].(string)+"/revoke", map[string]any{}, 200, nil)
	share(link, 404)
	r := httptest.NewRequest("GET", "/api/v1/hosts", nil)
	w := httptest.NewRecorder()
	public.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("management accessible on sharing listener")
	}
	var sealed []byte
	if e = db.QueryRow(ctx, "SELECT sealed FROM live_services WHERE id=$1", a["id"]).Scan(&sealed); e != nil || bytes.Contains(sealed, []byte("password")) {
		t.Fatal("plaintext persisted")
	}
}
