package live

import (
	"encoding/json"
	"testing"
)

func TestSSNativeListenerAndClientNetworks(t *testing.T) {
	for _, core := range []string{"sing-box", "xray"} {
		for _, network := range []string{"", "tcp,udp", "tcp", "udp"} {
			t.Run(core+"/"+network, func(t *testing.T) {
				s := Spec{Kind: "shadowsocks", Engine: core, Listen: "127.0.0.1", Address: "192.0.2.1", Port: 25419, Password: Secret(), ProtocolOptions: ProtocolOptions{ListenNetwork: network}}
				_, files, e := s.Render()
				if e != nil {
					t.Fatal(e)
				}
				var config map[string]any
				if e = json.Unmarshal(files["config.json"], &config); e != nil {
					t.Fatal(e)
				}
				in := config["inbounds"].([]any)[0].(map[string]any)
				if core == "xray" {
					if in["settings"].(map[string]any)["network"] != s.nativeListenNetwork() {
						t.Fatal("wrong Xray listen network", in)
					}
				} else if network == "tcp" || network == "udp" {
					if in["network"] != network {
						t.Fatal("wrong sing-box listen network", in)
					}
				} else if _, ok := in["network"]; ok {
					t.Fatal("sing-box dual network must use omitted field")
				}
				_, mh, _, e := s.Client("ss")
				if e != nil || mh["udp"] != (network != "tcp") {
					t.Fatal("client UDP mismatch", mh, e)
				}
				ports := portNetworks(s)
				if network == "tcp" || network == "udp" {
					if len(ports) != 1 || ports[0] != network {
						t.Fatal(ports)
					}
				} else if len(ports) != 2 {
					t.Fatal(ports)
				}
			})
		}
	}
	s := Spec{Kind: "vmess", ProtocolOptions: ProtocolOptions{ListenNetwork: "udp"}}
	if s.validateOptions() == nil {
		t.Fatal("network field leaked into another protocol")
	}
	if V19Capable("0.18.9") || !V19Capable("0.19.0") || !V19Capable("1.0.0") || V19Capable("broken") {
		t.Fatal("Agent capability version gate")
	}
}

func TestSOCKSUDPIsNotSilentlyDisabled(t *testing.T) {
	s := Spec{Kind: "socks", Engine: "xray", Listen: "127.0.0.1", Address: "192.0.2.1", Port: 25420, Password: "test-password"}
	_, files, e := s.Render()
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string]any
	json.Unmarshal(files["config.json"], &cfg)
	if cfg["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["udp"] != true {
		t.Fatal("Xray UDP is disabled")
	}
	_, mh, _, e := s.Client("socks")
	if e != nil || mh["udp"] != true {
		t.Fatal("Mihomo SOCKS UDP is disabled", e)
	}
}
