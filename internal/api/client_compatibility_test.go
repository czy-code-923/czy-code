package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppSubscriptionsHaveSelectableCompatibleNodes(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	snell := obj018(f.h.call("POST", "live", map[string]any{"name": "only-snell", "host_id": f.host["id"], "kind": "snell", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 26322}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+snell+"/toggle", map[string]any{"enabled": true}, 200, nil)
	f.changeGroup([]string{f.node, snell})
	f.ack()
	f.h.call("POST", "client-policies", map[string]any{"name": "filter-test", "client_id": "all", "source": "proxy-groups:\n- name: removed\n  type: select\n  proxies: [protocol:snell]\n- name: usable\n  type: select\n  proxies: [removed, ALL]\nrules: ['DOMAIN-SUFFIX,blocked.example,removed', 'MATCH,usable']\n"}, 200, nil)
	raw := strings.Split(f.url, "?")[0]
	for _, c := range []string{"v2rayng", "v2rayn", "nekobox", "shadowrocket", "happ", "v2raytun"} {
		w := f.share(raw+"?client="+c, nil, 200)
		decoded, err := base64.StdEncoding.DecodeString(w.Body.String())
		if err != nil || !strings.HasPrefix(string(decoded), "vmess://") || strings.Contains(string(decoded), "snell://") || w.Header().Get("X-Starhub-Excluded-Nodes") != "1" {
			t.Fatal(c, w.Header(), err)
		}
	}
	for _, c := range []string{"hiddify", "sing-box", "sfa"} {
		w := f.share(raw+"?client="+c, nil, 200)
		var cfg map[string]any
		json.Unmarshal(w.Body.Bytes(), &cfg)
		hasTun := false
		for _, v := range cfg["inbounds"].([]any) {
			if object(v)["type"] == "tun" {
				hasTun = true
			}
		}
		if !hasTun || object(object(cfg["experimental"])["clash_api"])["external_controller"] != "127.0.0.1:9090" {
			t.Fatal(c, "missing App TUN/selector API")
		}
		if c == "hiddify" && strings.Contains(w.Body.String(), "only-snell") {
			t.Fatal("Hiddify received unsupported Snell")
		}
		if c == "hiddify" && strings.Contains(w.Body.String(), `"tag":"removed"`) {
			t.Fatal("empty group survived")
		}
	}
	f.changeGroup([]string{snell})
	f.ack()
	for _, c := range []string{"hiddify", "v2rayng", "nekobox"} {
		f.share(raw+"?client="+c, nil, 422)
	}
}

func TestUserSharedCountersAreNotPersonalBilling(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	users := f.h.call("GET", "proxy-users", nil, 200, nil)
	if !strings.Contains(string(users), `"metered_nodes": 1`) && !strings.Contains(string(users), `"metered_nodes":1`) {
		t.Fatal(string(users))
	}
}

func TestAppDefaultPolicyValidatesWithOfficialCore(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("official sing-box binary required")
	}
	f := newSubscriptionFixture(t, 0)
	if err := f.h.s.EnsureDefaultClientPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(f.url, "?")[0]
	for _, c := range []string{"sing-box", "sfa", "hiddify"} {
		w := f.share(raw+"?client="+c, nil, 200)
		path := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(path, w.Body.Bytes(), 0600)
		result, err := exec.Command(binary, "check", "-c", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s rejected by official core: %s", c, result)
		}
	}
}
