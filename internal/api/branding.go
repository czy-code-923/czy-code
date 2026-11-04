package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/buildinfo"
)

func (s *Server) branding(w http.ResponseWriter, r *http.Request) error {
	title := "星枢"
	e := s.DB.QueryRow(r.Context(), `SELECT title FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&title)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	unit, e := s.trafficDisplayUnit(r.Context())
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"title": title, "subtitle": "VPS 多机管理", "version": buildinfo.Version, "traffic_unit": unit})
	return nil
}
