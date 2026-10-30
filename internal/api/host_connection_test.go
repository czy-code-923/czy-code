package api

import (
	"encoding/json"
	"testing"
	"vps-control/internal/live"
)

func TestPauseHostKeepsHeartbeatAndTelemetryAndResumes(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	hostID := f.host["id"].(string)
	f.h.call("POST", "hosts/"+hostID+"/connection", map[string]any{"paused": true}, 200, nil)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.23.6"}, 200, f.host)
	f.h.call("POST", "agent/telemetry", live.TelemetryBatch{}, 200, f.host)
	f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 423, f.host)
	f.h.call("POST", "agent/claim", nil, 423, f.host)
	var hosts []map[string]any
	if err := json.Unmarshal(f.h.call("GET", "hosts", nil, 200, nil), &hosts); err != nil || len(hosts) != 1 || hosts[0]["connection_paused"] != true {
		t.Fatal("paused state missing", err)
	}
	f.h.call("POST", "hosts/"+hostID+"/connection", map[string]any{"paused": false}, 200, nil)
	f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, f.host)
}
