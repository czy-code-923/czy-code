package live

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vps-control/internal/executor"
)

// Exercise real UDP packets through both server cores and a sing-box client.
func TestNativeSSUDPListener(t *testing.T) {
	sb := os.Getenv("TEST_SING_BOX_BINARY")
	if sb == "" {
		t.Skip("set TEST_SING_BOX_BINARY for native UDP forwarding")
	}
	for _, kind := range []string{"shadowsocks", "socks"} {
		for _, core := range []string{"sing-box", "xray"} {
			t.Run(kind+"/"+core, func(t *testing.T) {
				binary := sb
				if core == "xray" {
					binary = os.Getenv("TEST_XRAY_BINARY")
					if binary == "" {
						t.Skip("set TEST_XRAY_BINARY")
					}
				}
				echo, e := net.ListenPacket("udp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer echo.Close()
				go func() {
					b := make([]byte, 2048)
					for {
						n, a, e := echo.ReadFrom(b)
						if e != nil {
							return
						}
						echo.WriteTo(b[:n], a)
					}
				}()
				free := func() int {
					c, e := net.ListenPacket("udp", "127.0.0.1:0")
					if e != nil {
						t.Fatal(e)
					}
					defer c.Close()
					return c.LocalAddr().(*net.UDPAddr).Port
				}
				s := Spec{Kind: kind, Engine: core, Listen: "127.0.0.1", Address: "127.0.0.1", Port: free(), Password: Secret()}
				if kind == "shadowsocks" {
					s.ListenNetwork = "udp"
				}
				var r *Runner
				if core == "xray" {
					_, files, err := s.Render()
					if err != nil {
						t.Fatal(err)
					}
					var server map[string]any
					json.Unmarshal(files["config.json"], &server)
					// Official Xray restricts private destinations by default. This
					// isolated test explicitly permits only its local UDP echo target.
					server["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "udp", "ip": []string{"127.0.0.1/32"}, "port": echo.LocalAddr().(*net.UDPAddr).Port}}}
					b, _ := json.Marshal(server)
					path := filepath.Join(t.TempDir(), "server.json")
					if err = os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
					cmd := exec.Command(binary, "run", "-config", path)
					prepareChild(cmd)
					if err = cmd.Start(); err != nil {
						t.Fatal(err)
					}
					release, err := guardChild(cmd)
					if err != nil {
						cmd.Process.Kill()
						cmd.Wait()
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait(); release() }()
				} else {
					sum, e := executor.FileDigest(binary)
					if e != nil {
						t.Fatal(e)
					}
					state := t.TempDir()
					if e = os.Chmod(state, 0700); e != nil {
						t.Fatal(e)
					}
					r, e = NewRunner(state, map[string]executor.Binary{core: {Path: binary, SHA256: sum}})
					if e != nil {
						t.Fatal(e)
					}
					defer r.Close()
					if e = r.Reconcile(context.Background(), []Work{{ID: "19191919191919191919191919191919", Version: 1, Enabled: true, Spec: s}}); e != nil {
						t.Fatal(e)
					}
				}
				out, _, _, e := s.Client("proxy")
				if e != nil {
					t.Fatal(e)
				}
				local := free()
				cfg := map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "direct", "listen": "127.0.0.1", "listen_port": local, "network": "udp", "override_address": "127.0.0.1", "override_port": echo.LocalAddr().(*net.UDPAddr).Port}}, "outbounds": []any{out}, "route": map[string]any{"final": "proxy"}}
				b, _ := json.Marshal(cfg)
				path := filepath.Join(t.TempDir(), "udp-client.json")
				if e = os.WriteFile(path, b, 0600); e != nil {
					t.Fatal(e)
				}
				cmd := exec.Command(sb, "run", "-c", path)
				prepareChild(cmd)
				var logs bytes.Buffer
				cmd.Stderr = &logs
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
				c, e := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: local})
				if e != nil {
					t.Fatal(e)
				}
				defer c.Close()
				payload := []byte("starhub-native-udp")
				reply := make([]byte, 128)
				deadline := time.Now().Add(12 * time.Second)
				for time.Now().Before(deadline) {
					c.SetDeadline(time.Now().Add(300 * time.Millisecond))
					if _, e = c.Write(payload); e == nil {
						n, e := c.Read(reply)
						if e == nil && bytes.Equal(reply[:n], payload) {
							return
						}
					}
				}
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("native UDP forwarding failed; core=%s client=%s", core, logs.String())
			})
		}
	}
}
