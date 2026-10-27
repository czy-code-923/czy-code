package live

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolLayerNativeMatrix(t *testing.T) {
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	for _, engine := range []string{"sing-box", "xray"} {
		for _, kind := range []string{"shadowsocks", "snell", "vless", "vmess", "trojan", "hysteria2", "tuic", "anytls", "http", "socks", "mixed", "hysteria", "naive"} {
			for _, layer := range Layers(engine, kind) {
				for _, security := range layer.Security {
					t.Run(engine+"/"+kind+"/"+layer.Transport+"/"+security, func(t *testing.T) {
						s := Spec{Engine: engine, Kind: kind, Transport: layer.Transport, TLSMode: security, Address: "localhost", Listen: "127.0.0.1", Port: 23456, Password: Secret(), UUID: UUID()}
						if kind == "snell" {
							s.UUID = ""
						}
						if layer.Transport == "grpc" {
							s.ServiceName = "starhub"
						}
						if layer.Transport == "ws" || layer.Transport == "httpupgrade" || layer.Transport == "http" || layer.Transport == "xhttp" {
							s.Path = "/starhub"
							s.WSHost = "localhost"
						}
						if security == "tls" {
							s.ServerName = "localhost"
							s.Certificate = cert
							s.PrivateKey = key
						}
						if security == "reality" {
							s.ServerName = "www.microsoft.com"
							s.Reality = NewReality()
							s.Reality.Target = s.ServerName
						}
						if kind == "hysteria" {
							s.UpMbps = 100
							s.DownMbps = 100
						}
						_, files, e := s.Render()
						if e != nil {
							t.Fatal(e)
						}
						binary := os.Getenv("TEST_SING_BOX_BINARY")
						args := []string{"check", "-c", "config.json"}
						if engine == "xray" {
							binary = os.Getenv("TEST_XRAY_BINARY")
							args = []string{"run", "-test", "-c", "config.json"}
						}
						if binary == "" {
							t.Skip("native core required for decoder validation")
						}
						dir := t.TempDir()
						for name, raw := range files {
							if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
								t.Fatal(e)
							}
						}
						cmd := exec.Command(binary, args...)
						cmd.Dir = dir
						if out, e := cmd.CombinedOutput(); e != nil {
							t.Fatalf("native decoder: %v %s", e, out)
						}
						if s.ClientFormat("sing-box") == nil && os.Getenv("TEST_SING_BOX_BINARY") != "" {
							client, _, _, e := s.Client("test")
							if e != nil {
								t.Fatal(e)
							}
							if xrayBinary := os.Getenv("TEST_XRAY_BINARY"); xrayBinary != "" && s.ClientFormat("xray") == nil {
								raw, _ := json.Marshal(map[string]any{"outbounds": []any{s.XrayClient("test")}})
								file := filepath.Join(dir, "xray-client.json")
								os.WriteFile(file, raw, 0600)
								cmd := exec.Command(xrayBinary, "run", "-test", "-c", file)
								cmd.Dir = dir
								if out, e := cmd.CombinedOutput(); e != nil {
									t.Fatalf("Xray client: %v %s", e, out)
								}
							}
							if mhBinary := os.Getenv("TEST_MIHOMO_BINARY"); mhBinary != "" && s.ClientFormat("mihomo") == nil {
								_, node, _, e := s.Client("test")
								if e != nil {
									t.Fatal(e)
								}
								raw, _ := json.Marshal(map[string]any{"mixed-port": 1080, "proxies": []any{node}, "proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": []string{"test"}}}, "rules": []string{"MATCH,PROXY"}})
								file := filepath.Join(dir, "mihomo.yaml")
								os.WriteFile(file, raw, 0600)
								cmd := exec.Command(mhBinary, "-t", "-d", dir, "-f", file)
								if out, e := cmd.CombinedOutput(); e != nil {
									t.Fatalf("Mihomo client: %v %s", e, out)
								}
							}
							raw, _ := json.Marshal(map[string]any{"outbounds": []any{client}})
							os.WriteFile(filepath.Join(dir, "client.json"), raw, 0600)
							cmd := exec.Command(os.Getenv("TEST_SING_BOX_BINARY"), "check", "-c", "client.json")
							cmd.Dir = dir
							if out, e := cmd.CombinedOutput(); e != nil {
								t.Fatalf("native client: %v %s", e, out)
							}
						}
					})
				}
			}
		}
	}
}
func TestRejectIncompatibleLayers(t *testing.T) {
	s := Spec{Engine: "sing-box", Kind: "vless", Transport: "xhttp", TLSMode: "tls"}
	if s.validLayer() {
		t.Fatal("XHTTP offered to sing-box")
	}
	s = Spec{Engine: "sing-box", Kind: "vless", Transport: "ws", TLSMode: "reality"}
	if s.validLayer() {
		t.Fatal("REALITY offered with WebSocket")
	}
	s = Spec{Engine: "xray", Kind: "hysteria2", Transport: "quic", TLSMode: "tls"}
	if s.validLayer() {
		t.Fatal("unimplemented Xray Hysteria adapter offered")
	}
	for _, version := range []string{"0.12.0", "bad", strings.Repeat("9", 100)} {
		if V13Capable(version) {
			t.Fatal(version)
		}
	}
}

func TestShadowsocksCipherNativeMatrix(t *testing.T) {
	for _, engine := range []string{"sing-box", "xray"} {
		for _, cipher := range []string{Method, "2022-blake3-aes-128-gcm", "2022-blake3-chacha20-poly1305", "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305"} {
			t.Run(engine+"/"+cipher, func(t *testing.T) {
				s := Spec{Engine: engine, Kind: "shadowsocks", Address: "127.0.0.1", Listen: "127.0.0.1", Port: 23456, Password: CipherSecret(cipher), ProtocolOptions: ProtocolOptions{Cipher: cipher}}
				_, files, e := s.Render()
				if e != nil {
					t.Fatal(e)
				}
				binary := os.Getenv("TEST_SING_BOX_BINARY")
				args := []string{"check", "-c", "config.json"}
				if engine == "xray" {
					binary = os.Getenv("TEST_XRAY_BINARY")
					args = []string{"run", "-test", "-c", "config.json"}
				}
				if binary == "" {
					t.Skip("native core required")
				}
				dir := t.TempDir()
				os.WriteFile(filepath.Join(dir, "config.json"), files["config.json"], 0600)
				cmd := exec.Command(binary, args...)
				cmd.Dir = dir
				if out, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("native cipher: %v %s", e, out)
				}
			})
		}
	}
}
