package api

import (
	"strings"
	"testing"
)

func TestAuthorizedSharedProtocolsFollowExistingUserSubscription(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	create := func(name, kind string, port int) string {
		id := obj018(f.h.call("POST", "live", map[string]any{"name": name, "host_id": f.host["id"], "kind": kind, "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": port}, 201, nil))["id"].(string)
		f.h.call("POST", "live/"+id+"/toggle", map[string]any{"enabled": true}, 200, nil)
		return id
	}
	snell, ss := create("shared-snell", "snell", 26341), create("shared-ss", "shadowsocks", 26342)
	f.changeGroup([]string{f.node, snell, ss})
	deployed := f.ack()
	rows := proxies019(t, f.share(f.url, nil, 200))
	if len(rows) != 3 {
		t.Fatalf("expected managed VMess and both shared nodes, got %v", len(rows))
	}
	var snellKey, ssKey string
	for _, row := range rows {
		switch row["name"] {
		case "shared-snell":
			snellKey, _ = row["psk"].(string)
		case "shared-ss":
			ssKey, _ = row["password"].(string)
		}
	}
	if snellKey != findMember(t, deployed, snell).Spec.Password || ssKey != findMember(t, deployed, ss).Spec.Password {
		t.Fatal("exported shared key differs from deployed inbound")
	}
	if snellKey == "" || ssKey == "" {
		t.Fatal("shared protocol credentials missing from compatible client format")
	}
	f.changeGroup([]string{f.node, ss})
	f.ack()
	rows = proxies019(t, f.share(f.url, nil, 200))
	if len(rows) != 2 {
		t.Fatalf("permission removal did not update existing link: %d", len(rows))
	}
	for _, row := range rows {
		if row["name"] == "shared-snell" {
			t.Fatal("ungranted shared node leaked")
		}
	}
	f.changeGroup([]string{snell})
	f.ack()
	if rows = proxies019(t, f.share(f.url, nil, 200)); len(rows) != 1 || rows[0]["name"] != "shared-snell" || rows[0]["psk"] != snellKey {
		t.Fatal("shared-only user should have usable Snell")
	}
	denied := f.share(strings.Split(f.url, "?")[0]+"?client=v2rayng", nil, 422)
	if !strings.Contains(denied.Body.String(), "兼容") {
		t.Fatal("incompatible client must get a clear error")
	}
}

func TestHWIDSharedOnlySubscriptionRespectsSubscriptionSlots(t *testing.T) {
	f := newSubscriptionFixture(t, 1)
	snell := obj018(f.h.call("POST", "live", map[string]any{"name": "shared-device-snell", "host_id": f.host["id"], "kind": "snell", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 26343}, 201, nil))["id"].(string)
	f.h.call("POST", "live/"+snell+"/toggle", map[string]any{"enabled": true}, 200, nil)
	f.changeGroup([]string{snell})
	f.ack()
	headers := map[string]string{"X-Hwid": "shared-fixture-device-12345"}
	rows := proxies019(t, f.share(f.url, headers, 200))
	if len(rows) != 1 || rows[0]["name"] != "shared-device-snell" || rows[0]["psk"] == "" {
		t.Fatal("registered device did not receive authorized shared node")
	}
	f.share(f.url, map[string]string{"X-Hwid": "different-device-123456789"}, 403)
}
