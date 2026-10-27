package api

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestBootstrapScriptPortableLineEndings(t *testing.T) {
	s := &Server{}
	for _, name := range []string{"install.sh", "install.py"} {
		w := httptest.NewRecorder()
		s.bootstrapScript(w, httptest.NewRequest("GET", "/api/v1/agent/"+name, nil))
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("\r")) || w.Body.Len() == 0 {
			t.Fatalf("%s is not a portable nonempty installer", name)
		}
	}
}
