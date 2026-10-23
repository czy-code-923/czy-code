package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"vps-control/internal/live"
)

// This handler has no inventory, admin, agent or health routes. Bind only on loopback.
func (s *Server) ShareHandler() http.Handler {
	m := http.NewServeMux()
	var mu sync.Mutex
	var window time.Time
	count := 0
	m.HandleFunc("GET /s/{token}", wrap(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		mu.Lock()
		if time.Since(window) > time.Second {
			window = time.Now()
			count = 0
		}
		count++
		limited := count > 20
		mu.Unlock()
		if limited {
			w.Header().Set("Retry-After", "1")
			return problem{429, "rate limited"}
		}
		if e := s.liveReady(); e != nil {
			return e
		}
		token := r.PathValue("token")
		if len(token) != 64 {
			return problem{404, "subscription unavailable"}
		}
		origin, e := s.publicOrigin(r)
		if e != nil {
			return problem{404, "subscription unavailable"}
		}
		u, _ := url.Parse(origin)
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		if !strings.EqualFold(host, u.Hostname()) {
			return problem{404, "subscription unavailable"}
		}
		var id string
		var expiry time.Time
		e = s.DB.QueryRow(r.Context(), "SELECT id,expires_at FROM subscriptions WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now()", tenant, Hash(token)).Scan(&id, &expiry)
		if e != nil {
			return problem{404, "subscription unavailable"}
		}
		rows, e := s.DB.Query(r.Context(), `SELECT l.id,l.name,l.sealed FROM live_services l JOIN subscription_services x ON (l.tenant_id,l.id)=(x.tenant_id,x.service_id) WHERE l.tenant_id=$1 AND x.subscription_id=$2 AND l.enabled AND l.state='running' AND l.version=l.reported_version AND l.reported_at>now()-interval '45 seconds' ORDER BY l.created_at LIMIT 64`, tenant, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		clash := []any{}
		tags := []string{}
		uris := []string{}
		for rows.Next() {
			var service, name string
			var sealed []byte
			if e = rows.Scan(&service, &name, &sealed); e != nil {
				return e
			}
			raw, e := live.Open(s.MasterKey, sealed)
			if e != nil {
				return e
			}
			var spec live.Spec
			if e = json.Unmarshal(raw, &spec); e != nil {
				return e
			}
			if spec.Kind != "shadowsocks" {
				continue
			}
			tag := name + "-" + service[:8]
			tags = append(tags, tag)
			out = append(out, map[string]any{"type": "shadowsocks", "tag": tag, "server": spec.Address, "server_port": spec.Port, "method": live.Method, "password": spec.Password})
			clash = append(clash, map[string]any{"type": "ss", "name": tag, "server": spec.Address, "port": spec.Port, "cipher": live.Method, "password": spec.Password, "udp": true})
			// SIP002 explicitly forbids Base64URL userinfo for AEAD-2022.
			userinfo := url.QueryEscape(live.Method) + ":" + url.QueryEscape(spec.Password)
			uris = append(uris, fmt.Sprintf("ss://%s@%s#%s", userinfo, net.JoinHostPort(spec.Address, fmt.Sprint(spec.Port)), url.PathEscape(tag)))
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(out) == 0 {
			return problem{503, "no freshly reported running services; verify agent and network"}
		}
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("expire=%d", expiry.Unix()))
		w.Header().Set("Profile-Update-Interval", "12")
		switch r.URL.Query().Get("format") {
		case "sing-box", "":
			out = append(out, map[string]any{"type": "selector", "tag": "proxy", "outbounds": tags, "default": tags[0]})
			send(w, 200, map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 1080}}, "outbounds": out, "route": map[string]any{"final": "proxy"}})
		case "clash", "mihomo":
			send(w, 200, map[string]any{"mixed-port": 1080, "allow-lan": false, "proxies": clash, "proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": tags}}, "rules": []string{"MATCH,PROXY"}})
		case "base64", "shadowrocket":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte(strings.Join(uris, "\n"))))
		default:
			return bad("format must be sing-box, mihomo, shadowrocket, clash or base64")
		}
		return nil
	}))
	return m
}
