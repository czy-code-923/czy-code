package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"vps-control/internal/controlorigin"
)

func TestOriginMigrationProvesIdentityBeforeSendingCredentialAndRecovers(t *testing.T) {
	token := strings.Repeat("a", 64)
	var proofs, requests atomic.Int32
	next := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "verify-origin") {
			if r.Header.Get("Authorization") != "" {
				t.Error("bearer leaked before proof")
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			proofs.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"proof": controlorigin.Proof(controlorigin.TokenHash(token), body["nonce"], body["origin"])})
			return
		}
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("credential missing")
		}
		w.Write([]byte(`{}`))
	}))
	defer next.Close()
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	client := next.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dir := t.TempDir()
	c := newControlConnection(client, old.URL, "host", token, dir)
	c.update(context.Background(), []string{next.URL})
	if c.origin() != next.URL || proofs.Load() != 1 || requests.Load() != 0 {
		t.Fatal("migration was not proof-only")
	}
	if _, _, err := c.call(context.Background(), "heartbeat", nil); err != nil {
		t.Fatal(err)
	}
	// Old env remains a fallback; a restart recovers the verified saved endpoint.
	old.Close()
	restored := newControlConnection(client, old.URL, "host", token, dir)
	if _, _, err := restored.call(context.Background(), "claim", nil); err == nil {
		t.Fatal("ambiguous POST must not replay")
	}
	if restored.origin() != next.URL || requests.Load() != 1 {
		t.Fatal("fallback failed or claim replayed")
	}
	if _, _, err := restored.call(context.Background(), "heartbeat", nil); err != nil {
		t.Fatal(err)
	}
}

func TestOriginMigrationRejectsFakeProofAndHTTPDowngrade(t *testing.T) {
	var leaked atomic.Int32
	fake := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		w.Write([]byte(`{"proof":"fake"}`))
	}))
	defer fake.Close()
	c := newControlConnection(fake.Client(), "https://original.example", "host", strings.Repeat("a", 64), t.TempDir())
	for _, origins := range [][]string{{fake.URL}, {"http://evil.example"}, {"https://user:pass@evil.example"}, {"https://evil.example/path"}} {
		c.update(context.Background(), origins)
		if c.origin() != "https://original.example" {
			t.Fatal("unsafe migration")
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("credential leaked")
	}
}
