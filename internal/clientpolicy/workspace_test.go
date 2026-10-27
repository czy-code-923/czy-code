package clientpolicy

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceDefaultAndNative(t *testing.T) {
	p, e := Default()
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Groups) != 21 || len(p.Rules) != 186 || len(p.DNS) == 0 || len(p.Basic) == 0 {
		t.Fatal("default sections missing")
	}
	if p.Basic["allow-lan"] != false || p.Basic["external-controller"] != "127.0.0.1:9090" {
		t.Fatal("unsafe default listener")
	}
	nodes := []Node{{ID: "one", Name: "one", Protocol: "vless", Family: "ipv4", Tag: "one"}, {ID: "two", Name: "two", Protocol: "hysteria2", Family: "ipv6", Tag: "two"}}
	r, e := p.Resolve(nodes, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, format := range []string{"mihomo", "sing-box", "xray"} {
		t.Run(format, func(t *testing.T) {
			outs := []any{}
			for _, n := range nodes {
				v := map[string]any{"tag": n.Tag, "type": "socks", "server": "127.0.0.1", "server_port": 20000}
				if format == "xray" {
					v = map[string]any{"tag": n.Tag, "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 20000}}}}
				}
				outs = append(outs, v)
			}
			base := map[string]any{"outbounds": outs, "inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 1080}}}
			if format == "mihomo" {
				base = map[string]any{"proxies": []any{map[string]any{"name": "one", "type": "socks5", "server": "127.0.0.1", "port": 20000}, map[string]any{"name": "two", "type": "socks5", "server": "127.0.0.1", "port": 20001}}}
			} else if format == "xray" {
				base["inbounds"] = []any{map[string]any{"protocol": "socks", "listen": "127.0.0.1", "port": 1080, "settings": map[string]any{"udp": true}}}
				base["outbounds"] = append(outs, map[string]any{"tag": "direct", "protocol": "freedom"}, map[string]any{"tag": "block", "protocol": "blackhole"})
			} else {
				base["outbounds"] = append(outs, map[string]any{"tag": "direct", "type": "direct"})
			}
			config, warnings, e := r.Apply(format, base)
			if e != nil {
				t.Fatal(e)
			}
			if len(config["dns"].(map[string]any)) == 0 {
				t.Fatal("DNS missing")
			}
			if format != "mihomo" && len(warnings) == 0 {
				t.Fatal("conversion differences not surfaced")
			}
			binary := os.Getenv(map[string]string{"mihomo": "TEST_MIHOMO_BINARY", "sing-box": "TEST_SING_BOX_BINARY", "xray": "TEST_XRAY_BINARY"}[format])
			if binary == "" {
				return
			}
			if format == "xray" {
				assets := os.Getenv("TEST_XRAY_ASSETS")
				if assets == "" {
					assets = filepath.Dir(binary)
				}
				if _, e := os.Stat(filepath.Join(assets, "geoip.dat")); e != nil {
					t.Skip("native Xray Geo rules require the official geoip/geosite assets")
				}
			}
			dir := t.TempDir()
			if format == "mihomo" {
				if assets := os.Getenv("TEST_MIHOMO_ASSETS"); assets != "" {
					for _, name := range []string{"geoip.dat", "geosite.dat"} {
						raw, e := os.ReadFile(filepath.Join(assets, name))
						if e != nil {
							t.Fatal(e)
						}
						if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			file := filepath.Join(dir, "config.json")
			raw, _ := json.Marshal(config)
			args := []string{"check", "-c", file}
			if format == "mihomo" {
				raw, _ = yaml.Marshal(config)
				args = []string{"-t", "-d", dir, "-f", file}
			} else if format == "xray" {
				args = []string{"run", "-test", "-c", file}
			}
			os.WriteFile(file, raw, 0600)
			cmd := exec.Command(binary, args...)
			cmd.Dir = dir
			if format == "xray" && os.Getenv("TEST_XRAY_ASSETS") != "" {
				cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+os.Getenv("TEST_XRAY_ASSETS"))
			}
			if output, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("native %s %v: %s", format, e, output)
			}
		})
	}
	conf, warnings, e := r.Shadowrocket()
	if e != nil || !strings.Contains(conf, "[Rule]") || !strings.Contains(conf, "FINAL,PROXY") || !strings.Contains(conf, "RULE-SET,https://") || len(warnings) == 0 {
		t.Fatal("Shadowrocket conversion", e)
	}
	imported, e := Import(conf)
	if e != nil || len(imported.Policy.Rules) != len(r.Rules) || imported.UnsupportedRules != 0 {
		t.Fatal("own Shadowrocket configuration did not round-trip", e)
	}
}
func TestImportSeparatesIdentitiesAndConditions(t *testing.T) {
	input := `{"mixed-port":7890,"allow-lan":true,"external-controller":"0.0.0.0:9090","dns":{"listen":"0.0.0.0:1053","nameserver":["1.1.1.1"]},"proxies":[{"name":"old","type":"vless","server":"192.0.2.1","uuid":"SUPER_PRIVATE_ID"}],"proxy-groups":[{"name":"choice","type":"select","proxies":["old"]}],"rules":["MATCH,choice"]}`
	r, e := Import(input)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "SUPER_PRIVATE_ID") {
		t.Fatal("node credentials leaked into import result")
	}
	if r.Policy.Basic["allow-lan"] != false || r.Policy.DNS["listen"] != "127.0.0.1:1053" || len(r.Aliases) != 1 {
		t.Fatal("listener/mapping normalization")
	}
	for _, format := range []string{"sing-box", "xray"} {
		source := `{"outbounds":[{"tag":"old","type":"vless","server":"192.0.2.1","uuid":"DO_NOT_KEEP"},{"tag":"choice","type":"selector","outbounds":["old"]}],"route":{"rules":[{"domain_suffix":["example.org"],"outbound":"choice"}],"final":"choice"}}`
		if format == "xray" {
			source = `{"outbounds":[{"tag":"old","protocol":"vmess","settings":{"vnext":[{"address":"192.0.2.1","users":[{"id":"DO_NOT_KEEP"}]}]}}],"routing":{"rules":[{"domain":["domain:example.org"],"outboundTag":"old"}]}}`
		}
		r, e := Import(source)
		if e != nil {
			t.Fatal(format, e)
		}
		if !strings.Contains(strings.Join(r.Policy.Rules, "\n"), "DOMAIN-SUFFIX,example.org") {
			t.Fatal("native rule recognition", r.Policy)
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "DO_NOT_KEEP") {
			t.Fatal("native credentials leaked")
		}
	}
	if _, e = importNativeRule(map[string]any{"domain": []any{"a"}, "port": "443", "outboundTag": "proxy"}, "xray"); e == nil {
		t.Fatal("AND silently became OR")
	}
	p := Policy{Groups: []Group{{Name: "a", Type: "select", Proxies: []string{"old"}}}, Rules: []string{"DOMAIN-SUFFIX,example.org,old", "MATCH,a"}, DNS: map[string]any{"nameserver": []string{"https://1.1.1.1/dns-query#old"}}}
	p.ReplaceReference("old", "node:new")
	if p.Groups[0].Proxies[0] != "node:new" || !strings.HasSuffix(p.Rules[0], ",node:new") || !strings.HasSuffix(stringsOf(p.DNS["nameserver"])[0], "#node:new") {
		t.Fatal("reference rename incomplete")
	}
	for _, source := range []string{"basic: {external-controller: '0.0.0.0:9090'}\nrules: [MATCH,DIRECT]", "rules: ['GEOSITE,../../bad,DIRECT']"} {
		if _, e := Parse(source); e == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	for _, source := range []string{`{"outbounds":[{"tag":"proxy","type":"selector","outbounds":["old"]},{"tag":"old","type":"vless","server":"192.0.2.1"}],"route":{"rules":[{"action":"reject"}],"final":"proxy"}}`, `{"outbounds":[{"tag":"old","type":"vless","server":"192.0.2.1"}],"route":{"rule_set":[{"tag":"cn","url":"https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs"}],"rules":[{"rule_set":["cn"],"outbound":"old","action":"route"}],"final":"old"}}`} {
		v, e := Import(source)
		if e != nil {
			t.Fatal("native roundtrip", e)
		}
		if v.UnsupportedRules != 0 {
			t.Fatal("own compiler rules not recognized", v.Warnings)
		}
	}
}
