package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"vps-control/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testControlDB(t *testing.T) *Server {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	schema := "notify_" + ID()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); pool.Close() })
	return &Server{DB: db, AdminToken: strings.Repeat("a", 32), MasterKey: bytes.Repeat([]byte{8}, 32)}
}
func TestNotificationBindingAndCommands(t *testing.T) {
	s := testControlDB(t)
	ctx := context.Background()
	handler := s.Handler()
	token := "123456789:" + strings.Repeat("A", 35)
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	c := notificationConfig{Token: token, ChatID: "12345", Enabled: true, Commands: []string{"status", "help"}, CommandPolling: true, AllowedUsers: []int64{99}, QuotaPercent: 80, ReportMinutes: 0, Events: []string{}, ReportSections: []string{"status"}}
	call("PUT", "notifications/telegram", c, 200)
	raw := call("GET", "notifications/telegram", nil, 200)
	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("token leaked via GET")
	}
	var sealed []byte
	if e := s.DB.QueryRow(ctx, `SELECT sealed FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&sealed); e != nil || bytes.Contains(sealed, []byte(token)) {
		t.Fatal("token not encrypted", e)
	}
	var updates []map[string]any
	add := func(id, from, chat, seconds int64, text string) {
		updates = append(updates, map[string]any{"update_id": id, "message": map[string]any{"text": text, "date": time.Now().Unix() + seconds, "from": map[string]any{"id": from}, "chat": map[string]any{"id": chat}}})
	}
	add(1, 98, 12345, 0, "/status")
	add(2, 99, 12346, 0, "/status")
	add(3, 99, 12345, -300, "/status")
	add(4, 99, 12345, 0, "/users")
	add(5, 99, 12345, 0, "/status")
	sends := 0
	polls := 0
	s.telegramClient = &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" {
			t.Fatal("non-official API destination")
		}
		var result any = map[string]any{"message_id": 1}
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			result = updates
			polls++
		} else if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends++
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["chat_id"] != "12345" || strings.Contains(payload["text"].(string), token) {
				t.Fatal("recipient/secret leak")
			}
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}}, nil
	})}
	if e := s.notificationStep(ctx); e != nil {
		t.Fatal(e)
	}
	if sends != 1 || polls != 1 {
		t.Fatal("unauthorized/stale/disabled commands replied", sends, polls)
	}
	if e := s.notificationStep(ctx); e != nil {
		t.Fatal(e)
	}
	if sends != 1 {
		t.Fatal("replayed an already consumed command")
	}
	call("POST", "notifications/telegram/test", map[string]bool{"preview": true}, 200)
	if sends != 1 {
		t.Fatal("preview sent a message")
	}
	call("PUT", "notifications/telegram", c, 409)
	c.Version = 1
	c.Token = ""
	c.Enabled = false
	call("PUT", "notifications/telegram", c, 200)
	if e := s.notificationStep(ctx); e != nil {
		t.Fatal(e)
	}
	if polls != 2 {
		t.Fatal("disabled polling performed network IO")
	}
	call("POST", "notifications/telegram/test", map[string]bool{}, 400)
	call("DELETE", "notifications/telegram", nil, 200)
	raw = call("GET", "notifications/telegram", nil, 200)
	if bytes.Contains(raw, []byte(`"token_configured":true`)) {
		t.Fatal("unbind retained token")
	}
}
func TestTelegramErrorsAndAuthorization(t *testing.T) {
	s := &Server{telegramClient: &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("secret token in URL must not escape")
	})}}
	e := s.telegram(context.Background(), "123456:"+strings.Repeat("B", 32), "getUpdates", map[string]any{}, nil)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal("transport error leaked")
	}
	c := notificationConfig{ChatID: "1", QuotaPercent: 80, CommandPolling: true}
	if c.validate() == nil {
		t.Fatal("command polling without explicit user allowlist")
	}
	c.CommandPolling = false
	c.ReportMinutes = 1
	if c.validate() == nil {
		t.Fatal("over-aggressive report interval")
	}
	for _, path := range []string{"notifications/telegram", "notifications/log"} {
		r := httptest.NewRequest("GET", "/api/v1/"+path, nil)
		if got := requiredScopes(r); len(got) != 1 || got[0] != "notifications:read" {
			t.Fatal(got)
		}
	}
}
