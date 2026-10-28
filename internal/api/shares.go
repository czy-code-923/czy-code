package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"vps-control/internal/clientpolicy"
	"vps-control/internal/live"
)

// This handler has no inventory, admin, agent or health routes. Bind only on loopback.
func (s *Server) ShareHandler() http.Handler {
	m := http.NewServeMux()
	var mu sync.Mutex
	var window time.Time
	count := 0
	m.HandleFunc("GET /s/{token}", wrap(func(w http.ResponseWriter, r *http.Request) error {
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
		return s.serveSubscription(w, r)
	}))
	return s.cors(m)
}
func (s *Server) serveSubscription(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Add("Vary", "User-Agent, X-Hwid")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
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
		management, err := s.managementOrigin(r)
		if err != nil {
			return problem{404, "subscription unavailable"}
		}
		m, _ := url.Parse(management)
		if !strings.EqualFold(host, m.Hostname()) {
			return problem{404, "subscription unavailable"}
		}
	}
	var id, user, profileID, name, device string
	var deviceLimit int
	deviceAll := true
	var deviceServices []string
	var account live.Account
	profile := clientProfile{Port: 1080, Selection: "select", TestURL: "https://www.gstatic.com/generate_204"}
	var expiry time.Time
	var quota, upload, download int64
	quotaReached := false
	e = s.DB.QueryRow(r.Context(), "SELECT id,expires_at,COALESCE(user_id,''),COALESCE(profile_id,''),name,COALESCE(device_id,'') FROM subscriptions WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now()", tenant, Hash(token)).Scan(&id, &expiry, &user, &profileID, &name, &device)
	if e != nil {
		return problem{404, "subscription unavailable"}
	}
	if profileID != "" {
		if e = s.DB.QueryRow(r.Context(), `SELECT format,local_port,selection,test_url,extra_config FROM client_profiles WHERE tenant_id=$1 AND id=$2`, tenant, profileID).Scan(&profile.Format, &profile.Port, &profile.Selection, &profile.TestURL, &profile.Extra); e != nil {
			return e
		}
	}
	client, e := selectedClient(r, profile.Format)
	if e != nil {
		return e
	}
	if user != "" {
		var sealed []byte
		var expires time.Time
		if e = s.DB.QueryRow(r.Context(), `SELECT sealed,expires_at,quota_bytes,billed_upload_bytes,billed_download_bytes,`+deviceLimitSQL+` FROM proxy_users u WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now() `, tenant, user).Scan(&sealed, &expires, &quota, &upload, &download, &deviceLimit); e != nil {
			return problem{404, "subscription unavailable"}
		}
		if device == "" && r.Context().Value(subscriptionInspectionKey{}) != true {
			device, e = s.subscriptionDevice(w, r, user, client.ID, deviceLimit)
			if e != nil {
				return e
			}
		}
		if device != "" {
			if e = s.DB.QueryRow(r.Context(), `SELECT sealed,all_nodes,service_ids FROM user_devices WHERE tenant_id=$1 AND id=$2 AND user_id=$3 AND enabled`, tenant, device, user).Scan(&sealed, &deviceAll, &deviceServices); e != nil {
				return problem{404, "device subscription unavailable"}
			}
		}
		if device != "" && !deviceAll && len(deviceServices) == 0 {
			return problem{403, "此设备尚未授权任何入站"}
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
	if device != "" && r.Context().Value(subscriptionInspectionKey{}) != true {
		ua := clientMetadata(r.UserAgent(), 256)
		if _, e = s.DB.Exec(r.Context(), `UPDATE user_devices SET last_subscription_at=now(),user_agent=$3 WHERE tenant_id=$1 AND id=$2`, tenant, device, ua); e != nil {
			return e
		}
	}
	usage, err := s.subscriptionUsage(r, id, user)
	if err != nil {
		return err
	}
	quota, upload, download = usage.Total, usage.Upload, usage.Download
	quotaReached = user != "" && quota > 0 && usage.Used >= quota
	format := client.Format
	extra := profile.Extra
	if pc, ok := clientByID(profile.Format); !ok || pc.Format != format {
		extra = nil
	}
	nodes := []clientpolicy.Node{}
	warnings := []string{}
	excluded := 0
	apply := func(base map[string]any) (map[string]any, error) {
		config, notes, err := s.applyClientPolicy(r, user, client.ID, format, nodes, base)
		if err != nil {
			return nil, problem{422, err.Error()}
		}
		warnings = append(warnings, notes...)
		if len(warnings) > 0 {
			w.Header().Set("X-Starhub-Policy-Warnings", fmt.Sprint(len(warnings)))
		}
		w.Header().Set("X-Starhub-Excluded-Nodes", fmt.Sprint(excluded))
		if profileID != "" {
			switch format {
			case "mihomo":
				config["mixed-port"] = profile.Port
			case "sing-box", "xray":
				if ins, ok := config["inbounds"].([]any); ok {
					for _, value := range ins {
						in := object(value)
						key := "listen_port"
						if format == "xray" {
							key = "port"
						}
						in[key] = profile.Port
					}
				}
			}
		}
		config, err = mergeClientOptions(config, extra)
		if err != nil {
			return nil, problem{422, err.Error()}
		}
		return config, nil
	}
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", upload, download, quota, expiry.Unix()))
	w.Header().Set("Profile-Update-Interval", "1")
	w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(name)))
	ext := "json"
	if format == "mihomo" {
		ext = "yaml"
	}
	if format == "shadowrocket-config" {
		ext = "conf"
	}
	if format == "shadowrocket" || format == "base64" {
		ext = "txt"
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\"starhub."+ext+"\"; filename*=UTF-8''"+strings.ReplaceAll(url.QueryEscape(name+"."+ext), "+", "%20"))

	if format != "sing-box" && format != "mihomo" && format != "clash" && format != "shadowrocket" && format != "base64" && format != "xray" && format != "naive" && format != "shadowrocket-config" {
		return bad("unsupported client format")
	}
	if quotaReached {
		return sendBlockedClient(w, format, profile.Port)
	}
	if user != "" {
		pending, err := s.subscriptionPending(r.Context(), user, device)
		if err != nil {
			return err
		}
		if pending > 0 {
			w.Header().Set("Retry-After", "3")
			w.Header().Set("X-Starhub-Pending-Nodes", fmt.Sprint(pending))
			return problem{503, "用户权限或客户端身份正在同步至主机，请稍后更新同一订阅链接；无需重新创建用户或订阅"}
		}
	}
	rows, e := s.DB.Query(r.Context(), `SELECT l.id,l.name,l.host_id,l.sealed FROM live_services l WHERE l.tenant_id=$1 AND (($3='' AND EXISTS(SELECT 1 FROM subscription_services x WHERE x.tenant_id=l.tenant_id AND x.service_id=l.id AND x.subscription_id=$2)) OR ($3<>'' AND EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=$3))) AND NOT EXISTS (WITH RECURSIVE chain AS (SELECT n.id,n.next_id,n.enabled,n.state,n.version,n.reported_version,n.reported_at,1 depth FROM live_services n WHERE n.tenant_id=l.tenant_id AND n.id=l.next_id UNION ALL SELECT n.id,n.next_id,n.enabled,n.state,n.version,n.reported_version,n.reported_at,c.depth+1 FROM live_services n JOIN chain c ON n.id=c.next_id WHERE n.tenant_id=l.tenant_id AND c.depth<8) SELECT 1 FROM chain WHERE NOT enabled OR state<>'running' OR version<>reported_version OR reported_at IS NULL OR reported_at<now()-interval '45 seconds') AND l.enabled AND l.state='running' AND l.version=l.reported_version AND l.reported_at>now()-interval '45 seconds' ORDER BY l.created_at LIMIT 64`, tenant, id, user)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []any{}
	xray := []any{}
	clash := []any{}
	tags := []string{}
	uris := []string{}
	naiveMap := ""
	for rows.Next() {
		var service, name, hostID string
		var sealed []byte
		if e = rows.Scan(&service, &name, &hostID, &sealed); e != nil {
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
		if device != "" && (!spec.ManagedUsers || !deviceAll && !slices.Contains(deviceServices, service)) {
			excluded++
			continue
		}
		if !live.Proxy(spec.Kind) {
			continue
		}
		if spec.ManagedUsers {
			if user == "" {
				return problem{403, "this inbound requires a user subscription"}
			}
			spec.UUID = account.UUID
			spec.Username = account.Name
			spec.Password = account.Password
		} else if user != "" && !sharedGroupProtocol(spec) {
			return problem{403, "user group references an unmanaged inbound"}
		}
		spec.StripServerSecrets()
		addresses := spec.EndpointAddresses
		if len(addresses) == 0 {
			addresses = []string{spec.Address}
		}
		for _, address := range addresses {
			endpoint := spec
			endpoint.Address = address
			if format == "naive" && net.ParseIP(address) != nil && address != spec.ServerName {
				target := address
				if strings.Contains(address, ":") {
					target = "[" + address + "]"
				}
				naiveMap = "MAP " + spec.ServerName + " " + target
			}
			tag := name + "-" + service[:8]
			if len(addresses) > 1 {
				tag += "-" + policyFamily(address)
			}
			checkFormat := format
			if format == "shadowrocket-config" {
				checkFormat = "shadowrocket"
			}
			if slices.Contains([]string{"v2rayng-uri", "happ", "v2raytun"}, client.ID) {
				checkFormat = "xray"
			}
			if !client.Full && (endpoint.IsReality() && endpoint.Engine == "xray" || endpoint.Transport == "xhttp" || endpoint.Transport == "kcp" || endpoint.Transport == "httpupgrade") {
				excluded++
				continue
			}
			if err := endpoint.ClientFormat(checkFormat); err != nil {
				excluded++
				continue
			}
			sb, mh, uri, err := endpoint.Client(tag)
			if err != nil {
				return err
			}
			if uri == "" && !client.Full {
				excluded++
				continue
			}
			if format == "xray" {
				xray = append(xray, endpoint.XrayClient(tag))
			}
			tags = append(tags, tag)
			out = append(out, sb)
			clash = append(clash, mh)
			uris = append(uris, uri)
			nodes = append(nodes, clientpolicy.Node{ID: service, Host: hostID, Name: name, Protocol: spec.Kind, Family: policyFamily(address), Tag: tag})
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if len(out) == 0 {
		if excluded > 0 {
			return problem{422, "所选客户端不兼容已授权的节点协议，请选择兼容客户端"}
		}
		return problem{503, "没有可发布的运行中节点，请检查用户组授权、节点启用状态、部署结果与 Agent 在线状态"}
	}
	switch format {
	case "sing-box", "":
		group := map[string]any{"type": "selector", "tag": "proxy", "outbounds": tags, "default": tags[0]}
		if profile.Selection == "urltest" {
			group = map[string]any{"type": "urltest", "tag": "proxy", "outbounds": tags, "url": profile.TestURL, "interval": "5m"}
		}
		out = append(out, group)
		out = append(out, map[string]any{"type": "direct", "tag": "direct"})
		config, err := apply(map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": profile.Port}}, "outbounds": out, "dns": map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}}, "route": map[string]any{"final": "proxy"}})
		if err != nil {
			return err
		}
		send(w, 200, config)
	case "xray":
		if profile.Selection == "urltest" {
			return problem{422, "Xray JSON template uses the first outbound; select a manual client template"}
		}
		xray = append(xray, map[string]any{"protocol": "freedom", "tag": "direct"}, map[string]any{"protocol": "blackhole", "tag": "block"})
		config, err := apply(map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "local", "listen": "127.0.0.1", "port": profile.Port, "protocol": "socks", "settings": map[string]any{"udp": true}}}, "outbounds": xray, "routing": map[string]any{"rules": []any{}}})
		if err != nil {
			return err
		}
		send(w, 200, config)
	case "clash", "mihomo":
		group := map[string]any{"name": "PROXY", "type": "select", "proxies": tags}
		if profile.Selection == "urltest" {
			group["type"] = "url-test"
			group["url"] = profile.TestURL
			group["interval"] = 300
		}
		config, err := apply(map[string]any{"mixed-port": profile.Port, "allow-lan": false, "ipv6": true, "proxies": clash, "proxy-groups": []any{group}, "rules": []string{"MATCH,PROXY"}})
		if err != nil {
			return err
		}
		raw, err := yaml.Marshal(config)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(200)
		_, err = w.Write(raw)
		if err != nil {
			return err
		}
	case "naive":
		policy, _, _, _, err := s.selectedPolicy(r, user, client.ID)
		if err != nil {
			return err
		}
		if policy != nil && r.URL.Query().Get("policy") != "nodes-only" {
			return problem{422, "NaiveProxy does not support routing groups; add policy=nodes-only"}
		}
		if len(uris) != 1 {
			return problem{422, "NaiveProxy requires exactly one authorized compatible node"}
		}
		w.Header().Set("X-Starhub-Excluded-Nodes", fmt.Sprint(excluded))
		config := map[string]any{"listen": fmt.Sprintf("socks://127.0.0.1:%d", profile.Port), "proxy": uris[0]}
		if naiveMap != "" {
			config["host-resolver-rules"] = naiveMap
		}
		send(w, 200, config)
	case "shadowrocket-config":
		p, bindings, native, _, err := s.selectedPolicy(r, user, client.ID)
		if err != nil {
			return err
		}
		if p == nil {
			p = &clientpolicy.Policy{Groups: []clientpolicy.Group{{Name: "PROXY", Type: "select", Proxies: []string{"ALL"}}}, Rules: []string{"MATCH,PROXY"}}
		}
		resolved, err := p.Resolve(nodes, bindings)
		if err != nil {
			return problem{422, err.Error()}
		}
		config, notes, err := resolved.Shadowrocket()
		if err != nil {
			return problem{422, err.Error()}
		}
		raw := native[format]
		if len(extra) > 0 {
			raw = extra
		}
		config, err = shadowrocketOptions(config, raw)
		if err != nil {
			return problem{422, err.Error()}
		}
		w.Header().Set("X-Starhub-Policy-Warnings", fmt.Sprint(len(notes)))
		w.Header().Set("X-Starhub-Excluded-Nodes", fmt.Sprint(excluded))
		w.Header().Set("X-Starhub-Companion-Client", "shadowrocket")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, config)
	case "base64", "shadowrocket":
		policy, _, _, _, err := s.selectedPolicy(r, user, client.ID)
		if err != nil {
			return err
		}
		if policy != nil && r.URL.Query().Get("client") != "" && r.URL.Query().Get("policy") != "nodes-only" {
			return problem{422, "URI 节点列表不能携带策略规则；请选择完整配置，或明确添加 policy=nodes-only"}
		}
		w.Header().Set("X-Starhub-Excluded-Nodes", fmt.Sprint(excluded))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte(strings.Join(uris, "\n"))))
	default:
		return bad("format must be sing-box, mihomo, shadowrocket, clash or base64")
	}
	return nil
}
