package live

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNetworkNative(t *testing.T) {
	for _, engine := range []string{"sing-box", "xray"} {
		for _, family := range [][]string{{"ipv4"}, {"ipv6"}, {"ipv6", "ipv4"}} {
			t.Run(engine+family[0], func(t *testing.T) {
				s := Spec{Kind: "shadowsocks", Engine: engine, Password: Secret(), Address: "192.0.2.1", Port: 23456, Listen: "::", EgressFamilies: family, EndpointAddresses: []string{"192.0.2.1", "2001:db8::1"}}
				id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				w := Work{Spec: Spec{Engine: engine}, Members: []Work{{ID: id, Enabled: true, Spec: s}}}
				_, files, e := w.Render()
				if e != nil {
					t.Fatal(e)
				}
				var cfg map[string]any
				json.Unmarshal(files["config.json"], &cfg)
				if cfg["dns"] == nil {
					t.Fatal("bundle discarded DNS")
				}
				binary := os.Getenv("TEST_SING_BOX_BINARY")
				if engine == "xray" {
					binary = os.Getenv("TEST_XRAY_BINARY")
				}
				if binary == "" {
					return
				}
				dir := t.TempDir()
				path := filepath.Join(dir, "config.json")
				os.WriteFile(path, files["config.json"], 0600)
				args := []string{"check", "-c", path}
				if engine == "xray" {
					args = []string{"run", "-test", "-c", path}
				}
				cmd := exec.Command(binary, args...)
				cmd.Dir = dir
				if out, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("%v %s\n%s", e, out, files["config.json"])
				}
			})
		}
	}
}
