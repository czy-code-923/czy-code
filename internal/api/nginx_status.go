package api

import (
	"encoding/json"
	"net/http"
	"regexp"
)

var nginxVersion = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)

func (s *Server) nginxStatus(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodPost {
		var b struct {
			Version    string `json:"version"`
			Source     string `json:"source"`
			Certbot    *bool  `json:"certbot_available"`
			Cloudflare *bool  `json:"cloudflare_available"`
		}
		if err := decode(w, r, &b); err != nil {
			return err
		}
		if !nginxVersion.MatchString(b.Version) || (b.Source != "baota" && b.Source != "system") {
			return bad("invalid nginx status")
		}
		_, err := s.DB.Exec(r.Context(), `UPDATE site_settings SET nginx_version=$2,nginx_source=$3,nginx_checked_at=now(),certbot_available=COALESCE($4,certbot_available),cloudflare_available=COALESCE($5,cloudflare_available) WHERE tenant_id=$1`, tenant, b.Version, b.Source, b.Certbot, b.Cloudflare)
		if err != nil {
			return err
		}
		send(w, 200, map[string]bool{"recorded": true})
		return nil
	}
	var raw json.RawMessage
	err := s.DB.QueryRow(r.Context(), `SELECT json_build_object('version',nginx_version,'source',nginx_source,'checked_at',nginx_checked_at,'fresh',COALESCE(nginx_checked_at>now()-interval '3 minutes',false),'certbot_available',certbot_available,'cloudflare_available',cloudflare_available) FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&raw)
	if err != nil {
		return err
	}
	send(w, 200, raw)
	return nil
}
