package api

import "net/http"

func (s *Server) protocolTemplates(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, []map[string]any{
		{"kind": "vless", "name": "VLESS / TCP / TLS", "engines": []string{"sing-box", "xray"}, "network": "tcp", "certificate": true},
		{"kind": "hysteria2", "name": "Hysteria2 / QUIC / TLS", "engines": []string{"sing-box"}, "network": "udp", "certificate": true},
		{"kind": "tuic", "name": "TUIC v5 / QUIC / TLS", "engines": []string{"sing-box"}, "network": "udp", "certificate": true},
		{"kind": "trojan", "name": "Trojan / TCP / TLS", "engines": []string{"sing-box", "xray"}, "network": "tcp", "certificate": true},
		{"kind": "snell", "name": "Snell v4 兼容（sing-box v5 入站）", "engines": []string{"sing-box"}, "network": "tcp,udp", "certificate": false, "clients": []string{"sing-box", "mihomo"}},
	})
	return nil
}
