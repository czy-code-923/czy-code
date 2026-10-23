package live

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestInvalidInstanceDoesNotBlockStop(t *testing.T) {
	directory := t.TempDir()
	if e := os.Chmod(directory, 0700); e != nil {
		t.Fatal(e)
	}
	r, e := NewRunner(directory, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	r.processes[b] = &process{version: 1, state: "failed"}
	if e = r.Reconcile(context.Background(), []Work{{ID: a, Version: 1, Enabled: true, Spec: Spec{Kind: "vless"}}, {ID: b, Version: 2, Enabled: false, Spec: Spec{Kind: "vless"}}}); e != nil {
		t.Fatal(e)
	}
	if r.processes[a].state != "failed" || r.processes[b].state != "stopped" || r.processes[b].version != 2 {
		t.Fatal("invalid instance prevented another stop")
	}
}

func TestOutboundNeverCarriesServerPrivateKey(t *testing.T) {
	cert, key, e := Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	peer := Spec{Kind: "trojan", Address: "localhost", Listen: "127.0.0.1", Port: 22001, Password: Secret(), ServerName: "localhost", Certificate: cert, PrivateKey: key}
	entry := Spec{Kind: "shadowsocks", Address: "localhost", Listen: "127.0.0.1", Port: 22002, Password: Secret(), Outbound: &peer}
	if entry.Validate() == nil {
		t.Fatal("server private key accepted in outbound")
	}
	peer.PrivateKey = ""
	if e = entry.Validate(); e != nil {
		t.Fatal(e)
	}
	_, files, e := entry.Render()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(files["config.json"]), "PRIVATE KEY") {
		t.Fatal("outbound configuration leaked key")
	}
}
