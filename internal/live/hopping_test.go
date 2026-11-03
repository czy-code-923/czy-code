package live

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHopValidationAndExports(t *testing.T) {
	for _, text := range []string{"30000-30100,30200", "65534-65535", "20000,20001"} {
		if _, e := HopRanges(text); e != nil {
			t.Fatal(e)
		}
	}
	for _, text := range []string{"22-25", "30000", "30000-30002,30002-30005", "5000-9000", "10000-15000", "30000;flush ruleset", "30000-30001\n", "03000,3001", "30000-29999"} {
		if _, e := HopRanges(text); e == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	for _, kind := range []string{"hysteria", "hysteria2"} {
		s := Spec{Kind: kind, Engine: "sing-box", Listen: "::", Address: "2001:db8::1", Port: 24000, ProtocolOptions: ProtocolOptions{HopPorts: "30000-30100,30200", HopInterval: 25, UpMbps: 100, DownMbps: 100}}
		if e := s.validateHopping(); e != nil {
			t.Fatal(e)
		}
		sb, mh, uri, e := s.Client("node-v6")
		if e != nil {
			t.Fatal(e)
		}
		if sb["server_port"] != nil || sb["hop_interval"] != "25s" || mh["ports"] != s.HopPorts || mh["hop-interval"] != 25 {
			t.Fatalf("wrong client fields: %#v %#v", sb, mh)
		}
		if got := sb["server_ports"].([]string); len(got) != 2 || got[0] != "30000:30100" {
			t.Fatal(got)
		}
		u, e := url.Parse(uri)
		if e != nil || u.Hostname() != s.Address || u.Query().Get("mport") != s.HopPorts {
			t.Fatal(uri, e)
		}
		if len(s.ReservedPorts()) != 103 {
			t.Fatal("incomplete port reservations")
		}
		s.Kind = "tuic"
		if s.validateHopping() == nil {
			t.Fatal("TUIC incorrectly accepts hopping")
		}
	}
	if HoppingCapable("0.23.13") || HoppingCapable("0.23") || !HoppingCapable("0.23.14") || !HoppingCapable("0.24.0") {
		t.Fatal("Agent feature gate")
	}
}

func TestNativeHoppingClientConfiguration(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("native sing-box binary required")
	}
	for _, kind := range []string{"hysteria", "hysteria2"} {
		s := Spec{Kind: kind, Engine: "sing-box", Listen: "0.0.0.0", Address: "127.0.0.1", Port: 24000, Password: Secret(), ServerName: "localhost", ProtocolOptions: ProtocolOptions{HopPorts: "30000-30100,30200", HopInterval: 10, UpMbps: 100, DownMbps: 100}}
		cert, key, certErr := Certificate("localhost")
		if certErr != nil {
			t.Fatal(certErr)
		}
		s.Certificate = cert
		s.PrivateKey = key
		sb, _, _, err := s.Client("test")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"outbounds": []any{sb}})
		path := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(path, raw, 0600)
		if out, e := exec.Command(binary, "check", "-c", path).CombinedOutput(); e != nil {
			t.Fatalf("%s: %v %s", kind, e, out)
		}
		// Server listener keeps one port; hopping options belong to the client/helper.
		s.Certificate, s.PrivateKey, err = Certificate("localhost")
		if err != nil {
			t.Fatal(err)
		}
		_, files, err := s.Render()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(files["config.json"]), "server_ports") {
			t.Fatal("client hop fields leaked into inbound")
		}
	}
}
