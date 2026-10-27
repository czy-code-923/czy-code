package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"vps-control/internal/engine"
)

func TestEngineUpdateQueueAndAgentIsolation(t *testing.T) {
	s := testControlDB(t)
	s.resolveEngineRelease = func(ctx context.Context, name, version string) (engine.Release, error) {
		return engine.Release{Tag: "v1.14.4", Assets: []engine.Asset{{Name: "sing-box-1.14.4-linux-amd64.tar.gz", Size: 1024, Digest: "sha256:" + strings.Repeat("a", 64), URL: "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64.tar.gz"}}}, nil
	}
	h := s.Handler()
	call := func(method, path string, body any, host string, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		if host == "" {
			r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		} else {
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 64))
			r.Header.Set("X-Host-ID", host)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
		var out map[string]any
		if len(w.Body.Bytes()) > 0 && w.Body.Bytes()[0] == '{' {
			json.Unmarshal(w.Body.Bytes(), &out)
		}
		return out
	}
	ctx := context.Background()
	a, b := ID(), ID()
	for _, id := range []string{a, b} {
		if _, e := s.DB.Exec(ctx, `INSERT INTO hosts(tenant_id,id,name,provider,address,agent_hash,agent_version,platform,last_seen) VALUES('self',$1,$1,'fixture','192.0.2.1',$2,'0.17.0','linux/amd64',now())`, id, Hash(strings.Repeat("b", 64))); e != nil {
			t.Fatal(e)
		}
	}
	created := call("POST", "hosts/"+a+"/engine-updates", map[string]any{"engine": "sing-box", "version": "latest"}, "", 202)
	id := created["id"].(string)
	if created["version"] != "v1.14.4" {
		t.Fatal(created)
	}
	call("POST", "hosts/"+a+"/engine-updates", map[string]any{"engine": "sing-box", "version": "latest"}, "", 409)
	report := map[string]any{"reports": []engine.Report{{Request: engine.Request{ID: id, Engine: "sing-box", Version: "v1.14.4"}, State: "succeeded"}}}
	call("POST", "agent/engines/sync", report, b, 200)
	var state string
	s.DB.QueryRow(ctx, `SELECT state FROM engine_updates WHERE id=$1`, id).Scan(&state)
	if state != "queued" {
		t.Fatal("other host updated operation")
	}
	call("POST", "agent/engines/sync", report, a, 200)
	s.DB.QueryRow(ctx, `SELECT state FROM engine_updates WHERE id=$1`, id).Scan(&state)
	if state != "succeeded" {
		t.Fatal(state)
	}
	call("POST", "hosts/"+a+"/engine-updates", map[string]any{"engine": "sing-box", "version": "https://evil.test/core"}, "", 400)
}
