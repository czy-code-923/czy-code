package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"vps-control/internal/buildinfo"
)

func TestOpenAPIExplicitRouteCoverage(t *testing.T) {
	var doc struct {
		Info  struct{ Version string }
		Paths map[string]map[string]any
	}
	if e := json.Unmarshal(openAPI, &doc); e != nil {
		t.Fatal(e)
	}
	if doc.Info.Version != buildinfo.Version {
		t.Fatal("OpenAPI release version drift")
	}
	source, e := os.ReadFile("api.go")
	if os.IsNotExist(e) {
		t.Skip("precompiled isolated API test binary has no source checkout")
	}
	if e != nil {
		t.Fatal(e)
	}
	re := regexp.MustCompile(`m.HandleFunc\("(GET|POST|PUT|DELETE|PATCH) ([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(string(source), -1) {
		if !strings.HasPrefix(m[2], "/api/v1/") {
			continue
		}
		if _, ok := doc.Paths[m[2]][strings.ToLower(m[1])]; !ok {
			t.Errorf("undocumented route: %s %s", m[1], m[2])
		}
	}
}
func TestProtocolTemplatesExposePerCoreLayers(t *testing.T) {
	w := httptest.NewRecorder()
	if e := (&Server{}).protocolTemplates(w, httptest.NewRequest("GET", "/api/v1/protocol-templates", nil)); e != nil {
		t.Fatal(e)
	}
	var templates []struct {
		Kind   string
		Layers map[string][]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &templates); e != nil {
		t.Fatal(e)
	}
	if len(templates) != 13 {
		t.Fatal("protocol count", len(templates))
	}
	for _, row := range templates {
		if len(row.Layers) == 0 {
			t.Fatal("missing per-core compatibility", row.Kind)
		}
	}
}
