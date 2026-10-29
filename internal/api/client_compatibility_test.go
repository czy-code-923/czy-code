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
	for _, c := range []string{"v2rayng", "v2rayn", "nekobox-uri", "shadowrocket", "happ", "v2raytun"} {
		w := f.share(raw+"?client="+c, nil, 200)
		decoded, err := base64.StdEncoding.DecodeString(w.Body.String())
		if err != nil || !strings.HasPrefix(string(decoded), "vmess://") || strings.Contains(string(decoded), "snell://") || w.Header().Get("X-Starhub-Excluded-Nodes") != "1" {
			t.Fatal(c, w.Header(), err)
		}
	}
	w := f.share(raw+"?client=nekobox", nil, 200)
	if len(proxies019(t, w)) != 1 || strings.Contains(w.Body.String(), "proxy-groups:") || strings.Contains(w.Body.String(), "rules:") {
		t.Fatal("NekoBox must receive plain supported proxies only")
	}
	for _, c := range []string{"hiddify", "sing-box", "sfa"} {
		w := f.share(raw+"?client="+c, nil, 200)
		var cfg map[string]any
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if e := validateClientGraph("sing-box", cfg); e != nil {
			t.Fatal(e)
		}
		hasTun := false
		for _, v := range cfg["inbounds"].([]any) {
			if object(v)["type"] == "tun" {
				hasTun = true
			}
		}
		if !hasTun || object(object(cfg["experimental"])["clash_api"])["external_controller"] != "127.0.0.1:9090" {
			t.Fatal(c, "missing App TUN/selector API")
		}
		if strings.Contains(w.Body.String(), "only-snell") {
			t.Fatal(c, "received Snell without a confirmed compatible core")
		}
		if strings.Contains(w.Body.String(), `"tag":"removed"`) {
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

func TestAndroidVLESSRealitySubscriptions(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	id := obj018(f.h.call("POST", "live", map[string]any{"name": "vless-android", "host_id": f.host["id"], "kind": "vless", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 26331, "tls_mode": "reality", "server_name": "www.apple.com", "flow": "xtls-rprx-vision", "reality": map[string]any{"target": "www.apple.com", "port": 443, "fingerprint": "chrome"}, "managed_users": true}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+id+"/toggle", map[string]any{"enabled": true}, 200, nil)
	f.changeGroup([]string{id})
	f.ack()
	raw := strings.Split(f.url, "?")[0]
	w := f.share(raw+"?client=nekobox", nil, 200)
	nodes := proxies019(t, w)
	if len(nodes) != 1 || nodes[0]["type"] != "vless" || nodes[0]["servername"] != "www.apple.com" || nodes[0]["uuid"] == "" || nodes[0]["reality-opts"] == nil {
		t.Fatal("NekoBox VLESS/REALITY fields missing")
	}
	for _, c := range []string{"nekobox-uri", "v2rayng"} {
		w = f.share(raw+"?client="+c, nil, 200)
		decoded, e := base64.StdEncoding.DecodeString(w.Body.String())
		if e != nil || !strings.HasPrefix(string(decoded), "vless://") || !strings.Contains(string(decoded), "security=reality") || !strings.Contains(string(decoded), "pbk=") {
			t.Fatal(c, "VLESS URI fields missing")
		}
	}
	auto := f.share(raw, map[string]string{"User-Agent": "Dart/3.8 (dart:io)"}, 200)
	plain, e := base64.StdEncoding.DecodeString(auto.Body.String())
	if e != nil || !strings.HasPrefix(string(plain), "vless://") {
		t.Fatal("unrecognized Hiddify-style UA must receive portable nodes")
	}
	for _, c := range []string{"sing-box", "hiddify"} {
		w = f.share(raw+"?client="+c, nil, 200)
		var cfg map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &cfg); e != nil || validateClientGraph("sing-box", cfg) != nil {
			t.Fatal(c, "invalid config")
		}
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
		var config map[string]any
		json.Unmarshal(w.Body.Bytes(), &config)
		if e := validateClientGraph("sing-box", config); e != nil {
			t.Fatal(e)
		}
		for _, v := range array(object(config["route"])["rule_set"]) {
			if object(v)["download_detour"] != object(config["route"])["final"] {
				t.Fatal("rule set downloader references removed default selector")
			}
		}
		path := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(path, w.Body.Bytes(), 0600)
		result, err := exec.Command(binary, "check", "-c", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s rejected by official core: %s", c, result)
		}
	}
}
