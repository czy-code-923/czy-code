package api

import "vps-control/internal/live"

// Adapt only optional client behavior. Authentication, server identity, TLS
// verification and the actual wire transport are never removed or replaced.
// Copy the options before editing: subscriptions must not mutate stored Specs.
func adaptClientTransport(c clientInfo, spec live.Spec) (live.Spec, []string) {
	if spec.TransportOptions == nil {
		return spec, nil
	}
	o := *spec.TransportOptions
	notes := []string{}
	if o.GRPCMultiMode && !(c.Full && c.Format == "xray") {
		// Xray's server accepts both modes on the same service name.
		o.GRPCMultiMode = false
		notes = append(notes, "grpc-single-stream")
	}
	if spec.Kind == "http" && o.TLSFingerprint != "" && !(c.Full && (c.Format == "sing-box" || c.Format == "xray")) {
		o.TLSFingerprint = ""
		notes = append(notes, "http-native-tls")
	}
	spec.TransportOptions = &o
	return spec, notes
}
