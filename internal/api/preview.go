package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"vps-control/internal/manifest"
)

func (s *Server) preview(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	var instance, sum string
	if e := s.DB.QueryRow(r.Context(), "SELECT instance_id,spec,checksum FROM configs WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id")).Scan(&instance, &raw, &sum); e != nil {
		return e
	}
	spec, e := manifest.Verify(raw, sum)
	if e != nil {
		return problem{409, "stored manifest is invalid"}
	}
	files, e := manifest.Render(spec, instance, strings.Repeat("preview-only", 4))
	if e != nil {
		return e
	}
	for n := range files {
		var v any
		json.Unmarshal(files[n].Content, &v)
		redact(v)
		files[n].Content, _ = json.MarshalIndent(v, "", "  ")
	}
	send(w, 200, map[string]any{"mode": "preview", "files": files, "warning": "Redacted loopback laboratory configuration. Not started or remotely deployed. FRP files are a same-host server/client pair."})
	return nil
}
func redact(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if k == "password" || k == "token" {
				x[k] = "<agent-derived-secret>"
			} else {
				redact(value)
			}
		}
	case []any:
		for _, value := range x {
			redact(value)
		}
	}
}
