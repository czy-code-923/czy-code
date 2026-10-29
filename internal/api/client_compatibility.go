package api

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"vps-control/internal/live"
)

// An App's bundled core/parser is independent of the server's installed core.
// These conservative adapters export only formats we can represent without
// dropping authentication, transport or security fields.
func compatibleClient(c clientInfo, endpoint live.Spec) error {
	format := c.Format
	if format == "mihomo-nodes" {
		format = "mihomo"
	}
	switch c.ID {
	case "base64":
		if endpoint.Kind == "snell" || endpoint.Kind == "naive" {
			return fmt.Errorf("portable subscription excludes non-standard node URIs")
		}
	case "hiddify", "nekobox", "nekobox-uri":
		if endpoint.Kind == "snell" || endpoint.Kind == "naive" || (c.ID != "hiddify" && endpoint.Kind == "anytls") {
			return fmt.Errorf("%s 尚无已验证的 %s 适配", c.Name, endpoint.Kind)
		}
	case "stash":
		if !slices.Contains([]string{"shadowsocks", "vless", "vmess", "trojan", "hysteria2", "tuic", "http", "socks"}, endpoint.Kind) {
			return fmt.Errorf("Stash 尚无已验证的 %s 适配", endpoint.Kind)
		}
	case "v2rayng", "v2rayng-uri", "happ", "v2raytun":
		format = "xray"
	case "v2rayn":
		if !slices.Contains([]string{"shadowsocks", "vless", "vmess", "trojan", "hysteria2", "tuic", "anytls", "socks"}, endpoint.Kind) {
			return fmt.Errorf("v2rayN 尚无已验证的 %s 节点链接", endpoint.Kind)
		}
	}
	if format == "shadowrocket-config" {
		format = "shadowrocket"
	}
	if !c.Full && (endpoint.IsReality() && endpoint.Engine == "xray" || endpoint.Transport == "xhttp" || endpoint.Transport == "kcp" || endpoint.Transport == "httpupgrade") {
		return fmt.Errorf("节点链接无法无损表达此安全层或传输层，请使用对应内核 JSON")
	}
	return endpoint.ClientFormat(format)
}

// Native mobile clients need a TUN and the selector API; mixed-only JSON can be
// parsed successfully while carrying no VPN traffic or visible proxy choices.
func appSingBoxConfig(c clientInfo, config map[string]any) {
	if !slices.Contains([]string{"sing-box", "sing-box-latest", "sfa", "hiddify"}, c.ID) {
		return
	}
	ins, _ := config["inbounds"].([]any)
	hasTun := false
	for _, in := range ins {
		if object(in)["type"] == "tun" {
			hasTun = true
		}
	}
	if !hasTun {
		config["inbounds"] = append(ins, map[string]any{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}, "auto_route": true, "strict_route": true, "stack": "mixed"})
	}
	route := object(config["route"])
	route["auto_detect_interface"] = true
	rules, _ := route["rules"].([]any)
	route["rules"] = append([]any{map[string]any{"action": "sniff"}, map[string]any{"protocol": "dns", "action": "hijack-dns"}}, rules...)
	config["route"] = route
	experimental := object(config["experimental"])
	if experimental["clash_api"] == nil {
		experimental["clash_api"] = map[string]any{"external_controller": "127.0.0.1:9090"}
	}
	config["experimental"] = experimental
}

var coreVersionRE = regexp.MustCompile(`(?i)(?:sing[- ]?box|sfa|sfi)[/ v]*([0-9]+)\.([0-9]+)`)

func runtimeClientCompatibility(r *http.Request, c clientInfo, spec live.Spec) error {
	if err := compatibleClient(c, spec); err != nil {
		return err
	}
	if spec.Kind == "snell" && c.Format == "sing-box" {
		if strings.Contains(strings.ToLower(r.UserAgent()), "hiddify") {
			return fmt.Errorf("Hiddify does not support this Snell adapter")
		}
		if c.ID != "sing-box-latest" {
			version := coreVersionRE.FindStringSubmatch(r.UserAgent())
			ok := false
			if len(version) == 3 {
				major, _ := strconv.Atoi(version[1])
				minor, _ := strconv.Atoi(version[2])
				ok = major > 1 || major == 1 && minor >= 14
			}
			if !ok {
				return fmt.Errorf("Snell requires confirmed sing-box 1.14+; choose the explicit latest-core profile")
			}
		}
	}
	return nil
}
