package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"vps-control/internal/live"
)

type inspectedInbound struct {
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	Warnings  []string       `json:"warnings"`
	Supported bool           `json:"supported"`
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func text(v any) string { s, _ := v.(string); return s }
func pemText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if a, ok := v.([]any); ok {
		p := []string{}
		for _, v := range a {
			p = append(p, text(v))
		}
		return strings.Join(p, "\n")
	}
	return ""
}
func inspectInbound(engine string, m map[string]any) inspectedInbound {
	kind := text(m["type"])
	if engine == "xray" {
		kind = text(m["protocol"])
	}
	in := map[string]any{"engine": engine, "kind": kind, "listen": m["listen"], "port": m["listen_port"], "managed_users": false, "transport": "tcp", "tls_mode": "none"}
	if in["listen"] == nil {
		in["listen"] = "127.0.0.1"
	}
	if engine == "xray" {
		in["port"] = m["port"]
	}
	r := inspectedInbound{Name: text(m["tag"]), Input: in, Warnings: []string{}, Supported: live.EngineSupports(engine, kind)}
	if !r.Supported {
		r.Warnings = append(r.Warnings, "该内核协议尚无本平台受控适配；不会原样执行外部配置")
		return r
	}
	settings := m
	if engine == "xray" {
		settings = object(m["settings"])
	}
	users, _ := settings["users"].([]any)
	if len(users) == 0 {
		users, _ = settings["clients"].([]any)
	}
	if len(users) == 0 {
		users, _ = settings["accounts"].([]any)
	}
	if len(users) > 1 {
		r.Warnings = append(r.Warnings, "包含多个账号；仅识别首个模板身份。其余账号请迁移到用户/用户组，未自动授权")
	}
	u := map[string]any{}
	if len(users) > 0 {
		u = object(users[0])
	}
	for dst, keys := range map[string][]string{"password": {"password", "pass", "auth_str"}, "uuid": {"uuid", "id"}, "username": {"username", "user"}, "flow": {"flow"}} {
		for _, key := range keys {
			if v := text(u[key]); v != "" {
				in[dst] = v
				break
			}
		}
	}
	if kind == "shadowsocks" {
		in["cipher"] = settings["method"]
		in["password"] = settings["password"]
		if network := text(settings["network"]); network != "" {
			in["listen_network"] = network
		}
	}
	if kind == "snell" {
		in["password"] = settings["psk"]
		in["snell_version"] = settings["version"]
		in["snell_obfs"] = settings["obfs_mode"]
		in["snell_mode"] = settings["mode"]
	}
	for _, key := range []string{"up_mbps", "down_mbps"} {
		if v, ok := settings[key]; ok {
			in[key] = v
		}
	}
	if kind == "hysteria" {
		in["hysteria_obfs_password"] = text(settings["obfs"])
	}
	if kind == "hysteria2" {
		in["hy2_obfs_password"] = object(settings["obfs"])["password"]
	}
	if kind == "tuic" {
		in["congestion"] = settings["congestion_control"]
	}
	tls := object(m["tls"])
	transport := object(m["transport"])
	if engine == "xray" {
		stream := object(m["streamSettings"])
		in["transport"] = stream["network"]
		if in["transport"] == nil || in["transport"] == "raw" {
			in["transport"] = "tcp"
		}
		in["tls_mode"] = stream["security"]
		if in["tls_mode"] == nil {
			in["tls_mode"] = "none"
		}
		tls = object(stream["tlsSettings"])
		if in["tls_mode"] == "reality" {
			r.Warnings = append(r.Warnings, "REALITY 密钥不复用，保存时会生成新身份；请在表单设置握手目标/SNI 并更新客户端订阅")
		}
		tr := text(in["transport"])
		cfg := object(stream[map[string]string{"ws": "wsSettings", "grpc": "grpcSettings", "httpupgrade": "httpupgradeSettings", "xhttp": "xhttpSettings"}[tr]])
		in["path"] = cfg["path"]
		in["service_name"] = cfg["serviceName"]
		in["ws_host"] = cfg["host"]
		if tr == "ws" {
			in["ws_host"] = object(cfg["headers"])["Host"]
		}
		in["server_name"] = tls["serverName"]
		in["alpn"] = tls["alpn"]
		certs, _ := tls["certificates"].([]any)
		if len(certs) > 0 {
			c := object(certs[0])
			in["certificate"] = pemText(c["certificate"])
			in["private_key"] = pemText(c["key"])
			if c["certificateFile"] != nil || c["keyFile"] != nil {
				r.Warnings = append(r.Warnings, "证书文件路径不会读取；请选择受管证书或粘贴 PEM")
			}
		}
	} else {
		if tr := text(transport["type"]); tr != "" {
			in["transport"] = tr
			in["path"] = transport["path"]
			in["service_name"] = transport["service_name"]
			in["ws_host"] = object(transport["headers"])["Host"]
			if h := text(transport["host"]); h != "" {
				in["ws_host"] = h
			}
		}
		if tls["enabled"] == true {
			in["tls_mode"] = "tls"
		}
		in["server_name"] = tls["server_name"]
		in["alpn"] = tls["alpn"]
		in["certificate"] = pemText(tls["certificate"])
		in["private_key"] = pemText(tls["key"])
		if tls["certificate_path"] != nil || tls["key_path"] != nil {
			r.Warnings = append(r.Warnings, "证书路径未导入；请选择受管证书或粘贴 PEM")
		}
		if object(tls["reality"])["enabled"] == true {
			in["tls_mode"] = "reality"
			r.Warnings = append(r.Warnings, "REALITY 身份不复用；请填写握手目标，保存后更新客户端订阅")
		}
	}
	if in["tls_mode"] == "reality" {
		delete(in, "certificate")
		delete(in, "private_key")
	}
	if slices.Contains([]string{"hysteria", "hysteria2", "tuic"}, kind) {
		in["transport"] = "quic"
		in["tls_mode"] = "tls"
	}
	if r.Name != "" {
		in["name"] = r.Name
	}
	for key, v := range in {
		if v == nil || v == "" {
			delete(in, key)
		}
	}
	r.Warnings = append(r.Warnings, "这是字段识别草稿，不会自动部署。路由、外部文件、多账号、插件与未识别高级字段未迁移；请逐项核对。")
	return r
}
func (s *Server) inspectConfiguration(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Source string `json:"source"`
		Format string `json:"format"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	raw, e := parseClientText(b.Source)
	if e != nil {
		return e
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return bad("configuration must be a JSON/YAML object")
	}
	engine := b.Format
	if engine == "" || engine == "auto" {
		if _, ok := root["proxy-groups"]; ok {
			engine = "mihomo"
		}
		if rows, ok := root["inbounds"].([]any); ok && len(rows) > 0 {
			if object(rows[0])["type"] != nil {
				engine = "sing-box"
			} else if object(rows[0])["protocol"] != nil {
				engine = "xray"
			}
		}
		if root["kind"] != nil {
			engine = "starhub"
		}
	}
	if engine == "mihomo" {
		send(w, 200, map[string]any{"format": engine, "candidates": []any{}, "warnings": []string{"识别为客户端配置；策略组/规则请在客户端策略页面导入，服务器入站不能由客户端出站配置直接替代"}})
		return nil
	}
	if engine == "starhub" {
		var input liveInput
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.DisallowUnknownFields()
		if e = d.Decode(&input); e != nil {
			return bad("Starhub parameter JSON has unknown fields or invalid types")
		}
		send(w, 200, map[string]any{"format": engine, "candidates": []any{map[string]any{"name": input.Name, "input": root, "supported": live.EngineSupports(input.Engine, input.Kind), "warnings": []string{"请检查主机、凭据和必填项，保存时执行完整语义校验"}}}})
		return nil
	}
	if engine != "sing-box" && engine != "xray" {
		return bad("cannot identify configuration; choose sing-box/Xray or Starhub parameter JSON")
	}
	rows, ok := root["inbounds"].([]any)
	if !ok || len(rows) == 0 || len(rows) > 64 {
		return bad("native server config requires 1-64 inbounds")
	}
	candidates := []inspectedInbound{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return bad("inbound must be an object")
		}
		candidates = append(candidates, inspectInbound(engine, m))
	}
	send(w, 200, map[string]any{"format": engine, "candidates": candidates, "warnings": []string{fmt.Sprintf("识别 %d 个入站；不会读取配置中的文件路径或执行命令", len(candidates))}})
	return nil
}
