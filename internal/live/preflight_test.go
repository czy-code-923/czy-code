package live

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"
	"vps-control/internal/executor"
)

func TestAdditivePortConflictRetainsIdenticalRunningMembers(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("native core required")
	}
	digest, e := executor.FileDigest(binary)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	runner, e := NewRunner(dir, map[string]executor.Binary{"sing-box": {Path: binary, SHA256: digest}})
	if e != nil {
		t.Fatal(e)
	}
	defer runner.Close()
	free, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	old := Work{ID: strings.Repeat("a", 32), Version: 1, Enabled: true, Spec: Spec{Engine: "sing-box", Kind: "shadowsocks", Address: "127.0.0.1", Listen: "127.0.0.1", Port: port, Password: Secret()}}
	initial := BundleWork([]Work{old})
	if e = runner.Reconcile(context.Background(), initial); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 40; i++ {
		reports := runner.Reports()
		if len(reports) > 0 && reports[0].State == "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	proc := runner.processes[initial[0].ID]
	if proc.cmd == nil {
		t.Fatal("initial core did not start")
	}
	pid := proc.cmd.Process.Pid
	occupied, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer occupied.Close()
	added := old
	added.ID = strings.Repeat("b", 32)
	added.Spec.Port = occupied.Addr().(*net.TCPAddr).Port
	if e = runner.Reconcile(context.Background(), BundleWork([]Work{old, added})); e != nil {
		t.Fatal(e)
	}
	if proc.cmd == nil || proc.cmd.Process.Pid != pid {
		t.Fatal("port collision killed unrelated valid inbounds")
	}
	failed := false
	for _, r := range runner.Reports() {
		if r.ID == added.ID && r.State == "failed" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("failed addition was not reported")
	}
	if e = runner.Reconcile(context.Background(), initial); e != nil {
		t.Fatal(e)
	}
	if len(runner.Reports()) != 1 {
		t.Fatal("rejected members leaked after removal")
	}
}
func TestCannotRetainRevokedOrChangedIdentity(t *testing.T) {
	w := Work{ID: strings.Repeat("a", 32), Enabled: true, Spec: Spec{Password: "original"}}
	changed := w
	changed.Spec.Password = "rotated"
	if canRetainMembers([]Work{w}, []Work{changed}) {
		t.Fatal("retained revoked credential")
	}
	changed = w
	changed.Enabled = false
	if canRetainMembers([]Work{w}, []Work{changed}) {
		t.Fatal("retained stopped inbound")
	}
	withAccount := Work{ID: strings.Repeat("b", 32), Enabled: true, Spec: Spec{Accounts: []Account{{Name: "subscription", UUID: "expired"}}}}
	withoutAccount := withAccount
	withoutAccount.Spec.Accounts = nil
	if canRetainMembers([]Work{withAccount}, []Work{withoutAccount}) {
		t.Fatal("failed replacement could retain an expired subscription identity")
	}
}
