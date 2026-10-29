package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestControlRetryDiscardsRedirectConnectionWithoutForwardingCredentials(t *testing.T) {
	var connections, calls, redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer other.Close()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Redirect(w, r, other.URL, http.StatusPermanentRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	s.Start()
	defer s.Close()
	client := &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := controlCaller(client, s.URL, "host", "secret")
	if _, status, err := call(context.Background(), "heartbeat", nil); err == nil || status != 308 {
		t.Fatalf("expected redirect failure: %d %v", status, err)
	}
	if _, status, err := call(context.Background(), "heartbeat", nil); err != nil || status != 200 {
		t.Fatalf("retry: %d %v", status, err)
	}
	if connections.Load() < 2 || redirected.Load() != 0 {
		t.Fatalf("connections=%d redirected=%d", connections.Load(), redirected.Load())
	}
}
