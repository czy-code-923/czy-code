package api

import (
	"context"
	"strings"
	"testing"
)

func TestNginxStatusReportAndRead(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	if _, err := s.DB.Exec(context.Background(), `INSERT INTO site_settings(tenant_id,domain) VALUES('self','') ON CONFLICT (tenant_id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	h.call("POST", "gateway/nginx", map[string]any{"version": "1.30.4", "source": "baota"}, 200, nil)
	result := string(h.call("GET", "gateway/nginx", nil, 200, nil))
	if !strings.Contains(result, "1.30.4") || !strings.Contains(result, "baota") {
		t.Fatal(result)
	}
	h.call("POST", "gateway/nginx", map[string]any{"version": "$(bad)", "source": "baota"}, 400, nil)
	h.call("POST", "gateway/nginx", map[string]any{"version": "1.30.4", "source": "system", "certbot_available": true, "cloudflare_available": false}, 200, nil)
	state := obj018(h.call("GET", "gateway/nginx", nil, 200, nil))
	if state["fresh"] != true || state["certbot_available"] != true || state["cloudflare_available"] != false {
		t.Fatal(state)
	}
	// Older reviewed helpers only post the version; retain capability observations.
	h.call("POST", "gateway/nginx", map[string]any{"version": "1.30.4", "source": "system"}, 200, nil)
	state = obj018(h.call("GET", "gateway/nginx", nil, 200, nil))
	if state["certbot_available"] != true {
		t.Fatal("old helper erased capability report")
	}
	if _, err := s.DB.Exec(context.Background(), `UPDATE site_settings SET nginx_checked_at=now()-interval '4 minutes' WHERE tenant_id='self'`); err != nil {
		t.Fatal(err)
	}
	if state = obj018(h.call("GET", "gateway/nginx", nil, 200, nil)); state["fresh"] != false {
		t.Fatal("stale helper shown as available")
	}
}
