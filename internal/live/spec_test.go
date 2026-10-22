package live

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSpecAndEncryption(t *testing.T) {
	s := Spec{Kind: "shadowsocks", Listen: "::", Address: "2001:db8::1", Port: 22000, Password: Secret()}
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	engine, files, e := s.Render()
	if e != nil || engine != "sing-box" || !bytes.Contains(files["config.json"], []byte(Method)) {
		t.Fatal(engine, e)
	}
	for _, a := range []string{"bad;id", "https://example.com", "[::1]", "::", "fe80::1%eth0", "a\nexample.com", "-x.test"} {
		if Address(a) {
			t.Fatal("unsafe address", a)
		}
	}
	for _, a := range []string{"https://vps.example.com", "http://vps.example.com"} {
		if !Origin(a) {
			t.Fatal(a)
		}
	}
	for _, a := range []string{"https://vps.example.com/path", "https://a@vps.example.com", "https://vps.example.com?x=y", "https://vps.example.com:444"} {
		if Origin(a) {
			t.Fatal(a)
		}
	}
	raw, _ := json.Marshal(s)
	key := bytes.Repeat([]byte{1}, 32)
	sealed, e := Seal(key, raw)
	if e != nil || bytes.Contains(sealed, []byte(s.Password)) {
		t.Fatal(e)
	}
	plain, e := Open(key, sealed)
	if e != nil || !bytes.Equal(raw, plain) {
		t.Fatal(e)
	}
	sealed[len(sealed)-1] ^= 1
	if _, e = Open(key, sealed); e == nil {
		t.Fatal("tampering accepted")
	}
	s.Listen = "example.com"
	if s.Validate() == nil {
		t.Fatal("nonlocal bind")
	}
}
func TestFRPPair(t *testing.T) {
	cert, key, e := Certificate("::1")
	if e != nil {
		t.Fatal(e)
	}
	s := Spec{Kind: "frps", Listen: "::1", Address: "::1", Port: 22001, Password: Secret(), Certificate: cert, PrivateKey: key, Tunnels: []Tunnel{{Name: "app", LocalHost: "::1", LocalPort: 3000, RemotePort: 23000}}}
	if _, _, e = s.Render(); e != nil {
		t.Fatal(e)
	}
	s.Kind = "frpc"
	if s.Validate() == nil {
		t.Fatal("server key leaked to client")
	}
	s.PrivateKey = ""
	_, files, e := s.Render()
	if e != nil || bytes.Contains(files["config.json"], []byte("insecure")) {
		t.Fatal(e)
	}
	s.Tunnels[0].RemotePort = 22001
	if s.Validate() == nil {
		t.Fatal("overlap accepted")
	}
}
