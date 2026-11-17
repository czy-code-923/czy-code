package live

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransportOptionsDefaultsAndAgentBoundary(t *testing.T) {
	s := Spec{Kind: "vmess", Engine: "xray", Transport: "xhttp", TLSMode: "none", Path: "/proxy", Listen: "127.0.0.1", Address: "localhost", Port: 25057, UUID: UUID(), Password: Secret()}
	_, before, e := s.Render()
	if e != nil {
		t.Fatal(e)
	}
	s.TransportOptions = &TransportOptions{}
	_, after, e := s.Render()
	if e != nil || string(before["config.json"]) != string(after["config.json"]) {
		t.Fatal("empty options changed legacy config", e)
	}
	s.TransportOptions = &TransportOptions{XHTTPMode: "packet-up", XHTTPPadding: "100-1000"}
	if !s.RequiresTransportAgent() || TransportOptionsCapable("0.25.6") || !TransportOptionsCapable("0.25.7") || !TransportOptionsCapable("1.0.0") || TransportOptionsCapable("0.25.7junk") {
		t.Fatal("transport capability boundary")
	}
	s.TransportOptions = &TransportOptions{TLSFingerprint: "chrome", GRPCMultiMode: true}
	agent := s.AgentSpec()
	raw, _ := json.Marshal(agent)
	if agent.TransportOptions != nil || strings.Contains(string(raw), "transport_options") || s.TransportOptions.TLSFingerprint != "chrome" {
		t.Fatal("client fields leaked into old Agent wire or mutated stored settings")
	}
	s.Outbound = &Spec{ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: "firefox"}}}
	if !s.RequiresTransportAgent() || s.AgentSpec().Outbound.TransportOptions.TLSFingerprint != "firefox" {
		t.Fatal("chain lost client fingerprint")
	}
}

func TestTransportOptionsRejectInvalidCombinations(t *testing.T) {
	for _, s := range []Spec{
		{Kind: "hysteria2", TLSMode: "tls", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: "chrome"}}},
		{Kind: "naive", TLSMode: "tls", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: "chrome"}}},
		{Kind: "vless", TLSMode: "reality", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: "chrome"}}},
		{Kind: "vmess", Engine: "sing-box", Transport: "xhttp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{XHTTPMode: "auto"}}},
		{Kind: "vmess", Engine: "xray", Transport: "xhttp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{XHTTPMode: "other"}}},
		{Kind: "vmess", Engine: "xray", Transport: "xhttp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{XHTTPPadding: "1000-100"}}},
		{Kind: "vmess", Engine: "xray", Transport: "xhttp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{XHTTPMode: "packet-up", XHTTPNoGRPCHeader: true}}},
		{Kind: "vmess", Engine: "sing-box", Transport: "grpc", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{GRPCMultiMode: true}}},
		{Kind: "vmess", Engine: "xray", Transport: "kcp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{KCPMTU: 100}}},
		{Kind: "vmess", Engine: "xray", Transport: "kcp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{KCPTTI: 1}}},
		{Kind: "vmess", Engine: "xray", Transport: "kcp", ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{KCPHeader: "unknown"}}},
	} {
		if s.validateTransportOptions() == nil {
			t.Fatalf("accepted incompatible options %+v", s)
		}
	}
	next := Spec{Kind: "vmess", Engine: "xray", Transport: "xhttp", TLSMode: "none", Path: "/proxy", Address: "localhost", Port: 25057, UUID: UUID(), Password: Secret()}
	entry := Spec{Kind: "vmess", Engine: "sing-box", Transport: "tcp", TLSMode: "none", Outbound: &next}
	if entry.validateRouting() == nil {
		t.Fatal("sing-box chain silently lost XHTTP")
	}
	next.Transport = "grpc"
	next.Path = ""
	next.ServiceName = "proxy"
	next.TransportOptions = &TransportOptions{GRPCMultiMode: true}
	if next.ClientFormat("sing-box") == nil || next.ClientFormat("mihomo") == nil || next.ClientFormat("xray") != nil {
		t.Fatal("gRPC multiMode export silently downgraded")
	}
}

func checkTransportNative(t *testing.T, engine string, config map[string]any) {
	t.Helper()
	binary := os.Getenv("TEST_XRAY_BINARY")
	args := []string{"run", "-test", "-c", "config.json"}
	if engine == "sing-box" {
		binary = os.Getenv("TEST_SING_BOX_BINARY")
		args = []string{"check", "-c", "config.json"}
	}
	if engine == "mihomo" {
		binary = os.Getenv("TEST_MIHOMO_BINARY")
		args = []string{"-t", "-d", ".", "-f", "config.json"}
	}
	if binary == "" {
		return
	}
	dir := t.TempDir()
	raw, e := json.Marshal(config)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "config.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%s native validation: %v %s", engine, e, out)
	}
}

