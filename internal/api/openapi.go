package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPI []byte

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	_, e := w.Write(openAPI)
	return e
}
