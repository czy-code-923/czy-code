package live

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

func TestProtocolTemplates(t *testing.T) {
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"vless", "hysteria2", "tuic", "trojan", "snell", "xray-vless", "xray-trojan", "vmess", "vmess-plain", "vmess-ws", "xray-vmess", "xray-vmess-plain", "xray-vless-ws", "trojan-ws", "anytls", "chain-vmess-ws", "xray-chain-vless", "vless-reality", "xray-vless-reality", "vless-grpc", "xray-vless-grpc", "managed-vless", "xray-managed-vmess", "managed-anytls", "managed-trojan", "xray-managed-trojan", "snell-http", "snell-v6", "hysteria2-obfs", "vmess-grpc", "trojan-grpc", "shadowsocks-aead", "xray-shadowsocks", "xray-shadowsocks-aead"} {
		t.Run(kind, func(t *testing.T) {
			serverEngine := "sing-box"
			if strings.HasPrefix(kind, "xray-") {
				serverEngine = "xray"
				kind = strings.TrimPrefix(kind, "xray-")
			}
			chainMode := strings.HasPrefix(kind, "chain-")
			kind = strings.TrimPrefix(kind, "chain-")
			managed := strings.HasPrefix(kind, "managed-")
			kind = strings.TrimPrefix(kind, "managed-")
			reality := strings.HasSuffix(kind, "-reality")
			kind = strings.TrimSuffix(kind, "-reality")
			grpc := strings.HasSuffix(kind, "-grpc")
			kind = strings.TrimSuffix(kind, "-grpc")
			plain := strings.HasSuffix(kind, "-plain")
			ws := strings.HasSuffix(kind, "-ws")
			kind = strings.TrimSuffix(strings.TrimSuffix(kind, "-plain"), "-ws")
			options := ProtocolOptions{}
			if kind == "shadowsocks-aead" {
				kind = "shadowsocks"
				options.Cipher = "chacha20-ietf-poly1305"
			}
			if kind == "snell-http" {
				kind = "snell"
				options = ProtocolOptions{SnellVersion: 5, SnellObfs: "http", SnellHost: "bing.com"}
			}
			if kind == "snell-v6" {
				kind = "snell"
				options = ProtocolOptions{SnellVersion: 6, SnellMode: "unshaped"}
			}
			if kind == "hysteria2-obfs" {
				kind = "hysteria2"
				options = ProtocolOptions{HY2Obfs: "native-salamander-fixture", UpMbps: 100, DownMbps: 100}
			}
			s := Spec{ProtocolOptions: options, Engine: serverEngine, Kind: kind, Listen: "127.0.0.1", Address: "127.0.0.1", Port: 25000, Password: Secret()}
			if plain {
				s.TLSMode = "none"
			}
			if ws {
				s.Transport = "ws"
				s.Path = "/proxy"
			}
			if kind == "vmess" {
				s.UUID = UUID()
			}
			if s.UsesTLS() {
				s.UUID = UUID()
				s.ServerName = "localhost"
				s.Certificate = cert
				s.PrivateKey = key
			}
			if grpc {
				s.Transport = "grpc"
				s.ServiceName = "harbor-test"
			}
			if managed {
				s.ManagedUsers = true
				s.Accounts = []Account{{Name: "fixture-user", UUID: UUID(), Password: Secret()}}
			}
			if reality {
				handshake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
				handshake.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
				handshake.EnableHTTP2 = true
				handshake.StartTLS()
				defer handshake.Close()
				s.TLSMode = "reality"
				s.Certificate = ""
				s.PrivateKey = ""
				s.Flow = "xtls-rprx-vision"
				s.Reality = NewReality()
				s.Reality.Target = "localhost"
				s.Reality.Port = handshake.Listener.Addr().(*net.TCPAddr).Port
			}
			if chainMode {
				binary := os.Getenv("TEST_SING_BOX_BINARY")
				if binary == "" {
					return
				}
				listener, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				listener.Close()
				exit := Spec{Kind: "shadowsocks", Address: "127.0.0.1", Listen: "127.0.0.1", Port: port, Password: Secret()}
				_, exitFiles, e := exit.Render()
				if e != nil {
					t.Fatal(e)
				}
				dir := t.TempDir()
				os.WriteFile(filepath.Join(dir, "config.json"), exitFiles["config.json"], 0600)
				cmd := exec.Command(binary, "run", "-c", "config.json")
				cmd.Dir = dir
				if e = cmd.Start(); e != nil {
					t.Fatal(e)
				}
				defer func() { cmd.Process.Kill(); cmd.Wait() }()
				s.Outbound = &exit
			}
			engine, files, e := s.Render()
			if e != nil {
				t.Fatal(e)
			}
			sb, mh, uri, e := s.Client("test")
			if e != nil || sb["type"] != kind || mh["name"] != "test" || (uri == "" && kind != "snell" && kind != "anytls") {
				t.Fatal(sb, mh, uri, e)
			}
			if kind == "vless" && mh["servername"] != "localhost" {
				t.Fatal("missing Mihomo VLESS servername")
			}
			b, _ := json.Marshal(sb)
			if strings.Contains(string(b), "PRIVATE KEY") || strings.Contains(uri, "insecure") {
				t.Fatal("unsafe subscription")
			}
			clientBinary := os.Getenv("TEST_SING_BOX_BINARY")
			binary := clientBinary
			if serverEngine == "xray" {
				binary = os.Getenv("TEST_XRAY_BINARY")
			}
			if binary == "" || clientBinary == "" {
				return
			}
			dir := t.TempDir()
			for name, data := range files {
				if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			sum, _ := executor.FileDigest(binary)
			v := executor.NativeValidator{Binaries: map[string]executor.Binary{engine: {Path: binary, SHA256: sum}}}
			if e = v.Check(context.Background(), dir, []manifest.Artifact{{Name: "config.json", Engine: engine, Content: files["config.json"]}}); e != nil {
				if engine == "xray" {
					c := exec.Command(binary, "run", "-test", "-config", filepath.Join(dir, "config.json"))
					c.Dir = dir
					output, _ := c.CombinedOutput()
					t.Log(string(output))
				}
				t.Fatal("native server template", e)
			}
			// A real server/client pair exercises authentication, TLS and QUIC, not just JSON.
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Log("target received request", kind)
				w.Header().Set("Connection", "close")
				io.WriteString(w, "protocol-forwarding-ok")
			}))
			defer func() { target.CloseClientConnections(); target.Close() }()
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			s.Port = listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			_, files, e = s.Render()
			if e != nil {
				t.Fatal(e)
			}
			// Xray protects private targets by default. Scope the test-only exception to this fixture.
			if serverEngine == "xray" && !chainMode {
				var cfg map[string]any
				json.Unmarshal(files["config.json"], &cfg)
				cfg["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": target.Listener.Addr().(*net.TCPAddr).Port}}}
				files["config.json"], _ = json.Marshal(cfg)
			}
			os.WriteFile(filepath.Join(dir, "config.json"), files["config.json"], 0600)
			args := []string{"run", "-c", "config.json"}
			if serverEngine == "xray" {
				args = []string{"run", "-config", "config.json"}
			}
			server := exec.Command(binary, args...)
			server.Dir = dir
			logFile, _ := os.Create(filepath.Join(dir, "server.log"))
			defer logFile.Close()
			server.Stdout = logFile
			server.Stderr = logFile
			if e = server.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { server.Process.Kill(); server.Wait() }()
			l, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			clientPort := l.Addr().(*net.TCPAddr).Port
			l.Close()
			clientSpec := s
			if managed {
				clientSpec.UUID = s.Accounts[0].UUID
				clientSpec.Password = s.Accounts[0].Password
			}
			sb, _, _, _ = clientSpec.Client("proxy")
			config := map[string]any{"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": clientPort}}, "outbounds": []any{sb}, "route": map[string]any{"final": "proxy"}}
			if reality && serverEngine == "xray" {
				config = map[string]any{"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": clientPort, "protocol": "http"}}, "outbounds": []any{s.XrayClient("proxy")}}
			}
			content, _ := json.Marshal(config)
			os.WriteFile(filepath.Join(dir, "client.json"), content, 0600)
			clientArgs := []string{"run", "-c", "client.json"}
			if reality && serverEngine == "xray" {
				clientBinary = binary
				clientArgs = []string{"run", "-config", "client.json"}
			}
			client := exec.Command(clientBinary, clientArgs...)
			client.Dir = dir
			clientLog, _ := os.Create(filepath.Join(dir, "client.log"))
			defer clientLog.Close()
			client.Stderr = clientLog
			client.Stdout = clientLog
			if e = client.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { client.Process.Kill(); client.Wait() }()

			proxyURL, _ := url.Parse("http://127.0.0.1:" + portString(clientPort))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			for attempt := 0; attempt < 3; attempt++ {
				response, err := httpClient.Get(target.URL)
				if err == nil {
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if string(body) == "protocol-forwarding-ok" {
						return
					}
				}
				time.Sleep(150 * time.Millisecond)
			}
			server.Process.Kill()
			server.Wait()
			logFile.Sync()
			logData, _ := os.ReadFile(filepath.Join(dir, "server.log"))
			t.Log(string(logData))
			clientLog.Sync()
			clientData, _ := os.ReadFile(filepath.Join(dir, "client.log"))
			t.Log(string(clientData))
			t.Fatal("real protocol forwarding failed")
		})
	}
}

func TestProtocolSafety(t *testing.T) {
	for _, version := range []string{"0.5.9", "invalid", "0.6.-1", "0.6.0foo"} {
		if ProtocolCapable(version) {
			t.Fatal(version)
		}
	}
	for _, version := range []string{"0.6.0", "0.7.0", "1.0.0"} {
		if !ProtocolCapable(version) {
			t.Fatal(version)
		}
	}
	cert, key, _ := Certificate("localhost")
	s := Spec{Kind: "vless", Address: "localhost", Listen: "127.0.0.1", Port: 23000, Password: Secret(), UUID: UUID(), ServerName: "localhost", Certificate: cert, PrivateKey: key}
	if s.VerifyPublicCertificate() == nil {
		t.Fatal("self-signed certificate accepted for portable subscription")
	}
	s.ServerName = "wrong.example.com"
	if s.Validate() == nil {
		t.Fatal("certificate hostname mismatch accepted")
	}
	s.ServerName = "localhost"
	s.PrivateKey = Secret()
	if s.Validate() == nil {
		t.Fatal("bad key accepted")
	}
	s.Engine = "arbitrary-engine"
	if s.Validate() == nil {
		t.Fatal("unknown engine accepted")
	}
}
