package live

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

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
