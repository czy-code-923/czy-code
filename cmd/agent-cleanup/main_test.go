package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemovalBoundaries(t *testing.T) {
	bad := []string{"/", "/opt/harbor", "/etc/harbor/control.env", "/var/lib/harbor", "/etc/systemd/system", "/opt/harbor-agent/../../etc", "/opt/harbor-agent-other"}
	for _, p := range bad {
		if safePath(p) {
			t.Fatalf("dangerous removal allowed: %s", p)
		}
	}
	if runtime.GOOS == "windows" {
		if !safePath(filepath.Join(os.Getenv("ProgramData"), "StarhubAgent", "runtime")) {
			t.Fatal("Windows installation refused")
		}
	} else if !safePath("/var/lib/harbor-agent/live") {
		t.Fatal("Agent state refused")
	}
}
