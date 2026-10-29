package clientpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Alias struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Server   string `json:"server,omitempty"`
	Port     int    `json:"port,omitempty"`
}
type ImportResult struct {
	UnsupportedRules int                        `json:"unsupported_rules"`
	Format           string                     `json:"format"`
	Policy           Policy                     `json:"policy"`
	Warnings         []string                   `json:"warnings"`
	Aliases          []Alias                    `json:"aliases"`
	Native           map[string]json.RawMessage `json:"native_options"`
}

func Import(source string) (ImportResult, error) {
	r := ImportResult{Warnings: []string{}, Aliases: []Alias{}, Native: map[string]json.RawMessage{}}
	if len(source) > 128<<10 {
		return r, errors.New("导入配置最多 128 KiB")
	}
	if strings.Contains(source, "[Rule]") || strings.Contains(source, "[Proxy Group]") {
		return importINI(source)
	}
	var m map[string]any
	d := yaml.NewDecoder(strings.NewReader(source))
	if e := d.Decode(&m); e != nil || m == nil {
		return r, errors.New("请输入单个 JSON/YAML 配置对象")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return r, errors.New("只允许一个配置文档")
	}
	for _, key := range []string{"proxy-groups", "rules", "proxies", "outbounds"} {
		if v, ok := m[key]; ok {
			if _, ok := v.([]any); !ok {
				return r, fmt.Errorf("%s 需要列表", key)
			}
		}
	}
	for _, key := range []string{"basic", "dns"} {
		if v, ok := m[key]; ok {
			if _, ok := v.(map[string]any); !ok {
				return r, fmt.Errorf("%s 需要对象", key)
			}
		}
	}
	r.Policy = Policy{Basic: map[string]any{}, DNS: map[string]any{}, Groups: []Group{}, Rules: []string{}}
	if _, ok := m["outbounds"]; ok || m["route"] != nil || m["routing"] != nil {
		r.Format = "sing-box"
		if m["routing"] != nil {
			r.Format = "xray"
		}
		for _, v := range array(m["outbounds"]) {
			o := obj(v)
			kind := str(o["type"])
			if kind == "" {
				kind = str(o["protocol"])
				r.Format = "xray"
			}
			tag := str(o["tag"])
			switch kind {
			case "selector", "urltest":
				g := Group{Name: tag, Type: "select", Proxies: stringsOf(o["outbounds"])}
				if kind == "urltest" {
					g.Type = "url-test"
					g.Interval = 300
					g.URL = "https://www.gstatic.com/generate_204"
				}
				r.Policy.Groups = append(r.Policy.Groups, g)
			case "direct", "freedom", "block", "blackhole", "dns":
			default:
				server := str(o["server"])
				port, _ := strconv.Atoi(fmt.Sprint(o["server_port"]))
				if server == "" {
					settings := obj(o["settings"])
					for _, key := range []string{"vnext", "servers"} {
						if a := array(settings[key]); len(a) > 0 {
							server = str(obj(a[0])["address"])
							port, _ = strconv.Atoi(fmt.Sprint(obj(a[0])["port"]))
						}
					}
				}
				r.Aliases = append(r.Aliases, Alias{tag, normalizeKind(kind), server, port})
			}
		}
		// Direct/reject names are client-specific identities, never imported as credentials.
		fixed := map[string]string{}
		for _, v := range array(m["outbounds"]) {
			o := obj(v)
			kind := str(o["type"])
			if kind == "" {
				kind = str(o["protocol"])
			}
			switch kind {
			case "direct", "freedom":
				fixed[str(o["tag"])] = "DIRECT"
			case "block", "blackhole":
				fixed[str(o["tag"])] = "REJECT"
			}
		}
		route := obj(m["route"])
		if r.Format == "xray" {
			route = obj(m["routing"])
			for _, v := range array(route["balancers"]) {
				o := obj(v)
				r.Policy.Groups = append(r.Policy.Groups, Group{Name: str(o["tag"]), Type: "url-test", Proxies: stringsOf(o["selector"]), Interval: 300})
			}
		}
		for i, v := range array(route["rules"]) {
			rule := obj(v)
			if r.Format == "sing-box" {
				if values := stringsOf(rule["rule_set"]); len(values) == 1 {
					for _, definition := range array(route["rule_set"]) {
						set := obj(definition)
						if str(set["tag"]) != values[0] {
							continue
						}
						url := str(set["url"])
						for _, kind := range []string{"geosite", "geoip"} {
							prefix := "https://raw.githubusercontent.com/SagerNet/sing-" + kind + "/rule-set/" + kind + "-"
							if kind == "geoip" && strings.HasPrefix(url, "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/") {
								prefix = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/"
							}
							if strings.HasPrefix(url, prefix) && strings.HasSuffix(url, ".srs") && len(rule) <= 4 {
								code := strings.TrimSuffix(strings.TrimPrefix(url, prefix), ".srs")
								if geoCode.MatchString(code) {
									rule = map[string]any{kind: []any{code}, "action": rule["action"], "outbound": rule["outbound"]}
								}
							}
						}
					}
				}
			}
			lines, e := importNativeRule(rule, r.Format)
			if e != nil {
				r.UnsupportedRules++
				r.Warnings = append(r.Warnings, fmt.Sprintf("原生规则 %d 未迁移：%s", i+1, e))
				continue
			}
			r.Policy.Rules = append(r.Policy.Rules, lines...)
		}
		final := str(route["final"])
		if final == "" {
			if len(r.Policy.Groups) > 0 {
				final = r.Policy.Groups[0].Name
			} else if len(r.Aliases) > 0 {
				final = r.Aliases[0].Name
			} else {
				final = "PROXY"
			}
		}
		if !hasMatch(r.Policy.Rules) {
			r.Policy.Rules = append(r.Policy.Rules, "MATCH,"+final)
		}
		for from, to := range fixed {
			r.Policy.ReplaceReference(from, to)
		}
		// DNS is retained exactly for the same core; portable fields are recognized separately.
		native := map[string]any{}
		for _, key := range []string{"dns", "log"} {
			if v := m[key]; v != nil {
				native[key] = v
			}
		}
		if len(native) > 0 {
			raw, _ := json.Marshal(native)
			r.Native[r.Format] = raw
		}
		if dns := obj(m["dns"]); len(dns) > 0 {
			r.Policy.DNS = portableDNS(dns)
			r.Warnings = append(r.Warnings, "原生 DNS 已保留为该内核专用覆盖，并提取基础 DNS 服务器用于其他客户端；复杂 DNS 规则需核对")
		}
		for _, v := range array(m["inbounds"]) {
			o := obj(v)
			port := number(o["listen_port"])
			if port == 0 {
				port = number(o["port"])
			}
			if port >= 1024 && port <= 65535 {
				r.Policy.Basic["mixed-port"] = port
				break
			}
		}
		r.Warnings = append(r.Warnings, "原生出站中的服务器身份、密码、文件路径和未识别字段未导入；节点需映射到已部署服务")
	} else {
		r.Format = "mihomo"
		if m["basic"] != nil {
			r.Format = "starhub"
			r.Policy.Basic = obj(m["basic"])
		} else {
			for _, k := range BasicKeys {
				if v, ok := m[k]; ok {
					r.Policy.Basic[k] = v
				}
			}
		}
		r.Policy.DNS = obj(m["dns"])
		for _, v := range array(m["proxy-groups"]) {
			raw, _ := json.Marshal(v)
			var g Group
			if e := json.Unmarshal(raw, &g); e != nil {
				return r, errors.New("策略组字段格式错误")
			}
			for k := range obj(v) {
				if !slices.Contains([]string{"name", "type", "proxies", "url", "interval", "tolerance", "lazy"}, k) {
					r.Warnings = append(r.Warnings, "策略组 "+g.Name+" 未迁移字段 "+k)
				}
			}
			r.Policy.Groups = append(r.Policy.Groups, g)
		}
		r.Policy.Rules = stringsOf(m["rules"])
		for _, v := range array(m["proxies"]) {
			o := obj(v)
			port, _ := strconv.Atoi(fmt.Sprint(o["port"]))
			r.Aliases = append(r.Aliases, Alias{str(o["name"]), normalizeKind(str(o["type"])), str(o["server"]), port})
		}
		for k := range m {
			if !slices.Contains(BasicKeys, k) && !slices.Contains([]string{"basic", "dns", "proxy-groups", "rules", "proxies"}, k) {
				r.Warnings = append(r.Warnings, "未迁移顶层字段 "+k+"；不会下载远程节点/规则提供者")
			}
		}
		if len(r.Aliases) > 0 {
			r.Warnings = append(r.Warnings, "已识别节点别名；全部密码、UUID、私钥和原始节点定义已丢弃")
		}
	}
	aliasNames := map[string]bool{}
	for _, alias := range r.Aliases {
		valid := strings.TrimSpace(alias.Name) != "" && len([]rune(alias.Name)) <= 256 && utf8.ValidString(alias.Name) && !strings.Contains(alias.Name, ",")
		for _, c := range alias.Name {
			if unicode.IsControl(c) {
				valid = false
			}
		}
		if !valid || aliasNames[alias.Name] {
			return r, errors.New("导入节点名称不能为空或重复；请先为每个节点设置唯一名称")
		}
		aliasNames[alias.Name] = true
	}
	for i := range r.Policy.Groups {
		name := r.Policy.Groups[i].Name
		replacement := name
		if name == "proxy" {
			replacement = "PROXY"
		} else if strings.HasPrefix(name, "@starhub/") || slices.Contains([]string{"direct", "block", "GLOBAL"}, name) {
			replacement = "导入-" + strings.TrimPrefix(name, "@starhub/")
		}
		if replacement != name {
			r.Policy.Groups[i].Name = replacement
			r.Policy.ReplaceReference(name, replacement)
		}
	}
	// Normalize potentially public client listener addresses before applying a draft.
	if r.Policy.Basic["allow-lan"] == true {
		r.Policy.Basic["allow-lan"] = false
		r.Policy.Basic["bind-address"] = "127.0.0.1"
		r.Warnings = append(r.Warnings, "客户端局域网共享已默认关闭，可在基础配置中手动开启")
	}
	for _, item := range []struct {
		m       map[string]any
		k, port string
	}{{r.Policy.Basic, "external-controller", "9090"}, {r.Policy.DNS, "listen", "1053"}} {
		if value := str(item.m[item.k]); value != "" && !strings.HasPrefix(value, "127.0.0.1:") && !strings.HasPrefix(value, "[::1]:") {
			item.m[item.k] = "127.0.0.1:" + item.port
			r.Warnings = append(r.Warnings, item.k+" 已改为回环监听")
		}
	}
	if len(r.Policy.Groups) == 0 {
		r.Policy.Groups = []Group{{Name: "PROXY", Type: "select", Proxies: []string{"ALL"}}}
	}
	if len(r.Policy.Rules) == 0 {
		r.Policy.Rules = []string{"MATCH," + r.Policy.Groups[0].Name}
	}
	return r, r.Policy.Validate()
}
func array(v any) []any { a, _ := v.([]any); return a }
func str(v any) string  { s, _ := v.(string); return s }
func normalizeKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "ss":
		return "shadowsocks"
	case "hy2":
		return "hysteria2"
	case "socks5":
		return "socks"
	}
	return s
}
func hasMatch(a []string) bool {
	for _, s := range a {
		if strings.HasPrefix(s, "MATCH,") {
			return true
		}
	}
	return false
}
func (p *Policy) ReplaceReference(from, to string) {
	for i := range p.Groups {
		for j, ref := range p.Groups[i].Proxies {
			if ref == from {
				p.Groups[i].Proxies[j] = to
			}
		}
	}
	for i, line := range p.Rules {
		a, e := SplitRule(line)
		if e == nil && a[target(a)] == from {
			a[target(a)] = to
			p.Rules[i] = strings.Join(a, ",")
		}
	}
	for _, key := range []string{"nameserver", "direct-nameserver", "proxy-server-nameserver"} {
		a := stringsOf(p.DNS[key])
		if len(a) == 0 {
			continue
		}
		for i, s := range a {
			address, ref, _ := strings.Cut(s, "#")
			if ref == from {
				a[i] = address + "#" + to
			}
		}
		p.DNS[key] = a
	}
}
func portableDNS(d map[string]any) map[string]any {
	servers := []string{}
	for _, v := range array(d["servers"]) {
		if s, ok := v.(string); ok {
			servers = append(servers, s)
			continue
		}
		o := obj(v)
		server := str(o["address"])
		if server == "" {
			server = str(o["server"])
			kind := str(o["type"])
			if kind == "local" {
				server = "system"
			} else if kind != "" && kind != "fakeip" {
				server = kind + "://" + server + str(o["path"])
			}
		}
		if server != "" && !strings.HasPrefix(server, "fakeip:") {
			servers = append(servers, server)
		}
	}
	return map[string]any{"enable": true, "nameserver": servers}
}
func importNativeRule(m map[string]any, format string) ([]string, error) {
	dest := str(m["outbound"])
	if format == "xray" {
		dest = str(m["outboundTag"])
		if dest == "" {
			dest = str(m["balancerTag"])
		}
	}
	if m["action"] == "reject" {
		dest = "REJECT"
	}
	if dest == "" {
		return nil, errors.New("非路由动作需要原生配置")
	}
	keys := map[string]string{"domain": "DOMAIN", "domain_suffix": "DOMAIN-SUFFIX", "domain_keyword": "DOMAIN-KEYWORD", "ip_cidr": "IP-CIDR", "source_ip_cidr": "SRC-IP-CIDR", "port": "DST-PORT", "source_port": "SRC-PORT", "process_name": "PROCESS-NAME", "process_path": "PROCESS-PATH", "network": "NETWORK", "geosite": "GEOSITE", "geoip": "GEOIP"}
	if format == "xray" {
		keys = map[string]string{"domain": "DOMAIN", "ip": "IP-CIDR", "source": "SRC-IP-CIDR", "port": "DST-PORT", "sourcePort": "SRC-PORT", "network": "NETWORK"}
	}
	if m["ip_is_private"] == true {
		if len(m) > 3 {
			return nil, errors.New("私网规则存在其他条件，需要原生覆盖")
		}
		return []string{"GEOIP,private," + dest}, nil
	}
	for k := range m {
		if _, ok := keys[k]; !ok && !slices.Contains([]string{"type", "action", "outbound", "outboundTag", "balancerTag"}, k) {
			return nil, fmt.Errorf("条件/动作字段 %s 需要原生覆盖", k)
		}
	}
	found := 0
	lines := []string{}
	for k, kind := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		found++
		values := stringsOf(v)
		if a, ok := v.([]any); ok && len(values) == 0 {
			for _, x := range a {
				switch x.(type) {
				case int, float64:
					values = append(values, fmt.Sprint(x))
				default:
					return nil, errors.New("匹配字段需字符串或数字")
				}
			}
		}
		if s, ok := v.(string); ok {
			values = []string{s}
		}
		if n, ok := v.(int); ok {
			values = []string{fmt.Sprint(n)}
		}
		if n, ok := v.(float64); ok {
			values = []string{fmt.Sprint(int(n))}
		}
		for _, s := range values {
			typ := kind
			if kind == "NETWORK" {
				s = strings.ToUpper(s)
				if s == "TCP,UDP" && len(m) <= 3 {
					return []string{"MATCH," + dest}, nil
				}
			}
			if format == "xray" && k == "domain" {
				switch {
				case strings.HasPrefix(s, "full:"):
					s = strings.TrimPrefix(s, "full:")
				case strings.HasPrefix(s, "domain:"):
					typ = "DOMAIN-SUFFIX"
					s = strings.TrimPrefix(s, "domain:")
				case strings.HasPrefix(s, "geosite:"):
					typ = "GEOSITE"
					s = strings.TrimPrefix(s, "geosite:")
				case strings.HasPrefix(s, "regexp:"):
					return nil, errors.New("正则规则需原生覆盖")
				default:
					typ = "DOMAIN-KEYWORD"
				}
			}
			if strings.HasPrefix(s, "geoip:") {
				typ = "GEOIP"
				s = strings.TrimPrefix(s, "geoip:")
			}
			lines = append(lines, typ+","+s+","+dest)
		}
	}
	if found > 1 {
		return nil, errors.New("多条件 AND 规则不可展开为多条 OR 规则")
	}
	if found == 0 {
		return []string{"MATCH," + dest}, nil
	}
	return lines, nil
}
func importINI(source string) (ImportResult, error) {
	r := ImportResult{Format: "shadowrocket", Policy: Policy{Basic: map[string]any{}, DNS: map[string]any{}, Groups: []Group{}, Rules: []string{}}, Warnings: []string{"INI 中的节点身份不会导入；仅提取别名、分组、规则与 DNS"}, Aliases: []Alias{}, Native: map[string]json.RawMessage{}}
	section := ""
	nativeRules := []string{}
	unknownRuleSet := false
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		left, right, ok := strings.Cut(line, "=")
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		switch section {
		case "[Rule]":
			nativeRules = append(nativeRules, line)
			if strings.HasPrefix(line, "RULE-SET,") {
				parts := strings.Split(line, ",")
				if len(parts) < 3 {
					return r, errors.New("规则集缺少 URL/目标")
				}
				code := ""
				if parts[1] == "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Shadowrocket/China/China.list" {
					code = "cn"
				}
				if parts[1] == "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Shadowrocket/Telegram/Telegram.list" {
					code = "telegram"
				}
				if code != "" {
					r.Policy.Rules = append(r.Policy.Rules, "GEOSITE,"+code+","+parts[2])
					continue
				}
				unknownRuleSet = true
				r.UnsupportedRules++
				r.Warnings = append(r.Warnings, "独立规则集仅保留在 Shadowrocket 原生覆盖，其他客户端需要另设规则集")
				continue
			}
			if strings.HasPrefix(line, "FINAL,") {
				line = "MATCH," + strings.TrimPrefix(line, "FINAL,")
			}
			r.Policy.Rules = append(r.Policy.Rules, line)
		case "[Proxy Group]":
			if ok {
				parts := strings.Split(right, ",")
				if len(parts) > 1 {
					refs := []string{}
					for _, s := range parts[1:] {
						s = strings.TrimSpace(s)
						if !strings.Contains(s, "=") {
							refs = append(refs, s)
						}
					}
					r.Policy.Groups = append(r.Policy.Groups, Group{Name: left, Type: strings.TrimSpace(parts[0]), Proxies: refs})
				}
			}
		case "[Proxy]":
			if ok {
				kind, _, _ := strings.Cut(right, ",")
				r.Aliases = append(r.Aliases, Alias{Name: left, Protocol: normalizeKind(strings.TrimSpace(kind))})
			}
		case "[General]":
			if left == "dns-server" || left == "fallback-dns-server" {
				r.Policy.DNS["nameserver"] = strings.Split(strings.ReplaceAll(right, " ", ""), ",")
			}
		}
	}
	if unknownRuleSet {
		groups := map[string]Group{}
		for _, g := range r.Policy.Groups {
			groups[g.Name] = g
		}
		for i, line := range nativeRules {
			parts := strings.Split(line, ",")
			index := len(parts) - 1
			if parts[index] == "no-resolve" {
				index--
			}
			if index < 1 {
				return r, errors.New("INI 规则无效")
			}
			dest := strings.TrimSpace(parts[index])
			if dest != "DIRECT" && dest != "REJECT" {
				if g, ok := groups[dest]; ok && len(g.Proxies) > 0 && (g.Proxies[0] == "DIRECT" || g.Proxies[0] == "REJECT") {
					dest = g.Proxies[0]
				} else {
					dest = "PROXY"
				}
			}
			parts[index] = dest
			nativeRules[i] = strings.Join(parts, ",")
		}
		raw, _ := json.Marshal(map[string]any{"rules": nativeRules})
		r.Native["shadowrocket-config"] = raw
	}
	if len(r.Policy.Groups) == 0 {
		r.Policy.Groups = []Group{{Name: "PROXY", Type: "select", Proxies: []string{"ALL"}}}
	}
	if len(r.Policy.Rules) == 0 {
		r.Policy.Rules = []string{"MATCH,PROXY"}
	}
	return r, r.Policy.Validate()
}
