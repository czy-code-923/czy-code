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
	return engine == "xray" && (kind == "shadowsocks" || kind == "vless" || kind == "trojan" || kind == "vmess")
}
func (s Spec) UsesTLS() bool {
	return TLSProxy(s.Kind) && !(s.Kind == "vmess" && s.TLSMode == "none") && !s.IsReality()
}
func (s Spec) validateRouting() error {
	if e := s.validateSecurity(false); e != nil {
		return e
	}
	if s.TLSMode != "" && s.TLSMode != "tls" && s.TLSMode != "reality" && !(s.Kind == "vmess" && s.TLSMode == "none") {
		return errors.New("unsupported transport security")
	}
	if s.Transport != "" && s.Transport != "tcp" && s.Transport != "ws" && s.Transport != "grpc" {
		return errors.New("supported transports: tcp, ws")
	}
	if s.Transport == "ws" || s.Transport == "grpc" {
		if s.Kind != "vless" && s.Kind != "vmess" && s.Kind != "trojan" {
			return errors.New("WebSocket not supported for this protocol")
		}
		if s.Transport == "ws" && (!strings.HasPrefix(s.Path, "/") || len(s.Path) > 200 || strings.ContainsAny(s.Path, "\r\n\t #?\\")) {
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
		if e := next.validateClient(); e != nil {
			return e
		}
		if s.Engine != "xray" && next.Engine == "xray" && next.IsReality() {
			return errors.New("pinned sing-box 1.14.3 cannot dial Xray 26.10.21 REALITY; choose a sing-box REALITY next hop")
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
		settings["servers"] = []any{map[string]any{"address": s.Address, "port": s.Port, "method": s.SSMethod(), "password": s.Password}}
	case "trojan":
		settings["servers"] = []any{map[string]any{"address": s.Address, "port": s.Port, "password": s.Password}}
	default:
		user["id"] = s.UUID
		if s.Kind == "vmess" {
			user["security"] = "aes-128-gcm"
		} else {
			user["encryption"] = "none"
			if s.Flow != "" {
				user["flow"] = s.Flow
			}
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
	if s.Transport == "grpc" {
		stream["network"] = "grpc"
		stream["grpcSettings"] = map[string]any{"serviceName": s.ServiceName}
	}
	if s.WSHost != "" {
		stream["wsSettings"].(map[string]any)["headers"] = map[string]any{"Host": s.WSHost}
	}
	if s.IsReality() {
		stream["security"] = "reality"
		stream["realitySettings"] = s.realityStream(server)
	}
	if s.UsesTLS() {
		stream["security"] = "tls"
		tls := map[string]any{"serverName": s.ServerName}
		if len(s.ALPN) > 0 {
			tls["alpn"] = s.ALPN
		}
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
	if s.Flow != "" {
		user["flow"] = s.Flow
	}
	if s.Kind == "shadowsocks" {
		settings["method"] = s.SSMethod()
		settings["password"] = s.Password
		settings["network"] = "tcp,udp"
	} else {
		settings["users"] = []any{user}
	}
	if s.ManagedUsers {
		users := []any{}
		for _, a := range s.Accounts {
			u := map[string]any{"email": a.Name}
			if s.Kind == "vless" || s.Kind == "vmess" {
				u["id"] = a.UUID
				if s.Flow != "" {
					u["flow"] = s.Flow
				}
			} else {
				u["password"] = a.Password
			}
			users = append(users, u)
		}
		settings["users"] = users
	}
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
	return s.Kind == "vmess" || s.Kind == "anytls" || (s.Transport == "ws" || s.Transport == "grpc" || s.IsReality() || s.ManagedUsers) || s.Outbound != nil || s.TLSMode == "none"
}

// An outbound needs the peer public identity, never its TLS private key.
func (s Spec) validateClient() error {
	if !Proxy(s.Kind) || !Address(s.Address) || s.Port < 1024 || s.Port > 65535 || s.PrivateKey != "" {
		return errors.New("invalid client endpoint or leaked server private key")
	}
	if e := s.validateSecurity(true); e != nil {
		return e
	}
	check := s
	check.Reality = nil
	if s.IsReality() {
		check.TLSMode = "tls"
	}
	check.Flow = ""
	if e := check.validateRouting(); e != nil {
		return e
	}
	if s.UsesTLS() && !Address(s.ServerName) {
		return errors.New("TLS server name required")
	}

	// Credential validation is independent of transport certificate key ownership.
	if e := s.validateCredential(); e != nil {
		return e
	}
	if (s.Kind == "vless" || s.Kind == "vmess" || s.Kind == "tuic") && !uuidPattern.MatchString(s.UUID) {
		return errors.New("invalid endpoint UUID")
	}
	return nil
}
