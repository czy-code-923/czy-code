package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"
	"vps-control/internal/executor"
	"vps-control/internal/live"
)

func runLive(ctx context.Context, call func(context.Context, string, any) ([]byte, int, error)) error {
	// Linux parent-death signals track the starting OS thread. Keep it alive until children stop.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	binaries := map[string]executor.Binary{}
	for engine, prefix := range map[string]string{"sing-box": "SING_BOX", "frps": "FRPS", "frpc": "FRPC"} {
		binaries[engine] = executor.Binary{Path: os.Getenv(prefix + "_BINARY"), SHA256: os.Getenv(prefix + "_SHA256")}
	}
	runner, e := live.NewRunner(os.Getenv("LIVE_STATE_DIR"), binaries)
	if e != nil {
		return e
	}
	defer runner.Close()
	last := time.Now()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		reports := runner.Reports()
		raw, status, e := call(ctx, "live/sync", map[string]any{"reports": reports})
		if e == nil && status == 200 {
			var work []live.Work
			if json.Unmarshal(raw, &work) == nil {
				if e = runner.Reconcile(ctx, work); e == nil {
					last = time.Now()
				}
			}
		}
		if e != nil {
			log.Print("live reconciliation unavailable")
		}
		if time.Since(last) > 90*time.Second {
			runner.Reconcile(ctx, nil)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
