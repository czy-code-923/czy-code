package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"slices"
	"vps-control/internal/live"
)

//go:embed protocol_catalog.json
var protocolCatalog []byte

func (s *Server) protocolTemplates(w http.ResponseWriter, r *http.Request) error {
	templates := []map[string]any{
		{"kind": "shadowsocks", "name": "Shadowsocks AEAD / 2022", "engines": []string{"sing-box", "xray"}, "certificate": false},
		{"kind": "vless", "name": "VLESS", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "vmess", "name": "VMess AEAD", "engines": []string{"sing-box", "xray"}, "certificate": true, "optional_tls": true},
		{"kind": "trojan", "name": "Trojan", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "hysteria2", "name": "Hysteria2", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "tuic", "name": "TUIC v5", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "anytls", "name": "AnyTLS", "engines": []string{"sing-box"}, "certificate": true, "clients": []string{"sing-box", "mihomo"}},
		{"kind": "snell", "name": "Snell v4 compatible / v6", "engines": []string{"sing-box"}, "certificate": false, "clients": []string{"sing-box", "mihomo"}},
	}

	for _, kind := range []string{"http", "socks", "mixed", "hysteria", "naive"} {
		engines := []string{"sing-box"}
		if kind == "http" || kind == "socks" {
			engines = append(engines, "xray")
		}
		templates = append(templates, map[string]any{"kind": kind, "name": kind, "engines": engines, "certificate": kind == "hysteria" || kind == "naive" || kind == "http", "clients": []string{"sing-box", "mihomo", "shadowrocket"}})
	}
	for _, t := range templates {
		kind := t["kind"].(string)
		if _, ok := t["clients"]; !ok {
			t["clients"] = []string{"sing-box", "mihomo", "shadowrocket"}
			if kind == "vless" || kind == "vmess" || kind == "trojan" || kind == "shadowsocks" {
				t["clients"] = []string{"sing-box", "mihomo", "shadowrocket", "xray"}
			}
		}
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
			t["credential"] = "2022 uses a 32-byte base64 key; classic AEAD uses an 8-256 byte password"
			t["ciphers"] = []string{"2022-blake3-aes-256-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-chacha20-poly1305", "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305"}
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
			t["note"] = "REALITY TCP uses a generated X25519 keypair, not a CA certificate. Xray 26.10.21 REALITY exports only verified Xray JSON; sing-box 1.14.4 clients use sing-box REALITY."
		}
		if kind == "tuic" {
			t["transports"] = []string{"quic/udp"}
			t["optional"] = []string{"congestion", "managed_users"}
			t["credential"] = "UUID and password"
		}
		if kind == "snell" {
			t["optional"] = []string{"next_id", "snell_version", "snell_obfs", "snell_host", "snell_mode"}
			t["note"] = "Snell 5 server is v4 TCP/UDP compatible; v6 export is sing-box only. No v5 QUIC. New options require Agent 0.9+."
		}
		if kind == "anytls" {
			t["note"] = "TLS certificate and password required; default native padding and session settings. JSON clients only."
		}
		if kind == "hysteria2" {
			t["optional"] = []string{"next_id", "managed_users", "hy2_obfs_password", "up_mbps", "down_mbps"}
			t["transports"] = []string{"quic/udp"}
		}
	}

	for _, t := range templates {
		kind := t["kind"].(string)
		byCore := map[string]any{}
		transports := []string{}
		for _, core := range t["engines"].([]string) {
			byCore[core] = live.Layers(core, kind)
			for _, layer := range live.Layers(core, kind) {
				if !slices.Contains(transports, layer.Transport) {
					transports = append(transports, layer.Transport)
				}
			}
		}
		t["layers"] = byCore
		t["transports"] = transports
		if kind == "hysteria" {
			t["required_options"] = []string{"up_mbps", "down_mbps"}
			t["optional"] = []string{"hysteria_obfs_password", "managed_users", "next_id"}
			t["note"] = "Hysteria v1 over QUIC/TLS; both bandwidth values required"
		}
		if kind == "naive" {
			t["clients"] = []string{"naive-proxy"}
			t["note"] = "Naive HTTP/2 TLS; use dedicated NaiveProxy JSON and certificate domain; bundled core cannot dial Naive"
		}
		if live.BasicProxy(kind) {
			t["credential"] = "username and password; managed subscriptions use the stable user ID"
			if kind == "http" {
				t["optional_tls"] = true
			}
			t["security_modes"] = []string{"none"}
			if kind == "http" {
				t["security_modes"] = []string{"none", "tls"}
			}
			t["note"] = "Authenticated proxy; credentials required. Plain HTTP/SOCKS traffic is not encrypted."
		}
	}
	send(w, 200, templates)
	return nil
}
func (s *Server) engineCatalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, json.RawMessage(protocolCatalog))
	return nil
}
