package clientpolicy

import (
	"net"
	"strings"
)

type Candidate struct {
	ID, Name, Protocol string
	Addresses          []string
	Port               int
}

type aliasMatch struct {
	node   Candidate
	family string
}

// MatchAlias uses exact protocol/address/port or a unique stable exported tag.
// A bare protocol or a display-name fragment never selects unrelated nodes.
func MatchAlias(a Alias, nodes []Candidate) (string, string) {
	normalize := func(s string) string {
		s = strings.Trim(strings.TrimSpace(s), "[]")
		if ip := net.ParseIP(s); ip != nil {
			return ip.String()
		}
		return strings.ToLower(strings.TrimSuffix(s, "."))
	}
	matches := []aliasMatch{}
	for _, n := range nodes {
		if a.Protocol != "" && normalizeKind(n.Protocol) != normalizeKind(a.Protocol) || a.Port > 0 && a.Port != n.Port {
			continue
		}
		family := ""
		if a.Server != "" {
			found := false
			for _, address := range n.Addresses {
				if normalize(address) == normalize(a.Server) {
					found = true
					family = AddressFamily(strings.Trim(address, "[]"))
					break
				}
			}
			if !found {
				continue
			}
		} else if a.Name != n.Name {
			base := n.Name
			if len(n.ID) >= 8 && a.Name == base+"-"+n.ID[:8] {
				// A single-address own export is unambiguous; a dual-stack source omitting
				// the family must remain a manual choice, never silently include both.
				families := map[string]bool{}
				for _, address := range n.Addresses {
					families[AddressFamily(address)] = true
				}
				if len(families) != 1 {
					continue
				}
				for f := range families {
					family = f
				}
			} else {
				for _, address := range n.Addresses {
					f := AddressFamily(address)
					short := map[string]string{"ipv4": "v4", "ipv6": "v6", "dns": "域名"}[f]
					if a.Name == base+"-"+short || a.Name == base+"-"+f || len(n.ID) >= 8 && a.Name == base+"-"+n.ID[:8]+"-"+f {
						family = f
						break
					}
				}
				if family == "" {
					continue
				}
			}
		}
		matches = append(matches, aliasMatch{n, family})
	}
	if len(matches) > 1 {
		exact := []aliasMatch{}
		for _, m := range matches {
			if a.Name == m.node.Name {
				exact = append(exact, m)
			}
		}
		if len(exact) == 1 {
			matches = exact
		}
	}
	if len(matches) == 1 {
		if matches[0].family != "" {
			return EndpointReference(matches[0].node.ID, matches[0].family), "唯一协议、地址族与端点匹配"
		}
		return "node:" + matches[0].node.ID, "唯一完整名称与协议匹配（该节点全部地址）"
	}
	if len(matches) > 1 {
		return "", "匹配多个节点，请手动选择"
	}
	return "", "未找到对应端点，请手动选择；不会按协议扩大匹配"
}
