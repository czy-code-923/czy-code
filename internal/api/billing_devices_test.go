package api

import (
	"bytes"
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vps-control/internal/hostmeter"
	"vps-control/internal/live"
)

type api018 struct {
	t *testing.T
	s *Server
}

func (h api018) call(method, path string, body any, status int, agent map[string]any) []byte {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+h.s.AdminToken)
	if agent != nil {
		r.Header.Set("Authorization", "Bearer "+agent["agent_token"].(string))
		r.Header.Set("X-Host-ID", agent["id"].(string))
	}
	w := httptest.NewRecorder()
	h.s.Handler().ServeHTTP(w, r)
	if w.Code != status {
		h.t.Fatalf("%s %s: %d want %d %s", method, path, w.Code, status, w.Body)
	}
	return w.Body.Bytes()
}
func obj018(b []byte) map[string]any { var v map[string]any; json.Unmarshal(b, &v); return v }
func TestHostQuotaMaximumCalibrationAndRetry(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	ctx := context.Background()
	initial := int64(100)
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "metered", "traffic": hostTrafficInput{Quota: 1000, Mode: "max", ResetDay: 5, AutoReset: true, InitialUsed: &initial}}, 201, nil))
	id := host["id"].(string)
	path := "hosts/" + id + "/traffic"
	now := time.Now().UTC()
	report := hostmeter.Report{Epoch: strings.Repeat("a", 32), Started: now.Add(-time.Minute), At: now, State: "collecting", Interfaces: []hostmeter.Interface{{Name: "eth0", RX: 100, TX: 60, Uplink: true}, {Name: "docker0", RX: 10000, TX: 20000}}}
	h.call("POST", "agent/host-traffic", report, 200, host)
	report.At = now.Add(time.Second)
	report.Interfaces[0].RX = 120
	report.Interfaces[0].TX = 150
	h.call("POST", "agent/host-traffic", report, 200, host)
	h.call("POST", "agent/host-traffic", report, 200, host)
	state := obj018(h.call("GET", path, nil, 200, nil))
	if state["used_bytes"] != float64(250) || state["remaining_bytes"] != float64(750) {
		t.Fatal("must use period maxima, not sample maxima", state["used_bytes"], state["remaining_bytes"])
	}
	report.At = now.Add(2 * time.Second)
	report.Interfaces[0].RX = 119
	h.call("POST", "agent/host-traffic", report, 409, host)
	report.Interfaces[0].RX = 120
	h.call("POST", path+"/calibrate", map[string]any{"version": state["version"], "used_bytes": 400}, 200, nil)
	report.At = now.Add(3 * time.Second)
	report.Interfaces[0].RX = 130
	report.Interfaces[0].TX = 160
	h.call("POST", "agent/host-traffic", report, 200, host)
	state = obj018(h.call("GET", path, nil, 200, nil))
	if state["used_bytes"] != float64(410) {
		t.Fatal("calibration replayed old bytes", state["used_bytes"])
	}
	if _, e := s.DB.Exec(ctx, `UPDATE host_traffic SET next_reset_at=now()-interval '1 second' WHERE host_id=$1`, id); e != nil {
		t.Fatal(e)
	}
	report.At = now.Add(4 * time.Second)
	report.Interfaces[0].RX = 140
	report.Interfaces[0].TX = 180
	h.call("POST", "agent/host-traffic", report, 200, host)
	state = obj018(h.call("GET", path, nil, 200, nil))
	if state["used_bytes"] != float64(20) {
		t.Fatal("period reset restarted OS counters", state["used_bytes"])
	}
	next, e := time.Parse(time.RFC3339Nano, state["next_reset_at"].(string))
	if e != nil || next.In(billingLocation).Day() != 5 {
		t.Fatal("reset anchor lost", state["next_reset_at"])
	}
	report.Epoch = strings.Repeat("b", 32)
	report.Started = now
	report.At = now.Add(5 * time.Second)
	report.Interfaces[0].RX = 3
	report.Interfaces[0].TX = 4
	h.call("POST", "agent/host-traffic", report, 200, host)
	state = obj018(h.call("GET", path, nil, 200, nil))
	if state["quality"] != "gap" {
		t.Fatal("lost ledger was presented as precise", state["quality"])
	}
	h.call("PATCH", "hosts/"+id, map[string]any{"name": "must rollback", "traffic": hostTrafficInput{Quota: -1, Mode: "both", ResetDay: 5}}, 400, nil)
	var name string
	s.DB.QueryRow(ctx, `SELECT name FROM hosts WHERE id=$1`, id).Scan(&name)
	if name != "metered" {
		t.Fatal("partial host edit committed")
	}
}
func TestDeviceSlotsBillingAndSubscriptionIdentity(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	ctx := context.Background()
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "devices", "address": "192.0.2.1"}, 201, nil))
	hid := host["id"].(string)
	h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.18.0", "platform": "linux/amd64"}, 200, host)
	h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true, "capabilities": []live.Capability{{Engine: "sing-box", Available: true, Version: "sing-box version 1.14.4"}}}, 200, host)
	h.call("POST", "agent/telemetry", live.TelemetryBatch{Status: []live.MeterStatus{{Engine: "sing-box", State: "collecting"}}}, 200, host)
	h.call("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true}, 200, nil)
	node := obj018(h.call("POST", "live", map[string]any{"name": "managed", "host_id": hid, "kind": "vmess", "engine": "sing-box", "listen": "127.0.0.1", "address": "192.0.2.1", "port": 25017, "tls_mode": "none", "managed_users": true}, 201, nil))
	nid := node["id"].(string)
	h.call("POST", "live/"+nid+"/toggle", map[string]any{"enabled": true}, 200, nil)
	expiry := time.Now().Add(24 * time.Hour)
	user := obj018(h.call("POST", "proxy-users", map[string]any{"name": "Alice", "enabled": true, "expires_at": expiry, "quota_bytes": 1000, "billing_mode": "both", "multiplier_milli": 1500, "max_devices": 1, "device_limit_mode": "hwid"}, 200, nil))
	uid := user["id"].(string)
	h.call("POST", "user-groups", map[string]any{"name": "group", "user_ids": []string{uid}, "service_ids": []string{nid}, "max_devices": 2}, 200, nil)
	base := "proxy-users/" + uid + "/devices"
	device := obj018(h.call("POST", base, map[string]any{"name": "Phone", "client_id": "mihomo"}, 201, nil))
	did := device["id"].(string)
	h.call("POST", base, map[string]any{"name": "Second", "client_id": "mihomo"}, 409, nil)
	var work []live.Work
	json.Unmarshal(h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, host), &work)
	if len(work) != 1 || len(work[0].Members) != 1 || len(work[0].Members[0].Spec.Accounts) != 1 || work[0].Members[0].Spec.Accounts[0].Name != did {
		t.Fatal("legacy credential survived device enforcement")
	}
	member := work[0].Members[0]
	h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true, "reports": []live.Report{{ID: nid, Version: member.Version, State: "running"}}, "kernel_reports": []live.KernelReport{{Engine: "sing-box", Checksum: work[0].Revision(), State: "running"}}}, 200, host)
	counter := live.TrafficCounter{ID: Hash("sing-box/user/" + did)[:32], Kind: "user", Subject: did, Up: 100, Down: 100}
	h.call("POST", "agent/telemetry", live.TelemetryBatch{Counters: []live.TrafficCounter{counter}}, 200, host)
	h.call("POST", "agent/telemetry", live.TelemetryBatch{Counters: []live.TrafficCounter{counter}}, 200, host)
	current := obj018(h.call("GET", "proxy-users/"+uid, nil, 200, nil))
	if current["used_bytes"] != float64(300) || current["upload_bytes"] != float64(100) || current["billed_upload_bytes"] != float64(150) {
		t.Fatal("device traffic double billed or rate ignored", current["used_bytes"], current["upload_bytes"], current["billed_upload_bytes"])
	}
	share := func(url string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", url, nil)
		r.Header.Set("User-Agent", "Clash-Verge/fixture-client")
		w := httptest.NewRecorder()
		s.ShareHandler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("device share %d want %d %s", w.Code, status, w.Body)
		}
		return w
	}
	out := share(device["url"].(string), 200)
	if !strings.Contains(out.Header().Get("Subscription-Userinfo"), "upload=150; download=150; total=1000") {
		t.Fatal("Clash header ignored billed usage", out.Header().Get("Subscription-Userinfo"))
	}
	var config map[string]any
	if e := yaml.Unmarshal(out.Body.Bytes(), &config); e != nil {
		t.Fatal(e)
	}
	proxies := config["proxies"].([]any)
	if proxies[0].(map[string]any)["uuid"] != member.Spec.Accounts[0].UUID {
		t.Fatal("device export used another credential")
	}
	devicesRaw := string(h.call("GET", base, nil, 200, nil))
	if !strings.Contains(devicesRaw, "Clash-Verge/fixture-client") || strings.Contains(devicesRaw, "sealed") || strings.Contains(devicesRaw, member.Spec.Accounts[0].UUID) {
		t.Fatal("metadata missing or credentials exposed")
	}
	for _, command := range []string{"hosts", "traffic", "users", "devices"} {
		text, err := s.notificationSummary(ctx, command)
		if err != nil || strings.Contains(text, member.Spec.Accounts[0].UUID) || strings.Contains(text, "/s/") {
			t.Fatal("unsafe or broken Telegram summary", command, err)
		}
	}
	key := obj018(h.call("POST", "api-keys", map[string]any{"name": "device reader", "scopes": []string{"devices:read"}, "expires_at": time.Now().Add(time.Hour), "rate_limit": 120}, 201, nil))["token"].(string)
	for _, path := range []string{base, base + "/" + did + "/link"} {
		req := httptest.NewRequest("GET", "/api/v1/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		want := 200
		if strings.HasSuffix(path, "/link") {
			want = 403
		}
		if w.Code != want {
			t.Fatal("device links bypassed separate scope", w.Code)
		}
	}
	cfg := notificationConfig{Enabled: true, Token: "123456789:" + strings.Repeat("A", 30), ChatID: "12345", Commands: []string{"devices", "help"}, Events: []string{"host_quota", "device_limit"}, QuotaPercent: 80, ReportSections: []string{"devices"}}
	h.call("PUT", "notifications/telegram", cfg, 200, nil)
	if _, err := s.DB.Exec(ctx, `UPDATE host_traffic SET quota_bytes=1000,period_rx=900,last_sample_at=now() WHERE host_id=$1`, hid); err != nil {
		t.Fatal(err)
	}
	sends := 0
	s.telegramClient = &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected notification method")
		}
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte(member.Spec.Accounts[0].UUID)) {
			t.Fatal("notification leaked a credential")
		}
		sends++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1}}`))}, nil
	})}
	loaded, token, _, err := s.loadNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.notificationAlerts(ctx, loaded, token); err != nil {
		t.Fatal(err)
	}
	if sends != 2 {
		t.Fatal("host/device threshold notifications missing", sends)
	}
	if err = s.notificationAlerts(ctx, loaded, token); err != nil || sends != 2 {
		t.Fatal("unchanged thresholds notified repeatedly", sends, err)
	}
	h.call("PUT", base+"/"+did, map[string]any{"name": "Phone", "client_id": "mihomo", "enabled": true, "version": 1, "all_nodes": false, "service_ids": []string{}}, 200, nil)
	share(device["url"].(string), 403)
	work = nil
	json.Unmarshal(h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, host), &work)
	if len(work[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("empty device permission broadened to all nodes")
	}
	h.call("PUT", base+"/"+did, map[string]any{"name": "Phone", "client_id": "mihomo", "enabled": true, "version": 2, "all_nodes": false, "service_ids": []string{ID()}}, 400, nil)
	h.call("PUT", base+"/"+did, map[string]any{"name": "Phone", "client_id": "mihomo", "enabled": true, "version": 2, "all_nodes": true, "service_ids": []string{}}, 200, nil)
	h.call("PUT", base+"/"+did, map[string]any{"name": "Phone", "client_id": "mihomo", "enabled": false, "version": 3}, 200, nil)
	share(device["url"].(string), 404)
	work = nil
	json.Unmarshal(h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, host), &work)
	if len(work[0].Members[0].Spec.Accounts) != 0 {
		t.Fatal("disabled device credential still deployed")
	}
	d2 := obj018(h.call("POST", base, map[string]any{"name": "Desktop", "client_id": "mihomo"}, 201, nil))
	id2 := d2["id"].(string)
	h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, host)
	h.call("PUT", "proxy-users/"+uid, map[string]any{"name": "Alice", "enabled": true, "expires_at": expiry, "billing_mode": "max", "multiplier_milli": 500}, 200, nil)
	counter = live.TrafficCounter{ID: Hash("sing-box/user/" + id2)[:32], Kind: "user", Subject: id2, Up: 100, Down: 500}
	h.call("POST", "agent/telemetry", live.TelemetryBatch{Counters: []live.TrafficCounter{counter}}, 200, host)
	current = obj018(h.call("GET", "proxy-users/"+uid, nil, 200, nil))
	if current["used_bytes"] != float64(550) {
		t.Fatal("cumulative maximum or changed multiplier repriced historical traffic", current["used_bytes"])
	}
	if _, err := s.DB.Exec(ctx, `UPDATE user_devices SET last_connected_at=now()-interval '8 days',last_source_ip='203.0.113.249' WHERE id=$1`, id2); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(h.call("GET", base, nil, 200, nil), []byte("203.0.113.249")) {
		t.Fatal("device view retained an expired source IP")
	}
	if err := s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	var sourceRemoved bool
	if err := s.DB.QueryRow(ctx, `SELECT last_source_ip IS NULL FROM user_devices WHERE id=$1`, id2).Scan(&sourceRemoved); err != nil || !sourceRemoved {
		t.Fatal("expired device source IP was not purged", err)
	}
	h.call("DELETE", base+"/"+id2, nil, 200, nil)
	share(d2["url"].(string), 404)
	var count int
	s.DB.QueryRow(ctx, `SELECT count(*) FROM subscriptions WHERE tenant_id='self' AND device_id=$1`, id2).Scan(&count)
	if count != 0 {
		t.Fatal("deleted device left a subscription")
	}
}
func TestMetadataPageAllRowsAndOrdering(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	ctx := context.Background()
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "history"}, 201, nil))
	id := host["id"].(string)
	_, e := s.DB.Exec(ctx, `INSERT INTO access_observations(tenant_id,host_id,event_id,user_id,service_id,source_ip,destination,destination_port,network,observed_at,uplink,downlink) SELECT 'self',$1,md5(n::text),'','', '192.0.2.9'::inet,'example.test',443,'tcp',now()-n*interval '1 second',n,n*2 FROM generate_series(1,205)n`, id)
	if e != nil {
		t.Fatal(e)
	}
	a := obj018(h.call("GET", "access-records/page?limit=50&page=1&sort=traffic&order=desc", nil, 200, nil))
	if a["total"] != float64(205) || a["pages"] != float64(5) || len(a["items"].([]any)) != 50 || a["items"].([]any)[0].(map[string]any)["total_bytes"] != float64(615) {
		t.Fatal("traffic sort or pagination failed")
	}
	last := obj018(h.call("GET", "access-records/page?limit=50&page=5&sort=time&order=desc", nil, 200, nil))
	if len(last["items"].([]any)) != 5 {
		t.Fatal("older rows cannot be browsed")
	}
	asc := obj018(h.call("GET", "access-records/page?sort=time&order=asc", nil, 200, nil))
	if asc["items"].([]any)[0].(map[string]any)["total_bytes"] != float64(615) {
		t.Fatal("ascending connection time ignored")
	}
	h.call("GET", "access-records/page?sort=DROP", nil, 400, nil)
	h.call("GET", "access-records/page?limit=201", nil, 400, nil)
	h.call("POST", "agent/telemetry", live.TelemetryBatch{Access: make([]live.AccessObservation, 101)}, 400, host)
	_, e = s.DB.Exec(ctx, `INSERT INTO access_observations(tenant_id,host_id,event_id,user_id,service_id,source_ip,destination,destination_port,network,observed_at) VALUES('self',$1,md5('expired'),'','','192.0.2.9','old.test',443,'tcp',now()-interval '8 days')`, id)
	if e != nil {
		t.Fatal(e)
	}
	a = obj018(h.call("GET", "access-records/page", nil, 200, nil))
	if a["total"] != float64(205) {
		t.Fatal("expired metadata visible")
	}
}
