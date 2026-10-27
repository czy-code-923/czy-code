package engine

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOfficialReleaseStaging(t *testing.T) {
	if os.Getenv("TEST_ENGINE_DOWNLOAD") != "1" {
		t.Skip("explicit official network download test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	dir := t.TempDir()
	binary, e := Stage(ctx, dir, "sing-box", "v1.14.4", runtime.GOOS+"-"+runtime.GOARCH)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := exec.CommandContext(ctx, binary.Path, "version").Output()
	if e != nil || !strings.Contains(string(raw), "sing-box version 1.14.4") || strings.Contains(string(raw), "harbor") {
		t.Fatalf("official binary version %v", e)
	}
	again, e := Stage(ctx, dir, "sing-box", "v1.14.4", runtime.GOOS+"-"+runtime.GOARCH)
	if e != nil || again != binary {
		t.Fatal("cached release changed", e)
	}
}
