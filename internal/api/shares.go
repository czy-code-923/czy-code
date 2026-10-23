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
		var id, user, profileID string
		var account live.Account
		profile := clientProfile{Port: 1080, Selection: "select", TestURL: "https://www.gstatic.com/generate_204"}
		var expiry time.Time
		e = s.DB.QueryRow(r.Context(), "SELECT id,expires_at,COALESCE(user_id,''),COALESCE(profile_id,'') FROM subscriptions WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now()", tenant, Hash(token)).Scan(&id, &expiry, &user, &profileID)
		if e != nil {
			return problem{404, "subscription unavailable"}
		}
		if user != "" {
			var sealed []byte
			var expires time.Time
			if e = s.DB.QueryRow(r.Context(), `SELECT sealed,expires_at FROM proxy_users WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now()`, tenant, user).Scan(&sealed, &expires); e != nil {
				return problem{404, "subscription unavailable"}
			}
			raw, err := live.Open(s.MasterKey, sealed)
			if err != nil {
				return err
			}
			if e = json.Unmarshal(raw, &account); e != nil {
				return e
			}
			if expires.Before(expiry) {
				expiry = expires
			}
		}
		if profileID != "" {
			if e = s.DB.QueryRow(r.Context(), `SELECT format,local_port,selection,test_url FROM client_profiles WHERE tenant_id=$1 AND id=$2`, tenant, profileID).Scan(&profile.Format, &profile.Port, &profile.Selection, &profile.TestURL); e != nil {
				return e
			}
		}
		format := r.URL.Query().Get("format")
		if format == "" {
			format = profile.Format
		}
		if format == "" {
			format = "sing-box"
		}
		if format != "sing-box" && format != "mihomo" && format != "clash" && format != "shadowrocket" && format != "base64" && format != "xray" {
			return bad("unsupported client format")
		}
		rows, e := s.DB.Query(r.Context(), `SELECT l.id,l.name,l.sealed FROM live_services l WHERE l.tenant_id=$1 AND (($3='' AND EXISTS(SELECT 1 FROM subscription_services x WHERE x.tenant_id=l.tenant_id AND x.service_id=l.id AND x.subscription_id=$2)) OR ($3<>'' AND EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=$3))) AND NOT EXISTS (WITH RECURSIVE chain AS (SELECT n.id,n.next_id,n.enabled,n.state,n.version,n.reported_version,n.reported_at,1 depth FROM live_services n WHERE n.tenant_id=l.tenant_id AND n.id=l.next_id UNION ALL SELECT n.id,n.next_id,n.enabled,n.state,n.version,n.reported_version,n.reported_at,c.depth+1 FROM live_services n JOIN chain c ON n.id=c.next_id WHERE n.tenant_id=l.tenant_id AND c.depth<8) SELECT 1 FROM chain WHERE NOT enabled OR state<>'running' OR version<>reported_version OR reported_at IS NULL OR reported_at<now()-interval '45 seconds') AND l.enabled AND l.state='running' AND l.version=l.reported_version AND l.reported_at>now()-interval '45 seconds' ORDER BY l.created_at LIMIT 64`, tenant, id, user)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		xray := []any{}
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
			if !live.Proxy(spec.Kind) {
				continue
			}
			if spec.ManagedUsers {
				if user == "" {
					return problem{403, "this inbound requires a user subscription"}
				}
				spec.UUID = account.UUID
				spec.Password = account.Password
			} else if user != "" {
				return problem{403, "user group references an unmanaged inbound"}
			}
			spec.StripServerSecrets()
			tag := name + "-" + service[:8]
			if err := spec.ClientFormat(format); err != nil {
				return problem{422, err.Error()}
			}
			if format == "xray" {
				xray = append(xray, spec.XrayClient(tag))
			}
			sb, mh, uri, err := spec.Client(tag)
			if err != nil {
				return err
			}
			// format chosen explicitly above
			if uri == "" && (format == "shadowrocket" || format == "base64") {
				return problem{422, "Selected protocol has no supported URI export; select sing-box or Mihomo"}
			}
			tags = append(tags, tag)
			out = append(out, sb)
			clash = append(clash, mh)
			uris = append(uris, uri)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(out) == 0 {
			return problem{503, "no freshly reported running services; verify agent and network"}
		}
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("expire=%d", expiry.Unix()))
		w.Header().Set("Profile-Update-Interval", "12")
		switch format {
		case "sing-box", "":
			group := map[string]any{"type": "selector", "tag": "proxy", "outbounds": tags, "default": tags[0]}
			if profile.Selection == "urltest" {
				group = map[string]any{"type": "urltest", "tag": "proxy", "outbounds": tags, "url": profile.TestURL, "interval": "5m"}
			}
			out = append(out, group)
			send(w, 200, map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": profile.Port}}, "outbounds": out, "route": map[string]any{"final": "proxy"}})
		case "xray":
			if profile.Selection == "urltest" {
				return problem{422, "Xray JSON template uses the first outbound; select a manual client template"}
			}
			send(w, 200, map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "local", "listen": "127.0.0.1", "port": profile.Port, "protocol": "socks", "settings": map[string]any{"udp": true}}}, "outbounds": xray})
		case "clash", "mihomo":
			group := map[string]any{"name": "PROXY", "type": "select", "proxies": tags}
			if profile.Selection == "urltest" {
				group["type"] = "url-test"
				group["url"] = profile.TestURL
				group["interval"] = 300
			}
			send(w, 200, map[string]any{"mixed-port": profile.Port, "allow-lan": false, "proxies": clash, "proxy-groups": []any{group}, "rules": []string{"MATCH,PROXY"}})
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
