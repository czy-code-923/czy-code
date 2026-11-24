package live

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHopConflictDoesNotBlockKernelPrimaryPorts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux optional redirect")
	}
	foreign, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	port := foreign.LocalAddr().(*net.UDPAddr).Port
	free, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	primary := free.LocalAddr().(*net.UDPAddr).Port
	free.Close()
	id := strings.Repeat("a", 32)
	w := Work{ID: id, Enabled: true, Version: 1, Spec: Spec{Kind: "hysteria2", Listen: "127.0.0.1", Port: primary, ProtocolOptions: ProtocolOptions{HopPorts: fmt.Sprintf("%d,%d", port, primary)}}}
	if err := preflightAdditional(nil, []Work{w}); err != nil {
		t.Fatal("optional hop port blocked the primary listener", err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	r, err := NewRunner(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: &exec.Cmd{}, state: "running", started: time.Now().Add(-3 * time.Second), members: []Work{w, {ID: strings.Repeat("b", 32), Enabled: true, Version: 1}}, hops: workHops(w)}
	r.processes["kernel"] = p
	defer func() { p.cmd = nil; r.Close() }()
	r.syncHopping()
	if !r.hopFailures[id] {
		t.Fatal("hop conflict was not recorded")
	}
	for _, v := range r.Reports() {
		if v.ID == id && v.State != "pending" {
			t.Fatal("hop failure was hidden", v)
		}
		if v.ID != id && v.State != "running" {
			t.Fatal("unrelated inbound interrupted", v)
		}
	}
	var request struct {
		Rules []hopRule `json:"rules"`
	}
	raw, _ := r.root.ReadFile("port-hopping.json")
	json.Unmarshal(raw, &request)
	if len(request.Rules) != 0 {
		t.Fatal("foreign port was redirected")
	}
}

func TestHoppingLeaseTracksOnlyAppliedProcessesAndWaitsForCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux helper lease")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	r, e := NewRunner(dir, nil)
	if e != nil {
		t.Fatal(e)
	}
	p := &process{cmd: &exec.Cmd{}, hops: []hopRule{{strings.Repeat("a", 32), "::", 24000, "30000-30002"}}}
	r.processes["kernel"] = p
	defer func() { p.cmd = nil; r.Close() }()
	r.syncHopping()
	if r.hoppingReady() {
		t.Fatal("unacknowledged mapping reported ready")
	}
	var request struct {
		Rules    []hopRule `json:"rules"`
		Revision string    `json:"revision"`
	}
	raw, _ := r.root.ReadFile("port-hopping.json")
	json.Unmarshal(raw, &request)
	if len(request.Rules) != 1 || request.Rules[0].Port != 24000 {
		t.Fatal(string(raw))
	}
	ack := func(revision string, updated int64) {
		raw, _ := json.Marshal(map[string]any{"revision": revision, "updated_at": updated, "ready": true})
		if e := r.root.WriteFile("port-hopping.status", raw, 0644); e != nil {
			t.Fatal(e)
		}
	}
	ack(request.Revision, time.Now().Unix()-60)
	if r.hoppingReady() {
		t.Fatal("stale helper status accepted")
	}
	ack(request.Revision, time.Now().Unix())
	if !r.hoppingReady() {
		t.Fatal("current mapping not acknowledged")
	}
	p.cmd = nil
	r.syncHopping()
	if r.hoppingReady() {
		t.Fatal("removed mapping used old acknowledgement")
	}
	raw, _ = r.root.ReadFile("port-hopping.json")
	json.Unmarshal(raw, &request)
	if len(request.Rules) != 0 {
		t.Fatal("stopped process still has redirect")
	}
	ack(request.Revision, time.Now().Unix())
	r.syncHopping()
	if _, e = r.root.Stat("port-hopping.json"); !os.IsNotExist(e) {
		t.Fatal("idle request retained", e)
	}
}
