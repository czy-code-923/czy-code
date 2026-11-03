package api

import (
	"strings"
	"testing"
	"time"
	"vps-control/internal/hostmeter"
)

func TestHostDisplayUnitPersistsWithoutRebilling(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	initial := int64(25_000_000_000)
	in := hostTrafficInput{Quota: 500_000_000_000, Mode: "both", ResetDay: 5, AutoReset: true, InitialUsed: &initial, DisplayUnit: "GB"}
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "decimal provider", "traffic": in}, 201, nil))
	id := host["id"].(string)
	path := "hosts/" + id + "/traffic"
	now := time.Now().UTC()
	h.call("POST", "agent/host-traffic", hostmeter.Report{Epoch: strings.Repeat("a", 32), Started: now.Add(-time.Minute), At: now, State: "collecting", Interfaces: []hostmeter.Interface{{Name: "eth0", RX: 10_000_000_000, TX: 20_000_000_000, Uplink: true}}}, 200, host)
	state := obj018(h.call("GET", path, nil, 200, nil))
	check := func(unit string) {
		t.Helper()
		state = obj018(h.call("GET", path, nil, 200, nil))
		if state["display_unit"] != unit || state["quota_bytes"] != float64(500_000_000_000) || state["used_bytes"] != float64(55_000_000_000) || state["remaining_bytes"] != float64(445_000_000_000) {
			t.Fatalf("display preference changed the byte ledger: %v", state)
		}
	}
	check("GB")
	in.InitialUsed = nil
	version := int(state["version"].(float64))
	in.Version = &version
	in.DisplayUnit = "GiB"
	h.call("PUT", path, in, 200, nil)
	check("GiB")
	h.call("PUT", path, in, 409, nil)
	version = int(state["version"].(float64))
	in.DisplayUnit = "MiB"
	h.call("PUT", path, in, 400, nil)
	check("GiB")
	// Old API consumers omit the new preference and must not reset it.
	in.DisplayUnit = "GB"
	h.call("PUT", path, in, 200, nil)
	check("GB")
	version = int(state["version"].(float64))
	in.DisplayUnit = ""
	h.call("PUT", path, in, 200, nil)
	check("GB")
	legacy := obj018(h.call("POST", "hosts", map[string]any{"name": "legacy consumer"}, 201, nil))
	if got := obj018(h.call("GET", "hosts/"+legacy["id"].(string)+"/traffic", nil, 200, nil))["display_unit"]; got != "GiB" {
		t.Fatal("legacy display default changed", got)
	}
}
