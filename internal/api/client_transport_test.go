package api

import (
	"testing"
	"vps-control/internal/live"
)

func TestOptionalTransportAdaptationPreservesServerAndIdentity(t *testing.T) {
	for _, c := range clients {
		s := live.Spec{Kind: "vmess", Engine: "xray", Transport: "grpc", TLSMode: "tls", ServerName: "proxy.example", ServiceName: "kept", Password: "keep-password", UUID: "keep-uuid", ProtocolOptions: live.ProtocolOptions{TransportOptions: &live.TransportOptions{GRPCMultiMode: true, TLSFingerprint: "chrome"}}}
		out, notes := adaptClientTransport(c, s)
		fullXray := c.Full && c.Format == "xray"
		if out.TransportOptions.GRPCMultiMode != fullXray || (len(notes) == 0) != fullXray {
			t.Fatal(c.ID, "incorrect gRPC fallback")
		}
		if !s.TransportOptions.GRPCMultiMode || out.Transport != "grpc" || out.UUID != s.UUID || out.Password != s.Password || out.ServiceName != s.ServiceName || out.ServerName != s.ServerName {
			t.Fatal("adaptation mutated server or identity", c.ID)
		}
		s.Kind = "http"
		s.Transport = "tcp"
		s.TransportOptions = &live.TransportOptions{TLSFingerprint: "chrome"}
		out, notes = adaptClientTransport(c, s)
		native := !(c.Full && (c.Format == "xray" || c.Format == "sing-box"))
		if (out.TransportOptions.TLSFingerprint == "") != native || (len(notes) > 0) != native || s.TransportOptions.TLSFingerprint != "chrome" {
			t.Fatal("HTTP native TLS fallback mismatch", c.ID)
		}
		sb, mh, _, e := out.Client("proxy")
		if e != nil {
			t.Fatal(e)
		}
		if native && object(sb["tls"])["utls"] != nil {
			t.Fatal("unsupported optional TLS field emitted")
		}
		if mh["client-fingerprint"] != nil {
			t.Fatal("HTTP unsupported client fingerprint emitted")
		}
		if out.TLSMode != "tls" || object(sb["tls"])["enabled"] != true {
			t.Fatal("fallback disabled TLS")
		}
	}
}
