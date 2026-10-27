package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vps-control/internal/live"
)

func TestTelegramUsesRemoteClockAndPreservesPollErrors(t *testing.T) {
	s := testControlDB(t)
	ctx := context.Background()
	token := "123456789:" + strings.Repeat("A", 35)
	c := notificationConfig{Token: token, ChatID: "12345", Enabled: true, CommandPolling: true, AllowedUsers: []int64{99}, Commands: []string{"hosts", "help"}, QuotaPercent: 80}
	raw, _ := json.Marshal(c)
	r := httptest.NewRequest("PUT", "/api/v1/notifications/telegram", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+s.AdminToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	remote := time.Now().Add(7 * time.Minute).Truncate(time.Second)
	sends := 0
	pollError := false
	s.telegramClient = &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		var result any = map[string]any{"message_id": 1}
		code := 200
		ok := true
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["limit"] != float64(3) {
				t.Fatal("must limit fetched updates rather than discard replies", payload)
			}
			if pollError {
				code = 409
				ok = false
			} else {
				result = []map[string]any{{"update_id": 1, "message": map[string]any{"text": "/hosts", "date": remote.Unix(), "from": map[string]any{"id": 99}, "chat": map[string]any{"id": 12345}}}}
			}
		} else if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends++
		}
		b, _ := json.Marshal(map[string]any{"ok": ok, "result": result, "error_code": code})
		return &http.Response{StatusCode: code, Header: http.Header{"Date": []string{remote.UTC().Format(http.TimeFormat)}}, Body: io.NopCloser(bytes.NewReader(b))}, nil
	})}
	if e := s.notificationStep(ctx); e != nil {
		t.Fatal(e)
	}
	if sends != 1 {
		t.Fatal("valid command rejected because of VPS clock drift")
	}
	h := s.notificationHealth(ctx, c)
	if h["last_command"] != "hosts" || h["clock_skew_seconds"].(int) < 400 {
		t.Fatal(h)
	}
	pollError = true
	if e := s.notificationStep(ctx); e == nil {
		t.Fatal("missing polling conflict")
	}
	s.logNotification(ctx, "test", nil)
	h = s.notificationHealth(ctx, c)
	if h["poll_state"] != "error" || h["last_poll_error"] == "" {
		t.Fatal("test send masked polling error", h)
	}
	// Due reports are still delivered when command polling conflicts.
	_, e := s.DB.Exec(ctx, `UPDATE notification_settings SET settings=jsonb_set(settings,'{report_minutes}','15'),next_report_at=now()-interval '1 minute' WHERE tenant_id=$1`, tenant)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.notificationStep(ctx)
	if sends != 2 {
		t.Fatal("polling error blocked scheduled notification", sends)
	}
}

func TestTelegramDiagnosisDoesNotConsumeMessagesOrExposeWebhook(t *testing.T) {
	s := testControlDB(t)
	ctx := context.Background()
	c := notificationConfig{ChatID: "12345", QuotaPercent: 80, Commands: []string{"help"}, Version: 1, Enabled: true}
	raw, _ := json.Marshal(c)
	sealed := []byte{}
	var e error
	sealed, e = sealNotificationFixture(s)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(ctx, `INSERT INTO notification_settings(tenant_id,settings,sealed)VALUES($1,$2,$3)`, tenant, raw, sealed)
	if e != nil {
		t.Fatal(e)
	}
	called := []string{}
	s.telegramClient = &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		called = append(called, method)
		var result any
		switch method {
		case "getMe":
			result = map[string]any{"id": 123456789, "username": "fixture_bot", "first_name": "Fixture"}
		case "getWebhookInfo":
			result = map[string]any{"url": "https://secret.invalid/private-token", "pending_update_count": 3}
		case "getMyCommands":
			result = []map[string]string{{"command": "help"}}
		case "setMyCommands":
			result = true
		default:
			t.Fatal("unexpected message consumption", method)
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}}, nil
	})}
	for _, path := range []string{"diagnose", "commands"} {
		b := "{}"
		if path == "commands" {
			b = `{"version":1}`
		}
		r := httptest.NewRequest("POST", "/api/v1/notifications/telegram/"+path, strings.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-token") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if strings.Contains(strings.Join(called, ","), "getUpdates") {
		t.Fatal("diagnosis consumes updates")
	}
	if s.notificationHealth(ctx, c)["bot_username"] != "fixture_bot" {
		t.Fatal("bot identity not cached")
	}
}

func sealNotificationFixture(s *Server) ([]byte, error) {
	return live.Seal(s.MasterKey, []byte("123456789:"+strings.Repeat("B", 35)))
}

func TestTelegramRateLimitIsTypedAndRedacted(t *testing.T) {
	s := &Server{telegramClient: &http.Client{Transport: notificationTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":429,"description":"secret","parameters":{"retry_after":60}}`))}, nil
	})}}
	e := s.telegram(context.Background(), "secret", "getMe", map[string]any{}, nil)
	var typed telegramAPIError
	if !errors.As(e, &typed) || typed.RetryAfter != 60 || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}
