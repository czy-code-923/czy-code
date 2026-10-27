package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"vps-control/internal/live"
)

func normalizedAPIOrigin(value string) (string, bool) {
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || !live.Address(u.Hostname()) {
		return "", false
	}
	if u.Scheme != "https" {
		ip, e := netip.ParseAddr(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (e != nil || !ip.IsLoopback())) {
			return "", false
		}
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		normalized, valid := normalizedAPIOrigin(origin)
		allowed := false
		if origin != "" {
			for _, share := range []bool{false, true} {
				if configured, e := s.siteOrigin(r, share); e == nil && strings.EqualFold(configured, origin) {
					allowed = true
				}
			}
		}
		if !valid && !allowed && strings.HasPrefix(origin, "http://") {
			var domain string
			var https bool
			e := s.DB.QueryRow(r.Context(), `SELECT domain,https FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&domain, &https)
			if e != nil && e != pgx.ErrNoRows {
				wrap(func(http.ResponseWriter, *http.Request) error { return e })(w, r)
				return
			}
			allowed = e == nil && !https && strings.EqualFold(origin, "http://"+domain)
		}
		if valid {
			var domain string
			var https bool
			var raw []byte
			e := s.DB.QueryRow(r.Context(), `SELECT domain,https,api_origins FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&domain, &https, &raw)
			if e != nil && e != pgx.ErrNoRows {
				wrap(func(http.ResponseWriter, *http.Request) error { return e })(w, r)
				return
			}
			scheme := "http"
			if https {
				scheme = "https"
			}
			allowed = allowed || normalized == scheme+"://"+domain
			if !allowed {
				var origins []string
				_ = json.Unmarshal(raw, &origins)
				for _, v := range origins {
					if normalized == v {
						allowed = true
						break
					}
				}
			}
			// Native/local development still uses the same origin, without trusting forwarded headers.
			if !allowed && strings.EqualFold(strings.TrimPrefix(normalized, "http://"), r.Host) && strings.HasPrefix(normalized, "http://") {
				u, _ := url.Parse(normalized)
				ip, e := netip.ParseAddr(u.Hostname())
				allowed = u.Hostname() == "localhost" || (e == nil && ip.IsLoopback())
			}
		}
		w.Header().Add("Vary", "Origin")
		if !allowed {
			wrap(func(http.ResponseWriter, *http.Request) error { return problem{403, "origin is not permitted"} })(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Expose-Headers", "Subscription-Userinfo,Profile-Title,Profile-Update-Interval,Content-Disposition")
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
