package clientpolicy

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode"
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
		boundID, _, _ := EndpointIdentity(bindings[ref])
		if ids[bindings[ref]] || ids[boundID] {
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
		boundID, _, _ := EndpointIdentity(id)
		if ids[id] || ids[boundID] {
			delete(bindings, ref)
			changed = true
		}
	}
	return changed
}

// ExportName is the user-visible name. Stable endpoint IDs stay in policy references.
func ExportName(name, address string) string {
	if AddressFamily(address) == "ipv6" {
		return name + "-v6"
	}
	return name
}
func ValidateExportNames(nodes []Node) error {
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Tag == "" || strings.TrimSpace(n.Tag) != n.Tag || strings.Contains(n.Tag, ",") || strings.IndexFunc(n.Tag, unicode.IsControl) >= 0 {
			return fmt.Errorf("节点订阅名称无效，请使用无逗号或控制字符的名称: %q", n.Tag)
		}
		switch n.Tag {
		case "DIRECT", "REJECT", "ALL", "PROXY", "proxy", "direct", "block":
			return fmt.Errorf("节点订阅名称与客户端保留名称冲突，请修改: %s", n.Tag)
		}
		if seen[n.Tag] {
			return fmt.Errorf("节点订阅名称重复: %s；请修改重名节点（包含原名-v6 的冲突），不会自动附加随机 ID", n.Tag)
		}
		seen[n.Tag] = true
	}
	return nil
}

// Legacy export names remain importable without changing address selection.
func legacyExportMatch(ref string, n Node, nodes []Node) bool {
	if len(n.ID) < 8 {
		return false
	}
	old := n.Name + "-" + n.ID[:8]
	if ref == old+"-"+n.Family {
		return true
	}
	if ref != old {
		return false
	}
	families := map[string]bool{}
	for _, v := range nodes {
		if v.ID == n.ID {
			families[v.Family] = true
		}
	}
	return len(families) == 1
}
