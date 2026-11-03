package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectConfigurationDoesNotApplyOrReadFiles(t *testing.T) {
	s := &Server{}
	source := `{"inbounds":[{"type":"vless","tag":"Imported","listen":"::","listen_port":23456,"users":[{"uuid":"11111111-1111-4111-8111-111111111111"}],"tls":{"enabled":true,"certificate_path":"/etc/shadow","key_path":"/etc/passwd"},"transport":{"type":"ws","path":"/ws"}}]}`
	raw, _ := json.Marshal(map[string]string{"source": source})
	r := httptest.NewRequest("POST", "/api/v1/configuration/inspect", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	if e := s.inspectConfiguration(w, r); e != nil {
		t.Fatal(e)
	}
	var result struct {
		Format     string
		Candidates []inspectedInbound
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.Format != "sing-box" || len(result.Candidates) != 1 || result.Candidates[0].Input["path"] != "/ws" {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "root:") || len(result.Candidates[0].Warnings) == 0 {
		t.Fatal("file paths were dereferenced or warnings lost")
	}
}

func TestInspectHY2ServerBandwidthDoesNotBecomeClientHint(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/configuration/inspect", strings.NewReader(`{"source":"{\"inbounds\":[{\"type\":\"hysteria2\",\"listen_port\":24000,\"up_mbps\":900,\"down_mbps\":200}]}"}`))
	w := httptest.NewRecorder()
	if err := (&Server{}).inspectConfiguration(w, r); err != nil {
		t.Fatal(err)
	}
	var result struct{ Candidates []inspectedInbound }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Input["hy2_client_mode"] != "bbr" || result.Candidates[0].Input["up_mbps"] != float64(900) {
		t.Fatal(w.Body.String())
	}
}
