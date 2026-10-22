//go:build linux

package firewall

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLockReleasedWhenParentIsKilled(t *testing.T) {
	if dir := os.Getenv("HARBOR_LOCK_TEST_CHILD"); dir != "" {
		m, e := OpenManager(dir, &fakeBackend{}, nil)
		if e != nil {
			os.Exit(2)
		}
		_, e = m.lock(context.Background())
		if e != nil {
			os.Exit(3)
		}
		fmt.Println("locked")
		time.Sleep(time.Hour)
		return
	}
	dir := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command(exe, "-test.run=^TestLockReleasedWhenParentIsKilled$")
	child.Env = append(os.Environ(), "HARBOR_LOCK_TEST_CHILD="+dir)
	out, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer child.Process.Kill()
	ready := make(chan bool, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line == "locked\n" }()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not lock")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child timeout")
	}
	child.Process.Kill()
	child.Wait()
	m, e := OpenManager(dir, &fakeBackend{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, e := m.lock(ctx)
	if e != nil {
		t.Fatal("dead parent blocked rollback lock", e)
	}
	unlock()
}
