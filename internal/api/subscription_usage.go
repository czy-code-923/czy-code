package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"
	"vps-control/internal/live"
)

func sendBlockedClient(w http.ResponseWriter, format string, port int) error {
	switch format {
	case "mihomo", "clash":
		config := map[string]any{"mixed-port": port, "allow-lan": false, "proxies": []any{}, "proxy-groups": []any{}, "rules": []string{"MATCH,REJECT"}}
		raw, e := yaml.Marshal(config)
		if e != nil {
			return e
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, e = w.Write(raw)
		return e
	case "shadowrocket-config":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "[General]\n[Rule]\nFINAL,REJECT\n")
	case "sing-box":
		send(w, 200, map[string]any{"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "route": map[string]any{"rules": []any{map[string]any{"action": "reject"}}}})
	case "xray":
		send(w, 200, map[string]any{"outbounds": []any{map[string]any{"protocol": "blackhole", "tag": "quota-exhausted"}}})
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "")
	}
	return nil
}

type subscriptionUsage struct {
	Upload    int64      `json:"upload_bytes"`
	Download  int64      `json:"download_bytes"`
	Total     int64      `json:"quota_bytes"`
	Used      int64      `json:"used_bytes"`
	Remaining *int64     `json:"remaining_bytes"`
	Mode      string     `json:"mode"`
	UserID    string     `json:"user_id"`
	NextReset *time.Time `json:"next_reset_at"`
}

func (s *Server) subscriptionUsage(r *http.Request, id, user string) (subscriptionUsage, error) {
	u := subscriptionUsage{Mode: "shared", UserID: user}
	if user != "" {
		u.Mode = "user"
		e := s.DB.QueryRow(r.Context(), `SELECT upload_bytes,download_bytes,quota_bytes,used_bytes,next_reset_at FROM proxy_users WHERE tenant_id=$1 AND id=$2`, tenant, user).Scan(&u.Upload, &u.Download, &u.Total, &u.Used, &u.NextReset)
		if e != nil {
			return u, e
		}
		if u.Total > 0 {
			remaining := max(int64(0), u.Total-u.Used)
			u.Remaining = &remaining
		}
	} else {
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(sum(c.uplink),0),COALESCE(sum(c.downlink),0) FROM traffic_cursors c WHERE tenant_id=$1 AND subject_kind='service' AND EXISTS(SELECT 1 FROM subscription_services ss WHERE ss.tenant_id=c.tenant_id AND ss.subscription_id=$2 AND ss.service_id=c.subject_id)`, tenant, id).Scan(&u.Upload, &u.Download)
		if e != nil {
			return u, e
		}
		u.Used = u.Upload + u.Download
	}
	return u, nil
}
func (s *Server) inspectSubscription(w http.ResponseWriter, r *http.Request) error {
	if view := r.PathValue("view"); view != "usage" && view != "config" && view != "check" {
		return problem{404, "unknown subscription view"}
	}
	id := r.PathValue("id")
	var name, user string
	var expiry time.Time
	var sealed []byte
	e := s.DB.QueryRow(r.Context(), `SELECT name,COALESCE(user_id,''),expires_at,sealed_token FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL`, tenant, id).Scan(&name, &user, &expiry, &sealed)
	if e != nil {
		return e
	}
	usage, e := s.subscriptionUsage(r, id, user)
	if e != nil {
		return e
	}
	if r.PathValue("view") == "usage" {
		modeNotice := "按所选入口累计；未绑定用户额度。需要个人用量与剩余额度时，请编辑订阅并绑定用户。"
		if user != "" {
			modeNotice = "按独立用户身份计量；Clash 的订阅卡片在更新订阅时读取响应头，不是实时测速。"
		}
		send(w, 200, map[string]any{"name": name, "expires_at": expiry, "usage": usage, "notice": modeNotice})
		return nil
	}
	if len(sealed) == 0 {
		return problem{409, "legacy link must be rotated before preview"}
	}
	token, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return e
	}
	origin, e := s.publicOrigin(r)
	if e != nil {
		return e
	}
	query := url.Values{}
	if format := r.URL.Query().Get("format"); format != "" && format != "auto" {
		query.Set("format", format)
	}
	for _, key := range []string{"client", "policy"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	preview := httptest.NewRequest("GET", origin+"/s/"+string(token)+"?"+query.Encode(), nil).WithContext(r.Context())
	preview.SetPathValue("token", string(token))
	preview.Header.Set("User-Agent", r.Header.Get("User-Agent"))
	recorder := httptest.NewRecorder()
	if e = s.serveSubscription(recorder, preview); e != nil {
		if r.PathValue("view") == "check" {
			var p problem
			if !errors.As(e, &p) {
				return e
			}
			stage := "configuration"
			switch p.status {
			case 404:
				stage = "subscription"
			case 403:
				stage = "authorization"
			case 503:
				stage = "nodes"
			case 422:
				stage = "compatibility"
			}
			send(w, 200, map[string]any{"ready": false, "stage": stage, "status": p.status, "reason": p.message})
			return nil
		}
		return e
	}
	if r.PathValue("view") == "check" {
		ready := usage.Remaining == nil || *usage.Remaining > 0
		reason, stage := "订阅已生成，可导入所选客户端；客户端本机合并配置需另行核对", "ready"
		if !ready {
			reason, stage = "用户流量额度已用尽，当前输出为拒绝连接配置", "quota"
		}
		send(w, 200, map[string]any{"ready": ready, "stage": stage, "status": 200, "reason": reason, "content_type": recorder.Header().Get("Content-Type"), "excluded_nodes": recorder.Header().Get("X-Starhub-Excluded-Nodes"), "warnings": recorder.Header().Get("X-Starhub-Policy-Warnings")})
		return nil
	}
	var config any
	if query.Get("client") == "shadowrocket-config" && strings.Contains(recorder.Body.String(), "[Rule]") {
		config = recorder.Body.String()
	}

	content := recorder.Header().Get("Content-Type")
	if config != nil {
		// Shadowrocket routing text has no embedded node identities.
	} else if content == "application/yaml; charset=utf-8" {
		e = yaml.Unmarshal(recorder.Body.Bytes(), &config)
	} else {
		e = json.Unmarshal(recorder.Body.Bytes(), &config)
	}
	if config == nil && e != nil {
		config = "URI 订阅包含凭据，请通过下载链接导入客户端。"
	} else {
		config = redactLive(config)
	}
	headers := map[string]string{}
	for _, key := range []string{"Subscription-Userinfo", "Profile-Title", "Profile-Update-Interval", "Content-Type", "Content-Disposition", "X-Starhub-Policy-Warnings", "X-Starhub-Excluded-Nodes"} {
		headers[key] = recorder.Header().Get(key)
	}
	send(w, 200, map[string]any{"configuration": config, "redacted": true, "headers": headers, "usage": usage})
	return nil
}
