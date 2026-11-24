package main

import (
	"context"
	"log"
	"os"
	"time"
	"vps-control/internal/hostmeter"
)

// OS counters must keep reporting while a core is validating, upgrading or
// waiting for its proxy traffic baseline. It has a separate durable ledger.
func runHostTraffic(ctx context.Context, call func(context.Context, string, any) ([]byte, int, error)) {
	var meter *hostmeter.Meter
	for ctx.Err() == nil {
		if meter == nil {
			var err error
			meter, err = hostmeter.New(os.Getenv("LIVE_STATE_DIR"))
			if err != nil {
				log.Print("host traffic ledger unavailable; retrying")
			}
		}
		if meter != nil {
			if report, err := meter.Snapshot(time.Now()); err == nil {
				if _, status, err := call(ctx, "host-traffic", report); err != nil || status != 200 {
					log.Printf("host traffic report pending retry: HTTP %d: %v", status, err)
				}
			} else {
				log.Print("OS interface counters unavailable; retrying")
			}
		}
		if !waitRetry(ctx, 10*time.Second) {
			return
		}
	}
}

func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func superviseLive(ctx context.Context, run func(context.Context) error, delay time.Duration) error {
	for ctx.Err() == nil {
		if err := run(ctx); err != nil {
			log.Print("live runner stopped; retrying: ", err)
		}
		if !waitRetry(ctx, delay) {
			break
		}
	}
	return nil
}
