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
	"vps-control/internal/store"
)

func TestPoliciesNetworksAndRetirement(t *testing.T) {
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
	schema := "policy_" + ID()
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

	ipv6host := object(call("POST", "hosts", map[string]any{"name": "IPv6 host"}, 201, nil))
	call("POST", "agent/heartbeat", map[string]any{"version": "0.12.0", "addresses": []string{"172.17.0.1", "2001:db8::9"}}, 200, ipv6host)
	var primary string
	if e = db.QueryRow(ctx, `SELECT address FROM hosts WHERE id=$1`, ipv6host["id"]).Scan(&primary); e != nil || primary != "2001:db8::9" {
		t.Fatal("IPv6-only host preferred Docker IPv4", primary, e)
	}
	h := object(call("POST", "hosts", map[string]any{"name": "fixture", "address": "192.0.2.8"}, 201, nil))
	hid := h["id"].(string)
	call("POST", "agent/heartbeat", map[string]any{"version": "0.12.0", "addresses": []string{"192.0.2.8", "2001:db8::8"}, "cleanup_capable": true}, 200, h)
	call("PUT", "site", map[string]any{"domain": "", "https": false, "ip_origin": "http://192.0.2.8:18080", "share_domain": "sub.example.com", "share_https": true}, 200, nil)
	manager, e := s.managementOrigin(httptest.NewRequest("GET", "/", nil))
	if e != nil || manager != "http://192.0.2.8:18080" {
		t.Fatal(manager, e)
	}
	origin, e := s.publicOrigin(httptest.NewRequest("GET", "/", nil))
	if e != nil || origin != "https://sub.example.com" {
		t.Fatal(origin, e)
	}
	ids := []string{}
	for i, kind := range []string{"shadowsocks", "snell", "vmess"} {
		b := map[string]any{"name": kind, "host_id": hid, "kind": kind, "engine": "sing-box", "listen": "::", "port": 24000 + i, "address": "", "managed_users": kind == "vmess", "endpoint_addresses": []string{"192.0.2.8", "2001:db8::8"}, "egress_families": []string{"ipv6", "ipv4"}}
		if kind == "vmess" {
			b["tls_mode"] = "none"
		}
		row := object(call("POST", "live", b, 201, nil))
		ids = append(ids, row["id"].(string))
	}
	badInput := map[string]any{"name": "bad", "host_id": hid, "kind": "shadowsocks", "address": "", "listen": "::", "port": 24999, "endpoint_addresses": []string{"2001:db8::999"}}
	call("POST", "live", badInput, 400, nil)
	user := object(call("POST", "proxy-users", map[string]any{"name": "policy-user", "enabled": true, "expires_at": time.Now().Add(time.Hour), "quota_bytes": 1000000, "reset_strategy": "rolling_monthly", "reset_day": 5}, 200, nil))
	uid := user["id"].(string)
	call("POST", "user-groups", map[string]any{"name": "mixed protocols", "user_ids": []string{uid}, "service_ids": ids}, 200, nil)
	sub := object(call("POST", "subscriptions", map[string]any{"name": "mixed", "user_id": uid, "days": 30}, 201, nil))
	if !strings.HasPrefix(sub["url"].(string), "https://sub.example.com/") {
		t.Fatal("subscription origin ignored")
	}
	// Fixtures simulate acknowledged running inbounds; real bundle acknowledgement is exercised by the host/kernel test.
	db.Exec(ctx, `UPDATE live_services SET enabled=true,state='running',reported_version=version,reported_at=now() WHERE tenant_id=$1 AND host_id=$2`, tenant, hid)
	source := "proxy-groups:\n  - name: 流媒体\n    type: select\n    proxies: [ALL, DIRECT]\nrules:\n  - DOMAIN-SUFFIX,netflix.com,流媒体\n  - MATCH,流媒体\n"
	policy := object(call("POST", "client-policies", map[string]any{"name": "default", "source": source, "client_id": "all"}, 200, nil))
	badSource := strings.ReplaceAll(source, "[ALL, DIRECT]", "[unauthorized name]")
	call("POST", "client-policies", map[string]any{"name": "bad", "source": badSource, "client_id": "mihomo"}, 400, nil)
	request := func(query string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", sub["url"].(string)+query, nil)
		w := httptest.NewRecorder()
		public.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("share %s %d %s", query, w.Code, w.Body.String())
		}
		return w
	}
	mh := request("?client=mihomo", 200)
	var config map[string]any
	yaml.Unmarshal(mh.Body.Bytes(), &config)
	if len(config["proxies"].([]any)) != 6 || !strings.Contains(mh.Body.String(), "netflix.com") {
		t.Fatal("mixed nodes or rules missing")
	}
	xr := request("?client=v2rayng", 200)
	if xr.Header().Get("X-Starhub-Excluded-Nodes") != "2" {
		t.Fatal("unsupported Snell not filtered", xr.Header())
	}
	if !strings.Contains(xr.Body.String(), "routing") {
		t.Fatal("v2rayNG custom JSON needs routing")
	}
	request("?client=hiddify", 200)
	request("?client=shadowrocket", 422)
	request("?client=shadowrocket&policy=nodes-only", 200)
	// An explicit user/client policy overrides global routing and is editable with optimistic locking.
	own := object(call("POST", "client-policies", map[string]any{"name": "own", "user_id": uid, "client_id": "mihomo", "source": "rules: [\"DOMAIN-SUFFIX,example.com,DIRECT\",\"MATCH,REJECT\"]"}, 200, nil))
	if !strings.Contains(request("?client=mihomo", 200).Body.String(), "example.com") {
		t.Fatal("user/client precedence ignored")
	}
	call("PUT", "client-policies/"+own["id"].(string), map[string]any{"name": "own2", "user_id": uid, "client_id": "mihomo", "source": source, "version": 1}, 200, nil)
	call("PUT", "client-policies/"+own["id"].(string), map[string]any{"name": "stale", "source": source, "version": 1}, 409, nil)
	call("DELETE", "client-policies/"+own["id"].(string), nil, 200, nil)
	call("DELETE", "client-policies/"+policy["id"].(string), nil, 200, nil)
	// A host uninstall is pending until authenticated remote acknowledgement; data is retained until then.
	retire := object(call("POST", "hosts/"+hid+"/retirement", map[string]any{}, 202, nil))
	receipt := map[string]any{"retire_id": retire["retire_id"]}
	request("?client=mihomo", 503)
	call("POST", "agent/retirement/ack", map[string]any{"retire_id": ID()}, 409, h)
	call("POST", "agent/retirement/begin", receipt, 200, h)
	call("POST", "agent/retirement/ack", receipt, 200, h)
	call("POST", "agent/retirement/ack", receipt, 200, h)
	var count int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM live_services WHERE host_id=$1`, hid).Scan(&count); e != nil || count != 0 {
		t.Fatal("host config not purged", e)
	}
	call("POST", "agent/heartbeat", map[string]any{}, 401, h)
	call("GET", "proxy-users", nil, 200, nil) // control plane remains usable
}