func TestTLSFingerprintsAndNativeExports(t *testing.T) {
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"vless", "vmess", "trojan", "anytls", "http"} {
		for _, fp := range []string{"chrome", "firefox", "safari", "edge", "ios", "android", "randomized"} {
			t.Run(kind+"/"+fp, func(t *testing.T) {
				s := Spec{Kind: kind, Engine: "sing-box", TLSMode: "tls", ServerName: "localhost", Certificate: cert, PrivateKey: key, Address: "localhost", Listen: "127.0.0.1", Port: 25057, Password: Secret(), UUID: UUID(), ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: fp}}}
				if e := s.Validate(); e != nil {
					t.Fatal(e)
				}
				sb, mh, uri, e := s.Client("proxy")
				if e != nil {
					t.Fatal(e)
				}
				if kind != "http" && mh["client-fingerprint"] != fp || sb["tls"].(map[string]any)["utls"].(map[string]any)["fingerprint"] != fp {
					t.Fatal("fingerprint missing from export")
				}
				if kind != "vmess" && kind != "anytls" && !strings.Contains(uri, "fp="+fp) {
					t.Fatal("URI lost fingerprint", uri)
				}
				checkTransportNative(t, "sing-box", map[string]any{"outbounds": []any{sb}})
				if s.ClientFormat("mihomo") == nil {
					checkTransportNative(t, "mihomo", map[string]any{"proxies": []any{mh}, "proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": []string{"proxy"}}}, "rules": []string{"MATCH,PROXY"}})
				} else if kind == "http" && s.ClientFormat("shadowrocket") == nil {
					t.Fatal("HTTP browser fingerprint silently dropped")
				}
				if EngineSupports("xray", kind) {
					checkTransportNative(t, "xray", map[string]any{"outbounds": []any{s.XrayClient("proxy")}})
				}
				_, files, e := s.Render()
				if e != nil {
					t.Fatal(e)
				}
				if strings.Contains(string(files["config.json"]), "utls") {
					t.Fatal("client fingerprint entered server TLS")
				}
			})
		}
	}
}

func TestXrayTransportNativeOptions(t *testing.T) {
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	cases := map[string]TransportOptions{}
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		cases["xhttp/"+mode] = TransportOptions{XHTTPMode: mode, XHTTPPadding: "100-1000", TLSFingerprint: "chrome", XHTTPNoGRPCHeader: mode == "stream-up"}
	}
	cases["grpc/multi"] = TransportOptions{GRPCMultiMode: true, TLSFingerprint: "firefox"}
	for _, header := range []string{"none", "srtp", "utp", "wechat-video", "dtls", "wireguard"} {
		cases["kcp/"+header] = TransportOptions{KCPHeader: header, KCPSeed: "native-seed", KCPMTU: 1350, KCPTTI: 20, KCPUplink: 5, KCPDownlink: 20, KCPCongestion: true}
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			s := Spec{Engine: "xray", Kind: "vmess", Transport: strings.Split(name, "/")[0], TLSMode: "tls", ServerName: "localhost", Certificate: cert, PrivateKey: key, Address: "localhost", Listen: "127.0.0.1", Port: 25057, Password: Secret(), UUID: UUID(), Path: "/proxy", ProtocolOptions: ProtocolOptions{TransportOptions: &o}}
			if s.Transport == "grpc" {
				s.Path = ""
				s.ServiceName = "starhub"
			}
			if s.Transport == "kcp" {
				s.Path = ""
				s.TLSMode = "none"
				s.Certificate = ""
				s.PrivateKey = ""
				s.ServerName = ""
			}
			_, files, e := s.Render()
			if e != nil {
				t.Fatal(e)
			}
			var server map[string]any
			if e = json.Unmarshal(files["config.json"], &server); e != nil {
				t.Fatal(e)
			}
			checkTransportNative(t, "xray", server)
			checkTransportNative(t, "xray", map[string]any{"outbounds": []any{s.XrayClient("proxy")}})
		})
	}
}
