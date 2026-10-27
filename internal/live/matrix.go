package live

import (
	"slices"
	"strconv"
	"strings"
)

// These are platform adapters, not a claim that every feature of a core is exposed.
// The same matrix drives API validation and the protocol/transport/security editor.
type ProtocolLayer struct {
	Transport string   `json:"transport"`
	Security  []string `json:"security_modes"`
}

func Layers(engine, kind string) []ProtocolLayer {
	if !EngineSupports(engine, kind) {
		return nil
	}
	switch kind {
	case "vless", "vmess", "trojan":
		result := []ProtocolLayer{}
		transports := []string{"tcp", "ws", "grpc", "httpupgrade"}
		if engine == "xray" {
			transports = append(transports, "xhttp", "kcp")
		} else {
			transports = append(transports, "http")
		}
		for _, tr := range transports {
			modes := []string{"tls"}
			if kind == "vmess" {
				modes = append(modes, "none")
			}
			if tr == "kcp" {
				if kind != "vmess" {
					continue
				}
				modes = []string{"none"}
			}
			if (kind == "vless" && tr == "tcp") || engine == "xray" && (kind == "vless" || kind == "trojan" || kind == "vmess") && (tr == "tcp" || tr == "grpc" || tr == "xhttp") {
				modes = append(modes, "reality")
			}
			result = append(result, ProtocolLayer{tr, modes})
		}
		return result
	case "http":
		return []ProtocolLayer{{"tcp", []string{"none", "tls"}}}
	case "socks", "mixed", "shadowsocks", "snell":
		return []ProtocolLayer{{"tcp", []string{"none"}}}
	case "hysteria", "hysteria2", "tuic":
		return []ProtocolLayer{{"quic", []string{"tls"}}}
	case "naive", "anytls":
		return []ProtocolLayer{{"tcp", []string{"tls"}}}
	}
	return nil
}

func (s Spec) validLayer() bool {
	tr := s.Transport
	if tr == "" {
		tr = "tcp"
	}
	if tr == "tcp" && slices.Contains([]string{"hysteria", "hysteria2", "tuic"}, s.Kind) {
		tr = "quic"
	}
	security := s.TLSMode
	if security == "" {
		security = "none"
		if TLSProxy(s.Kind) && s.Kind != "http" {
			security = "tls"
		}
	}
	for _, layer := range Layers(s.Engine, s.Kind) {
		if layer.Transport == tr && slices.Contains(layer.Security, security) {
			return true
		}
	}
	return !Proxy(s.Kind)
}

func (s Spec) RequiresV13() bool {
	return s.Cipher == "2022-blake3-aes-128-gcm" || s.Cipher == "2022-blake3-chacha20-poly1305" || s.Outbound != nil && s.Outbound.RequiresV13() || BasicProxy(s.Kind) || s.Kind == "hysteria" || s.Kind == "naive" || slices.Contains([]string{"http", "httpupgrade", "xhttp", "kcp", "quic"}, s.Transport) || s.IsReality() && (s.Transport == "grpc" || s.Kind != "vless")
}
func V13Capable(version string) bool {
	if !ProtocolCapable(version) {
		return false
	}
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major > 0 || minor >= 13
}
func BasicProxy(kind string) bool { return kind == "http" || kind == "socks" || kind == "mixed" }
