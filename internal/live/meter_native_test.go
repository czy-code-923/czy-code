package live

import (
	"context"
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
)

func TestNativeTrafficAndAccess(t *testing.T) {
	for _, engine := range []string{"sing-box", "xray"} {
		t.Run(engine, func(t *testing.T) {
			binary := os.Getenv("TEST_SING_BOX_BINARY")
			serverBinary := binary
			if engine == "xray" {
				serverBinary = os.Getenv("TEST_XRAY_BINARY")
			}
			if binary == "" || serverBinary == "" {
				t.Skip("native core binaries required")
			}
			free := func() int {
				ln, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				return ln.Addr().(*net.TCPAddr).Port
			}
			payload := strings.Repeat("usage-fixture-", 8192)
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Connection", "close")
				io.WriteString(w, payload)
			}))
			defer target.Close()
			userID, serviceID := strings.Repeat("c", 32), strings.Repeat("d", 32)
			spec := Spec{Engine: engine, Kind: "vmess", TLSMode: "none", Address: "127.0.0.1", Listen: "127.0.0.1", Port: free(), UUID: UUID(), ManagedUsers: true, Accounts: []Account{{Name: userID, UUID: UUID(), Password: Secret()}}}
			work := BundleWork([]Work{{ID: serviceID, Version: 1, Enabled: true, Spec: spec}})[0]
			_, files, e := work.Render()
			if e != nil {
				t.Fatal(e)
			}
			if engine == "xray" {
				var cfg map[string]any
				json.Unmarshal(files["config.json"], &cfg)
				cfg["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": target.Listener.Addr().(*net.TCPAddr).Port}}}
				files["config.json"], _ = json.Marshal(cfg)
			}
			usage := newUsageStore()
			meter, e := prepareMeter(engine, files, usage)
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.json"), files["config.json"], 0600)
			args := []string{"run", "-c", "config.json"}
			if engine == "xray" {
				args = []string{"run", "-config", "config.json"}
			}
			server := exec.Command(serverBinary, args...)
			server.Dir = dir
			server.Stdout = &accessWriter{meter: meter}
			server.Stderr = os.Stderr
			if e = server.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { server.Process.Kill(); server.Wait() }()
			if e = meter.start(); e != nil {
				t.Fatal(e)
			}
			defer meter.close()
			for i := 0; i < 40 && !meter.poll(context.Background()); i++ {
				time.Sleep(50 * time.Millisecond)
			}
			if !meter.poll(context.Background()) {
				t.Fatal("statistics unavailable")
			}
			clientSpec := spec
			clientSpec.UUID = spec.Accounts[0].UUID
			sb, _, _, _ := clientSpec.Client("proxy")
			port := free()
			raw, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{sb}, "route": map[string]any{"final": "proxy"}})
			path := filepath.Join(dir, "client.json")
			os.WriteFile(path, raw, 0600)
			client := exec.Command(binary, "run", "-c", path)
			if e = client.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { client.Process.Kill(); client.Wait() }()
			proxy, _ := url.Parse("http://127.0.0.1:" + portString(port))
			tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
			defer tr.CloseIdleConnections()
			httpClient := &http.Client{Transport: tr, Timeout: time.Second}
			delivered := false
			for i := 0; i < 20; i++ {
				res, e := httpClient.Get(target.URL)
				if e == nil {
					data, _ := io.ReadAll(res.Body)
					res.Body.Close()
					if string(data) == payload {
						delivered = true
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !delivered {
				t.Fatal("metered forwarding failed")
			}
			for i := 0; i < 30; i++ {
				meter.poll(context.Background())
				usage.mu.Lock()
				var bytes int64
				for _, v := range usage.Counters {
					if v.Kind == "user" && v.Subject == userID {
						bytes = v.Up + v.Down
					}
				}
				observed := len(usage.Access) > 0
				usage.mu.Unlock()
				if bytes >= int64(len(payload)) && observed {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			usage.mu.Lock()
			defer usage.mu.Unlock()
			t.Fatalf("real traffic or source/destination observation missing: counters=%v access=%v", usage.Counters, usage.Access)
		})
	}
}
