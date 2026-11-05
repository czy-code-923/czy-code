package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"vps-control/internal/live"
)

func TestAutomaticCheckDoesNotMistakeInspectorForActualAnyTLSClient(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	spec, e := f.h.s.loadSpec(httptest.NewRequest("GET", "/", nil), f.node)
	if e != nil {
		t.Fatal(e)
	}
	spec.Kind = "anytls"
	spec.TLSMode = "tls"
	spec.ServerName = "proxy.example.test"
	sealed, e := live.Seal(f.h.s.MasterKey, mustJSON(spec))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.h.s.DB.Exec(context.Background(), "UPDATE live_services SET kind='anytls',sealed=$2,version=version+1,state='pending' WHERE id=$1", f.node, sealed); e != nil {
		t.Fatal(e)
	}
	f.ack()
	for _, query := range []string{"", "?client=auto", "?client=mihomo", "?client=sing-box", "?client=hiddify"} {
		d := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/check"+query, nil, 200, nil))
		if d["ready"] != true {
			t.Fatal(query, d)
		}
	}
	for _, query := range []string{"?client=base64", "?client=v2rayng", "?client=shadowrocket"} {
		d := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/check"+query, nil, 200, nil))
		if d["ready"] != false || d["stage"] != "compatibility" {
			t.Fatal(query, d)
		}
	}
	d := obj018(f.h.call("GET", "subscriptions/"+f.subscription+"/share", nil, 200, nil))
	if d["url"] == "" {
		t.Fatal("automatic link absent")
	}
}
