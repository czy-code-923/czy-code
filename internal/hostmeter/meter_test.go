package hostmeter

import (
	"testing"
	"time"
)

func TestDurableCountersAndRebootGap(t *testing.T) {
	boot := "one"
	rx, tx := int64(100), int64(200)
	read := func() (string, []Raw, error) { return boot, []Raw{{Name: "eth0", RX: rx, TX: tx, Uplink: true}}, nil }
	dir := t.TempDir()
	m, e := newMeter(dir, read)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	a, e := m.Snapshot(now)
	if e != nil || a.Interfaces[0].RX != 0 {
		t.Fatal(a, e)
	}
	rx = 130
	tx = 250
	a, e = m.Snapshot(now.Add(time.Second))
	if e != nil || a.Interfaces[0].RX != 30 || a.Interfaces[0].TX != 50 {
		t.Fatal(a, e)
	}
	m, e = newMeter(dir, read)
	if e != nil {
		t.Fatal(e)
	}
	a, e = m.Snapshot(now.Add(2 * time.Second))
	if e != nil || a.Interfaces[0].RX != 30 {
		t.Fatal("restart double counted", a, e)
	}
	boot = "two"
	rx = 10
	tx = 20
	a, e = m.Snapshot(now.Add(3 * time.Second))
	if e != nil || a.Interfaces[0].RX != 40 || a.Gaps != 1 {
		t.Fatal(a, e)
	}
}
func TestNativeInterfaceCounters(t *testing.T) {
	boot, rows, e := readNative()
	if e != nil {
		t.Fatal(e)
	}
	if boot == "" || len(rows) == 0 {
		t.Fatal("native network counters missing")
	}
	for _, v := range rows {
		if v.RX < 0 || v.TX < 0 {
			t.Fatal("invalid counter")
		}
	}
}
