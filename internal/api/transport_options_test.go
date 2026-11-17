package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"vps-control/internal/live"
)

func TestTransportSubscriptionDistributionAcrossClients(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	f.compatible(0)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.25.7", "platform": "linux/amd64"}, 200, f.host)
	f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true, "capabilities": []live.Capability{{Engine: "sing-box", Available: true, Version: "sing-box version 1.14.4"}, {Engine: "xray", Available: true, Version: "Xray 26.10.21"}}}, 200, f.host)
	ids := []string{f.node}
	for i, tr := range []string{"xhttp", "grpc"} {
		b := map[string]any{"name": tr, "host_id": f.host["id"], "kind": "vmess", "engine": "xray", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 26058 + i, "tls_mode": "none", "transport": tr, "managed_users": true}
		if tr == "xhttp" {
			b["path"] = "/kept"
			b["transport_options"] = map[string]any{"xhttp_mode": "packet-up", "xhttp_padding_bytes": "100-1000"}
		} else {
			b["service_name"] = "starhub"
			b["transport_options"] = map[string]any{"grpc_multi_mode": true}
		}
		id := obj018(f.h.call("POST", "live", b, 201, nil))["id"].(string)
		ids = append(ids, id)
		f.h.call("POST", "live/"+id+"/toggle", map[string]any{"enabled": true}, 200, nil)
	}
	f.changeGroup(ids)
	f.ack()
	raw := strings.Split(f.url, "?")[0]
	for _, c := range clients {
		if c.ID == "naive-proxy" {
			f.share(raw+"?client="+c.ID+"&policy=nodes-only", nil, 422)
			continue
		}
		w := f.share(raw+"?client="+c.ID+"&policy=nodes-only", nil, 200)
		included, excluded := "2", "1"
		if c.ID == "xray" {
			included, excluded = "3", "0"
		}
		if w.Header().Get("X-Starhub-Included-Nodes") != included || w.Header().Get("X-Starhub-Excluded-Nodes") != excluded {
			t.Fatal("lossy client compatibility filter", c.ID, w.Header())
		}
		adapted := "1"
		if c.ID == "xray" {
			adapted = "0"
		}
		if w.Header().Get("X-Starhub-Adapted-Nodes") != adapted {
			t.Fatal("adaptation accounting mismatch", c.ID, w.Header())
		}
		if c.ID == "xray" {
			var cfg map[string]any
			if e := json.Unmarshal(w.Body.Bytes(), &cfg); e != nil {
				t.Fatal(e)
			}
			found := 0
			for _, v := range array(cfg["outbounds"]) {
				stream := object(object(v)["streamSettings"])
				switch stream["network"] {
				case "xhttp":
					o := object(stream["xhttpSettings"])
					if o["mode"] != "packet-up" || o["xPaddingBytes"] != "100-1000" {
						t.Fatal("Xray subscription lost XHTTP fields")
					}
					found++
				case "grpc":
					if object(stream["grpcSettings"])["multiMode"] != true {
						t.Fatal("Xray subscription lost multiMode")
					}
					found++
				}
			}
			if found != 2 {
				t.Fatal("advanced transports missing", w.Body.String())
			}
		}
	}
}

func TestExistingSubscriptionReceivesTLSFingerprint(t *testing.T) {
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
	spec.TLSMode = "tls"
	spec.ServerName = "localhost"
	spec.Certificate, spec.PrivateKey, e = live.Certificate("localhost")
	if e != nil {
		t.Fatal(e)
	}
	spec.TransportOptions = &live.TransportOptions{TLSFingerprint: "firefox"}
	// Isolate subscription identity/format behavior from public certificate issuance.
	raw, _ = json.Marshal(spec)
	sealed, e = live.Seal(f.h.s.MasterKey, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.h.s.DB.Exec(ctx, "UPDATE live_services SET sealed=$2,version=version+1 WHERE id=$1", f.node, sealed); e != nil {
		t.Fatal(e)
	}
	f.ack()
	nodes := proxies019(t, f.share(f.url, nil, 200))
	if len(nodes) != 1 || nodes[0]["client-fingerprint"] != "firefox" {
		t.Fatal("existing Mihomo URL did not refresh fingerprint")
	}
	uri := f.share(strings.Split(f.url, "?")[0]+"?client=v2rayng", nil, 200)
	decoded, e := base64.StdEncoding.DecodeString(strings.TrimSpace(uri.Body.String()))
	if e != nil {
		t.Fatal(e)
	}
	vmess, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(decoded)), "vmess://"))
	if e != nil {
		t.Fatal(e)
	}
	var payload map[string]any
	if e = json.Unmarshal(vmess, &payload); e != nil {
		t.Fatal(e)
	}
	if payload["fp"] != "firefox" || payload["id"] != nodes[0]["uuid"] {
		t.Fatal("URI lost fingerprint or changed the existing user identity")
	}
}

