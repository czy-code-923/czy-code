package live

import (
	"context"
	"fmt"
	"os"
	"testing"
	"vps-control/internal/executor"
)

func TestTrafficLedgerRestartAndBaseline(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
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

func TestAccessBatchIsBoundedAndAcknowledgementPreservesNewerBytes(t *testing.T) {
	r, e := NewRunner(t.TempDir(), map[string]executor.Binary{})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	for i := 0; i < 150; i++ {
		r.usage.enqueue(AccessObservation{ID: fmt.Sprintf("%032x", i), Service: "11111111111111111111111111111111", Source: "192.0.2.1", Destination: "example.test", Port: 443, Network: "tcp", BytesKnown: true, Up: int64(i)})
	}
	first, e := r.TelemetrySnapshot(context.Background())
	if e != nil || len(first.Access) != 100 {
		t.Fatal("wire batch exceeded 100", len(first.Access), e)
	}
	changed := first.Access[0]
	changed.Up++
	r.usage.enqueue(changed)
	if e = r.AckTelemetry(first, nil); e != nil {
		t.Fatal(e)
	}
	if _, ok := r.usage.Access[changed.ID]; !ok {
		t.Fatal("new bytes were lost by an old acknowledgement")
	}
	second, e := r.TelemetrySnapshot(context.Background())
	if e != nil || len(second.Access) != 51 {
		t.Fatal("queued records cannot be drained", len(second.Access), e)
	}
}
