package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestFirewallEditDeleteAndRestore(t *testing.T) {
	s := testControlDB(t)
	handler := s.Handler()
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	h := call("POST", "hosts", map[string]any{"name": "Firewall test"}, 201)
	policy := map[string]any{"name": "SSH guard", "ssh_port": 22, "admin_cidrs": []string{"192.0.2.1/32"}, "default_action": "drop", "rules": []any{}}
	old := call("POST", "hosts/"+h["id"].(string)+"/firewalls", policy, 201)
	updated := call("PUT", "firewalls/"+old["id"].(string), map[string]any{"version": old["version"], "policy": policy}, 201)
	if updated["id"] == old["id"] || updated["applied"] != false {
		t.Fatal("edit overwrote immutable policy or claimed remote apply")
	}
	call("PUT", "firewalls/"+old["id"].(string), map[string]any{"version": old["version"], "policy": policy}, 409)
	removed := call("DELETE", "firewalls/"+updated["id"].(string), nil, 200)
	if removed["remote_changed"] != false {
		t.Fatal("database deletion claimed to change kernel")
	}
	call("GET", "firewalls/"+updated["id"].(string)+"/preview", nil, 404)
	call("POST", "firewalls/"+updated["id"].(string)+"/archive", map[string]bool{"archived": false}, 200)
	call("GET", "firewalls/"+updated["id"].(string)+"/preview", nil, 200)
}
