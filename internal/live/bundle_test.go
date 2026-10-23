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
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

func TestHostKernelBundleNative(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("TEST_SING_BOX_BINARY required")
	}
	sum, _ := executor.FileDigest(binary)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	runner, e := NewRunner(dir, map[string]executor.Binary{"sing-box": {Path: binary, SHA256: sum}})
	if e != nil {
		t.Fatal(e)
	}
	defer runner.Close()
	free := func() int {
		t.Helper()
		ln, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		return port
	}
	account := Account{Name: "alice", UUID: UUID(), Password: Secret()}
	members := []Work{}
	for _, id := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		members = append(members, Work{ID: id, Version: 1, Enabled: true, Spec: Spec{Kind: "vmess", TLSMode: "none", ManagedUsers: true, Accounts: []Account{account}, UUID: UUID(), Password: Secret(), Address: "127.0.0.1", Listen: "127.0.0.1", Port: free()}})
	}
	bundles := BundleWork(members)
	if len(bundles) != 1 {
		t.Fatal("expected one kernel process")
	}
	_, files, e := bundles[0].Render()
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string]any
	json.Unmarshal(files["config.json"], &cfg)
	if len(cfg["inbounds"].([]any)) != 2 || len(cfg["route"].(map[string]any)["rules"].([]any)) != 2 {
		t.Fatal("inbound routing not bundled")
	}
	if e = runner.Reconcile(context.Background(), bundles); e != nil {
		t.Fatal(e)
	}
	if len(runner.processes) != 1 {
		t.Fatal("more than one process per kernel")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "bundle-forwarding-ok") }))
	defer target.Close()
	clients := []*http.Client{}
	for i, m := range members {
		s := m.Spec
		s.UUID = account.UUID
		s.Password = account.Password
		sb, _, _, _ := s.Client("proxy")
		port := free()
		raw, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{sb}, "route": map[string]any{"final": "proxy"}})
		path := filepath.Join(t.TempDir(), "client.json")
		os.WriteFile(path, raw, 0600)
		cmd := exec.Command(binary, "run", "-c", path)
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		defer func() { cmd.Process.Kill(); cmd.Wait() }()
		u, _ := url.Parse("http://127.0.0.1:" + portString(port))
		tr := &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, Timeout: time.Second}
		clients = append(clients, client)
		ok := false
		for n := 0; n < 10; n++ {
			res, err := client.Get(target.URL)
			if err == nil {
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if string(b) == "bundle-forwarding-ok" {
					ok = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !ok {
			t.Fatalf("bundled inbound %d cannot forward", i)
		}
	}
	// Removing the user must change the entire kernel revision and reject old credentials.
	members[0].Spec.Accounts = nil
	members[0].Version++
	before := bundles[0].Revision()
	bundles = BundleWork(members)
	if before == bundles[0].Revision() {
		t.Fatal("user removal did not change revision")
	}
	if e = runner.Reconcile(context.Background(), bundles); e != nil {
		t.Fatal(e)
	}
	time.Sleep(200 * time.Millisecond)
	if res, e := clients[0].Get(target.URL); e == nil {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) == "bundle-forwarding-ok" {
			t.Fatal("revoked user still accepted")
		}
	}
	if res, e := clients[1].Get(target.URL); e != nil {
		t.Fatal("revocation broke another inbound", e)
	} else {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) != "bundle-forwarding-ok" {
			t.Fatal("another inbound lost access")
		}
	}
	// Xray's merged schema is also checked with its pinned official validator.
	if xb := os.Getenv("TEST_XRAY_BINARY"); xb != "" {
		for i := range members {
			members[i].Spec.Engine = "xray"
		}
		w := BundleWork(members)[0]
		engine, files, err := w.Render()
		if err != nil {
			t.Fatal(err)
		}
		xd := t.TempDir()
		for name, b := range files {
			os.WriteFile(filepath.Join(xd, name), b, 0600)
		}
		hash, _ := executor.FileDigest(xb)
		v := executor.NativeValidator{Binaries: map[string]executor.Binary{"xray": {Path: xb, SHA256: hash}}}
		if e = v.Check(context.Background(), xd, []manifest.Artifact{{Name: "config.json", Engine: engine, Content: files["config.json"]}}); e != nil {
			t.Fatal(e)
		}
	}
}
func TestRealityFormatAndKeyBoundaries(t *testing.T) {
	s := Spec{Kind: "vless", Engine: "xray", TLSMode: "reality", Reality: NewReality()}
	if s.ClientFormat("sing-box") == nil || s.ClientFormat("mihomo") == nil || s.ClientFormat("xray") != nil {
		t.Fatal("unverified incompatible client allowed")
	}
	s.StripServerSecrets()
	if s.Reality.PrivateKey != "" {
		t.Fatal("private key leaked")
	}
	for _, v := range []string{"0.7.9", "0.bad.0", "0.8.-1", "1.invalid.0"} {
		if BundleCapable(v) {
			t.Fatal(v)
		}
	}
}
