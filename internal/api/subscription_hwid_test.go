package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSubscriptionHWIDAutomaticallyRegistersAndEnforcesLimit(t *testing.T) {
	f := newSubscriptionFixture(t, 1)
	base := "proxy-users/" + f.user + "/devices"
	// Administrative sharing checks never invent a client or consume a slot.
	check := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/check?format=mihomo", nil, 200, nil))
	if check["ready"] != true || check["hwid_required"] != true {
		t.Fatal("limited subscription cannot be shared before first real client", check)
	}
	f.h.call("GET", "subscriptions/"+f.subscription+"/config?format=mihomo", nil, 409, nil)
	if string(f.h.call("GET", base, nil, 200, nil)) != "[]\n" {
		t.Fatal("administrator preview registered a fake device")
	}
	missing := f.share(f.url, map[string]string{"User-Agent": "Clash-Verge/2"}, 409)
	if missing.Header().Get("X-Hwid-Not-Supported") != "true" || missing.Header().Get("X-Hwid-Active") != "true" {
		t.Fatal("client cannot recognize missing hardware-ID support")
	}
	f.share(f.url, map[string]string{"X-Hwid": "short"}, 409)
	headers := map[string]string{"X-Hwid": "fixture-client-abcdefgh1234", "User-Agent": "Happ/fixture", "X-Device-Os": "Android", "X-Ver-Os": "15", "X-Device-Model": "Fixture Phone"}
	created := f.share(f.url, headers, 503)
	if created.Header().Get("Retry-After") != "3" || created.Header().Get("X-Starhub-Device-Recognition") != "client-reported-hwid" {
		t.Fatal("new identity did not wait for real deployment")
	}
	f.share(f.url, headers, 503)
	var devices []map[string]any
	json.Unmarshal(f.h.call("GET", base, nil, 200, nil), &devices)
	if len(devices) != 1 || devices[0]["registration_source"] != "subscription_hwid" || devices[0]["device_model"] != "Fixture Phone" || devices[0]["hwid_registered"] != true || devices[0]["subscription_id"] != nil {
		t.Fatal("automatic registration metadata", devices)
	}
	id := devices[0]["id"].(string)
	if raw := string(f.h.call("GET", base, nil, 200, nil)); strings.Contains(raw, headers["X-Hwid"]) || strings.Contains(raw, "hwid_hash") || strings.Contains(raw, "sealed") {
		t.Fatal("raw client fingerprint or credential leaked in list")
	}
	var stored string
	if err := f.h.s.DB.QueryRow(context.Background(), `SELECT hwid_hash FROM user_devices WHERE id=$1`, id).Scan(&stored); err != nil || stored == headers["X-Hwid"] || len(stored) != 64 {
		t.Fatal("fingerprint not stored as bounded hash", err)
	}
	work := f.ack()
	var uuid string
	for _, b := range work {
		for _, m := range b.Members {
			if m.ID == f.node {
				if len(m.Spec.Accounts) != 1 || m.Spec.Accounts[0].Name != id {
					t.Fatal("generic user identity survived enforced HWID")
				}
				uuid = m.Spec.Accounts[0].UUID
			}
		}
	}
	first := proxies019(t, f.share(f.url, headers, 200))
	if len(first) != 1 || first[0]["uuid"] != uuid {
		t.Fatal("automatic device subscription differs from deployed credential")
	}
	// Changing self-reported UA/platform or network cannot consume another slot.
	headers["User-Agent"] = "Happ/new-version"
	headers["X-Device-Model"] = "Updated Model"
	if current := proxies019(t, f.share(f.url, headers, 200)); current[0]["uuid"] != uuid {
		t.Fatal("self-reported metadata rotated device identity")
	}
	blocked := f.share(f.url, map[string]string{"X-Hwid": "different-client-abcdefgh", "User-Agent": "Happ/fixture"}, 403)
	if blocked.Header().Get("X-Hwid-Max-Devices-Reached") != "true" || blocked.Header().Get("X-Hwid-Limit") != "true" || !strings.Contains(blocked.Body.String(), "设备数量") || strings.Contains(blocked.Body.String(), uuid) {
		t.Fatal("over-limit request lacks safe client-visible feedback")
	}
	// An already imported stable user URL follows new group nodes after their ACK.
	newNode := f.addNode("HWID new inbound", 26021)
	f.changeGroup([]string{f.node, newNode})
	f.share(f.url, headers, 503)
	f.ack()
	if current := proxies019(t, f.share(f.url, headers, 200)); len(current) != 2 || current[1]["uuid"] != uuid {
		t.Fatal("automatic device did not inherit newly authorized node")
	}
	f.h.call("PUT", base+"/"+id, map[string]any{"version": devices[0]["version"], "name": "auto-phone", "client_id": "mihomo", "enabled": false}, 200, nil)
	disabled := f.share(f.url, headers, 403)
	if disabled.Header().Get("X-Starhub-Device-Error") != "device_disabled" {
		t.Fatal("disabled hardware-ID was silently re-registered")
	}
}

func TestConcurrentHWIDDownloadsCannotOverbookOrDuplicate(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(map[bool]string{true: "same-installation", false: "different-installations"}[same], func(t *testing.T) {
			f := newSubscriptionFixture(t, 1)
			start := make(chan struct{})
			codes := make([]int, 2)
			var wg sync.WaitGroup
			for i := range codes {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					hwid := "concurrent-installation-0001"
					if !same && i == 1 {
						hwid = "concurrent-installation-0002"
					}
					r := httptest.NewRequest("GET", f.url, nil)
					r.Header.Set("X-Hwid", hwid)
					w := httptest.NewRecorder()
					f.h.s.ShareHandler().ServeHTTP(w, r)
					codes[i] = w.Code
				}(i)
			}
			close(start)
			wg.Wait()
			count := 0
			for _, status := range codes {
				if status == 503 {
					count++
				} else if status != 403 {
					t.Fatal("unexpected concurrent registration response", codes)
				}
			}
			if same && count != 2 || !same && count != 1 {
				t.Fatal("limit race", codes)
			}
			var total int
			f.h.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM user_devices WHERE user_id=$1`, f.user).Scan(&total)
			if total != 1 {
				t.Fatal("duplicate or overbooked device registration", total)
			}
		})
	}
}

func TestClientDeviceMetadataSanitizesControlsAndUTF8(t *testing.T) {
	if got := clientMetadata("  Android\r\n\x00型号 ", 13); got != "Android型号" {
		t.Fatal("unsafe metadata", got)
	}
	if got := clientMetadata(strings.Repeat("界", 10), 10); len(got) != 9 {
		t.Fatal("metadata split a UTF8 character", got)
	}
}
