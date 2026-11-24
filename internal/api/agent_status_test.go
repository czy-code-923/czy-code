package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"vps-control/internal/hostmeter"
)

func TestAgentRecoveryStatusAndClockSkew(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	now := time.Now().UTC()
	report := hostmeter.Report{Epoch: strings.Repeat("a", 32), Started: now.Add(-time.Minute), At: now.Add(62 * time.Second), State: "collecting", Interfaces: []hostmeter.Interface{}}
	response := f.h.call("POST", "agent/host-traffic", report, 400, f.host)
	if !strings.Contains(string(response), "clock ahead") || !strings.Contains(string(response), "NTP") {
		t.Fatal("clock fault must be actionable", string(response))
	}
	before := obj018(f.h.call("POST", "agent/status", nil, 200, f.host))
	if before["traffic_fresh"] != false {
		t.Fatal("recovery falsely reported healthy", before)
	}
	report.At = now
	f.h.call("POST", "agent/host-traffic", report, 200, f.host)
	f.h.call("POST", "agent/heartbeat", map[string]any{}, 200, f.host)
	status := obj018(f.h.call("POST", "agent/status", nil, 200, f.host))
	if status["online"] != true || status["traffic_fresh"] != true || status["server_time"] == nil {
		t.Fatal("recovered report was not reflected", status)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "agent_token") || strings.Contains(string(raw), "sealed") {
		t.Fatal("status leaked private configuration")
	}
	f.h.call("POST", "agent/status", nil, 401, nil)
}