func TestTransportOptionsClientFormatAndImport(t *testing.T) {
	s := live.Spec{Kind: "vmess", Engine: "xray", Transport: "grpc", ProtocolOptions: live.ProtocolOptions{TransportOptions: &live.TransportOptions{GRPCMultiMode: true}}}
	if compatibleClient(clientInfo{ID: "v2rayng", Format: "base64", Full: false}, s) != nil {
		t.Fatal("compatible gRPC single stream fallback was filtered")
	}
	if e := compatibleClient(clientInfo{ID: "xray", Format: "xray", Full: true}, s); e != nil {
		t.Fatal(e)
	}
	source := `{"inbounds":[{"protocol":"vmess","port":25057,"settings":{},"streamSettings":{"network":"xhttp","security":"none","xhttpSettings":{"path":"/kept","mode":"packet-up","xPaddingBytes":"10-20","extra":{"xPaddingBytes":"100-1000"}}}}]}`
	body, _ := json.Marshal(map[string]string{"source": source})
	w := httptest.NewRecorder()
	if e := (&Server{}).inspectConfiguration(w, httptest.NewRequest("POST", "/api/v1/configuration/inspect", strings.NewReader(string(body)))); e != nil {
		t.Fatal(e)
	}
	var result struct{ Candidates []inspectedInbound }
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Candidates) != 1 {
		t.Fatal(w.Body.String())
	}
	o := object(result.Candidates[0].Input["transport_options"])
	if o["xhttp_mode"] != "packet-up" || o["xhttp_padding_bytes"] != "100-1000" {
		t.Fatal("native import lost effective XHTTP fields", w.Body.String())
	}
}

func TestTransportOptionsPersistAndProtectOldAgents(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	b := map[string]any{"name": "xhttp", "host_id": f.host["id"], "kind": "vmess", "engine": "xray", "listen": "127.0.0.1", "address": "192.0.2.10", "port": 26057, "tls_mode": "none", "transport": "xhttp", "path": "/kept", "managed_users": false}
	b["transport_options"] = map[string]any{"xhttp_mode": "packet-up", "xhttp_padding_bytes": "100-1000"}
	f.h.call("POST", "live", b, 409, nil)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.25.7", "platform": "linux/amd64"}, 200, f.host)
	id := obj018(f.h.call("POST", "live", b, 201, nil))["id"].(string)
	readSpec := func() live.Spec {
		t.Helper()
		var sealed []byte
		if e := f.h.s.DB.QueryRow(context.Background(), "SELECT sealed FROM live_services WHERE id=$1", id).Scan(&sealed); e != nil {
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
		return spec
	}
	before := readSpec()
	detail := obj018(f.h.call("GET", "live/"+id, nil, 200, nil))
	b["version"] = detail["version"]
	delete(b, "transport_options") // A pre-0.25.7 editor knows none of the new fields.
	f.h.call("PUT", "live/"+id, b, 200, nil)
	after := readSpec()
	if after.TransportOptions == nil || after.TransportOptions.XHTTPMode != "packet-up" || after.UUID != before.UUID || after.Password != before.Password {
		t.Fatal("edit lost transport settings or rotated identity")
	}
	detail = obj018(f.h.call("GET", "live/"+id, nil, 200, nil))
	b["version"] = detail["version"]
	b["transport_options"] = map[string]any{"xhttp_mode": "packet-up", "unknown_field": true}
	f.h.call("PUT", "live/"+id, b, 400, nil)
	f.h.call("POST", "agent/heartbeat", map[string]any{"mode": "simulation", "version": "0.25.6", "platform": "linux/amd64"}, 200, f.host)
	var work []live.Work
	if e := json.Unmarshal(f.h.call("POST", "agent/live/sync", map[string]any{"bundle_mode": true}, 200, f.host), &work); e != nil {
		t.Fatal(e)
	}
	for _, bundle := range work {
		for _, member := range bundle.Members {
			if member.Spec.TransportOptions != nil {
				t.Fatal("old Agent received unknown JSON field")
			}
			if member.ID == id && member.Enabled {
				t.Fatal("old Agent enabled unsupported transport")
			}
		}
	}
	// The original user's link, group and protocol still render after the new node's edits.
	if len(proxies019(t, f.share(f.url, nil, 200))) != 1 {
		t.Fatal("existing subscription changed")
	}
}

func TestTransportOptionsOldEditorCompatibility(t *testing.T) {
	s := live.Spec{Kind: "vmess", TLSMode: "none", ProtocolOptions: live.ProtocolOptions{TransportOptions: &live.TransportOptions{XHTTPMode: "packet-up"}}}
	if e := applySecurity(&s, liveInput{}); e != nil || s.TransportOptions == nil || s.TransportOptions.XHTTPMode != "packet-up" {
		t.Fatal("old editor erased new transport settings", e)
	}
	if e := applySecurity(&s, liveInput{ProtocolOptions: live.ProtocolOptions{TransportOptions: &live.TransportOptions{}}}); e != nil || s.TransportOptions != nil {
		t.Fatal("explicit clear failed", e)
	}
	s.TransportOptions = &live.TransportOptions{TLSFingerprint: "chrome"}
	if e := requireTunedChainAgent(s, "0.19.0"); e != nil {
		t.Fatal("client fingerprint forced server upgrade", e)
	}
	entry := live.Spec{Kind: "vmess", Outbound: &s}
	if requireTunedChainAgent(entry, "0.25.6") == nil || requireTunedChainAgent(entry, "0.25.7") != nil {
		t.Fatal("old chain Agent ignored fingerprint")
	}
	s.TransportOptions = &live.TransportOptions{XHTTPMode: "packet-up"}
	if requireTunedChainAgent(s, "0.25.6") == nil {
		t.Fatal("old server Agent ignored XHTTP mode")
	}
}
