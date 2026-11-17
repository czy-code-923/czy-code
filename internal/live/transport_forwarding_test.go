package live

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This verifies the paired wire behavior of new options, beyond parser acceptance.
func TestNativeTransportOptionsForwarding(t *testing.T) {
	binary := os.Getenv("TEST_XRAY_BINARY")
	if binary == "" {
		t.Skip("set TEST_XRAY_BINARY for real transport forwarding")
	}
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "transport-options-ok") }))
	defer target.Close()
	free := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	cases := map[string]TransportOptions{}
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		cases["xhttp/"+mode] = TransportOptions{XHTTPMode: mode, XHTTPPadding: "100-1000", TLSFingerprint: "chrome", XHTTPNoGRPCHeader: mode == "stream-up"}
	}
	cases["grpc/multi"] = TransportOptions{GRPCMultiMode: true, TLSFingerprint: "firefox"}
	cases["grpc/compatible-client"] = TransportOptions{GRPCMultiMode: true, TLSFingerprint: "chrome"}
	cases["kcp/utp"] = TransportOptions{KCPSeed: "transport-seed", KCPHeader: "utp", KCPMTU: 1300, KCPTTI: 20, KCPUplink: 5, KCPDownlink: 20, KCPCongestion: true}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			s := Spec{Engine: "xray", Kind: "vmess", Transport: strings.Split(name, "/")[0], TLSMode: "tls", ServerName: "localhost", Certificate: cert, PrivateKey: key, Address: "127.0.0.1", Listen: "127.0.0.1", Port: free(), Password: Secret(), UUID: UUID(), Path: "/proxy", ALPN: []string{"h2", "http/1.1"}, ProtocolOptions: ProtocolOptions{TransportOptions: &o}}
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
				s.ALPN = nil
			}
			_, files, e := s.Render()
			if e != nil {
				t.Fatal(e)
			}
			var server map[string]any
			json.Unmarshal(files["config.json"], &server)
			// The official core blocks private targets by default. Only this local fixture is allowed.
			server["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": target.Listener.Addr().(*net.TCPAddr).Port}}}
			clientSpec := s
			if name == "grpc/compatible-client" {
				o := *s.TransportOptions
				o.GRPCMultiMode = false
				clientSpec.TransportOptions = &o
			}
			outbound := clientSpec.XrayClient("proxy")
			if s.UsesTLS() {
				// Trust only the fixture's CA, while keeping allowInsecure=false.
				tls := outbound["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
				tls["certificates"] = []any{map[string]any{"usage": "verify", "certificate": strings.Split(strings.TrimSpace(cert), "\n")}}
			}
			port := free()
			client := map[string]any{"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "http"}}, "outbounds": []any{outbound}}
			dir := t.TempDir()
			for name, cfg := range map[string]any{"server.json": server, "client.json": client} {
				raw, e := json.Marshal(cfg)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			for _, name := range []string{"server", "client"} {
				cmd := exec.Command(binary, "run", "-c", name+".json")
				cmd.Dir = dir
				log, e := os.Create(filepath.Join(dir, name+".log"))
				if e != nil {
					t.Fatal(e)
				}
				defer log.Close()
				cmd.Stdout = log
				cmd.Stderr = log
				if e = cmd.Start(); e != nil {
					t.Fatal(e)
				}
				defer func() { cmd.Process.Kill(); cmd.Wait() }()
			}
			proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
			transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
			defer transport.CloseIdleConnections()
			httpClient := &http.Client{Transport: transport, Timeout: time.Second}
			deadline := time.Now().Add(8 * time.Second)
			var last error
			for time.Now().Before(deadline) {
				res, e := httpClient.Get(target.URL)
				last = e
				if e == nil {
					raw, _ := io.ReadAll(res.Body)
					res.Body.Close()
					if string(raw) == "transport-options-ok" {
						return
					}
					last = fmt.Errorf("unexpected response: %s", raw)
				}
				time.Sleep(100 * time.Millisecond)
			}
			for _, name := range []string{"server", "client"} {
				raw, _ := os.ReadFile(filepath.Join(dir, name+".log"))
				t.Log(name, string(raw))
			}
			t.Fatal("real transport did not forward", last)
		})
	}
}

func TestNativeTLSFingerprintForwarding(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("set TEST_SING_BOX_BINARY")
	}
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fingerprint-forwarding-ok") }))
	defer target.Close()
	free := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	for _, kind := range []string{"vless", "vmess", "trojan", "anytls", "http"} {
		for _, fp := range []string{"chrome", "firefox"} {
			t.Run(kind+"/"+fp, func(t *testing.T) {
				s := Spec{Engine: "sing-box", Kind: kind, TLSMode: "tls", ServerName: "localhost", Certificate: cert, PrivateKey: key, Address: "127.0.0.1", Listen: "127.0.0.1", Port: free(), Password: Secret(), UUID: UUID(), ProtocolOptions: ProtocolOptions{TransportOptions: &TransportOptions{TLSFingerprint: fp}}}
				if kind == "http" {
					s.ALPN = []string{"http/1.1"}
				}
				_, files, e := s.Render()
				if e != nil {
					t.Fatal(e)
				}
				out, _, _, e := s.Client("proxy")
				if e != nil {
					t.Fatal(e)
				}
				port := free()
				raw, e := json.Marshal(map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{out}})
				if e != nil {
					t.Fatal(e)
				}
				files["client.json"] = raw
				dir := t.TempDir()
				for name, data := range files {
					if e = os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
						t.Fatal(e)
					}
				}
				for _, name := range []string{"config", "client"} {
					cmd := exec.Command(binary, "run", "-c", name+".json")
					cmd.Dir = dir
					log, e := os.Create(filepath.Join(dir, name+".log"))
					if e != nil {
						t.Fatal(e)
					}
					defer log.Close()
					cmd.Stdout = log
					cmd.Stderr = log
					if e = cmd.Start(); e != nil {
						t.Fatal(e)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
				}
				proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
				transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
				defer transport.CloseIdleConnections()
				httpClient := &http.Client{Transport: transport, Timeout: time.Second}
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					res, e := httpClient.Get(target.URL)
					if e == nil {
						raw, _ := io.ReadAll(res.Body)
						res.Body.Close()
						if string(raw) == "fingerprint-forwarding-ok" {
							return
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
				for _, name := range []string{"config", "client"} {
					raw, _ := os.ReadFile(filepath.Join(dir, name+".log"))
					t.Log(name, string(raw))
				}
				t.Fatal("TLS browser fingerprint did not forward")
			})
		}
	}
}
