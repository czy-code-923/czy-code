package api

import "vps-control/internal/live"

func applySecurity(s *live.Spec, b liveInput) error {
	previousTransport := s.TransportOptions
	s.ProtocolOptions = b.ProtocolOptions
	// Old API clients omit the new object. An explicit empty object clears it.
	if b.TransportOptions == nil {
		s.TransportOptions = previousTransport
	}
	if s.TransportOptions != nil && *s.TransportOptions == (live.TransportOptions{}) {
		s.TransportOptions = nil
	}
	s.Flow = b.Flow
	s.ALPN = b.ALPN
	s.Congestion = b.Congestion
	s.WSHost = b.WSHost
	s.ServiceName = b.ServiceName
	s.ManagedUsers = b.ManagedUsers
	s.Username = b.Username
	if s.IsReality() {
		if b.CertificateID != "" || b.Certificate != "" || b.PrivateKey != "" {
			return bad("REALITY uses its X25519 keypair, not a CA certificate")
		}
		current := s.Reality
		if current == nil {
			current = live.NewReality()
		}
		if b.Reality != nil {
			v := *b.Reality
			if v.PrivateKey != "" || v.PublicKey != "" && v.PublicKey != current.PublicKey {
				return bad("REALITY keypair is server-generated; private key cannot be supplied")
			}
			current.Target = v.Target
			current.Port = v.Port
			if v.ShortID != "" {
				current.ShortID = v.ShortID
			}
			if v.Fingerprint != "" {
				current.Fingerprint = v.Fingerprint
			}
		}
		s.Reality = current
		s.Certificate = ""
		s.PrivateKey = ""
	} else {
		s.Reality = nil
	}
	return nil
}

func requireTunedChainAgent(s live.Spec, version string) error {
	if s.RequiresTransportAgent() && !live.TransportOptionsCapable(version) {
		return problem{409, "upgrade host/chain entry Agent to 0.25.7 for transport options"}
	}
	if s.Outbound != nil && s.Outbound.NeedsTunedClientAgent() && !live.ClientTuningCapable(version) {
		return problem{409, "upgrade chain entry Agent to 0.23.17 for client tuning"}
	}
	return nil
}
