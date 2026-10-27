// Package clientpolicy compiles a bounded Mihomo policy into native client routing.
// It never fetches remote content or grants access to a node outside its input inventory.
package clientpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Group struct {
	Name      string   `json:"name" yaml:"name"`
	Type      string   `json:"type" yaml:"type"`
	Proxies   []string `json:"proxies" yaml:"proxies"`
	URL       string   `json:"url,omitempty" yaml:"url,omitempty"`
	Interval  int      `json:"interval,omitempty" yaml:"interval,omitempty"`
	Tolerance int      `json:"tolerance,omitempty" yaml:"tolerance,omitempty"`
	Lazy      *bool    `json:"lazy,omitempty" yaml:"lazy,omitempty"`
}
type Policy struct {
	Basic  map[string]any `json:"basic,omitempty" yaml:"basic,omitempty"`
	DNS    map[string]any `json:"dns,omitempty" yaml:"dns,omitempty"`
	Groups []Group        `json:"proxy-groups" yaml:"proxy-groups"`
	Rules  []string       `json:"rules" yaml:"rules"`
}
type Node struct {
	ID       string `json:"id"`
	Host     string `json:"host_id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Family   string `json:"family"`
	Tag      string `json:"tag"`
}
type Result struct {
	Basic    map[string]any  `json:"basic,omitempty"`
	DNS      map[string]any  `json:"dns,omitempty"`
	Groups   []Group         `json:"groups"`
	Rules    []string        `json:"rules"`
	Warnings []string        `json:"warnings"`
	Blocked  map[string]bool `json:"blocked"`
}

func Parse(source string) (Policy, error) {
	var p Policy
	if len(source) > 32768 {
		return p, errors.New("策略最多 32 KiB")
	}
	if strings.TrimSpace(source) == "" {
		return p, errors.New("请填写策略组与规则")
	}
	d := yaml.NewDecoder(strings.NewReader(source))
	d.KnownFields(true)
	if e := d.Decode(&p); e != nil {
		return p, fmt.Errorf("Mihomo YAML: %w", e)
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return p, errors.New("只能填写一个 YAML 文档")
	}
	return p, p.Validate()
}
func label(s string) bool {
	if s == "" || !utf8.ValidString(s) || len([]rune(s)) > 100 || strings.ContainsAny(s, ",\r\n") || strings.HasPrefix(s, "@starhub/") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (p Policy) Validate() error {
	if e := ValidateSections(p.Basic, p.DNS); e != nil {
		return e
	}
	if len(p.Groups) > 32 || len(p.Rules) > 512 {
		return errors.New("最多 32 个策略组、512 条规则")
	}
	names := map[string]Group{}
	for _, g := range p.Groups {
		if !label(g.Name) || (Dynamic(g.Name) || slices.Contains([]string{"GLOBAL", "proxy", "direct", "block"}, g.Name)) {
			return fmt.Errorf("策略组名称无效或保留: %s", g.Name)
		}
		if _, ok := names[g.Name]; ok {
			return errors.New("策略组名称重复")
		}
		if !slices.Contains([]string{"select", "url-test", "fallback", "load-balance"}, g.Type) {
			return errors.New("支持 select/url-test/fallback/load-balance")
		}
		if len(g.Proxies) > 256 {
			return errors.New("组内引用过多")
		}
		if g.Interval < 0 || g.Interval > 86400 || g.Tolerance < 0 || g.Tolerance > 10000 {
			return errors.New("测试间隔或容差无效")
		}
		if g.URL != "" && g.URL != "https://www.gstatic.com/generate_204" && g.URL != "https://cp.cloudflare.com/generate_204" {
			return errors.New("请选择受支持的连通性测试 URL")
		}
		names[g.Name] = g
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(n string) error {
		if visiting[n] {
			return fmt.Errorf("策略组引用成环: %s", n)
		}
		if done[n] {
			return nil
		}
		visiting[n] = true
		for _, ref := range names[n].Proxies {
			if _, ok := names[ref]; ok {
				if e := visit(ref); e != nil {
					return e
				}
			}
		}
		delete(visiting, n)
		done[n] = true
		return nil
	}
	for n := range names {
		if e := visit(n); e != nil {
			return e
		}
	}
	for i, line := range p.Rules {
		parts, e := SplitRule(line)
		if e != nil {
			return fmt.Errorf("规则 %d: %w", i+1, e)
		}
		if parts[0] == "MATCH" && i != len(p.Rules)-1 {
			return errors.New("MATCH 必须是最后一条规则")
		}
	}
	return nil
}
func SplitRule(line string) ([]string, error) {
	if len(line) > 2048 {
		return nil, errors.New("规则过长")
	}
	a := strings.Split(strings.TrimSpace(line), ",")
	for i := range a {
		a[i] = strings.TrimSpace(a[i])
		if a[i] == "" {
			return nil, errors.New("规则含空字段")
		}
	}
	if len(a) == 2 && a[0] == "MATCH" {
		return a, nil
	}
	if len(a) < 3 || len(a) > 4 {
		return nil, errors.New("规则格式为 TYPE,VALUE,TARGET[,no-resolve]")
	}
	if len(a) == 4 && (a[3] != "no-resolve" || !slices.Contains([]string{"IP-CIDR", "IP-CIDR6", "GEOIP"}, a[0])) {
		return nil, errors.New("不支持的规则附加参数")
	}
	switch a[0] {
	case "RULE-SET":
		return nil, errors.New("RULE-SET 需要客户端 rule-providers，请使用该客户端原生规则覆盖并显式提供规则集")
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "PROCESS-NAME", "PROCESS-PATH", "GEOSITE", "GEOIP":
		if (a[0] == "GEOIP" || a[0] == "GEOSITE") && !geoCode.MatchString(a[1]) {
			return nil, errors.New("Geo 数据类别无效")
		}
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR":
		if _, e := netip.ParsePrefix(a[1]); e != nil {
			return nil, errors.New("IP 网段无效")
		}
	case "DST-PORT", "SRC-PORT":
		r := strings.Split(a[1], "-")
		if len(r) > 2 {
			return nil, errors.New("端口范围无效")
		}
		last := 0
		for _, part := range r {
			v, e := strconv.Atoi(part)
			if e != nil || v < 1 || v > 65535 || v < last {
				return nil, errors.New("端口范围无效")
			}
			last = v
		}
	case "NETWORK":
		if a[1] != "TCP" && a[1] != "UDP" {
			return nil, errors.New("NETWORK 只能是 TCP/UDP")
		}
	default:
		return nil, fmt.Errorf("尚未支持的规则类型 %s", a[0])
	}
	return a, nil
}
func target(a []string) int {
	if a[0] == "MATCH" {
		return 1
	}
	return 2
}
func (p Policy) References() []string {
	r := []string{}
	for _, g := range p.Groups {
		r = append(r, g.Proxies...)
	}
	for _, line := range p.Rules {
		a, e := SplitRule(line)
		if e == nil {
			r = append(r, a[target(a)])
		}
	}
	for _, field := range []string{"nameserver", "direct-nameserver", "proxy-server-nameserver"} {
		for _, v := range stringsOf(p.DNS[field]) {
			if _, ref, ok := strings.Cut(v, "#"); ok {
				r = append(r, ref)
			}
		}
	}
	return r
}
func Dynamic(ref string) bool {
	return ref == "ALL" || ref == "DIRECT" || ref == "REJECT" || strings.HasPrefix(ref, "protocol:") || strings.HasPrefix(ref, "node:") || strings.HasPrefix(ref, "host:") || ref == "family:ipv4" || ref == "family:ipv6"
}
func (p Policy) Resolve(nodes []Node, bindings map[string]string) (Result, error) {
	r := Result{Basic: p.Basic, DNS: copyMap(p.DNS), Groups: []Group{}, Rules: []string{}, Warnings: []string{}, Blocked: map[string]bool{}}
	if e := p.Validate(); e != nil {
		return r, e
	}
	names := map[string]bool{}
	for _, g := range p.Groups {
		names[g.Name] = true
	}
	if !names["PROXY"] {
		p.Groups = append([]Group{{Name: "PROXY", Type: "select", Proxies: []string{"ALL"}}}, p.Groups...)
		names["PROXY"] = true
	}
	for _, n := range nodes {
		if names[n.Tag] {
			return r, fmt.Errorf("策略组与节点名称冲突: %s", n.Tag)
		}
	}
	expand := func(ref string) []string {
		if names[ref] || ref == "DIRECT" || ref == "REJECT" {
			return []string{ref}
		}
		tags := []string{}
		for _, n := range nodes {
			if bound := bindings[ref]; bound != "" {
				if bound == n.ID {
					tags = append(tags, n.Tag)
				}
				continue
			}
			if ref == "ALL" || ref == n.Tag || ref == n.Name || bindings[ref] == n.ID || ref == "node:"+n.ID || ref == "host:"+n.Host || ref == "protocol:"+n.Protocol || ref == "family:"+n.Family {
				if !slices.Contains(tags, n.Tag) {
					tags = append(tags, n.Tag)
				}
			}
		}
		return tags
	}
	for _, g := range p.Groups {
		refs := []string{}
		for _, ref := range g.Proxies {
			values := expand(ref)
			if len(values) == 0 {
				r.Warnings = append(r.Warnings, "引用没有已授权且运行中的节点: "+ref)
			}
			for _, v := range values {
				if !slices.Contains(refs, v) {
					refs = append(refs, v)
				}
			}
		}
		if len(refs) == 0 {
			refs = []string{"REJECT"}
		}
		g.Proxies = refs
		if g.Interval == 0 {
			g.Interval = 300
		}
		if g.URL == "" {
			g.URL = "https://www.gstatic.com/generate_204"
		}
		r.Groups = append(r.Groups, g)
	}
	implicit := map[string]string{}
	for _, line := range p.Rules {
		a, _ := SplitRule(line)
		index := target(a)
		values := expand(a[index])
		if len(values) == 0 {
			r.Warnings = append(r.Warnings, "不可用规则目标已改为 REJECT: "+a[index])
			a[index] = "REJECT"
		} else if len(values) == 1 {
			a[index] = values[0]
		} else {
			ref := a[index]
			name := implicit[ref]
			if name == "" {
				sum := sha256.Sum256([]byte(ref))
				name = "@starhub/" + hex.EncodeToString(sum[:4])
				implicit[ref] = name
				r.Groups = append(r.Groups, Group{Name: name, Type: "select", Proxies: values})
			}
			a[index] = name
		}
		r.Rules = append(r.Rules, strings.Join(a, ","))
	}
	if len(r.Rules) == 0 || !strings.HasPrefix(r.Rules[len(r.Rules)-1], "MATCH,") {
		r.Rules = append(r.Rules, "MATCH,PROXY")
	}
	for _, key := range []string{"nameserver", "direct-nameserver", "proxy-server-nameserver"} {
		values := stringsOf(r.DNS[key])
		if len(values) == 0 {
			continue
		}
		for i, value := range values {
			address, ref, ok := strings.Cut(value, "#")
			if !ok {
				continue
			}
			if ref == "REJECT" {
				return r, errors.New("DNS 连接代理不能是 REJECT")
			}
			matched := expand(ref)
			if len(matched) == 0 {
				return r, fmt.Errorf("DNS 代理引用没有已授权节点: %s", ref)
			}
			chosen := matched[0]
			if len(matched) > 1 {
				sum := sha256.Sum256([]byte("dns:" + ref))
				chosen = "@starhub/dns-" + hex.EncodeToString(sum[:4])
				r.Groups = append(r.Groups, Group{Name: chosen, Type: "select", Proxies: matched})
			}
			values[i] = address + "#" + chosen
		}
		r.DNS[key] = values
	}
	return r, nil
}

// Mihomo retains native group semantics. Sing-box has selector and urltest;
// Xray selectors are fixed to their first member and url-test uses a balancer.
func (r Result) Apply(format string, base map[string]any) (map[string]any, []string, error) {
	warnings := append([]string{}, r.Warnings...)
	if format == "mihomo" {
		groups := []any{}
		for _, g := range r.Groups {
			v := map[string]any{"name": g.Name, "type": g.Type, "proxies": g.Proxies}
			if g.Lazy != nil {
				v["lazy"] = *g.Lazy
			}
			if g.Type != "select" {
				v["url"] = g.URL
				v["interval"] = g.Interval
				if g.Type == "url-test" {
					v["tolerance"] = g.Tolerance
				}
			}
			groups = append(groups, v)
		}
		base["proxy-groups"] = groups
		base["rules"] = r.Rules
		return r.ApplySections(format, base, warnings)
	}
	if format != "sing-box" && format != "xray" {
		return nil, warnings, errors.New("节点列表格式无法携带策略组和路由规则；请选择完整 JSON/YAML")
	}
	ref := func(v string) string {
		switch v {
		case "DIRECT":
			return "direct"
		case "REJECT":
			return "block"
		case "PROXY":
			return "proxy"
		}
		return v
	}
	original, _ := base["outbounds"].([]any)
	out := []any{}
	for _, item := range original {
		m, _ := item.(map[string]any)
		if m["tag"] != "proxy" {
			out = append(out, item)
		}
	}
	if format == "sing-box" {
		groups := map[string]Group{}
		for _, g := range r.Groups {
			groups[g.Name] = g
		}
		built := map[string]bool{}
		blocked := map[string]bool{}
		var build func(string) error
		build = func(name string) error {
			if built[name] {
				return nil
			}
			g := groups[name]
			built[name] = true
			members := []string{}
			hasReject := false
			for _, v := range g.Proxies {
				if _, ok := groups[v]; ok {
					if e := build(v); e != nil {
						return e
					}
					if blocked[v] {
						hasReject = true
						continue
					}
				}
				if v == "REJECT" {
					hasReject = true
					continue
				}
				members = append(members, ref(v))
			}
			if len(members) == 0 {
				blocked[name] = true
				return nil
			}
			if hasReject {
				warnings = append(warnings, "sing-box 策略组 "+name+" 含 REJECT：按默认首项转换，不提供拒绝/直连切换")
				if g.Proxies[0] == "REJECT" || blocked[g.Proxies[0]] {
					blocked[name] = true
					return nil
				}
			}
			kind := "selector"
			if g.Type == "url-test" {
				kind = "urltest"
			} else if g.Type != "select" {
				return fmt.Errorf("sing-box 无法等价转换 %s 策略组，请配置该客户端专用策略", g.Type)
			}
			v := map[string]any{"type": kind, "tag": ref(name), "outbounds": members}
			if kind == "urltest" {
				v["url"] = g.URL
				v["interval"] = strconv.Itoa(max(1, g.Interval)) + "s"
				v["tolerance"] = g.Tolerance
			}
			out = append(out, v)
			return nil
		}
		for _, g := range r.Groups {
			if e := build(g.Name); e != nil {
				return nil, warnings, e
			}
		}
		rules := []any{}
		final := "direct"
		for i, line := range r.Rules {
			a, _ := SplitRule(line)
			v, e := singRule(a)
			if e != nil {
				return nil, warnings, fmt.Errorf("规则 %d: %w", i+1, e)
			}
			t := a[target(a)]
			if t == "REJECT" || blocked[t] {
				v["action"] = "reject"
				rules = append(rules, v)
			} else if a[0] == "MATCH" {
				final = ref(t)
			} else {
				v["action"] = "route"
				v["outbound"] = ref(t)
				rules = append(rules, v)
			}
		}
		base["outbounds"] = out
		base["route"] = map[string]any{"rules": rules, "final": final}
		attachRuleSets(base, r.Rules)
		return r.ApplySections(format, base, warnings)
	}
	groups := map[string]Group{}
	for _, g := range r.Groups {
		groups[g.Name] = g
	}
	leafCache := map[string][]string{}
	var leaves func(string) []string
	leaves = func(name string) []string {
		if cached, ok := leafCache[name]; ok {
			return cached
		}
		g, ok := groups[name]
		if !ok {
			return []string{ref(name)}
		}
		v := []string{}
		for _, child := range g.Proxies {
			for _, leaf := range leaves(child) {
				if !slices.Contains(v, leaf) {
					v = append(v, leaf)
				}
			}
		}
		leafCache[name] = v
		return v
	}
	balancers := []any{}
	probes := []string{}
	for _, g := range r.Groups {
		if g.Type == "url-test" {
			members := leaves(g.Name)
			for _, m := range members {
				if m != "block" && m != "direct" {
					if !slices.Contains(probes, m) {
						probes = append(probes, m)
					}
				}
			}
			balancers = append(balancers, map[string]any{"tag": ref(g.Name), "selector": members, "strategy": map[string]any{"type": "leastPing"}})
		} else if g.Type == "select" {
			warnings = append(warnings, "Xray 策略组 "+g.Name+" 使用第一个可用成员；客户端不会提供 Mihomo 式交互选择")
		} else {
			return nil, warnings, fmt.Errorf("Xray 无法等价转换 %s 策略组，请设置专用策略", g.Type)
		}
	}
	var destination func(string) (string, bool)
	destination = func(name string) (string, bool) {
		if g, ok := groups[name]; ok {
			if g.Type == "url-test" {
				return ref(name), true
			}
			return destination(g.Proxies[0])
		}
		return ref(name), false
	}
	rules := []any{}
	for i, line := range r.Rules {
		a, _ := SplitRule(line)
		v, e := xrayRule(a)
		if e != nil {
			return nil, warnings, fmt.Errorf("规则 %d: %w", i+1, e)
		}
		t := a[target(a)]
		chosen, balanced := destination(t)
		if balanced {
			v["balancerTag"] = chosen
		} else {
			v["outboundTag"] = chosen
		}
		rules = append(rules, v)
	}
	base["outbounds"] = out
	strategy := "IPIfNonMatch"
	for _, line := range r.Rules {
		if strings.HasSuffix(line, ",no-resolve") {
			strategy = "AsIs"
			warnings = append(warnings, "Xray 无逐条 no-resolve，使用全局 AsIs：IP 规则仅匹配已知 IP，不主动解析域名")
			break
		}
	}
	base["routing"] = map[string]any{"domainStrategy": strategy, "rules": rules, "balancers": balancers}
	if len(probes) > 0 {
		base["observatory"] = map[string]any{"subjectSelector": probes, "probeURL": "https://www.gstatic.com/generate_204", "probeInterval": "5m", "enableConcurrency": true}
	}
	return r.ApplySections(format, base, warnings)
}
func singRule(a []string) (map[string]any, error) {
	v := map[string]any{}
	if a[0] == "MATCH" {
		return v, nil
	}
	value := a[1]
	switch a[0] {
	case "DOMAIN":
		v["domain"] = []string{value}
	case "DOMAIN-SUFFIX":
		v["domain_suffix"] = []string{value}
	case "DOMAIN-KEYWORD":
		v["domain_keyword"] = []string{value}
	case "IP-CIDR", "IP-CIDR6":
		v["ip_cidr"] = []string{value}
	case "SRC-IP-CIDR":
		v["source_ip_cidr"] = []string{value}
	case "DST-PORT", "SRC-PORT":
		key := "port"
		if a[0] == "SRC-PORT" {
			key = "source_port"
		}
		if strings.Contains(value, "-") {
			v[key+"_range"] = []string{strings.ReplaceAll(value, "-", ":")}
		} else {
			n, _ := strconv.Atoi(value)
			v[key] = []int{n}
		}
	case "NETWORK":
		v["network"] = strings.ToLower(value)
	case "PROCESS-NAME":
		v["process_name"] = []string{value}
	case "PROCESS-PATH":
		v["process_path"] = []string{value}
	case "GEOIP":
		if value == "private" || value == "LAN" {
			v["ip_is_private"] = true
		} else {
			v["rule_set"] = []string{"geoip-" + strings.ToLower(value)}
		}
	case "GEOSITE":
		v["rule_set"] = []string{"geosite-" + strings.ToLower(value)}
	default:
		return nil, fmt.Errorf("%s 需要客户端专用规则或规则集，未静默丢弃", a[0])
	}
	return v, nil
}
func xrayRule(a []string) (map[string]any, error) {
	v := map[string]any{"type": "field"}
	if a[0] == "MATCH" {
		v["network"] = "tcp,udp"
		return v, nil
	}
	value := a[1]
	switch a[0] {
	case "DOMAIN":
		v["domain"] = []string{"full:" + value}
	case "DOMAIN-SUFFIX":
		v["domain"] = []string{"domain:" + value}
	case "DOMAIN-KEYWORD":
		v["domain"] = []string{value}
	case "IP-CIDR", "IP-CIDR6":
		v["ip"] = []string{value}
	case "SRC-IP-CIDR":
		v["source"] = []string{value}
	case "DST-PORT":
		v["port"] = value
	case "SRC-PORT":
		v["sourcePort"] = value
	case "NETWORK":
		v["network"] = strings.ToLower(value)
	case "GEOIP":
		v["ip"] = []string{"geoip:" + strings.ToLower(value)}
	case "GEOSITE":
		v["domain"] = []string{"geosite:" + strings.ToLower(value)}
	default:
		return nil, fmt.Errorf("%s 需要 Xray 专用路由数据/字段，未静默丢弃", a[0])
	}
	return v, nil
}
