package main

import (
	"testing"
	"time"
)

func TestIdleBackoffKeepsBoundedLatency(t *testing.T) {
	for n, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second} {
		if got := idleDelay(n+1, 15*time.Second); got != want {
			t.Fatal(n, got, want)
		}
	}
	if idleDelay(100, time.Minute) != time.Minute {
		t.Fatal("retry not bounded")
	}
}
