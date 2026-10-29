package clientpolicy

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed default.yaml
var DefaultSource string

func Default() (Policy, error) { return Parse(DefaultSource) }

var BasicKeys = []string{"mixed-port", "port", "socks-port", "allow-lan", "bind-address", "external-controller", "mode", "log-level", "ipv6", "find-process-mode", "unified-delay", "tcp-concurrent", "keep-alive-idle", "keep-alive-interval", "profile", "geodata-mode", "geo-auto-update", "geo-update-interval", "sniffer", "tun"}
var dnsKeys = []string{"enable", "listen", "ipv6", "cache-algorithm", "respect-rules", "prefer-h3", "enhanced-mode", "fake-ip-range", "fake-ip-range6", "fake-ip-filter-mode", "fake-ip-filter", "default-nameserver", "proxy-server-nameserver", "direct-nameserver", "nameserver", "fallback", "fallback-filter", "nameserver-policy", "use-hosts", "use-system-hosts", "direct-nameserver-follow-policy"}

func ValidateSections(basic, dns map[string]any) error {
	for _, part := range []struct {
		m    map[string]any
		keys []string
	}{{basic, BasicKeys}, {dns, dnsKeys}} {
		raw, e := json.Marshal(part.m)
		if e != nil || len(raw) > 16384 {
			return errors.New("基础或 DNS 配置最多 16 KiB")
		}
		for k := range part.m {
			if !slices.Contains(part.keys, k) {
				return fmt.Errorf("未支持的基础/DNS 字段: %s", k)
			}
		}
	}
	if mode, ok := basic["mode"]; ok && !slices.Contains([]string{"rule", "global", "direct"}, fmt.Sprint(mode)) {
		return errors.New("客户端模式需要 rule/global/direct")
	}
	if level, ok := basic["log-level"]; ok && !slices.Contains([]string{"silent", "error", "warning", "warn", "info", "debug"}, fmt.Sprint(level)) {
		return errors.New("客户端日志级别无效")
	}
	for _, k := range []string{"mixed-port", "port", "socks-port"} {
		if v, ok := basic[k]; ok {
			n := number(v)
			if n < 1024 || n > 65535 {
				return fmt.Errorf("%s 需要 1024–65535", k)
			}
		}
	}
	for _, item := range []struct {
		m map[string]any
		k string
	}{{basic, "external-controller"}, {dns, "listen"}} {
		if v, ok := item.m[item.k]; ok && v != "" {
			host, _, e := net.SplitHostPort(fmt.Sprint(v))
			ip := net.ParseIP(host)
			if e != nil || ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("%s 仅允许本地回环监听", item.k)
			}
		}
	}
	for _, k := range []string{"allow-lan", "ipv6", "unified-delay", "tcp-concurrent", "geodata-mode", "geo-auto-update"} {
		if v, ok := basic[k]; ok {
			if _, ok = v.(bool); !ok {
				return fmt.Errorf("%s 必须为布尔值", k)
			}
		}
	}
	for _, k := range []string{"enable", "ipv6", "respect-rules", "prefer-h3", "use-hosts", "use-system-hosts", "direct-nameserver-follow-policy"} {
		if v, ok := dns[k]; ok {
			if _, ok = v.(bool); !ok {
				return fmt.Errorf("DNS %s 必须为布尔值", k)
			}
		}
	}
	for _, k := range []string{"nameserver", "default-nameserver", "proxy-server-nameserver", "direct-nameserver", "fallback", "fake-ip-filter"} {
		if v, ok := dns[k]; ok {
			if values, ok := v.([]any); ok {
				for _, value := range values {
					if _, ok := value.(string); !ok {
						return fmt.Errorf("DNS %s 必须是字符串列表", k)
					}
				}
			}
			list := stringsOf(v)
			if len(list) > 64 {
				return errors.New("DNS 列表最多 64 条")
			}
			if _, ok := v.([]any); !ok {
				if _, ok = v.([]string); !ok {
					return fmt.Errorf("DNS %s 必须为字符串列表", k)
				}
			}
			for _, s := range list {
				if len(s) > 2048 || strings.ContainsAny(s, "\r\n\x00") {
					return errors.New("DNS 值无效")
				}
			}
		}
	}
	return nil
}
func number(v any) int { n, _ := strconv.Atoi(fmt.Sprint(v)); return n }
func stringsOf(v any) []string {
	r := []string{}
	switch a := v.(type) {
	case []string:
		return a
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				r = append(r, s)
			}
		}
	}
	return r
}
func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func boolValue(m map[string]any, k string, fallback bool) bool {
	v, ok := m[k].(bool)
	if !ok {
		return fallback
	}
	return v
}
func copyMap(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}
func nativeTag(s string) string {
	switch s {
	case "PROXY":
		return "proxy"
	case "DIRECT":
		return "direct"
	case "REJECT":
		return "block"
	}
	return s
}

var geoCode = regexp.MustCompile(`^[a-zA-Z0-9_@!.-]{1,80}$`)

