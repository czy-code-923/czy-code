package live

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"vps-control/internal/executor"
)

func TestNativeEngineSwitchKeepsRunningVersionOnVerificationFailure(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("native sing-box required")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hash, e := executor.FileDigest(binary)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	r, e := NewRunner(dir, map[string]executor.Binary{"sing-box": {Path: binary, SHA256: hash}})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	work := BundleWork([]Work{{ID: strings.Repeat("a", 32), Version: 1, Enabled: true, Spec: Spec{Engine: "sing-box", Kind: "shadowsocks", Address: "127.0.0.1", Listen: "127.0.0.1", Port: port, Password: Secret()}}})
	if e = r.Reconcile(context.Background(), work); e != nil {
		t.Fatal(e)
	}
	process := r.processes[work[0].ID]
	if process.cmd == nil {
		t.Fatal("initial core not started")
	}
	pid := process.cmd.Process.Pid
	if e = r.SwitchBinary(context.Background(), "sing-box", executor.Binary{Path: binary, SHA256: strings.Repeat("0", 64)}); e == nil {
		t.Fatal("bad digest accepted")
	}
	if process.cmd == nil || process.cmd.Process.Pid != pid {
		t.Fatal("verification failure stopped existing core")
	}
	file := filepath.Join(t.TempDir(), filepath.Base(binary))
	raw, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, raw, 0700); e != nil {
		t.Fatal(e)
	}
	if e = r.SwitchBinary(context.Background(), "sing-box", executor.Binary{Path: file, SHA256: hash}); e != nil {
		t.Fatal(e)
	}
	if r.Binary("sing-box").Path != file || process.cmd == nil {
		t.Fatal("verified candidate not active")
	}
}
