package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

//go:embed protocol_catalog.json
var protocolCatalog []byte

func (s *Server) protocolTemplates(w http.ResponseWriter, r *http.Request) error {
	templates := []map[string]any{
		{"kind": "shadowsocks", "name": "Shadowsocks 2022", "engines": []string{"sing-box"}, "certificate": false},
		{"kind": "vless", "name": "VLESS", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "vmess", "name": "VMess AEAD", "engines": []string{"sing-box", "xray"}, "certificate": true, "optional_tls": true},
		{"kind": "trojan", "name": "Trojan", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "hysteria2", "name": "Hysteria2", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "tuic", "name": "TUIC v5", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "anytls", "name": "AnyTLS", "engines": []string{"sing-box"}, "certificate": true, "clients": []string{"sing-box", "mihomo"}},
		{"kind": "snell", "name": "Snell v4 compatible", "engines": []string{"sing-box"}, "certificate": false, "clients": []string{"sing-box", "mihomo"}},
	}
	for _, t := range templates {
		kind := t["kind"].(string)
		t["required"] = []string{"host_id", "engine", "name", "address", "listen", "port"}
		t["optional"] = []string{"node_id", "next_id"}
		t["transports"] = []string{"tcp"}
		t["security_modes"] = []string{}
		t["managed_users"] = kind != "snell" && kind != "shadowsocks"
		if kind == "vless" || kind == "vmess" || kind == "trojan" {
			t["transports"] = []string{"tcp", "ws", "grpc"}
			t["optional"] = []string{"node_id", "next_id", "ws_host", "alpn", "managed_users"}
		}
		if kind == "vless" || kind == "vmess" || kind == "tuic" {
			t["credential"] = "uuid (auto-generated or valid UUID)"
		} else {
			t["credential"] = "password (auto-generated or 8-256 bytes)"
		}
		if kind == "shadowsocks" {
			t["credential"] = "32-byte base64 key; fixed 2022-blake3-aes-256-gcm cipher"
		}
		if t["certificate"] == true {
			t["security_modes"] = []string{"tls"}
			t["tls_required"] = []string{"server_name", "certificate_id OR matching certificate+private_key PEM"}
		}
		if kind == "vmess" {
			t["security_modes"] = []string{"tls", "none"}
			t["note"] = "VMess AEAD itself does not require a certificate; outer TLS is optional"
		}
		if kind == "vless" {
			t["security_modes"] = []string{"tls", "reality"}
			t["reality_required"] = []string{"server_name", "reality.target", "reality.port"}
			t["reality_optional"] = []string{"reality.short_id", "reality.fingerprint", "flow"}
			t["note"] = "REALITY TCP uses a generated X25519 keypair, not a CA certificate. Xray 26.10.21 REALITY exports only verified Xray JSON; sing-box 1.14.3 clients use sing-box REALITY."
		}
		if kind == "tuic" {
			t["transports"] = []string{"quic/udp"}
			t["optional"] = []string{"congestion", "managed_users"}
			t["credential"] = "UUID and password"
		}
		if kind == "hysteria2" {
			t["transports"] = []string{"quic/udp"}
		}
	}
	send(w, 200, templates)
	return nil
}
func (s *Server) engineCatalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, json.RawMessage(protocolCatalog))
	return nil
}
