package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelReleaseCheckUsesOnlyValidatedOfficialRelease(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"tag_name":"v0.24.0-distribution","draft":false,"prerelease":false}`, 200},
		{`{"tag_name":"v0.24.0-distribution","prerelease":true}`, 502},
		{`{"tag_name":"v0.24.0-distribution/../../evil"}`, 502},
	} {
		s := &Server{AdminToken: strings.Repeat("a", 32), panelReleaseClient: &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://api.github.com/repos/czy-code-923/czy-code/releases/latest" {
				t.Fatal("unexpected release URL")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}}
		req := httptest.NewRequest("GET", "/api/v1/panel-release", nil)
		req.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("status %d expected %d: %s", w.Code, tc.status, w.Body)
		}
		if tc.status == 200 && (!strings.Contains(w.Body.String(), `"update_available":true`) || !strings.Contains(w.Body.String(), `"automatic_update":false`)) {
			t.Fatal(w.Body.String())
		}
	}
	if panelNewer("v0.23.5-distribution", "0.23.6") || panelNewer("v0.23.6-distribution", "0.23.6") || !panelNewer("v0.24.0-distribution", "0.23.6") {
		t.Fatal("incorrect version comparison")
	}
}
