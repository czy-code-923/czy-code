package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShareExportAndQRCodePermissions(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(1)
	path := "subscriptions/" + f.subscription
	share := obj018(f.h.call("GET", path+"/share?client=nekobox", nil, 200, nil))
	if !strings.Contains(share["url"].(string), "?client=nekobox") || share["qr_payload"] != share["url"] {
		t.Fatal("share query was lost")
	}
	nodes := []sharedNode{}
	json.Unmarshal(f.h.call("GET", path+"/nodes", nil, 200, nil), &nodes)
	if len(nodes) != 1 || nodes[0].Name != "original" || !strings.HasPrefix(nodes[0].URI, "vmess://") {
		t.Fatal("missing individual node URI")
	}
	for _, query := range []string{"?client=nekobox", "?kind=node&node_id=" + nodes[0].ID} {
		raw := f.h.call("GET", path+"/qr"+query, nil, 200, nil)
		if _, e := png.Decode(bytes.NewReader(raw)); e != nil {
			t.Fatal(e)
		}
	}
	body := f.h.call("GET", path+"/content?client=nekobox", nil, 200, nil)
	if !strings.Contains(string(body), "proxies:") || strings.Contains(string(body), "proxy-groups:") {
		t.Fatal("wrong NekoBox representation")
	}
	if len(f.observations()) != 0 {
		t.Fatal("administrator API consumed device slots")
	}
	// Exported credentials and QR images require the sensitive links scope.
	for _, view := range []string{"share", "content", "nodes", "qr"} {
		r := httptest.NewRequest("GET", "/api/v1/"+path+"/"+view, nil)
		scopes := requiredScopes(r)
		if len(scopes) != 1 || scopes[0] != "subscriptions:links" {
			t.Fatal(view, scopes)
		}
	}
}

func TestGatewayEditDeleteConcurrencyAndIndependentUsage(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	cert := obj018(h.call("POST", "certificates", map[string]any{"domain": "app.example.com", "provider": "http-01", "email": "ops@example.com"}, 201, nil))["id"].(string)
	work := obj018(h.call("POST", "gateway/work", map[string]any{}, 200, nil))
	job := object(work["certificates"].([]any)[0])
	if job["version"] != float64(1) {
		t.Fatal("unversioned certificate claim")
	}
	body := map[string]any{"domain": "app.example.com", "provider": "http-01", "email": "other@example.com", "version": 1}
	h.call("PUT", "certificates/"+cert, body, 200, nil)
	h.call("PUT", "certificates/"+cert, body, 409, nil)
	h.call("POST", "gateway/report", map[string]any{"id": cert, "kind": "certificate", "state": "failed", "version": 1}, 404, nil)
	proxy := map[string]any{"domain": "app.example.com", "upstream": "https://www.example.com/api", "certificate_id": cert, "enabled": false, "options": map[string]any{"read_timeout": 50}}
	id := obj018(h.call("POST", "reverse-proxies", proxy, 201, nil))["id"].(string)
	h.call("DELETE", "certificates/"+cert, nil, 409, nil)
	for _, bad := range []string{"http://169.254.169.254/latest", "http://127.0.0.1:8080", "https://user:pass@example.com", "https://example.com/;include"} {
		clone := map[string]any{}
		for k, v := range proxy {
			clone[k] = v
		}
		clone["upstream"] = bad
		h.call("POST", "reverse-proxies", clone, 400, nil)
	}
	for i := 0; i < 2; i++ {
		h.call("POST", "gateway/report", map[string]any{"id": id, "kind": "reverse-proxy-usage", "upload_bytes": 123, "download_bytes": 456}, 200, nil)
	}
	state := obj018(h.call("GET", "reverse-proxies/"+id, nil, 200, nil))
	if state["engine"] != "nginx" || state["upload_bytes"] != float64(123) || state["download_bytes"] != float64(456) {
		t.Fatal(state)
	}
	var used int64
	s.DB.QueryRow(context.Background(), "SELECT COALESCE(sum(used_bytes),0) FROM proxy_users").Scan(&used)
	if used != 0 {
		t.Fatal("reverse proxy usage changed user quotas")
	}
	proxy["version"] = 1
	h.call("PUT", "reverse-proxies/"+id, proxy, 200, nil)
	h.call("PUT", "reverse-proxies/"+id, proxy, 409, nil)
	h.call("DELETE", "reverse-proxies/"+id, nil, 202, nil)
	h.call("POST", "gateway/report", map[string]any{"id": id, "kind": "reverse-proxy", "state": "disabled", "version": 3}, 200, nil)
	h.call("GET", "reverse-proxies/"+id, nil, 404, nil)
	h.call("DELETE", "certificates/"+cert, nil, 202, nil)
	h.call("POST", "gateway/report", map[string]any{"id": cert, "kind": "certificate-delete", "version": 3}, 200, nil)
}
