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
}
