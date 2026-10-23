package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

//go:embed protocol_catalog.json
var protocolCatalog []byte

func (s *Server) protocolTemplates(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, []map[string]any{
		{"kind": "shadowsocks", "name": "Shadowsocks 2022", "engines": []string{"sing-box"}, "certificate": false},
		{"kind": "vless", "name": "VLESS", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "vmess", "name": "VMess AEAD", "engines": []string{"sing-box", "xray"}, "certificate": true, "optional_tls": true},
		{"kind": "trojan", "name": "Trojan", "engines": []string{"sing-box", "xray"}, "certificate": true},
		{"kind": "hysteria2", "name": "Hysteria2", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "tuic", "name": "TUIC v5", "engines": []string{"sing-box"}, "certificate": true},
		{"kind": "anytls", "name": "AnyTLS", "engines": []string{"sing-box"}, "certificate": true, "clients": []string{"sing-box", "mihomo"}},
		{"kind": "snell", "name": "Snell v4 compatible", "engines": []string{"sing-box"}, "certificate": false, "clients": []string{"sing-box", "mihomo"}},
	})
	return nil
}
func (s *Server) engineCatalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, json.RawMessage(protocolCatalog))
	return nil
}
