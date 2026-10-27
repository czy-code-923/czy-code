package live

import "testing"

func TestProtocolOptionBoundaries(t *testing.T) {
	for _, s := range []Spec{
		{Kind: "trojan", ProtocolOptions: ProtocolOptions{SnellVersion: 6}},
		{Kind: "snell", Password: Secret(), ProtocolOptions: ProtocolOptions{SnellVersion: 6, SnellMode: "unsafe-raw"}},
		{Kind: "snell", Password: Secret(), ProtocolOptions: ProtocolOptions{SnellVersion: 6, SnellObfs: "http"}},
		{Kind: "vmess", ProtocolOptions: ProtocolOptions{HY2Obfs: "should-not-apply"}},
		{Kind: "shadowsocks", ProtocolOptions: ProtocolOptions{Cipher: "none"}},
	} {
		if s.validateOptions() == nil {
			t.Fatalf("unsafe combination accepted: %s", s.Kind)
		}
	}
	s := Spec{Kind: "snell", ProtocolOptions: ProtocolOptions{SnellVersion: 6}}
	if s.ClientFormat("mihomo") == nil || s.ClientFormat("sing-box") != nil {
		t.Fatal("Snell v6 compatibility is not enforced")
	}
	if OptionsCapable("0.8.9") || !OptionsCapable("0.9.0") || OptionsCapable("0.9.bad") {
		t.Fatal("Agent version gate")
	}
}
