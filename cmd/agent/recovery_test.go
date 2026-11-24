package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveSupervisorRetriesAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts int
	err := superviseLive(ctx, func(context.Context) error {
		attempts++
		if attempts == 3 {
			cancel()
		}
		return errors.New("temporary initialization failure")
	}, time.Millisecond)
	if err != nil || attempts != 3 {
		t.Fatal("runner must recover without an Agent restart", attempts, err)
	}
}

func TestOSMeterDoesNotWaitForLiveBaseline(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	t.Setenv("LIVE_STATE_DIR", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var samples atomic.Int32
	runHostTraffic(ctx, func(ctx context.Context, path string, body any) ([]byte, int, error) {
		if path != "host-traffic" {
			t.Error("OS reporting unexpectedly depends on proxy APIs", path)
		}
		samples.Add(1)
		cancel()
		return []byte(`{"accepted":true}`), 200, nil
	})
	if samples.Load() != 1 {
		t.Fatal("OS sampler did not run independently")
	}
	if _, err := os.Stat(filepath.Join(dir, "host-traffic.json")); err != nil {
		t.Fatal("durable host ledger missing", err)
	}
}
