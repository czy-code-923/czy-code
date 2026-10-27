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
