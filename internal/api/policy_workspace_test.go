package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"vps-control/internal/clientpolicy"
)

func TestPolicyWorkspaceEndpoints(t *testing.T) {
	s := testControlDB(t)
	h := s.Handler()
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s = %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	factory := call("GET", "client-policies/default", nil, 200)
	source := factory["source"].(string)
	p, e := clientpolicy.Parse(source)
	if e != nil || len(p.Groups) != 21 || len(p.Rules) != 186 {
		t.Fatal("factory template", e)
	}
	imported := call("POST", "client-policies/import", map[string]any{"source": `{"dns":{"nameserver":["1.1.1.1"]},"proxies":[{"name":"old","type":"vmess","server":"192.0.2.1","password":"private-test-secret","uuid":"private-uuid"}],"proxy-groups":[{"name":"choice","type":"select","proxies":["old"]}],"rules":["MATCH,choice"]}`}, 200)
	raw, _ := json.Marshal(imported)
	if bytes.Contains(raw, []byte("private-test-secret")) || bytes.Contains(raw, []byte("private-uuid")) {
		t.Fatal("import leaked identity")
	}
	if len(imported["mappings"].([]any)) != 1 || len(imported["unresolved"].([]any)) != 1 {
		t.Fatal("missing node mapping")
	}
	var count int
	if e = s.DB.QueryRow(context.Background(), `SELECT count(*) FROM client_policies`).Scan(&count); e != nil || count != 0 {
		t.Fatal("import mutated stored policies", e)
	}
	call("POST", "client-policies", map[string]any{"name": "Max default", "client_id": "all", "source": source}, 200)
	for _, client := range []string{"mihomo", "hiddify", "v2rayng", "shadowrocket-config"} {
		out := call("POST", "client-policies/preview", map[string]any{"source": source, "client_id": client}, 200)
		if out["preview_only"] != true {
			t.Fatal("preview can be mistaken for deployable configuration")
		}
		raw, _ := json.Marshal(out)
		if bytes.Contains(raw, []byte("password")) || bytes.Contains(raw, []byte("uuid")) {
			t.Fatal("preview disclosed node identity")
		}
	}
	call("POST", "client-policies/preview", map[string]any{"source": source, "client_id": "base64"}, 400)
	r := httptest.NewRequest("POST", "/api/v1/client-policies/import", strings.NewReader(`{"source":"rules: ['MATCH,DIRECT']"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated import")
	}
	if e := s.EnsureDefaultClientPolicy(context.Background()); e != nil {
		t.Fatal(e)
	}
	var saved string
	if e = s.DB.QueryRow(context.Background(), `SELECT name FROM client_policies WHERE client_id='all' AND user_id IS NULL`).Scan(&saved); e != nil || saved != "Max default" {
		t.Fatal("bootstrap overwrote existing global policy", e)
	}
	if _, e = s.DB.Exec(context.Background(), `DELETE FROM client_policies`); e != nil {
		t.Fatal(e)
	}
	if e = s.EnsureDefaultClientPolicy(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = s.DB.QueryRow(context.Background(), `SELECT count(*) FROM client_policies`).Scan(&count); e != nil || count != 0 {
		t.Fatal("deleted policy unexpectedly recreated", e)
	}
}
