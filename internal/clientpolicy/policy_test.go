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

const example = `proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: [ALL]
  - name: ♻️ IPv4自动
    type: url-test
    proxies: [family:ipv4]
  - name: 📺 流媒体
    type: select
    proxies: [🚀 节点选择, ♻️ IPv4自动, DIRECT]
rules:
  - DOMAIN-SUFFIX,netflix.com,📺 流媒体
  - DOMAIN-SUFFIX,netflix.net,📺 流媒体
  - DOMAIN-SUFFIX,nflxvideo.net,📺 流媒体
  - DOMAIN-SUFFIX,example.com,protocol:vmess
  - MATCH,🚀 节点选择
`

func TestPolicies(t *testing.T) {
	p, e := Parse(example)
	if e != nil {
		t.Fatal(e)
	}
	nodes := []Node{{ID: "1", Name: "node1", Protocol: "vmess", Family: "ipv4", Tag: "n1"}, {ID: "2", Name: "node2", Protocol: "vmess", Family: "ipv6", Tag: "n2"}}
	r, e := p.Resolve(nodes, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Groups) != 5 || len(r.Groups[2].Proxies) != 1 {
		t.Fatalf("bad group expansion: %+v", r.Groups)
	}
	for _, format := range []string{"mihomo", "sing-box", "xray"} {
		t.Run(format, func(t *testing.T) {
			base := map[string]any{}
			outs := []any{}
			for _, n := range nodes {
				if format == "xray" {
					outs = append(outs, map[string]any{"tag": n.Tag, "protocol": "vmess", "settings": map[string]any{"vnext": []any{map[string]any{"address": "192.0.2.1", "port": 2080, "users": []any{map[string]any{"id": "aa5474fd-7962-4756-bf7e-5ee0e49b852a", "security": "auto"}}}}}})
				} else {
					outs = append(outs, map[string]any{"tag": n.Tag, "type": "vmess", "server": "192.0.2.1", "server_port": 2080, "uuid": "aa5474fd-7962-4756-bf7e-5ee0e49b852a", "security": "auto"})
				}
			}
			if format == "mihomo" {
				base["proxies"] = []any{map[string]any{"name": "n1", "type": "vmess", "server": "192.0.2.1", "port": 2080, "uuid": "aa5474fd-7962-4756-bf7e-5ee0e49b852a", "alterId": 0, "cipher": "auto"}, map[string]any{"name": "n2", "type": "vmess", "server": "192.0.2.2", "port": 2080, "uuid": "aa5474fd-7962-4756-bf7e-5ee0e49b852a", "alterId": 0, "cipher": "auto"}}
			} else {
				if format == "xray" {
					outs = append(outs, map[string]any{"tag": "direct", "protocol": "freedom"}, map[string]any{"tag": "block", "protocol": "blackhole"})
				} else {
					outs = append(outs, map[string]any{"tag": "direct", "type": "direct"})
				}
				base["outbounds"] = outs
			}
			cfg, _, e := r.Apply(format, base)
			if e != nil {
				t.Fatal(e)
			}
			binary := os.Getenv(map[string]string{"mihomo": "TEST_MIHOMO_BINARY", "sing-box": "TEST_SING_BOX_BINARY", "xray": "TEST_XRAY_BINARY"}[format])
			if binary == "" {
				t.Log("native binary not configured")
				return
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			raw, _ := json.Marshal(cfg)
			args := []string{"check", "-c", path}
			if format == "mihomo" {
				raw, _ = yaml.Marshal(cfg)
				args = []string{"-t", "-d", dir, "-f", path}
			} else if format == "xray" {
				args = []string{"run", "-test", "-c", path}
			}
			os.WriteFile(path, raw, 0600)
			cmd := exec.Command(binary, args...)
			cmd.Dir = dir
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("native %s: %v %s\n%s", format, e, out, raw)
			}
		})
	}
	missing, _ := p.Resolve(nil, nil)
	for _, line := range missing.Rules {
		if strings.Contains(line, ",DIRECT") {
			t.Fatal("missing nodes became DIRECT")
		}
	}
	for _, source := range []string{"proxy-groups: [{name: a, type: select, proxies: [b]}, {name: b, type: select, proxies: [a]}]", "rules: [MATCH,DIRECT]\n---\nrules: []", "rules: [DST-PORT,70000,DIRECT]"} {
		if _, e = Parse(source); e == nil {
			t.Fatalf("unsafe policy accepted %s", source)
		}
	}
}
