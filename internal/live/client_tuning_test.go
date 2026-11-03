package live

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHY2BandwidthDirectionsAndLegacyCompatibility(t *testing.T) {
	s := Spec{Kind: "hysteria2", Listen: "127.0.0.1", Address: "127.0.0.1", Port: 24000, Password: Secret(), ServerName: "localhost", ProtocolOptions: ProtocolOptions{UpMbps: 900, DownMbps: 200}}
	s.Certificate, s.PrivateKey, _ = Certificate("localhost")
	for _, mode := range []string{"", "bbr", "brutal"} {
		t.Run("mode="+mode, func(t *testing.T) {
			v := s
			v.HY2ClientMode = mode
			if mode == "brutal" {
				v.HY2ClientUpMbps = 40
				v.HY2ClientDownMbps = 500
			}
			sb, mh, _, err := v.Client("fixture")
			if err != nil {
				t.Fatal(err)
			}
			up, down := 900, 200
			if mode == "bbr" {
				up, down = 0, 0
			}
			if mode == "brutal" {
				up, down = 40, 500
			}
			if up == 0 {
				if sb["up_mbps"] != nil || sb["down_mbps"] != nil || mh["up"] != nil || mh["down"] != nil {
					t.Fatal("BBR export contains fixed bandwidth")
				}
			} else if sb["up_mbps"] != up || sb["down_mbps"] != down || mh["up"] != up || mh["down"] != down {
				t.Fatal("client directions or legacy values changed", sb, mh)
			}
			_, files, err := v.Render()
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			json.Unmarshal(files["config.json"], &cfg)
			in := cfg["inbounds"].([]any)[0].(map[string]any)
			if in["up_mbps"] != float64(900) || in["down_mbps"] != float64(200) || in["hy2_client_mode"] != nil {
				t.Fatal("client tuning changed the server limits", in)
			}
			chain := Spec{Kind: "trojan", Outbound: &v}.AgentSpec()
			if chain.Outbound.UpMbps != up || chain.Outbound.DownMbps != down || chain.Outbound.HY2ClientMode != "" {
				t.Fatal("old Agent chain wire did not normalize bandwidth")
			}
			if v.AgentSpec().ProtocolOptions.HY2ClientMode != "" || v.HY2ClientMode != mode {
				t.Fatal("client-only settings leaked to server work or original mutated")
			}
		})
	}
	s.HY2ClientMode = "brutal"
	s.HY2ClientUpMbps = 0
	s.HY2ClientDownMbps = 350
	sb, _, _, err := s.Client("one-direction")
	if err != nil || sb["up_mbps"] != nil || sb["down_mbps"] != 350 {
		t.Fatal("per-direction BBR fallback", err, sb)
	}
}

func TestProtocolClientTuningValidationAndFormats(t *testing.T) {
	invalid := []Spec{
		{Kind: "trojan", ProtocolOptions: ProtocolOptions{HY2ClientMode: "bbr"}},
		{Kind: "hysteria2", ProtocolOptions: ProtocolOptions{HY2ClientMode: "brutal"}},
		{Kind: "hysteria2", ProtocolOptions: ProtocolOptions{HY2ClientMode: "bbr", HY2ClientUpMbps: 100}},
		{Kind: "hysteria2", ProtocolOptions: ProtocolOptions{HY2ClientMode: "brutal", HY2ClientUpMbps: -1}},
		{Kind: "hysteria2", ProtocolOptions: ProtocolOptions{HY2BBRProfile: "unsafe"}},
		{Kind: "tuic", ProtocolOptions: ProtocolOptions{TUICHeartbeat: 301}},
		{Kind: "tuic", ProtocolOptions: ProtocolOptions{TUICRelayMode: "tcp"}},
		{Kind: "anytls", ProtocolOptions: ProtocolOptions{AnyTLSMinIdle: 33}},
		{Kind: "vless", ProtocolOptions: ProtocolOptions{AnyTLSIdleCheck: 30}},
	}
	for _, s := range invalid {
		if s.validateClientTuning() == nil {
			t.Fatalf("accepted invalid tuning: %+v", s.ProtocolOptions)
		}
	}
	sb, mh := map[string]any{}, map[string]any{}
	Spec{Kind: "tuic", ProtocolOptions: ProtocolOptions{TUICRelayMode: "quic", TUICHeartbeat: 15}}.clientTuning(sb, mh)
	if sb["heartbeat"] != "15s" || mh["heartbeat-interval"] != 15000 || sb["udp_relay_mode"] != "quic" || mh["udp-relay-mode"] != "quic" {
		t.Fatal("TUIC unit conversion", sb, mh)
	}
	sb, mh = map[string]any{}, map[string]any{}
	Spec{Kind: "anytls", ProtocolOptions: ProtocolOptions{AnyTLSIdleCheck: 20, AnyTLSIdleTimeout: 60, AnyTLSMinIdle: 2}}.clientTuning(sb, mh)
	if sb["idle_session_check_interval"] != "20s" || mh["idle-session-check-interval"] != 20 || sb["idle_session_timeout"] != "60s" || mh["idle-session-timeout"] != 60 || mh["min-idle-session"] != 2 {
		t.Fatal("AnyTLS fields", sb, mh)
	}
	if ClientTuningCapable("0.23.15") || ClientTuningCapable("0.23") || !ClientTuningCapable("0.23.16") || !ClientTuningCapable("0.24.0") {
		t.Fatal("Agent capability boundary")
	}
}

func TestNativeClientTuningConfigurations(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("official sing-box binary required")
	}
	for _, kind := range []string{"hysteria2", "tuic", "anytls"} {
		s := Spec{Kind: kind, Listen: "127.0.0.1", Address: "127.0.0.1", Port: 24000, Password: Secret(), UUID: UUID(), ServerName: "localhost"}
		s.Certificate, s.PrivateKey, _ = Certificate("localhost")
		switch kind {
		case "hysteria2":
			s.HY2ClientMode = "brutal"
			s.HY2ClientDownMbps = 500
			s.HY2BBRProfile = "conservative"
		case "tuic":
			s.TUICRelayMode = "quic"
			s.TUICHeartbeat = 15
		case "anytls":
			s.AnyTLSIdleCheck = 20
			s.AnyTLSIdleTimeout = 60
			s.AnyTLSMinIdle = 2
		}
		sb, mh, _, err := s.Client("fixture")
		if err != nil {
			t.Fatal(err)
		}
		if mhBinary := os.Getenv("TEST_MIHOMO_BINARY"); mhBinary != "" {
			rawMH, _ := json.Marshal(map[string]any{"mode": "direct", "proxies": []any{mh}})
			folder := t.TempDir()
			mhPath := filepath.Join(folder, "mihomo.json")
			os.WriteFile(mhPath, rawMH, 0600)
			if out, e := exec.Command(mhBinary, "-t", "-d", folder, "-f", mhPath).CombinedOutput(); e != nil {
				t.Fatalf("Mihomo %s: %v %s", kind, e, out)
			}
		}
		raw, _ := json.Marshal(map[string]any{"outbounds": []any{sb}})
		p := filepath.Join(t.TempDir(), "client.json")
		os.WriteFile(p, raw, 0600)
		if out, e := exec.Command(binary, "check", "-c", p).CombinedOutput(); e != nil {
			t.Fatalf("%s %v %s", kind, e, out)
		}
	}
}
