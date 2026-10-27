package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"slices"
	"strings"
)

func parseClientText(text string) (json.RawMessage, error) {
	if len(text) > 32768 {
		return nil, bad("client options exceed 32 KiB")
	}
	if strings.TrimSpace(text) == "" {
		return json.RawMessage(`{}`), nil
	}
	var value any
	decoder := yaml.NewDecoder(strings.NewReader(text))
	if e := decoder.Decode(&value); e != nil {
		return nil, bad("invalid client JSON/YAML")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, bad("one client configuration document required")
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, bad("client option keys must be strings")
	}
	return raw, nil
}

// Templates control client policy; generated identities and node lists remain authoritative.
func validateClientOptions(format string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > 32768 {
		return bad("client options exceed 32 KiB")
	}
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&m); e != nil || m == nil {
		return bad("client options must be a JSON object")
	}
	allowed := map[string][]string{
		"mihomo":   {"mode", "ipv6", "log-level", "unified-delay", "tcp-concurrent", "dns", "tun", "sniffer", "rules", "profile", "find-process-mode", "geodata-mode", "keep-alive-idle", "keep-alive-interval"},
		"sing-box": {"log", "dns", "route", "inbounds", "experimental", "ntp"},
		"xray":     {"log", "dns", "routing", "inbounds", "policy"},
	}
	var check func(any, int) error
	nodes := 0
	check = func(v any, depth int) error {
		nodes++
		if nodes > 2000 || depth > 12 {
			return errors.New("client options are too complex")
		}
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if len(k) > 100 {
					return errors.New("option key too long")
				}
				if e := check(v, depth+1); e != nil {
					return e
				}
			}
		case []any:
			if len(x) > 512 {
				return errors.New("option list too long")
			}
			for _, v := range x {
				if e := check(v, depth+1); e != nil {
					return e
				}
			}
		case string:
			if len(x) > 8192 {
				return errors.New("option string too long")
			}
		}
		return nil
	}
	if e := check(m, 0); e != nil {
		return bad(e.Error())
	}
	for k, v := range m {
		if !slices.Contains(allowed[format], k) {
			return bad("client option is not editable for this format: " + k)
		}
		switch k {
		case "dns", "route", "routing", "log", "experimental", "ntp", "tun", "sniffer", "profile", "policy":
			if _, ok := v.(map[string]any); !ok {
				return bad(k + " must be an object")
			}
		case "inbounds":
			rows, ok := v.([]any)
			if !ok || len(rows) > 8 {
				return bad("choose up to eight local client inbounds")
			}
			for _, row := range rows {
				inbound, ok := row.(map[string]any)
				if !ok {
					return bad("invalid client inbound")
				}
				kind, _ := inbound["type"].(string)
				if format == "xray" {
					kind, _ = inbound["protocol"].(string)
				}
				if !slices.Contains([]string{"mixed", "socks", "http"}, kind) {
					return bad("manual client inbounds support local mixed/socks/http only")
				}
				address, _ := inbound["listen"].(string)
				ip := net.ParseIP(address)
				if ip == nil || !ip.IsLoopback() {
					return bad("client inbounds must explicitly listen on loopback")
				}
			}
		case "rules":
			rows, ok := v.([]any)
			if !ok {
				return bad("rules must be a list")
			}
			for _, v := range rows {
				if _, ok := v.(string); !ok {
					return bad("Mihomo rules must be strings")
				}
			}
		case "ipv6", "unified-delay", "tcp-concurrent", "geodata-mode":
			if _, ok := v.(bool); !ok {
				return bad(k + " must be boolean")
			}
		}
	}
	if experimental, ok := m["experimental"].(map[string]any); ok {
		for _, key := range []string{"clash_api", "v2ray_api"} {
			if config, ok := experimental[key].(map[string]any); ok {
				for _, field := range []string{"external_controller", "listen"} {
					if address, ok := config[field].(string); ok && address != "" {
						host, _, err := net.SplitHostPort(address)
						ip := net.ParseIP(host)
						if err != nil || ip == nil || !ip.IsLoopback() {
							return bad("client management API must listen on loopback")
						}
					}
				}
			}
		}
	}
	if format == "mihomo" {
		if mode, ok := m["mode"]; ok && !slices.Contains([]string{"rule", "global", "direct"}, fmt.Sprint(mode)) {
			return bad("invalid Mihomo mode")
		}
	}
	return nil
}

func mergeClientOptions(base map[string]any, raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return base, nil
	}
	var extra map[string]any
	if e := json.Unmarshal(raw, &extra); e != nil {
		return nil, e
	}
	var merge func(map[string]any, map[string]any)
	merge = func(dst, src map[string]any) {
		for k, v := range src {
			a, ok := v.(map[string]any)
			b, bok := dst[k].(map[string]any)
			if ok && bok {
				merge(b, a)
			} else {
				dst[k] = v
			}
		}
	}
	merge(base, extra)
	if e := validateClientReferences(base); e != nil {
		return nil, e
	}
	// Catch common routing mistakes rather than emitting a client that cannot select a generated group.
	if route, ok := base["route"].(map[string]any); ok {
		if final, ok := route["final"].(string); ok {
			valid := false
			for _, item := range base["outbounds"].([]any) {
				if v, ok := item.(map[string]any); ok && v["tag"] == final {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("client route.final references an unavailable outbound: %s", final)
			}
		}
	}
	return base, nil
}

func validateClientReferences(base map[string]any) error {
	tags := map[string]bool{}
	groups := map[string]bool{}
	for _, key := range []string{"outbounds", "proxies", "proxy-groups"} {
		items, _ := base[key].([]any)
		for _, item := range items {
			m := object(item)
			tag := text(m["tag"])
			if tag == "" {
				tag = text(m["name"])
			}
			if tag != "" {
				tags[tag] = true
			}
		}
	}
	if _, mihomo := base["proxies"]; mihomo {
		tags["DIRECT"] = true
		tags["REJECT"] = true
		tags["REJECT-DROP"] = true
	}
	routing := object(base["routing"])
	if b, ok := routing["balancers"].([]any); ok {
		for _, v := range b {
			groups[text(object(v)["tag"])] = true
		}
	}
	var check func(any) error
	check = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "outbound" || k == "outboundTag" || k == "final" {
					if tag, ok := v.(string); ok && tag != "" && !tags[tag] {
						return fmt.Errorf("client route references unavailable outbound: %s", tag)
					}
				}
				if k == "balancerTag" {
					if !groups[text(v)] {
						return errors.New("client route references unavailable balancer")
					}
				}
				if e := check(v); e != nil {
					return e
				}
			}
		case []any:
			for _, v := range x {
				if e := check(v); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := check(base["route"]); e != nil {
		return e
	}
	if e := check(base["routing"]); e != nil {
		return e
	}
	if rules, ok := base["rules"].([]any); ok {
		for _, line := range rules {
			parts := strings.Split(text(line), ",")
			if len(parts) < 2 {
				return errors.New("invalid client rule")
			}
			target := strings.TrimSpace(parts[len(parts)-1])
			if target == "no-resolve" && len(parts) > 2 {
				target = strings.TrimSpace(parts[len(parts)-2])
			}
			if !tags[target] {
				return fmt.Errorf("client rule references unavailable group/node: %s", target)
			}
			if strings.HasPrefix(text(line), "RULE-SET,") {
				return errors.New("manual RULE-SET requires provisioned providers; use supported canonical rules")
			}
		}
	}
	return nil
}
