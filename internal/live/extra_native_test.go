package live

import (
	"encoding/json"
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

func TestExtraProtocolsNativeForwarding(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("native sing-box required")
	}
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	free := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "starhub-extended-forwarding") }))
	defer target.Close()
	for _, kind := range []string{"http", "socks", "mixed", "hysteria"} {
		t.Run(kind, func(t *testing.T) {
			s := Spec{Engine: "sing-box", Kind: kind, Listen: "127.0.0.1", Address: "localhost", Port: free(), Username: "test", Password: Secret(), TLSMode: "none", Transport: "tcp"}
			if kind == "hysteria" {
				s.Username = ""
				s.TLSMode = "tls"
				s.Transport = "quic"
				s.ServerName = "localhost"
				s.Certificate = cert
				s.PrivateKey = key
				s.HysteriaObfs = "starhub-obfuscation-test"
				s.UpMbps = 100
				s.DownMbps = 100
			}
			_, files, e := s.Render()
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			for name, b := range files {
				os.WriteFile(filepath.Join(dir, name), b, 0600)
			}
			server := exec.Command(binary, "run", "-c", "config.json")
			server.Dir = dir
			server.Stderr = os.Stderr
			if e = server.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { server.Process.Kill(); server.Wait() }()
			client, _, _, e := s.Client("egress")
			if e != nil {
				t.Fatal(e)
			}
			port := free()
			cfg := map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{client}}
			raw, _ := json.Marshal(cfg)
			os.WriteFile(filepath.Join(dir, "client.json"), raw, 0600)
			process := exec.Command(binary, "run", "-c", "client.json")
			process.Dir = dir
			process.Stderr = os.Stderr
			if e = process.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { process.Process.Kill(); process.Wait() }()
			proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
			httpClient := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
			for i := 0; i < 30; i++ {
				res, e := httpClient.Get(target.URL)
				if e == nil {
					b, _ := io.ReadAll(res.Body)
					res.Body.Close()
					if string(b) == "starhub-extended-forwarding" {
						return
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatal("authenticated extended protocol did not forward")
		})
	}
}
func TestNaiveDedicatedClientBoundary(t *testing.T) {
	s := Spec{Kind: "naive", Address: "proxy.example.com", ServerName: "proxy.example.com", Port: 23456, Username: "user", Password: strings.Repeat("p", 32)}
	if s.ClientFormat("sing-box") == nil || s.ClientFormat("mihomo") == nil || s.ClientFormat("naive") != nil {
		t.Fatal("invalid Naive client matrix")
	}
	_, _, uri, e := s.Client("Naive")
	if e != nil || !strings.HasPrefix(uri, "https://") {
		t.Fatal("native proxy URI", e)
	}
}

func TestUnassignedHTTPIsNeverOpenProxy(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("native core required")
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	s := Spec{Kind: "http", Listen: "127.0.0.1", Address: "127.0.0.1", Port: port, Password: Secret(), ManagedUsers: true, TLSMode: "none"}
	_, files, e := s.Render()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), files["config.json"], 0600)
	process := exec.Command(binary, "run", "-c", "config.json")
	process.Dir = dir
	if e = process.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	for i := 0; i < 30; i++ {
		res, e := client.Get("http://127.0.0.1:9/")
		if e == nil {
			res.Body.Close()
			if res.StatusCode != 407 {
				t.Fatalf("unauthenticated proxy returned %d instead of 407", res.StatusCode)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("proxy did not start")
}
