package live

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Optional fields deliberately preserve the native defaults of existing nodes.
// Client-only fields never enter the server listener's TLS configuration.
type TransportOptions struct {
	TLSFingerprint    string `json:"tls_fingerprint,omitempty"`
	XHTTPMode         string `json:"xhttp_mode,omitempty"`
	XHTTPPadding      string `json:"xhttp_padding_bytes,omitempty"`
	XHTTPNoGRPCHeader bool   `json:"xhttp_no_grpc_header,omitempty"`
	GRPCMultiMode     bool   `json:"grpc_multi_mode,omitempty"`
	KCPSeed           string `json:"kcp_seed,omitempty"`
	KCPHeader         string `json:"kcp_header,omitempty"`
	KCPMTU            int    `json:"kcp_mtu,omitempty"`
	KCPTTI            int    `json:"kcp_tti,omitempty"`
	KCPUplink         int    `json:"kcp_uplink_capacity,omitempty"`
	KCPDownlink       int    `json:"kcp_downlink_capacity,omitempty"`
	KCPCongestion     bool   `json:"kcp_congestion,omitempty"`
}

func (s Spec) transportOptions() TransportOptions {
	if s.TransportOptions != nil {
		return *s.TransportOptions
	}
	return TransportOptions{}
}

func (s Spec) validateTransportOptions() error {
	o := s.transportOptions()
	if o.TLSFingerprint != "" {
		if !s.UsesTLS() || !slices.Contains([]string{"vless", "vmess", "trojan", "anytls", "http"}, s.Kind) {
			return errors.New("browser fingerprint requires TCP/TLS; REALITY uses reality.fingerprint; QUIC and Naive use their native TLS stacks")
		}
		if !slices.Contains([]string{"chrome", "firefox", "safari", "edge", "ios", "android", "randomized"}, o.TLSFingerprint) {
			return errors.New("unsupported TLS browser fingerprint")
		}
	}
	if o.XHTTPMode != "" || o.XHTTPPadding != "" || o.XHTTPNoGRPCHeader {
		if s.Engine != "xray" || s.Transport != "xhttp" {
			return errors.New("XHTTP options require Xray XHTTP")
		}
		if !slices.Contains([]string{"", "auto", "packet-up", "stream-up", "stream-one"}, o.XHTTPMode) {
			return errors.New("invalid XHTTP mode")
		}
		if o.XHTTPPadding != "" {
			parts := strings.Split(o.XHTTPPadding, "-")
			if len(parts) < 1 || len(parts) > 2 {
				return errors.New("XHTTP padding must be N or N-M, 0-65535 bytes")
			}
			previous := -1
			for _, part := range parts {
				n, e := strconv.Atoi(part)
				if e != nil || n < 0 || n > 65535 || n < previous || strconv.Itoa(n) != part {
					return errors.New("XHTTP padding must be an ascending byte range, 0-65535")
				}
				previous = n
			}
		}
		if o.XHTTPNoGRPCHeader && o.XHTTPMode == "packet-up" {
			return errors.New("noGRPCHeader applies to streaming XHTTP modes")
		}
	}
	if o.GRPCMultiMode && (s.Engine != "xray" || s.Transport != "grpc") {
		return errors.New("gRPC multi mode requires Xray gRPC")
	}
	if o.KCPSeed != "" || o.KCPHeader != "" || o.KCPMTU != 0 || o.KCPTTI != 0 || o.KCPUplink != 0 || o.KCPDownlink != 0 || o.KCPCongestion {
		if s.Engine != "xray" || s.Transport != "kcp" {
			return errors.New("mKCP options require Xray mKCP")
		}
		if len(o.KCPSeed) > 128 || strings.ContainsAny(o.KCPSeed, "\r\n\x00") {
			return errors.New("mKCP seed exceeds 128 bytes or contains line breaks")
		}
		if !slices.Contains([]string{"", "none", "srtp", "utp", "wechat-video", "dtls", "wireguard"}, o.KCPHeader) {
			return errors.New("unsupported mKCP header")
		}
		if o.KCPMTU != 0 && (o.KCPMTU < 576 || o.KCPMTU > 1460) {
			return errors.New("mKCP MTU must be 576-1460, or 0 for the native default")
		}
		if o.KCPTTI != 0 && (o.KCPTTI < 10 || o.KCPTTI > 100) {
			return errors.New("mKCP TTI must be 10-100 ms, or 0 for the native default")
		}
		if o.KCPUplink < 0 || o.KCPDownlink < 0 || o.KCPUplink > 10000 || o.KCPDownlink > 10000 {
			return errors.New("mKCP capacity must be 0-10000 MB/s")
		}
	}
	return nil
}

func (s Spec) xrayTransportOptions(stream map[string]any, server bool) {
	o := s.transportOptions()
	if s.Transport == "xhttp" {
		cfg := stream["xhttpSettings"].(map[string]any)
		if o.XHTTPMode != "" {
			cfg["mode"] = o.XHTTPMode
		}
		if o.XHTTPPadding != "" {
			cfg["xPaddingBytes"] = o.XHTTPPadding
		}
		if !server && o.XHTTPNoGRPCHeader {
			cfg["noGRPCHeader"] = true
		}
	}
	if s.Transport == "grpc" && !server && o.GRPCMultiMode {
		stream["grpcSettings"].(map[string]any)["multiMode"] = true
	}
	if s.Transport == "kcp" {
		cfg := stream["kcpSettings"].(map[string]any)
		if o.KCPHeader != "" {
			cfg["header"] = map[string]any{"type": o.KCPHeader}
		}
		if o.KCPSeed != "" {
			cfg["seed"] = o.KCPSeed
		}
		for key, value := range map[string]int{"mtu": o.KCPMTU, "tti": o.KCPTTI, "uplinkCapacity": o.KCPUplink, "downlinkCapacity": o.KCPDownlink} {
			if value > 0 {
				cfg[key] = value
			}
		}
		if o.KCPCongestion {
			cfg["congestion"] = true
		}
	}
	if !server && o.TLSFingerprint != "" && s.UsesTLS() {
		stream["tlsSettings"].(map[string]any)["fingerprint"] = o.TLSFingerprint
	}
}

func (s Spec) clientTLSFingerprint(sb, mh map[string]any, q url.Values) {
	if fp := s.transportOptions().TLSFingerprint; fp != "" && s.UsesTLS() {
		sb["tls"].(map[string]any)["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		if s.Kind != "http" {
			mh["client-fingerprint"] = fp
		}
		q.Set("fp", fp)
	}
}

// A server does not consume the TLS fingerprint, noGRPCHeader or gRPC multiMode.
func (s Spec) serverTransportOptions() TransportOptions {
	o := s.transportOptions()
	o.TLSFingerprint, o.XHTTPNoGRPCHeader, o.GRPCMultiMode = "", false, false
	return o
}

func (s Spec) RequiresTransportAgent() bool {
	return s.serverTransportOptions() != (TransportOptions{}) || s.Outbound != nil && s.Outbound.transportOptions() != (TransportOptions{})
}

func TransportOptionsCapable(version string) bool {
	p := strings.Split(version, ".")
	if len(p) != 3 {
		return false
	}
	a, e1 := strconv.Atoi(p[0])
	b, e2 := strconv.Atoi(p[1])
	c, e3 := strconv.Atoi(p[2])
	return e1 == nil && e2 == nil && e3 == nil && a >= 0 && b >= 0 && c >= 0 && (a > 0 || b > 25 || b == 25 && c >= 7)
}
