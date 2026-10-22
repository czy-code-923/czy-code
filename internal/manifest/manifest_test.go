package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectUnsafeManifests(t *testing.T) {
	base := Spec{Schema: 1, Mode: "simulation", Engine: "frp", Listen: "127.0.0.1", Port: 7000, Outbound: "block", Tunnels: []Tunnel{{LocalHost: "127.0.0.1", LocalPort: 8080, RemotePort: 18080}}}
	cases := []struct {
		name   string
		change func(*Spec)
	}{
		{"public listener", func(s *Spec) { s.Listen = "0.0.0.0" }},
		{"metadata target", func(s *Spec) { s.Tunnels[0].LocalHost = "169.254.169.254" }},
		{"zero target port", func(s *Spec) { s.Tunnels[0].LocalPort = 0 }},
		{"remote listen conflict", func(s *Spec) { s.Tunnels[0].RemotePort = 7000 }},
		{"duplicate remote", func(s *Spec) { s.Tunnels = append(s.Tunnels, s.Tunnels[0]) }},
		{"unknown outbound", func(s *Spec) { s.Outbound = "shell" }},
		{"wrong engine tunnels", func(s *Spec) { s.Engine = "sing-box" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base
			s.Tunnels = append([]Tunnel{}, base.Tunnels...)
			c.change(&s)
			raw, _ := json.Marshal(s)
			if _, e := Parse(raw); e == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
	raw, _ := json.Marshal(base)
	canonical, _ := Canonical(raw)
	if _, e := Verify(raw, Digest(canonical)); e != nil {
		t.Fatal(e)
	}
	if _, e := Verify(raw, strings.Repeat("0", 64)); e == nil {
		t.Fatal("wrong checksum accepted")
	}
	if _, e := Parse([]byte(`{"schema":1,"shell":"id"}`)); e == nil {
		t.Fatal("shell field accepted")
	}
}
func TestRenderSafeNativeConfig(t *testing.T) {
	s := Spec{Schema: 1, Mode: "simulation", Engine: "sing-box", Listen: "127.0.0.1", Port: 2080, Outbound: "block"}
	files, e := Render(s, strings.Repeat("a", 32), strings.Repeat("secret", 8))
	if e != nil {
		t.Fatal(e)
	}
	var native map[string]any
	json.Unmarshal(files[0].Content, &native)
	if native["route"].(map[string]any)["rules"].([]any)[0].(map[string]any)["action"] != "reject" {
		t.Fatal("block policy does not reject")
	}
	if bytes := string(files[0].Content); strings.Contains(bytes, `"type": "block"`) || !strings.Contains(bytes, `"users"`) {
		t.Fatal("deprecated outbound or missing auth")
	}
	s.Engine = "frp"
	files, e = Render(s, strings.Repeat("a", 32), strings.Repeat("secret", 8))
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(files[0].Content, &native)
	if len(native["allowPorts"].([]any)) == 0 {
		t.Fatal("empty allowPorts opens all ports")
	}
}
