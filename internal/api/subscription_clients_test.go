package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (f subscriptionFixture) compatible(limit int) {
	f.h.t.Helper()
	f.h.call("PUT", "proxy-users/"+f.user, map[string]any{"name": "existing-user", "enabled": true, "expires_at": time.Now().Add(24 * time.Hour), "device_limit_mode": "compatible", "max_devices": limit}, 200, nil)
	f.ack()
}
func (f subscriptionFixture) observations() []map[string]any {
	f.h.t.Helper()
	var rows []map[string]any
	if e := json.Unmarshal(f.h.call("GET", "proxy-users/"+f.user+"/devices", nil, 200, nil), &rows); e != nil {
		f.h.t.Fatal(e)
	}
	return rows
}
func TestClashCompatibleDeviceLimitAndObservation(t *testing.T) {
	f := newSubscriptionFixture(t, 1)
	f.compatible(1)
	raw := strings.Split(f.url, "?")[0]
	for _, ua := range []string{"Clash-Verge/2.4", "FlClash/0.8"} {
		w := f.share(raw, map[string]string{"User-Agent": ua}, 200)
		if len(proxies019(t, w)) != 1 || w.Header().Get("X-Starhub-Device-Recognition") != "source_and_client" {
			t.Fatal("ordinary Clash failed")
		}
	}
	rows := f.observations()
	if len(rows) != 1 || rows[0]["registration_source"] != "observed_client" || rows[0]["user_agent"] != "FlClash/0.8" || rows[0]["source_ip"] == nil {
		t.Fatal("missing ordinary client metadata", rows)
	}
	if _, ok := rows[0]["fingerprint"]; ok {
		t.Fatal("private fingerprint exposed")
	}
	// A direct public peer cannot spoof the reverse proxy's trusted address header.
	f.share(raw, map[string]string{"User-Agent": "FlClash/0.8", "X-Real-IP": "203.0.113.2"}, 200)
	f.share(raw, map[string]string{"User-Agent": "Shadowrocket/2.2"}, 403)
	id := rows[0]["id"].(string)
	f.h.call("PUT", "proxy-users/"+f.user+"/devices/"+id, map[string]any{"name": "Laptop", "enabled": false, "version": rows[0]["version"]}, 200, nil)
	f.share(raw, map[string]string{"User-Agent": "FlClash/0.8"}, 403)
	f.share(raw, map[string]string{"User-Agent": "Shadowrocket/2.2"}, 200)
	f.h.call("PUT", "proxy-users/"+f.user+"/devices/"+id, map[string]any{"name": "Laptop", "enabled": true, "version": 2}, 409, nil)
	f.h.call("DELETE", "proxy-users/"+f.user+"/devices/"+id, nil, 200, nil)
	var count int
	f.h.s.DB.QueryRow(context.Background(), "SELECT count(*) FROM user_devices WHERE user_id=$1", f.user).Scan(&count)
	if count != 0 {
		t.Fatal("compatibility creates unnecessary proxy identities")
	}
}
func TestUnlimitedClientsRecordedAndAdminCheckDoesNotReserveSlots(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	f.h.call("GET", "subscriptions/"+f.subscription+"/check?client=mihomo", nil, 200, nil)
	if len(f.observations()) != 0 {
		t.Fatal("admin check reserves a client slot")
	}
	f.share(f.url, map[string]string{"User-Agent": "Clash-Verge/2.4"}, 200)
	if len(f.observations()) != 1 {
		t.Fatal("unlimited user client not recorded")
	}
	// Compatible subscription import doesn't change the deployed account version.
	var pending bool
	f.h.s.DB.QueryRow(context.Background(), "SELECT state!='running' FROM live_services WHERE id=$1", f.node).Scan(&pending)
	if pending {
		t.Fatal("observation queued a deployment")
	}
}
func TestUserShareReusesOriginalLinkAndFormatMatrix(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	for i := 0; i < 2; i++ {
		got := obj018(f.h.call("POST", "proxy-users/"+f.user+"/subscription", map[string]any{}, 200, nil))
		if got["id"] != f.subscription || got["url"] != strings.Split(f.url, "?")[0] || got["reused"] != true {
			t.Fatal("share recreated or rotated original", got["id"])
		}
	}
	raw := strings.Split(f.url, "?")[0]
	// Every application alias produces an appropriate parseable configuration.
	for _, c := range clients {
		if c.ID == "naive-proxy" {
			continue
		} // VMess is deliberately incompatible with Naive.
		w := f.share(raw+"?client="+c.ID, nil, 200)
		switch c.Format {
		case "sing-box", "xray":
			var config map[string]any
			if e := json.Unmarshal(w.Body.Bytes(), &config); e != nil || len(config["outbounds"].([]any)) == 0 {
				t.Fatal(c.ID, "invalid JSON")
			}
		case "mihomo":
			if len(proxies019(t, w)) != 1 {
				t.Fatal(c.ID, "missing proxy")
			}
		case "base64", "shadowrocket":
			plain, e := base64.StdEncoding.DecodeString(w.Body.String())
			if e != nil || !strings.Contains(string(plain), "vmess://") {
				t.Fatal(c.ID, "invalid node subscription")
			}
		case "shadowrocket-config":
			if !strings.Contains(w.Body.String(), "[Rule]") {
				t.Fatal("missing separate Shadowrocket rules")
			}
		}
	}
	auto := f.share(raw, map[string]string{"User-Agent": "Shadowrocket/2.2"}, 200)
	manual := f.share(raw+"?client=shadowrocket", nil, 200)
	if auto.Body.String() != manual.Body.String() {
		t.Fatal("automatic/manual Shadowrocket differ")
	}
	f.share(raw+"?client=flclash", nil, 200)
}
func TestSubscriptionPeerTrustBoundary(t *testing.T) {
	for _, v := range []struct{ peer, header, want string }{{"192.0.2.1:5000", "203.0.113.1", "192.0.2.1"}, {"127.0.0.1:8091", "203.0.113.1", "203.0.113.1"}, {"[::1]:8091", "2001:db8::2", "2001:db8::2"}, {"127.0.0.1:8091", "garbage", "127.0.0.1"}} {
		r := httptest.NewRequest("GET", "http://example.test", nil)
		r.RemoteAddr = v.peer
		r.Header.Set("X-Real-IP", v.header)
		if got := subscriptionPeer(r); got != v.want {
			t.Fatal(v, got)
		}
	}
}
