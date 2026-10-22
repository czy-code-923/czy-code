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
	"runtime"
	"strconv"
	"testing"
	"time"
	"vps-control/internal/executor"
)

func TestNativeForwarding(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			ln, e := net.Listen("tcp", net.JoinHostPort(address, "0"))
			if e != nil {
				t.Skip("local address family unavailable")
			}
			ln.Close()
			nativeForwarding(t, address)
		})
	}
}
func nativeForwarding(t *testing.T, bindAddress string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	binaries := map[string]executor.Binary{}
	for engine, env := range map[string]string{"sing-box": "TEST_SING_BOX_BINARY", "frps": "TEST_FRPS_BINARY", "frpc": "TEST_FRPC_BINARY"} {
		p := os.Getenv(env)
		if p == "" {
			t.Skip("set all TEST_*_BINARY for real local forwarding tests")
		}
		sum, e := executor.FileDigest(p)
		if e != nil {
			t.Fatal(e)
		}
		binaries[engine] = executor.Binary{Path: p, SHA256: sum}
	}
	free := func() int {
		l, e := net.Listen("tcp", net.JoinHostPort(bindAddress, "0"))
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "harbor-forwarding-ok") }))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	targetPort, _ := strconv.Atoi(u.Port())
	cert, key, e := Certificate(bindAddress)
	if e != nil {
		t.Fatal(e)
	}
	next := Spec{Kind: "shadowsocks", Listen: bindAddress, Address: bindAddress, Port: free(), Password: Secret()}
	entry := Spec{Kind: "shadowsocks", Listen: bindAddress, Address: bindAddress, Port: free(), Password: Secret(), Peer: &Peer{Address: next.Address, Port: next.Port, Password: next.Password}}
	server := Spec{Kind: "frps", Listen: bindAddress, Address: bindAddress, Port: free(), Password: Secret(), Certificate: cert, PrivateKey: key, Tunnels: []Tunnel{{Name: "native", LocalHost: "127.0.0.1", LocalPort: targetPort, RemotePort: free()}}}
	client := server
	client.Kind = "frpc"
	client.PrivateKey = ""
	runner, e := NewRunner(t.TempDir(), binaries)
	if e != nil {
		t.Fatal(e)
	}
	defer runner.Close()
	work := []Work{{ID: "11111111111111111111111111111111", Version: 1, Enabled: true, Spec: next}, {ID: "22222222222222222222222222222222", Version: 1, Enabled: true, Spec: entry}, {ID: "33333333333333333333333333333333", Version: 1, Enabled: true, Spec: server}, {ID: "44444444444444444444444444444444", Version: 1, Enabled: true, Spec: client}}
	if e = runner.Reconcile(context.Background(), work); e != nil {
		t.Fatal(e)
	}
	waitGet := func(c *http.Client, address string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			res, e := c.Get(address)
			if e == nil {
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if string(b) == "harbor-forwarding-ok" {
					return
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("forwarding did not become available; reports=%+v", runner.Reports())
	}
	waitGet(&http.Client{Timeout: time.Second}, "http://"+net.JoinHostPort(bindAddress, strconv.Itoa(server.Tunnels[0].RemotePort)))
	localPort := free()
	config := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": localPort}}, "outbounds": []any{map[string]any{"type": "shadowsocks", "tag": "proxy", "server": entry.Address, "server_port": entry.Port, "method": Method, "password": entry.Password}}, "route": map[string]any{"final": "proxy"}}
	b, _ := json.Marshal(config)
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binaries["sing-box"].Path, "run", "-c", path)
	prepareChild(cmd)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	release, e := guardChild(cmd)
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait(); release() }()
	proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(localPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	waitGet(&http.Client{Transport: transport, Timeout: time.Second}, target.URL)
	if e = runner.Reconcile(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	ln, e := net.Listen("tcp", net.JoinHostPort(entry.Listen, strconv.Itoa(entry.Port)))
	if e != nil {
		t.Fatal("stopped service kept port", e)
	}
	ln.Close()
}
