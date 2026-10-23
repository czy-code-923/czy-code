package live

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func EngineSupports(engine, kind string) bool {
	if engine == "" || engine == "sing-box" {
		return Proxy(kind) || kind == "frps" || kind == "frpc"
	}
	return engine == "xray" && (kind == "vless" || kind == "trojan" || kind == "vmess")
}
func (s Spec) UsesTLS() bool { return TLSProxy(s.Kind) && !(s.Kind == "vmess" && s.TLSMode == "none") }
func (s Spec) validateRouting() error {
	if s.TLSMode != "" && s.TLSMode != "tls" && !(s.Kind == "vmess" && s.TLSMode == "none") {
		return errors.New("unsupported transport security")
	}
	if s.Transport != "" && s.Transport != "tcp" && s.Transport != "ws" {
		return errors.New("supported transports: tcp, ws")
	}
	if s.Transport == "ws" {
		if s.Kind != "vless" && s.Kind != "vmess" && s.Kind != "trojan" {
			return errors.New("WebSocket not supported for this protocol")
		}
		if !strings.HasPrefix(s.Path, "/") || len(s.Path) > 200 || strings.ContainsAny(s.Path, "\r\n\t #?\\") {
			return errors.New("valid WebSocket path required")
		}
	} else if s.Path != "" {
		return errors.New("path only valid for WebSocket")
	}
	if s.Outbound != nil {
		next := *s.Outbound
		if !Proxy(s.Kind) || !Proxy(next.Kind) || next.Outbound != nil || next.Peer != nil || s.Peer != nil {
			return errors.New("invalid referenced outbound")
		}
		if e := next.Validate(); e != nil {
			return e
		}
		if s.Engine == "xray" && next.Kind != "shadowsocks" && next.Kind != "vless" && next.Kind != "vmess" && next.Kind != "trojan" {
			return errors.New("selected Xray outbound protocol unsupported")
		}
	}
	return nil
}
func (s Spec) xrayOutbound(tag string) map[string]any {
	user := map[string]any{}
	settings := map[string]any{}
	switch s.Kind {
	case "shadowsocks":
		settings["servers"] = []any{map[string]any{"address": s.Address, "port": s.Port, "method": Method, "password": s.Password}}
	case "trojan":
		settings["servers"] = []any{map[string]any{"address": s.Address, "port": s.Port, "password": s.Password}}
	default:
		user["id"] = s.UUID
		if s.Kind == "vmess" {
			user["security"] = "aes-128-gcm"
		} else {
			user["encryption"] = "none"
		}
		settings["vnext"] = []any{map[string]any{"address": s.Address, "port": s.Port, "users": []any{user}}}
	}
	return map[string]any{"tag": tag, "protocol": s.Kind, "settings": settings, "streamSettings": s.xrayStream(false)}
}
func (s Spec) xrayStream(server bool) map[string]any {
	stream := map[string]any{"network": "tcp", "security": "none"}
	if s.Transport == "ws" {
		stream["network"] = "ws"
		stream["wsSettings"] = map[string]any{"path": s.Path}
	}
	if s.UsesTLS() {
		stream["security"] = "tls"
		tls := map[string]any{"serverName": s.ServerName}
		if server {
			tls["certificates"] = []any{map[string]any{"certificate": strings.Split(strings.TrimSpace(s.Certificate), "\n"), "key": strings.Split(strings.TrimSpace(s.PrivateKey), "\n")}}
		} else {
			tls["allowInsecure"] = false
		}
		stream["tlsSettings"] = tls
	}
	return stream
}
func (s Spec) renderXray() (string, map[string][]byte, error) {
	user := map[string]any{"email": "harbor"}
	settings := map[string]any{}
	if s.Kind == "vless" || s.Kind == "vmess" {
		user["id"] = s.UUID
		if s.Kind == "vless" {
			settings["decryption"] = "none"
		}
	} else {
		user["password"] = s.Password
	}
	settings["users"] = []any{user}
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "entry", "listen": s.Listen, "port": s.Port, "protocol": s.Kind, "settings": settings, "streamSettings": s.xrayStream(true)}}, "outbounds": []any{map[string]any{"protocol": "freedom", "tag": "egress"}}}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	files := map[string][]byte{"config.json": raw}
	if s.UsesTLS() {
		files["tls.crt"] = []byte(s.Certificate)
		files["tls.key"] = []byte(s.PrivateKey)
	}
	if e == nil && s.Outbound != nil {
		e = s.patchOutbound("xray", files)
	}
	return "xray", files, e
}
func (s Spec) patchOutbound(engine string, files map[string][]byte) error {
	var cfg map[string]any
	if e := json.Unmarshal(files["config.json"], &cfg); e != nil {
		return e
	}
	if engine == "xray" {
		cfg["outbounds"] = []any{s.Outbound.xrayOutbound("egress")}
	} else {
		out, _, _, e := s.Outbound.Client("egress")
		if e != nil {
			return e
		}
		cfg["outbounds"] = []any{out}
	}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	files["config.json"] = raw
	return e
}
func AdvancedCapable(version string) bool {
	parts := strings.Split(version, ".")
	if !ProtocolCapable(version) {
		return false
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major > 0 || minor >= 7
}

func (s Spec) RequiresAdvanced() bool {
	return s.Kind == "vmess" || s.Kind == "anytls" || s.Transport == "ws" || s.Outbound != nil || s.TLSMode == "none"
}
