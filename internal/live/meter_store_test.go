package live

import (
	"context"
	"testing"
	"vps-control/internal/executor"
)

func TestTrafficLedgerRestartAndBaseline(t *testing.T) {
	dir := t.TempDir()
	r, e := NewRunner(dir, map[string]executor.Binary{})
	if e != nil {
		t.Fatal(e)
	}
	id := "11111111111111111111111111111111"
	r.usage.add("sing-box", "user", id, 400, 600)
	before, e := r.TelemetrySnapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	r, e = NewRunner(dir, map[string]executor.Binary{})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	after, e := r.TelemetrySnapshot(context.Background())
	if e != nil || len(after.Counters) != 1 || after.Counters[0] != before.Counters[0] {
		t.Fatal("restart changed durable absolute counters", e)
	}
	if e = r.SeedCounters([]TrafficCounter{{ID: after.Counters[0].ID, Kind: "user", Subject: id, Up: 800, Down: 900}}); e != nil {
		t.Fatal(e)
	}
	// An older controller acknowledgement must not roll the local ledger back.
	if e = r.SeedCounters(before.Counters); e != nil {
		t.Fatal(e)
	}
	r.usage.add("sing-box", "user", id, 20, 30)
	after, e = r.TelemetrySnapshot(context.Background())
	if e != nil || after.Counters[0].Up != 820 || after.Counters[0].Down != 930 {
		t.Fatal("baseline recovery lost new traffic", after, e)
	}
}
