package live

import (
	"strconv"
	"strings"
)

func (s Spec) nativeListenNetwork() string {
	if s.ListenNetwork != "" {
		return s.ListenNetwork
	}
	return "tcp,udp"
}
func (s Spec) RequiresV19() bool {
	return s.ListenNetwork != "" || s.Outbound != nil && s.Outbound.RequiresV19()
}
func V19Capable(version string) bool {
	p := strings.Split(version, ".")
	if len(p) != 3 {
		return false
	}
	major, e := strconv.Atoi(p[0])
	if e != nil || major < 0 {
		return false
	}
	minor, e := strconv.Atoi(p[1])
	if e != nil || minor < 0 {
		return false
	}
	patch, e := strconv.Atoi(p[2])
	return e == nil && patch >= 0 && (major > 0 || minor >= 19)
}

// Socket networks, proxy payloads and protocol framing are separate concepts.
func NetworkDescription(kind string) string {
	switch kind {
	case "shadowsocks":
		return "原生 TCP / UDP 监听；默认两者开启。TCP 封装选项不代表禁用 UDP。"
	case "hysteria", "hysteria2", "tuic":
		return "底层 QUIC / UDP；可代理 TCP 和 UDP 业务，需放行 UDP 业务端口。"
	case "vless", "vmess", "trojan":
		return "支持 TCP / UDP 代理业务；UDP 可以封装在所选连接中，承载取决于 TCP、WS、gRPC 或 mKCP 等封装。"
	case "snell":
		return "本适配使用 TCP 连接并中继 TCP / UDP 业务；不是 Snell v5 QUIC 传输。"
	case "socks", "mixed":
		return "SOCKS5 使用 TCP 控制连接和 UDP 关联；UDP 中继还需要客户端支持和防火墙放行。"
	case "anytls":
		return "TLS / TCP 会话；UDP 中继取决于客户端的 UDP-over-TCP 能力，不使用原生 UDP 入站。"
	case "http", "naive":
		return "本适配提供 HTTP CONNECT / TCP 业务，不声称支持 CONNECT-UDP / HTTP3。"
	}
	return ""
}
