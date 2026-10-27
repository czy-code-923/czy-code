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