func attachRuleSets(base map[string]any, rules []string) {
	detour, _ := obj(base["route"])["final"].(string)
	sets := []any{}
	seen := map[string]bool{}
	for _, line := range rules {
		a, _ := SplitRule(line)
		if a[0] != "GEOSITE" && a[0] != "GEOIP" {
			continue
		}
		if a[0] == "GEOIP" && (a[1] == "private" || a[1] == "LAN") {
			continue
		}
		kind := strings.ToLower(a[0])
		code := strings.ToLower(a[1])
		tag := kind + "-" + code
		if seen[tag] {
			continue
		}
		seen[tag] = true
		definition := map[string]any{"type": "remote", "tag": tag, "format": "binary", "url": "https://raw.githubusercontent.com/SagerNet/sing-" + kind + "/rule-set/" + tag + ".srs", "update_interval": "1d"}
		if detour != "" {
			definition["download_detour"] = detour
		}
		sets = append(sets, definition)
	}
	if len(sets) > 0 {
		obj(base["route"])["rule_set"] = sets
	}
}
func (r Result) ApplySections(format string, base map[string]any, warnings []string) (map[string]any, []string, error) {
	if format == "mihomo" {
		for k, v := range r.Basic {
			base[k] = v
		}
		if len(r.DNS) > 0 {
			base["dns"] = copyMap(r.DNS)
		}
		return base, warnings, nil
	}
	if len(r.Basic) > 0 {
		port := number(r.Basic["mixed-port"])
		if port == 0 {
			port = number(r.Basic["socks-port"])
		}
		ins, _ := base["inbounds"].([]any)
		if port > 0 && len(ins) > 0 {
			key := "listen_port"
			if format == "xray" {
				key = "port"
			}
			obj(ins[0])[key] = port
		}
		if level, ok := r.Basic["log-level"].(string); ok {
			key := "level"
			if format == "xray" {
				key = "loglevel"
				if level == "warn" {
					level = "warning"
				}
			} else if level == "warning" {
				level = "warn"
			}
			base["log"] = map[string]any{key: level}
		}
		if len(r.Basic) > 3 {
			warnings = append(warnings, "基础配置按客户端能力映射；Mihomo 的 TUN/嗅探/控制器/进程及内核优化字段保留在 Mihomo，其他客户端使用其本地入站与原生设置")
		}
	}
	if mode, _ := r.Basic["mode"].(string); mode == "direct" || mode == "global" {
		dest := "proxy"
		if mode == "direct" {
			dest = "direct"
		}
		if format == "sing-box" {
			route := obj(base["route"])
			route["rules"] = []any{}
			route["final"] = dest
		} else if format == "xray" {
			rule := map[string]any{"type": "field", "network": "tcp,udp"}
			key := "outboundTag"
			if mode == "global" {
				var balanced bool
				dest, balanced = r.xrayDestination("PROXY")
				if balanced {
					key = "balancerTag"
				}
			}
			rule[key] = dest
			obj(base["routing"])["rules"] = []any{rule}
		}
		warnings = append(warnings, "已映射客户端全局模式 "+mode+"；规则仍保留在模板中")
	}
	if len(r.DNS) == 0 {
		return base, warnings, nil
	}
	if !boolValue(r.DNS, "enable", true) {
		warnings = append(warnings, "此客户端未关闭系统解析；DNS 关闭仅对 Mihomo 本地 DNS 服务生效")
		return base, warnings, nil
	}
	names := stringsOf(r.DNS["nameserver"])
	if len(names) == 0 {
		names = []string{"1.1.1.1"}
	}
	if format == "sing-box" {
		bootstrap := map[string]any{"type": "local", "tag": "bootstrap"}
		if a := stringsOf(r.DNS["default-nameserver"]); len(a) > 0 {
			v, e := r.singDNSServer(a[0], "bootstrap")
			if e != nil {
				return nil, warnings, e
			}
			if v["domain_resolver"] != nil {
				return nil, warnings, errors.New("引导 DNS 需要 IP 地址，避免解析循环")
			}
			bootstrap = v
		}
		servers := []any{bootstrap}
		dnsRules := []any{}
		for i, address := range names {
			v, e := r.singDNSServer(address, fmt.Sprintf("dns-%d", i))
			if e != nil {
				return nil, warnings, e
			}
			servers = append(servers, v)
		}
		direct := stringsOf(r.DNS["direct-nameserver"])
		if len(direct) > 0 {
			v, e := r.singDNSServer(direct[0], "dns-direct")
			if e != nil {
				return nil, warnings, e
			}
			servers = append(servers, v)
		} else {
			servers = append(servers, map[string]any{"type": "local", "tag": "dns-direct"})
		}
		for _, domain := range stringsOf(r.DNS["fake-ip-filter"]) {
			domain = strings.TrimPrefix(strings.TrimPrefix(domain, "+."), "*.")
			if strings.ContainsAny(domain, "*#") {
				warnings = append(warnings, "未转换复杂 fake-ip-filter: "+domain)
				continue
			}
			dnsRules = append(dnsRules, map[string]any{"domain_suffix": []string{domain}, "action": "route", "server": "dns-direct"})
		}
		if r.DNS["enhanced-mode"] == "fake-ip" {
			fake := map[string]any{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.0/15"}
			if v, ok := r.DNS["fake-ip-range"].(string); ok {
				fake["inet4_range"] = v
			}
			types := []int{1}
			if boolValue(r.DNS, "ipv6", true) {
				fake["inet6_range"] = "fc00::/18"
				types = append(types, 28)
			}
			servers = append(servers, fake)
			dnsRules = append(dnsRules, map[string]any{"query_type": types, "action": "route", "server": "fakeip"})
		}
		dns := map[string]any{"servers": servers, "rules": dnsRules, "final": "dns-0"}
		if !boolValue(r.DNS, "ipv6", true) {
			dns["strategy"] = "ipv4_only"
		}
		base["dns"] = dns
		resolver := "bootstrap"
		if a := stringsOf(r.DNS["proxy-server-nameserver"]); len(a) > 0 {
			v, e := r.singDNSServer(a[0], "dns-node")
			if e != nil {
				return nil, warnings, e
			}
			if v["detour"] != nil {
				return nil, warnings, errors.New("节点域名 DNS 不应通过代理自身连接，可能循环")
			}
			servers = append(servers, v)
			dns["servers"] = servers
			resolver = "dns-node"
		}
		obj(base["route"])["default_domain_resolver"] = resolver
	} else if format == "xray" {
		values := []any{}
		for _, s := range names {
			address, _, _ := strings.Cut(s, "#")
			values = append(values, address)
		}
		strategy := "UseIP"
		if !boolValue(r.DNS, "ipv6", true) {
			strategy = "UseIPv4"
		}
		base["dns"] = map[string]any{"servers": values, "queryStrategy": strategy, "tag": "starhub-dns"}
		if _, group, ok := strings.Cut(names[0], "#"); ok {
			dest, balanced := r.xrayDestination(group)
			route := obj(base["routing"])
			rule := map[string]any{"type": "field", "inboundTag": []string{"starhub-dns"}}
			key := "outboundTag"
			if balanced {
				key = "balancerTag"
			}
			rule[key] = dest
			old, _ := route["rules"].([]any)
			route["rules"] = append([]any{rule}, old...)
		}
		if r.DNS["enhanced-mode"] == "fake-ip" {
			warnings = append(warnings, "Xray JSON DNS 使用真实 IP，未启用 Mihomo fake-ip/TUN；域名分流与 DNS 服务器保留")
		}
	}
	if _, ok := r.DNS["nameserver-policy"]; ok {
		warnings = append(warnings, "nameserver-policy 保留在 Mihomo；其他客户端请配置原生 DNS 规则覆盖，未静默宣称等价")
	}
	if len(names) > 1 {
		warnings = append(warnings, "DNS 列表已映射；各客户端的并发、测速和故障切换时序由其内核决定")
	}
	if _, ok := r.DNS["fallback-filter"]; ok {
		warnings = append(warnings, "Mihomo 响应 GeoIP fallback-filter 无等价跨客户端行为，其他客户端使用主 DNS；可在原生覆盖中细化")
	}
	if format == "sing-box" {
		for _, line := range r.Rules {
			if strings.HasPrefix(line, "GEOSITE,") || strings.HasPrefix(line, "GEOIP,") {
				warnings = append(warnings, "GEOSITE/GEOIP 已转成 SagerNet 官方远程 rule_set；客户端首次运行需下载规则数据")
				break
			}
		}
	}
	return base, warnings, nil
}
func (r Result) xrayDestination(name string) (string, bool) {
	for depth := 0; depth < 33; depth++ {
		found := false
		for _, g := range r.Groups {
			if g.Name == name {
				if g.Type == "url-test" {
					return nativeTag(name), true
				}
				if len(g.Proxies) == 0 {
					return "block", false
				}
				name = g.Proxies[0]
				found = true
				break
			}
		}
		if !found {
			return nativeTag(name), false
		}
	}
	return "block", false
}
func (r Result) singDNSServer(address, tag string) (map[string]any, error) {
	address, group, _ := strings.Cut(address, "#")
	v := map[string]any{"tag": tag}
	if address == "system" || address == "local" {
		v["type"] = "local"
		return v, nil
	}
	if !strings.Contains(address, "://") {
		address = "udp://" + address
	}
	u, e := url.Parse(address)
	if e != nil || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("DNS 地址格式无效")
	}
	kind := u.Scheme
	switch kind {
	case "https", "tls", "quic", "tcp", "udp":
	default:
		return nil, fmt.Errorf("不支持转换 DNS 协议 %s", kind)
	}
	v["type"] = kind
	v["server"] = u.Hostname()
	if u.Port() != "" {
		v["server_port"] = number(u.Port())
	}
	if kind == "https" && u.Path != "" {
		v["path"] = u.Path
	}
	if net.ParseIP(u.Hostname()) == nil {
		v["domain_resolver"] = "bootstrap"
	}
	if group != "" {
		v["detour"] = nativeTag(group)
	}
	return v, nil
}
