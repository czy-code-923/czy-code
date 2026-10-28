package live

import (
	"encoding/json"
	"net"
)

// Retain only byte-identical active members when an additive deployment fails.
// Permission removal, quota changes, stops and credential edits must fail closed.
func canRetainMembers(old, next []Work) bool {
	if len(old) == 0 {
		return false
	}
	for _, previous := range old {
		if !previous.Enabled {
			continue
		}
		found := false
		for _, candidate := range next {
			if candidate.ID == previous.ID && candidate.Enabled {
				a, _ := json.Marshal(previous.Spec)
				b, _ := json.Marshal(candidate.Spec)
				found = string(a) == string(b)
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func preflightAdditional(old, next []Work) error {
	occupied := map[string]bool{}
	for _, w := range old {
		if w.Enabled {
			for _, network := range portNetworks(w.Spec) {
				occupied[network+"/"+portString(w.Spec.Port)] = true
			}
		}
	}
	for _, w := range next {
		if !w.Enabled {
			continue
		}
		for _, network := range portNetworks(w.Spec) {
			if occupied[network+"/"+portString(w.Spec.Port)] {
				continue
			}
			address := net.JoinHostPort(w.Spec.Listen, portString(w.Spec.Port))
			if network == "tcp" {
				l, e := net.Listen("tcp", address)
				if e != nil {
					return e
				}
				l.Close()
			} else {
				l, e := net.ListenPacket("udp", address)
				if e != nil {
					return e
				}
				l.Close()
			}
		}
	}
	return nil
}
func portNetworks(s Spec) []string {
	if s.Kind == "hysteria" || s.Kind == "hysteria2" || s.Kind == "tuic" || s.Transport == "kcp" {
		return []string{"udp"}
	}
	if s.Kind == "shadowsocks" {
		if s.ListenNetwork == "tcp" || s.ListenNetwork == "udp" {
			return []string{s.ListenNetwork}
		}
		return []string{"tcp", "udp"}
	}
	return []string{"tcp"}
}
func rejectAdditive(p *process, members []Work) {
	p.rejected = nil
	for _, m := range members {
		found := false
		for _, old := range p.members {
			if old.ID == m.ID && old.Enabled {
				found = true
				break
			}
		}
		if !found {
			state := "failed"
			if !m.Enabled {
				state = "stopped"
			}
			p.rejected = append(p.rejected, Report{m.ID, m.Version, state})
		}
	}
}
