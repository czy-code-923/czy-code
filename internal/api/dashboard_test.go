package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDashboardBalancesAndZeroDays(t *testing.T) {
	s := testControlDB(t)
	ctx := context.Background()
	handler := s.Handler()
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(path, w.Code, w.Body.String())
		}
		var data map[string]any
		if want == 200 {
			if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
				t.Fatal(e)
			}
		}
		return data
	}
	limited := call("POST", "proxy-users", map[string]any{"name": "limited", "enabled": true, "expires_at": time.Now().Add(time.Hour), "quota_bytes": 1000, "reset_strategy": "never"}, 200)
	unlimited := call("POST", "proxy-users", map[string]any{"name": "unlimited", "enabled": true, "expires_at": time.Now().Add(time.Hour), "quota_bytes": 0, "reset_strategy": "never"}, 200)
	_, e := s.DB.Exec(ctx, `UPDATE proxy_users SET used_bytes=CASE WHEN id=$2 THEN 100 ELSE 10000 END WHERE tenant_id=$1 AND id IN ($2,$3)`, tenant, limited["id"], unlimited["id"])
	if e != nil {
		t.Fatal(e)
	}
	d := call("GET", "dashboard", nil, 200)
	if d["remaining_bytes"] != float64(900) || d["used_bytes"] != float64(10100) || d["unlimited_users"] != float64(1) {
		t.Fatal("unlimited consumption deducted from finite balance", d)
	}
	for _, key := range []string{"daily", "user_daily"} {
		days := d[key].([]any)
		if len(days) != 14 {
			t.Fatal("missing zero-traffic dates", key, len(days))
		}
		for _, v := range days {
			if v.(map[string]any)["uplink"] != float64(0) {
				t.Fatal(v)
			}
		}
	}
	d = call("GET", "dashboard?user_id="+limited["id"].(string), nil, 200)
	if d["user_today_bytes"] != float64(0) {
		t.Fatal(d)
	}
	call("GET", "dashboard?user_id=invalid", nil, 400)
	call("GET", "access-records?limit=201", nil, 400)
}
