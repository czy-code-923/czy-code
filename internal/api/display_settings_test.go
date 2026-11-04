package api

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestTrafficDisplaySettingsArePersistedAndDoNotChangeAccounting(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	ctx := context.Background()
	initial := int64(50_000_000_000)
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "provider", "traffic": hostTrafficInput{Quota: 500_000_000_000, Mode: "both", ResetDay: 5, AutoReset: true, InitialUsed: &initial, DisplayUnit: "GB"}}, 201, nil))
	path := "hosts/" + host["id"].(string) + "/traffic"
	before := obj018(h.call("GET", path, nil, 200, nil))
	if got := obj018(h.call("GET", "display-settings", nil, 200, nil))["traffic_unit"]; got != "GB" {
		t.Fatal(got)
	}
	for _, unit := range []string{"GiB", "auto", "GB"} {
		h.call("PUT", "display-settings", map[string]any{"traffic_unit": unit}, 200, nil)
		if got := obj018(h.call("GET", "display-settings", nil, 200, nil))["traffic_unit"]; got != unit {
			t.Fatal("preference not persisted", got)
		}
		if got := obj018(h.call("GET", "branding", nil, 200, nil))["traffic_unit"]; got != unit {
			t.Fatal("bootstrap mismatch", got)
		}
		if got := obj018(h.call("GET", path, nil, 200, nil)); !reflect.DeepEqual(before, got) {
			t.Fatal("display update changed host accounting")
		}
	}
	h.call("PUT", "display-settings", map[string]any{"traffic_unit": "MB"}, 400, nil)
	h.call("PUT", "display-settings", map[string]any{"traffic_unit": "GB", "quota_bytes": 1}, 400, nil)
	text, err := s.notificationSummary(ctx, "hosts")
	if err != nil || !strings.Contains(text, "50.00 GB") || !strings.Contains(text, "450.00 GB") {
		t.Fatal(text, err)
	}
	for method, scope := range map[string]string{"GET": "site:read", "PUT": "site:write"} {
		if got := requiredScopes(httptest.NewRequest(method, "/api/v1/display-settings", nil)); !reflect.DeepEqual(got, []string{scope}) {
			t.Fatal("incorrect permission", method, got)
		}
	}
}

func TestTrafficUnitsDoNotMixDecimalAndBinary(t *testing.T) {
	for _, item := range []struct {
		n          int64
		unit, want string
	}{{500_000_000_000, "GB", "500.00 GB"}, {1 << 30, "GiB", "1.00 GiB"}, {1 << 30, "GB", "1.07 GB"}, {1 << 20, "GB", "0.001048576 GB"}, {0, "GB", "0.00 GB"}, {2048, "auto", "2.05 KB"}, {2_000_000_000_000, "auto", "2.00 TB"}} {
		if got := trafficBytes(item.n, item.unit); got != item.want {
			t.Errorf("%d %s got %q want %q", item.n, item.unit, got, item.want)
		}
	}
}
