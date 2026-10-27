package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

func validateShadowrocketOptions(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > 32768 {
		return bad("Shadowrocket 覆盖最多 32 KiB")
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return bad("Shadowrocket 覆盖需要 JSON 对象")
	}
	for k, v := range m {
		switch k {
		case "general":
			g, ok := v.(map[string]any)
			if !ok {
				return bad("general 需要对象")
			}
			for field, x := range g {
				if !slices.Contains([]string{"dns-server", "fallback-dns-server", "ipv6", "always-real-ip", "bypass-system", "skip-proxy", "dns-fallback-system"}, field) {
					return bad("不支持 Shadowrocket General 字段 " + field)
				}
				value := fmt.Sprint(x)
				if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
					return bad("General 值无效")
				}
			}
		case "rules":
			a, ok := v.([]any)
			if !ok || len(a) > 512 {
				return bad("rules 需要最多 512 条规则")
			}
			for _, x := range a {
				line, ok := x.(string)
				if !ok || len(line) > 2048 || strings.ContainsAny(line, "\r\n\x00") {
					return bad("Shadowrocket 规则无效")
				}
				p := strings.Split(line, ",")
				if len(p) < 2 {
					return bad("Shadowrocket 规则无效")
				}
				target := p[len(p)-1]
				if target == "no-resolve" && len(p) > 2 {
					target = p[len(p)-2]
				}
				if !slices.Contains([]string{"PROXY", "DIRECT", "REJECT"}, strings.TrimSpace(target)) {
					return bad("Shadowrocket 目标使用 PROXY/DIRECT/REJECT")
				}
				if p[0] == "RULE-SET" {
					if len(p) < 3 {
						return bad("RULE-SET 缺少地址")
					}
					u, e := url.Parse(p[1])
					if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
						return bad("规则集使用无凭据 HTTPS URL")
					}
				}
			}
		default:
			return bad("Shadowrocket 覆盖仅支持 general/rules")
		}
	}
	return nil
}
func shadowrocketOptions(source string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return source, nil
	}
	if e := validateShadowrocketOptions(raw); e != nil {
		return "", e
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	sections := strings.SplitN(source, "[Rule]\n", 2)
	if len(sections) != 2 {
		return "", bad("Shadowrocket 模板缺少 Rule 区")
	}
	head := sections[0]
	if g, ok := m["general"].(map[string]any); ok {
		keys := []string{}
		for k := range g {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines := strings.Split(head, "\n")
			kept := []string{}
			for _, line := range lines {
				if !strings.HasPrefix(line, k+" =") {
					kept = append(kept, line)
				}
			}
			head = strings.Join(kept, "\n") + k + " = " + fmt.Sprint(g[k]) + "\n"
		}
	}
	rules := sections[1]
	if a, ok := m["rules"].([]any); ok {
		lines := []string{}
		for _, v := range a {
			lines = append(lines, v.(string))
		}
		rules = strings.Join(lines, "\n") + "\n"
	}
	return head + "\n[Rule]\n" + rules, nil
}
