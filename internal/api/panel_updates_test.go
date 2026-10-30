package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"vps-control/internal/buildinfo"
)

func TestPanelUpdateQueueRootAndLocalWorker(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	s.panelReleaseClient = &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.24.0-distribution","draft":false,"prerelease":false}`))}, nil
	})}
	body := map[string]any{"version": "0.24.0", "current": buildinfo.Version}
	h.call("POST", "panel-updates", body, 409, nil)
	worker := func(action string, body any, want int, forwarded bool) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/panel-updates/worker/"+action, bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		if forwarded {
			r.Header.Set("X-Real-IP", "203.0.113.1")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("worker %s got %d expected %d %s", action, w.Code, want, w.Body)
		}
		return w.Body.Bytes()
	}
	worker("heartbeat", map[string]string{"layout": "legacy", "platform": "linux-amd64"}, 403, true)
	h.call("POST", "panel-updates/worker/heartbeat", map[string]string{"layout": "legacy", "platform": "linux-amd64"}, 403, nil)
	worker("heartbeat", map[string]string{"layout": "legacy", "platform": "linux-amd64"}, 200, false)
	if got := obj018(h.call("GET", "panel-release", nil, 200, nil)); got["automatic_update"] != true {
		t.Fatal("helper readiness missing")
	}
	h.call("POST", "panel-updates", map[string]string{"version": "0.24.0", "current": "0.1.0"}, 409, nil)
	job := obj018(h.call("POST", "panel-updates", body, 202, nil))
	id := job["id"].(string)
	h.call("POST", "panel-updates", body, 409, nil)
	claim := obj018(worker("claim", map[string]any{}, 200, false))["job"].(map[string]any)
	if claim["id"] != id || claim["target_version"] != "0.24.0" {
		t.Fatal("incorrect queued version")
	}
	if got := obj018(worker("claim", map[string]any{}, 200, false)); got["job"] != nil {
		t.Fatal("job claimed twice")
	}
	worker("report", map[string]string{"id": id, "state": "applying", "detail": ""}, 200, false)
	worker("report", map[string]string{"id": id, "state": "rolled_back", "detail": "health failed; restored"}, 200, false)
	worker("report", map[string]string{"id": id, "state": "succeeded", "detail": ""}, 409, false)
	status := obj018(h.call("GET", "panel-updates", nil, 200, nil))
	if status["items"].([]any)[0].(map[string]any)["state"] != "rolled_back" {
		t.Fatal("rollback status missing")
	}
	key := obj018(h.call("POST", "api-keys", map[string]any{"name": "reader", "scopes": []string{"site:read"}, "expires_at": "2027-01-01T00:00:00Z", "rate_limit": 120}, 201, nil))["token"].(string)
	r := httptest.NewRequest("GET", "/api/v1/panel-updates", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("delegated key accessed privileged update queue")
	}
}
