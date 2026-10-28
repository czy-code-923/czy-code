package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionExactNamesAllFormatsAndPolicy(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.21.0", "platform": "linux/amd64", "addresses": []string{"192.0.2.10", "2001:db8::10"}}, 200, f.host)
	n := obj018(f.h.call("POST", "live", map[string]any{"name": "snell_pro", "host_id": f.host["id"], "engine": "sing-box", "kind": "vmess", "listen": "::", "address": "192.0.2.10", "port": 26035, "tls_mode": "none", "managed_users": true, "endpoint_addresses": []string{"192.0.2.10", "2001:db8::10"}}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+n+"/toggle", map[string]any{"enabled": true}, 200, nil)
	f.changeGroup([]string{n})
	f.ack()
	raw := strings.Split(f.url, "?")[0]
	proxies := proxies019(t, f.share(raw+"?client=mihomo", nil, 200))
	if len(proxies) != 2 || proxies[0]["name"] != "snell_pro" || proxies[1]["name"] != "snell_pro-v6" {
		t.Fatal(proxies)
	}
	for _, client := range []string{"sing-box", "xray"} {
		var config map[string]any
		json.Unmarshal(f.share(raw+"?client="+client, nil, 200).Body.Bytes(), &config)
		out := config["outbounds"].([]any)
		if out[0].(map[string]any)["tag"] != "snell_pro" || out[1].(map[string]any)["tag"] != "snell_pro-v6" {
			t.Fatal(client, out)
		}
	}
	plain, e := base64.StdEncoding.DecodeString(f.share(raw+"?client=shadowrocket", nil, 200).Body.String())
	if e != nil {
		t.Fatal(e)
	}
	for i, line := range strings.Fields(string(plain)) {
		b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "vmess://"))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		json.Unmarshal(b, &v)
		want := "snell_pro"
		if i == 1 {
			want += "-v6"
		}
		if v["ps"] != want {
			t.Fatal(v)
		}
	}
	f.h.call("POST", "client-policies", map[string]any{"name": "manual exact", "client_id": "mihomo", "source": "proxy-groups:\n- name: choice\n  type: select\n  proxies: [snell_pro-v6]\nrules: ['MATCH,choice']\n"}, 200, nil)
	var cfg map[string]any
	v := f.h.call("GET", "subscriptions/"+f.subscription+"/config?client=mihomo", nil, 200, nil)
	json.Unmarshal(v, &cfg)
	group := cfg["configuration"].(map[string]any)["proxy-groups"].([]any)[0].(map[string]any)
	if group["proxies"].([]any)[0] != "snell_pro-v6" || len(group["proxies"].([]any)) != 1 {
		t.Fatal(group)
	}
	duplicate := f.addNode("snell_pro-v6", 26036)
	f.changeGroup([]string{n, duplicate})
	f.ack()
	f.share(raw+"?client=mihomo", nil, 422)
}
