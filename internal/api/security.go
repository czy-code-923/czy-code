package api

import "vps-control/internal/live"

func applySecurity(s *live.Spec, b liveInput) error {
	s.ProtocolOptions = b.ProtocolOptions
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
