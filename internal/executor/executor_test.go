package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vps-control/internal/manifest"
)

type validationFunc func(context.Context, string, []manifest.Artifact) error

func (f validationFunc) Check(c context.Context, d string, a []manifest.Artifact) error {
	return f(c, d, a)
}
func sampleTask() Task {
	s := manifest.Spec{Schema: 1, Mode: "simulation", Engine: "sing-box", Listen: "127.0.0.1", Port: 2080, Outbound: "block"}
	raw, _ := json.Marshal(s)
	raw, _ = manifest.Canonical(raw)
	return Task{ID: strings.Repeat("a", 32), InstanceID: strings.Repeat("b", 32), ConfigID: strings.Repeat("c", 32), Lease: strings.Repeat("d", 32), Mode: "staging", Spec: raw, Checksum: manifest.Digest(raw)}
}
func TestStagingKeepsGoodRevision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	reject := false
	checks := 0
	v := validationFunc(func(_ context.Context, dir string, files []manifest.Artifact) error {
		checks++
		if reject {
			return errors.New("validator failure")
		}
		if _, e := os.Stat(filepath.Join(dir, files[0].Name)); e != nil {
			t.Fatal(e)
		}
		return nil
	})
	s, e := Open(dir, strings.Repeat("s", 32), v)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = Open(dir, strings.Repeat("s", 32), v); e == nil {
		t.Fatal("second writer allowed")
	}
	task := sampleTask()
	authorize := func(context.Context) error { return nil }
	sum, e := s.Stage(ctx, task, authorize)
	if e != nil || len(sum) != 64 {
		t.Fatal(sum, e)
	}
	pointer := filepath.Join(dir, "instances", task.InstanceID, "current.json")
	before, _ := os.ReadFile(pointer)
	if sum2, e := s.Stage(ctx, task, authorize); e != nil || sum2 != sum {
		t.Fatal("repeat not idempotent", e)
	}
	reject = true
	task.ID = strings.Repeat("e", 32)
	task.ConfigID = strings.Repeat("f", 32)
	if _, e = s.Stage(ctx, task, authorize); e == nil {
		t.Fatal("failed validation published")
	}
	after, _ := os.ReadFile(pointer)
	if string(before) != string(after) {
		t.Fatal("old pointer changed after validation failure")
	}
	reject = false
	if _, e = s.Stage(ctx, task, func(context.Context) error { return errors.New("stale lease") }); e == nil {
		t.Fatal("stale lease published")
	}
	after, _ = os.ReadFile(pointer)
	if string(before) != string(after) {
		t.Fatal("stale lease replaced pointer")
	}
	task.InstanceID = "../../outside"
	priorChecks := checks
	if _, e = s.Stage(ctx, task, authorize); e == nil || checks != priorChecks {
		t.Fatal("path traversal reached validator")
	}
	// A privileged/local actor can corrupt its private state: fail closed rather than overwrite it.
	task = sampleTask()
	target := filepath.Join(dir, "instances", task.InstanceID, "revisions", task.ID+"-"+task.Lease, "sing-box.json")
	os.WriteFile(target, []byte("corrupted"), 0600)
	if _, e = s.Stage(ctx, task, authorize); e == nil {
		t.Fatal("corrupted revision reused")
	}
}
func TestNativeBinaryPins(t *testing.T) {
	f := filepath.Join(t.TempDir(), "untrusted.exe")
	os.WriteFile(f, []byte("never execute"), 0700)
	v := NativeValidator{Binaries: map[string]Binary{"sing-box": {Path: f, SHA256: strings.Repeat("0", 64)}}}
	if e := v.Check(context.Background(), t.TempDir(), []manifest.Artifact{{Name: "sing-box.json", Engine: "sing-box"}}); e == nil {
		t.Fatal("bad binary accepted")
	}
}
func TestNativeEngineIntegration(t *testing.T) {
	if os.Getenv("TEST_SING_BOX_BINARY") == "" {
		t.Skip("set TEST_SING_BOX_BINARY/TEST_FRPS_BINARY/TEST_FRPC_BINARY for official engine validation")
	}
	binaries := map[string]Binary{}
	for engine, prefix := range map[string]string{"sing-box": "SING_BOX", "frps": "FRPS", "frpc": "FRPC"} {
		path := os.Getenv("TEST_" + prefix + "_BINARY")
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		binaries[engine] = Binary{Path: path, SHA256: manifest.Digest(b)}
	}
	s, e := Open(t.TempDir(), strings.Repeat("s", 32), NativeValidator{Binaries: binaries})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task := sampleTask()
	authorize := func(context.Context) error { return nil }
	if _, e = s.Stage(context.Background(), task, authorize); e != nil {
		t.Fatal("sing-box block check", e)
	}
	spec, _ := manifest.Parse(task.Spec)
	spec.Outbound = "direct"
	task.ID = strings.Repeat("e", 32)
	task.ConfigID = strings.Repeat("f", 32)
	task.Spec, _ = json.Marshal(spec)
	task.Spec, _ = manifest.Canonical(task.Spec)
	task.Checksum = manifest.Digest(task.Spec)
	if _, e = s.Stage(context.Background(), task, authorize); e != nil {
		t.Fatal("sing-box direct check", e)
	}
	// Re-publish the original known-good revision as a rollback.
	if _, e = s.Stage(context.Background(), sampleTask(), authorize); e != nil {
		t.Fatal("rollback publish", e)
	}
	task = sampleTask()
	task.ID = strings.Repeat("1", 32)
	task.InstanceID = strings.Repeat("2", 32)
	task.ConfigID = strings.Repeat("3", 32)
	spec.Engine = "frp"
	spec.Port = 7000
	spec.Tunnels = []manifest.Tunnel{{LocalHost: "127.0.0.1", LocalPort: 8080, RemotePort: 18080}}
	task.Spec, _ = json.Marshal(spec)
	task.Spec, _ = manifest.Canonical(task.Spec)
	task.Checksum = manifest.Digest(task.Spec)
	if _, e = s.Stage(context.Background(), task, authorize); e != nil {
		t.Fatal("FRP pair verify", e)
	}
}
