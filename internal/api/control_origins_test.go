package api

import (
	"encoding/json"
	"strings"
	"testing"
	"vps-control/internal/controlorigin"
)

func TestControlOriginsAreIndependentAndOldHeartbeatIsCompatible(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.h.call("PUT", "site", map[string]any{"domain": "panel.example.com", "https": true, "agent_origins": []string{"https://agent.example.com"}}, 200, nil)
	raw := f.h.call("POST", "agent/heartbeat", map[string]any{"version": "0.23.10", "control_origin": "https://old.example.com"}, 200, f.host)
	var response struct {
		Origins []string `json:"control_origins"`
	}
	if e := json.Unmarshal(raw, &response); e != nil || len(response.Origins) != 1 || response.Origins[0] != "https://agent.example.com" {
		t.Fatal(string(raw), e)
	}
	f.h.call("POST", "agent/heartbeat", map[string]any{}, 200, f.host)
	nonce := strings.Repeat("b", 64)
	var proof struct {
		Proof string `json:"proof"`
	}
	json.Unmarshal(f.h.call("POST", "agent/verify-origin", map[string]string{"host_id": f.host["id"].(string), "nonce": nonce, "origin": "https://agent.example.com"}, 200, nil), &proof)
	if proof.Proof != controlorigin.Proof(Hash(f.host["agent_token"].(string)), nonce, "https://agent.example.com") {
		t.Fatal("identity proof invalid")
	}
	f.h.call("POST", "agent/verify-origin", map[string]string{"host_id": f.host["id"].(string), "nonce": "bad"}, 400, nil)
	f.h.call("POST", "agent/verify-origin", map[string]string{"host_id": f.host["id"].(string), "nonce": nonce, "origin": "https://unconfigured.example"}, 403, nil)
	f.h.call("PUT", "site", map[string]any{"domain": "panel.example.com", "https": true, "agent_origins": []string{"http://unsafe.example"}}, 400, nil)
	var command map[string]string
	json.Unmarshal(f.h.call("GET", "hosts/"+f.host["id"].(string)+"/repair-command", nil, 200, nil), &command)
	if !strings.Contains(command["command"], "sha256sum -c -") || strings.Contains(command["command"], f.host["agent_token"].(string)) {
		t.Fatal("repair command not pinned or leaked credential")
	}
}
