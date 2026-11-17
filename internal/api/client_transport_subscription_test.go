package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vps-control/internal/live"
)

func TestHTTPFingerprintFallbackKeepsExistingSubscriptionUsable(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	ctx := context.Background()
	var sealed []byte
	if e := f.h.s.DB.QueryRow(ctx, "SELECT sealed FROM live_services WHERE id=$1", f.node).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	raw, e := live.Open(f.h.s.MasterKey, sealed)
	if e != nil {
		t.Fatal(e)
	}
	var spec live.Spec
	if e = json.Unmarshal(raw, &spec); e != nil {
		t.Fatal(e)
	}
	spec.Kind, spec.TLSMode, spec.ServerName = "http", "tls", "localhost"
	spec.Certificate, spec.PrivateKey, e = live.Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	spec.TransportOptions = &live.TransportOptions{TLSFingerprint: "chrome"}
	raw, _ = json.Marshal(spec)
	sealed, e = live.Seal(f.h.s.MasterKey, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.h.s.DB.Exec(ctx, "UPDATE live_services SET kind='http',sealed=$2,version=version+1 WHERE id=$1", f.node, sealed); e != nil {
		t.Fatal(e)
	}
	f.ack()
	for _, c := range []string{"mihomo", "clash-verge", "stash", "shadowrocket", "nekobox"} {
		w := f.share(strings.Split(f.url, "?")[0]+"?client="+c+"&policy=nodes-only", nil, 200)
		if w.Header().Get("X-Starhub-Included-Nodes") != "1" || w.Header().Get("X-Starhub-Adapted-Nodes") != "1" {
			t.Fatal("optional fingerprint caused whole-node exclusion", c, w.Header())
		}
		if strings.Contains(w.Body.String(), "client-fingerprint:") || strings.Contains(w.Body.String(), "fp=") || strings.Contains(w.Body.String(), "insecure=1") {
			t.Fatal("unsupported option or insecure TLS emitted", c)
		}
	}
	w := f.share(f.url, nil, 200)
	node := proxies019(t, w)[0]
	if node["tls"] != true || node["password"] == nil || node["username"] == nil || node["sni"] != "localhost" {
		t.Fatal("fallback lost authentication or TLS identity", node)
	}
	check := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/check?client=mihomo", nil, 200, nil))
	if check["ready"] != true || check["adapted_nodes"] != "1" || check["adaptation_notice"] == "" {
		t.Fatal("compatibility processing invisible in preview", check)
	}
	w = f.share(strings.Split(f.url, "?")[0]+"?client=sing-box-latest", nil, 200)
	if w.Header().Get("X-Starhub-Adapted-Nodes") != "0" || !strings.Contains(w.Body.String(), `"fingerprint":"chrome"`) {
		t.Fatal("capable client lost configured fingerprint", w.Body.String())
	}
}
