package live

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// HopRanges is a bounded numeric language, never native firewall syntax.
func HopRanges(text string) ([][2]int, error) {
	if text == "" {
		return nil, nil
	}
	if len(text) > 256 {
		return nil, errors.New("跳跃端口配置过长")
	}
	parts := strings.Split(text, ",")
	if len(parts) > 16 {
		return nil, errors.New("最多设置 16 段跳跃端口")
	}
	ranges := make([][2]int, 0, len(parts))
	count := 0
	for _, part := range parts {
		ends := strings.Split(part, "-")
		if len(ends) > 2 {
			return nil, errors.New("跳跃端口格式为 30000-30100,30200")
		}
		a, err := strconv.Atoi(ends[0])
		if err != nil || strconv.Itoa(a) != ends[0] {
			return nil, errors.New("跳跃端口必须为十进制整数")
		}
		b := a
		if len(ends) == 2 {
			b, err = strconv.Atoi(ends[1])
			if err != nil || strconv.Itoa(b) != ends[1] {
				return nil, errors.New("跳跃端口范围无效")
			}
		}
		if a < 1024 || b < a || b > 65535 {
			return nil, errors.New("跳跃端口范围须在 1024–65535")
		}
		for _, port := range []int{5173, 8080, 8091} {
			if a <= port && b >= port {
				return nil, errors.New("跳跃范围包含保留的管理端口")
			}
		}
		for _, r := range ranges {
			if a <= r[1] && b >= r[0] {
				return nil, errors.New("跳跃端口范围不能重复或重叠")
			}
		}
		count += b - a + 1
		if count > 4096 {
			return nil, errors.New("每个节点最多配置 4096 个跳跃端口")
		}
		ranges = append(ranges, [2]int{a, b})
	}
	if count < 2 {
		return nil, errors.New("端口跳跃至少需要两个端口")
	}
	return ranges, nil
}

func (s Spec) validateHopping() error {
	if s.HopPorts == "" {
		if s.HopInterval != 0 {
			return errors.New("跳跃间隔需要先设置跳跃端口")
		}
		return nil
	}
	if s.Kind != "hysteria" && s.Kind != "hysteria2" {
		return errors.New("端口跳跃仅适用于 Hysteria / Hysteria2")
	}
	if s.Engine != "" && s.Engine != "sing-box" {
		return errors.New("端口跳跃需要 sing-box 内核")
	}
	if s.Listen != "0.0.0.0" && s.Listen != "::" {
		return errors.New("端口跳跃需要公开的 IPv4 / IPv6 监听地址")
	}
	if s.HopInterval != 0 && (s.HopInterval < 5 || s.HopInterval > 3600) {
		return errors.New("跳跃间隔须为 5–3600 秒")
	}
	_, err := HopRanges(s.HopPorts)
	return err
}

func (s Spec) RequiresHopping() bool {
	return s.HopPorts != "" || s.Outbound != nil && s.Outbound.HopPorts != ""
}
func HoppingCapable(version string) bool {
	var a, b, c int
	if _, e := fmt.Sscanf(version, "%d.%d.%d", &a, &b, &c); e != nil {
		return false
	}
	return a > 0 || b > 23 || b == 23 && c >= 14
}

// ReservedPorts conservatively reserves every listener / hop port regardless of
// protocol, matching the existing host inventory's collision policy.
func (s Spec) ReservedPorts() []int {
	ports := []int{s.Port}
	seen := map[int]bool{s.Port: true}
	ranges, _ := HopRanges(s.HopPorts)
	for _, r := range ranges {
		for p := r[0]; p <= r[1]; p++ {
			if !seen[p] {
				ports = append(ports, p)
				seen[p] = true
			}
		}
	}
	if s.Kind == "frps" {
		for _, t := range s.Tunnels {
			if !seen[t.RemotePort] {
				ports = append(ports, t.RemotePort)
				seen[t.RemotePort] = true
			}
		}
	}
	return ports
}

func (s Spec) hopInterval() int {
	if s.HopInterval > 0 {
		return s.HopInterval
	}
	return 30
}
func (s Spec) hoppingClient(sb, mh map[string]any, q url.Values) {
	if s.HopPorts == "" {
		return
	}
	ranges, _ := HopRanges(s.HopPorts)
	ports := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r[0] == r[1] {
			ports = append(ports, strconv.Itoa(r[0]))
		} else {
			ports = append(ports, fmt.Sprintf("%d:%d", r[0], r[1]))
		}
	}
	delete(sb, "server_port") // Mutually exclusive in current sing-box.
	sb["server_ports"] = ports
	sb["hop_interval"] = fmt.Sprintf("%ds", s.hopInterval())
	mh["ports"] = s.HopPorts
	mh["hop-interval"] = s.hopInterval()
	q.Set("mport", s.HopPorts)
}
