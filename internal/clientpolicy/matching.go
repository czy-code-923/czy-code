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

// MatchAlias never broadens a single imported server into every node of a protocol.
// Endpoints take precedence over display names; ambiguity requires a manual choice.
func MatchAlias(a Alias, nodes []Candidate) (string, string) {
	normalize := func(s string) string {
		s = strings.Trim(strings.TrimSpace(s), "[]")
		if ip := net.ParseIP(s); ip != nil {
			return ip.String()
		}
		return strings.ToLower(strings.TrimSuffix(s, "."))
	}
	matches := []Candidate{}
	for _, n := range nodes {
		if a.Protocol != "" && n.Protocol != a.Protocol {
			continue
		}
		if a.Server != "" {
			found := false
			for _, address := range n.Addresses {
				if normalize(address) == normalize(a.Server) {
					found = true
				}
			}
			if !found || (a.Port > 0 && a.Port != n.Port) {
				continue
			}
		} else if a.Name != n.Name {
			continue
		}
		matches = append(matches, n)
	}
	if len(matches) > 1 {
		exact := []Candidate{}
		for _, n := range matches {
			if a.Name == n.Name {
				exact = append(exact, n)
			}
		}
		if len(exact) == 1 {
			matches = exact
		}
	}
	if len(matches) == 1 {
		if a.Server != "" {
			return EndpointReference(matches[0].ID, AddressFamily(strings.Trim(a.Server, "[]"))), "唯一协议、地址族与端点匹配"
		}
		return "node:" + matches[0].ID, "唯一名称与协议匹配"
	}
	if len(matches) > 1 {
		return "", "匹配多个节点，请手动选择"
	}
	return "", "未找到对应端点，请手动选择；不会按协议扩大匹配"
}
