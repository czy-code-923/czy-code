package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"
	"vps-control/internal/engine"
	"vps-control/internal/executor"
	"vps-control/internal/hostmeter"
	"vps-control/internal/live"
)

func runLive(ctx context.Context, call func(context.Context, string, any) ([]byte, int, error)) error {
	// Linux parent-death signals track the starting OS thread. Keep it alive until children stop.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	binaries := map[string]executor.Binary{}
	for engine, prefix := range map[string]string{"xray": "XRAY", "sing-box": "SING_BOX", "frps": "FRPS", "frpc": "FRPC"} {
		binaries[engine] = executor.Binary{Path: os.Getenv(prefix + "_BINARY"), SHA256: os.Getenv(prefix + "_SHA256")}
	}
	updates, e := engine.NewManager(os.Getenv("LIVE_STATE_DIR"))
	if e != nil {
		return e
	}
	for name, binary := range updates.Overrides() {
		binaries[name] = binary
	}
	runner, e := live.NewRunner(os.Getenv("LIVE_STATE_DIR"), binaries)
	if e != nil {
		return e
	}
	defer runner.Close()
	// Do not start an empty ledger while the controller baseline is unavailable.
	// Retry here so a temporary network failure recovers without an Agent restart.
	for {
		raw, status, err := call(ctx, "telemetry/baseline", nil)
		var counters []live.TrafficCounter
		if err == nil && status == 200 && json.Unmarshal(raw, &counters) == nil {
			if err = runner.SeedCounters(counters); err != nil {
				return err
			}
			break
		}
		log.Print("waiting for authenticated traffic baseline")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}
	}
	sendUsage := func(guard context.Context) {
		batch, err := runner.TelemetrySnapshot(guard)
		if err != nil {
			log.Print("traffic ledger unavailable")
			return
		}
		raw, status, err := call(guard, "telemetry", batch)
		if err == nil && status == 200 {
			var ack struct {
				Retired []string `json:"retired_counter_ids"`
			}
			json.Unmarshal(raw, &ack)
			runner.AckTelemetry(batch, ack.Retired)
		}
	}
	defer func() {
		runner.StopAll()
		guard, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sendUsage(guard)
	}()
	hostUsage, err := hostmeter.New(os.Getenv("LIVE_STATE_DIR"))
	if err != nil {
		return err
	}
	capabilities := live.Capabilities(binaries)
	last := time.Now()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		if report, err := hostUsage.Snapshot(time.Now()); err == nil {
			if _, status, err := call(ctx, "host-traffic", report); err != nil || status != 200 {
				log.Print("host traffic report pending retry")
			}
		} else {
			log.Print("OS interface counters unavailable")
		}
		sendUsage(ctx)
		if candidate, ok := updates.Pending(); ok {
			previous := runner.Binary(candidate.Engine)
			if err := runner.SwitchBinary(ctx, candidate.Engine, candidate.Binary); err != nil {
				updates.Finish(candidate.Request, executor.Binary{}, "candidate configuration or startup failed; previous version retained")
			} else if err = updates.Finish(candidate.Request, candidate.Binary, ""); err != nil {
				runner.SwitchBinary(ctx, candidate.Engine, previous)
			}
			binaries[candidate.Engine] = runner.Binary(candidate.Engine)
			capabilities = live.Capabilities(binaries)
		}
		if raw, status, err := call(ctx, "engines/sync", map[string]any{"reports": updates.Reports()}); err == nil && status == 200 {
			var requests []engine.Request
			if json.Unmarshal(raw, &requests) == nil && len(requests) <= 1 {
				for _, request := range requests {
					updates.Submit(ctx, request)
				}
			}
		}
		reports := runner.Reports()
		raw, status, e := call(ctx, "live/sync", map[string]any{"reports": reports, "bundle_mode": true, "capabilities": capabilities, "kernel_reports": runner.KernelReports()})
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
