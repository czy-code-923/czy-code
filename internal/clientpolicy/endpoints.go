package clientpolicy

import (
	"net/netip"
	"strings"
)

func AddressFamily(address string) string {
	if ip, e := netip.ParseAddr(address); e == nil {
		if ip.Unmap().Is4() {
			return "ipv4"
		}
		return "ipv6"
	}
	return "dns"
}
func EndpointReference(id, family string) string { return "endpoint:" + id + ":" + family }
func EndpointIdentity(ref string) (id, family string, ok bool) {
	parts := strings.Split(ref, ":")
	if len(parts) != 3 || parts[0] != "endpoint" || parts[1] == "" {
		return "", "", false
	}
	if parts[2] != "ipv4" && parts[2] != "ipv6" && parts[2] != "dns" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// RemoveNodes removes explicit references only. Dynamic ALL/family/group choices
// remain meaningful when another node is created. Empty choices fail closed.
func (p *Policy) RemoveNodes(ids map[string]bool, bindings map[string]string) bool {
	changed := false
	removed := func(ref string) bool {
		if ids[bindings[ref]] {
			return true
		}
		if strings.HasPrefix(ref, "node:") && ids[strings.TrimPrefix(ref, "node:")] {
			return true
		}
		id, _, ok := EndpointIdentity(ref)
		return ok && ids[id]
	}
	for i := range p.Groups {
		members := []string{}
		for _, ref := range p.Groups[i].Proxies {
			if removed(ref) {
				changed = true
			} else {
				members = append(members, ref)
			}
		}
		if len(members) == 0 {
			members = []string{"REJECT"}
		}
		p.Groups[i].Proxies = members
	}
	for i, line := range p.Rules {
		a, e := SplitRule(line)
		if e == nil && removed(a[target(a)]) {
			a[target(a)] = "REJECT"
			p.Rules[i] = strings.Join(a, ",")
			changed = true
		}
	}
	for _, key := range []string{"nameserver", "direct-nameserver", "proxy-server-nameserver"} {
		values := stringsOf(p.DNS[key])
		for i, v := range values {
			address, ref, ok := strings.Cut(v, "#")
			if ok && removed(ref) {
				values[i] = address
				changed = true
			}
		}
		if len(values) > 0 {
			p.DNS[key] = values
		}
	}
	for ref, id := range bindings {
		if ids[id] {
			delete(bindings, ref)
			changed = true
		}
	}
	return changed
}
