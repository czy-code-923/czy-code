package live

import (
	"errors"
	"fmt"
)

// These settings describe a client of this inbound, not its server listener.
// An empty HY2 mode preserves the pre-0.23.16 export behavior exactly.
func (s Spec) clientBandwidth() (int, int) {
	switch s.HY2ClientMode {
	case "bbr":
		return 0, 0
	case "brutal":
		return s.HY2ClientUpMbps, s.HY2ClientDownMbps
	default:
		return s.UpMbps, s.DownMbps
	}
}

func (s Spec) validateClientTuning() error {
	if s.Kind != "hysteria2" && (s.HY2ClientMode != "" || s.HY2ClientUpMbps != 0 || s.HY2ClientDownMbps != 0 || s.HY2BBRProfile != "") {
		return errors.New("HY2 client bandwidth options require Hysteria2")
	}
	if s.HY2ClientMode != "" && s.HY2ClientMode != "bbr" && s.HY2ClientMode != "brutal" {
		return errors.New("HY2 client mode must be bbr, brutal, or empty for legacy compatibility")
	}
	if s.HY2ClientUpMbps < 0 || s.HY2ClientDownMbps < 0 || s.HY2ClientUpMbps > 100000 || s.HY2ClientDownMbps > 100000 {
		return errors.New("HY2 client bandwidth must be 0-100000 Mbps")
	}
	if s.HY2ClientMode != "brutal" && (s.HY2ClientUpMbps != 0 || s.HY2ClientDownMbps != 0) {
		return errors.New("explicit HY2 client bandwidth requires brutal mode")
	}
	if s.HY2ClientMode == "brutal" && s.HY2ClientUpMbps == 0 && s.HY2ClientDownMbps == 0 {
		return errors.New("HY2 brutal mode needs at least one positive client bandwidth; use bbr for automatic estimation")
	}
	if s.HY2BBRProfile != "" && s.HY2BBRProfile != "standard" && s.HY2BBRProfile != "conservative" && s.HY2BBRProfile != "aggressive" {
		return errors.New("HY2 BBR profile must be standard, conservative, or aggressive")
	}
	if s.Kind != "tuic" && (s.TUICRelayMode != "" || s.TUICHeartbeat != 0) {
		return errors.New("TUIC client options require TUIC")
	}
	if s.TUICRelayMode != "" && s.TUICRelayMode != "native" && s.TUICRelayMode != "quic" {
		return errors.New("TUIC UDP relay mode must be native or quic")
	}
	if s.TUICHeartbeat < 0 || s.TUICHeartbeat > 300 {
		return errors.New("TUIC heartbeat must be 0-300 seconds; 0 uses the client default")
	}
	if s.Kind != "anytls" && (s.AnyTLSIdleCheck != 0 || s.AnyTLSIdleTimeout != 0 || s.AnyTLSMinIdle != 0) {
		return errors.New("AnyTLS session options require AnyTLS")
	}
	if s.AnyTLSIdleCheck < 0 || s.AnyTLSIdleCheck > 3600 || s.AnyTLSIdleTimeout < 0 || s.AnyTLSIdleTimeout > 86400 || s.AnyTLSMinIdle < 0 || s.AnyTLSMinIdle > 32 {
		return errors.New("AnyTLS session options exceed bounded limits")
	}
	return nil
}

func (s Spec) clientTuning(sb, mh map[string]any) {
	switch s.Kind {
	case "hysteria2":
		up, down := s.clientBandwidth()
		if up > 0 {
			sb["up_mbps"], mh["up"] = up, up
		}
		if down > 0 {
			sb["down_mbps"], mh["down"] = down, down
		}
		if s.HY2BBRProfile != "" {
			sb["bbr_profile"], mh["bbr-profile"] = s.HY2BBRProfile, s.HY2BBRProfile
		}
	case "tuic":
		if s.TUICRelayMode != "" {
			sb["udp_relay_mode"], mh["udp-relay-mode"] = s.TUICRelayMode, s.TUICRelayMode
		}
		if s.TUICHeartbeat > 0 {
			sb["heartbeat"], mh["heartbeat-interval"] = fmt.Sprintf("%ds", s.TUICHeartbeat), s.TUICHeartbeat*1000
		}
	case "anytls":
		if s.AnyTLSIdleCheck > 0 {
			sb["idle_session_check_interval"], mh["idle-session-check-interval"] = fmt.Sprintf("%ds", s.AnyTLSIdleCheck), s.AnyTLSIdleCheck
		}
		if s.AnyTLSIdleTimeout > 0 {
			sb["idle_session_timeout"], mh["idle-session-timeout"] = fmt.Sprintf("%ds", s.AnyTLSIdleTimeout), s.AnyTLSIdleTimeout
		}
		if s.AnyTLSMinIdle > 0 {
			sb["min_idle_session"], mh["min-idle-session"] = s.AnyTLSMinIdle, s.AnyTLSMinIdle
		}
	}
}

func (s *Spec) clearClientTuning() {
	s.HY2ClientMode, s.HY2BBRProfile, s.TUICRelayMode = "", "", ""
	s.HY2ClientUpMbps, s.HY2ClientDownMbps, s.TUICHeartbeat = 0, 0, 0
	s.AnyTLSIdleCheck, s.AnyTLSIdleTimeout, s.AnyTLSMinIdle = 0, 0, 0
}

// A server Agent does not need subscription-only settings. For a chain, the
// outbound HY2 bandwidth is normalized to the established wire fields, so old
// Agents retain correct BBR / Brutal behavior without a forced reinstall.
func (s Spec) AgentSpec() Spec {
	s.clearClientTuning()
	if s.Outbound != nil {
		next := *s.Outbound
		if next.Kind == "hysteria2" {
			next.UpMbps, next.DownMbps = next.clientBandwidth()
			next.HY2ClientMode = ""
			next.HY2ClientUpMbps, next.HY2ClientDownMbps = 0, 0
		}
		s.Outbound = &next
	}
	return s
}

func (s Spec) NeedsTunedClientAgent() bool {
	return s.HY2BBRProfile != "" || s.TUICRelayMode != "" || s.TUICHeartbeat != 0 || s.AnyTLSIdleCheck != 0 || s.AnyTLSIdleTimeout != 0 || s.AnyTLSMinIdle != 0
}

func ClientTuningCapable(version string) bool {
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	return major > 0 || minor > 23 || minor == 23 && patch >= 16
}
