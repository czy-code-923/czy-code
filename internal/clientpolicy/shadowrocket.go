package clientpolicy

import (
	"errors"
	"fmt"
	"strings"
)

// Shadowrocket's configuration and server subscriptions are separate imports.
// Routing uses the app's PROXY selection; there is no fake JSON selector object.
func (r Result) Shadowrocket() (string, []string, error) {
	warnings := append([]string{}, r.Warnings...)
	warnings = append(warnings, "Shadowrocket 分流配置使用应用当前 PROXY 节点；节点订阅需单独导入。交互策略组折叠为 PROXY/DIRECT/REJECT，不保留分组测速选择")
	var b strings.Builder
	b.WriteString("# Starhub Shadowrocket routing configuration\n# Import the companion server subscription separately, then select a PROXY server.\n[General]\n")
	names := stringsOf(r.DNS["nameserver"])
	clean := []string{}
	for _, s := range names {
		address, _, _ := strings.Cut(s, "#")
		if strings.ContainsAny(address, "\r\n,") {
			return "", warnings, errors.New("Shadowrocket DNS 地址无效")
		}
		clean = append(clean, address)
	}
	if len(clean) > 0 {
		b.WriteString("dns-server = " + strings.Join(clean, ", ") + "\n")
	}
	if !boolValue(r.DNS, "ipv6", true) {
		b.WriteString("ipv6 = false\n")
	}
	b.WriteString("\n[Rule]\n")
	groups := map[string]Group{}
	for _, g := range r.Groups {
		groups[g.Name] = g
	}
	var target func(string, int) string
	target = func(name string, depth int) string {
		if depth > 32 {
			return "REJECT"
		}
		if name == "DIRECT" || name == "REJECT" {
			return name
		}
		if g, ok := groups[name]; ok {
			if len(g.Proxies) == 0 {
				return "REJECT"
			}
			if g.Type == "select" && (g.Proxies[0] == "DIRECT" || g.Proxies[0] == "REJECT") {
				return g.Proxies[0]
			}
			return "PROXY"
		}
		return "PROXY"
	}
	for i, line := range r.Rules {
		a, e := SplitRule(line)
		if e != nil {
			return "", warnings, e
		}
		a[len(a)-1] = strings.TrimSpace(a[len(a)-1])
		dest := target(a[targetIndex(a)], 0)
		if a[0] == "MATCH" {
			b.WriteString("FINAL," + dest + "\n")
			continue
		}
		if a[0] == "GEOSITE" || a[0] == "GEOIP" && strings.EqualFold(a[1], "telegram") {
			path := ""
			switch strings.ToLower(a[1]) {
			case "cn", "geolocation-cn":
				path = "China/China.list"
			case "telegram":
				path = "Telegram/Telegram.list"
			default:
				return "", warnings, fmt.Errorf("Shadowrocket GEOSITE %s 需要专用规则集，规则 %d", a[1], i+1)
			}
			b.WriteString("RULE-SET,https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Shadowrocket/" + path + "," + dest + "\n")
			warnings = append(warnings, "Shadowrocket GEOSITE "+a[1]+" 使用独立 Shadowrocket 规则集，数据来源与 Mihomo 内置 Geo 数据可能不同")
			continue
		}
		a[targetIndex(a)] = dest
		b.WriteString(strings.Join(a, ",") + "\n")
	}
	if len(r.DNS) > 0 {
		warnings = append(warnings, "Shadowrocket 保留基础 DNS；Mihomo fake-ip、fallback-filter、TUN 与嗅探使用应用自身设置，可在 Shadowrocket 原生覆盖中调整")
	}
	return b.String(), warnings, nil
}
func targetIndex(a []string) int {
	if a[0] == "MATCH" {
		return 1
	}
	return 2
}
