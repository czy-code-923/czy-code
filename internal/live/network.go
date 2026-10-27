package live

import (
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
)

func NetworkCapable(version string) bool {
	p := strings.Split(version, ".")
	return len(p) == 3 && (p[0] != "0" || func() bool {
		var n int
		for _, c := range p[1] {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		return n >= 12
	}())
}
func (s Spec) validateNetwork() error {
	if len(s.EndpointAddresses) > 2 || len(s.EgressFamilies) > 2 {
		return errors.New("最多选择 IPv4 和 IPv6 两种地址")
	}
	seen := map[string]bool{}
	for _, address := range s.EndpointAddresses {
		a, e := netip.ParseAddr(address)
		if e != nil || a.Zone() != "" || a.IsUnspecified() || a.IsMulticast() {
			return errors.New("发布地址必须为有效 IP")
		}
		family := "ipv6"
		if a.Is4() {
			family = "ipv4"
		}
		if seen[family] {
			return errors.New("每种 IP 类型最多选择一个发布地址")
		}
		seen[family] = true
		if a.Is6() && s.Listen != "::" && s.Listen != "::1" {
			return errors.New("发布 IPv6 时请使用 :: 双栈监听")
		}
	}
	seen = map[string]bool{}
	for _, f := range s.EgressFamilies {
		if !slices.Contains([]string{"ipv4", "ipv6"}, f) || seen[f] {
			return errors.New("出站类型只能选择 IPv4 / IPv6，且不可重复")
		}
		seen[f] = true
	}
	if len(s.EgressFamilies) > 0 && !Proxy(s.Kind) {
		return errors.New("FRP 不使用代理出站策略")
	}
	if len(s.EgressFamilies) == 1 && s.Outbound != nil {
		if a, e := netip.ParseAddr(s.Outbound.Address); e == nil && ((a.Is4() && s.EgressFamilies[0] == "ipv6") || (a.Is6() && s.EgressFamilies[0] == "ipv4")) {
			return errors.New("下一跳地址与所选出站类型不兼容")
		}
	}
	return nil
}
func (s Spec) patchEgress(engine string, files map[string][]byte) error {
	if len(s.EgressFamilies) == 0 || !Proxy(s.Kind) {
		return nil
	}
	var c map[string]any
	if e := json.Unmarshal(files["config.json"], &c); e != nil {
		return e
	}
	strategy := "prefer_ipv4"
	xstrategy := "ForceIPv4v6"
	if s.EgressFamilies[0] == "ipv6" {
		strategy = "prefer_ipv6"
		xstrategy = "ForceIPv6v4"
	}
	if len(s.EgressFamilies) == 1 {
		strategy = s.EgressFamilies[0] + "_only"
		if s.EgressFamilies[0] == "ipv4" {
			xstrategy = "ForceIPv4"
		} else {
			xstrategy = "ForceIPv6"
		}
	}
	outs := c["outbounds"].([]any)
	for _, item := range outs {
		o := item.(map[string]any)
		if o["tag"] != "egress" {
			continue
		}
		if engine == "xray" {
			stream, _ := o["streamSettings"].(map[string]any)
			if stream == nil {
				stream = map[string]any{}
				o["streamSettings"] = stream
			}
			stream["sockopt"] = map[string]any{"domainStrategy": xstrategy}
		} else {
			o["domain_resolver"] = map[string]any{"server": "host-dns", "strategy": strategy}
		}
	}
	if engine == "xray" {
		c["dns"] = map[string]any{"servers": []string{"localhost"}}
	} else {
		c["dns"] = map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "host-dns"}}}
	}
	// DNS selection alone does not restrict literal destinations. Reject the other family.
	if len(s.EgressFamilies) == 1 && s.Outbound == nil && s.Peer == nil {
		opposite := 6
		cidr := "::/0"
		if s.EgressFamilies[0] == "ipv6" {
			opposite = 4
			cidr = "0.0.0.0/0"
		}
		if engine == "xray" {
			c["outbounds"] = append(outs, map[string]any{"protocol": "blackhole", "tag": "egress-block"})
			c["routing"] = map[string]any{"rules": []any{map[string]any{"type": "field", "ip": []string{cidr}, "outboundTag": "egress-block"}}}
		} else {
			route := c["route"].(map[string]any)
			route["rules"] = []any{map[string]any{"ip_version": opposite, "action": "reject"}}
		}
	}
	raw, e := json.MarshalIndent(c, "", "  ")
	files["config.json"] = raw
	return e
}
