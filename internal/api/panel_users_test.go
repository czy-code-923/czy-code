package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExternalPanelUserCRUDAndIdempotency(t *testing.T) {
	s := testControlDB(t)
	handler := s.Handler()
	call := func(method, path string, body any, token, key string, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	object := func(raw []byte) map[string]any {
		var out map[string]any
		if e := json.Unmarshal(raw, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	key := object(call("POST", "api-keys", map[string]any{"name": "panel", "scopes": []string{"users:read", "users:write"}, "expires_at": time.Now().Add(time.Hour), "rate_limit": 120}, s.AdminToken, "", 201))["token"].(string)
	body := map[string]any{"name": "panel-user", "enabled": true, "expires_at": time.Now().AddDate(0, 1, 0), "quota_bytes": 0, "external_id": "airport-123"}
	created := object(call("POST", "proxy-users", body, key, "order-001", 200))
	id := created["id"].(string)
	replay := object(call("POST", "proxy-users", body, key, "order-001", 200))
	if replay["id"] != id || replay["replayed"] != true {
		t.Fatal(replay)
	}
	body["name"] = "changed"
	call("POST", "proxy-users", body, key, "order-001", 409)
	body["name"] = "panel-user"
	current := object(call("GET", "proxy-users/"+id, nil, key, "", 200))
	if current["external_id"] != "airport-123" || current["version"] != float64(1) || strings.Contains(string(call("GET", "proxy-users/"+id, nil, key, "", 200)), "sealed") {
		t.Fatal(current)
	}
	body["version"] = 1
	body["enabled"] = false
	call("PUT", "proxy-users/"+id, body, key, "", 200)
	call("PUT", "proxy-users/"+id, body, key, "", 409)
	body2 := map[string]any{"name": "second", "enabled": true, "expires_at": time.Now().Add(time.Hour)}
	call("POST", "proxy-users", body2, key, "", 200)
	page := object(call("GET", "proxy-users/page?limit=1", nil, key, "", 200))
	if len(page["items"].([]any)) != 1 || page["next_cursor"] == "" {
		t.Fatal(page)
	}
	next := object(call("GET", "proxy-users/page?limit=1&after="+page["next_cursor"].(string), nil, key, "", 200))
	if len(next["items"].([]any)) != 1 || next["next_cursor"] != "" {
		t.Fatal(next)
	}
	call("POST", "hosts", map[string]any{"name": "forbidden"}, key, "", 403)
	call("POST", "hosts/"+ID()+"/engine-updates", map[string]any{"engine": "sing-box", "version": "latest"}, key, "", 403)
	call("DELETE", "proxy-users/"+id, nil, key, "", 200)
	call("GET", "proxy-users/"+id, nil, key, "", 404)
	delete(body, "version")
	body["enabled"] = true
	call("POST", "proxy-users", body, key, "order-001", 410)
}
