package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vps-control/internal/live"
)

func TestClientTuningChainAgentBoundary(t *testing.T) {
	next := live.Spec{Kind: "hysteria2", ProtocolOptions: live.ProtocolOptions{HY2ClientMode: "brutal", HY2ClientDownMbps: 500}}
	entry := live.Spec{Kind: "trojan", Outbound: &next}
	if err := requireTunedChainAgent(entry, "0.23.13"); err != nil {
		t.Fatal("old Agents can use normalized bandwidth", err)
	}
	next.HY2BBRProfile = "conservative"
	if requireTunedChainAgent(entry, "0.23.15") == nil {
		t.Fatal("old chain Agent silently accepted unknown tuning")
	}
	if err := requireTunedChainAgent(entry, "0.23.16"); err != nil {
		t.Fatal(err)
	}
	if err := requireTunedChainAgent(next, "0.23.13"); err != nil {
		t.Fatal("subscription-only tuning forced a server Agent upgrade", err)
	}
}

// TLS trust validation is covered separately. This isolated database fixture
// exercises encrypted persistence, old Agent wire work and real share rendering.
func TestHY2TuningUsesExistingUserSubscription(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	ctx := context.Background()
	var sealed []byte
	if err := f.h.s.DB.QueryRow(ctx, "SELECT sealed FROM live_services WHERE id=$1", f.node).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := live.Open(f.h.s.MasterKey, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var spec live.Spec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	spec.Kind, spec.TLSMode, spec.Transport, spec.ServerName = "hysteria2", "tls", "quic", "localhost"
	spec.Certificate, spec.PrivateKey, err = live.Certificate("localhost")
	if err != nil {
		t.Fatal(err)
	}
	spec.UpMbps, spec.DownMbps, spec.HY2ClientMode = 900, 200, "bbr"
	store := func() {
		t.Helper()
		raw, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := live.Seal(f.h.s.MasterKey, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.h.s.DB.Exec(ctx, "UPDATE live_services SET kind='hysteria2',sealed=$2,version=version+1 WHERE id=$1", f.node, sealed); err != nil {
			t.Fatal(err)
		}
	}
	store()
	work := findMember(t, f.ack(), f.node)
	if work.Spec.UpMbps != 900 || work.Spec.DownMbps != 200 || work.Spec.HY2ClientMode != "" {
		t.Fatal("server limits changed or subscription fields leaked into Agent work")
	}
	rows := proxies019(t, f.share(f.url, nil, 200))
	if len(rows) != 1 || rows[0]["up"] != nil || rows[0]["down"] != nil {
		t.Fatal("automatic mode exported fixed rates", rows)
	}
	password := rows[0]["password"]
	spec.HY2ClientMode, spec.HY2ClientUpMbps, spec.HY2ClientDownMbps = "brutal", 40, 500
	store()
	f.ack()
	rows = proxies019(t, f.share(f.url, nil, 200))
	if rows[0]["up"] != 40 || rows[0]["down"] != 500 || rows[0]["password"] != password {
		t.Fatal("stable subscription did not refresh tuning or identity changed", rows)
	}
	w := f.share(strings.Replace(f.url, "format=mihomo", "client=sing-box-latest", 1), nil, 200)
	var cfg struct{ Outbounds []map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, out := range cfg.Outbounds {
		if out["type"] == "hysteria2" {
			found = true
			if out["up_mbps"] != float64(40) || out["down_mbps"] != float64(500) || out["password"] != password {
				t.Fatal("sing-box export mismatch", out)
			}
		}
	}
	if !found {
		t.Fatal("HY2 missing from sing-box subscription")
	}
}
