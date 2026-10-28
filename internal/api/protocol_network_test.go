package api

import "testing"

func TestNativeSSNetworkRequiresCompatibleAgentAndPreservesEdits(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	for _, version := range []string{"0.18.0", "0.19.0"} {
		host := obj018(h.call("POST", "hosts", map[string]any{"name": "network-" + version, "address": "192.0.2.1"}, 201, nil))
		h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": version, "platform": "linux/amd64"}, 200, host)
		input := map[string]any{"host_id": host["id"], "name": "SS UDP", "kind": "shadowsocks", "engine": "sing-box", "address": "192.0.2.1", "listen": "127.0.0.1", "port": 25319, "listen_network": "udp"}
		if version == "0.18.0" {
			h.call("POST", "live", input, 409, nil)
			continue
		}
		node := obj018(h.call("POST", "live", input, 201, nil))
		detail := obj018(h.call("GET", "live/"+node["id"].(string), nil, 200, nil))
		if detail["spec"].(map[string]any)["listen_network"] != "udp" {
			t.Fatal("network selection was dropped")
		}
		input["version"] = detail["version"]
		input["listen_network"] = "tcp"
		h.call("PUT", "live/"+node["id"].(string), input, 200, nil)
		detail = obj018(h.call("GET", "live/"+node["id"].(string), nil, 200, nil))
		if detail["spec"].(map[string]any)["listen_network"] != "tcp" {
			t.Fatal("edited network was dropped")
		}
	}
}
