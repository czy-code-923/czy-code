//go:build linux

package hostmeter

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

func readNative() (string, []Raw, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return "", nil, e
	}
	boot := strings.TrimSpace(string(b))
	links, e := net.Interfaces()
	if e != nil {
		return "", nil, e
	}
	routes := map[string]bool{}
	if b, e := os.ReadFile("/proc/net/route"); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 8 && f[1] == "00000000" && f[7] == "00000000" {
				routes[f[0]] = true
			}
		}
	}
	if b, e := os.ReadFile("/proc/net/ipv6_route"); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 10 && f[0] == strings.Repeat("0", 32) && f[1] == "00" {
				routes[f[len(f)-1]] = true
			}
		}
	}
	out := []Raw{}
	for _, n := range links {
		if n.Flags&net.FlagLoopback != 0 || strings.HasPrefix(n.Name, "veth") || strings.HasPrefix(n.Name, "docker") || strings.HasPrefix(n.Name, "br-") || strings.HasPrefix(n.Name, "tun") || strings.HasPrefix(n.Name, "tap") {
			continue
		}
		var rx, tx int64
		ok := true
		for name, v := range map[string]*int64{"rx_bytes": &rx, "tx_bytes": &tx} {
			b, e := os.ReadFile("/sys/class/net/" + n.Name + "/statistics/" + name)
			if e != nil {
				ok = false
				break
			}
			*v, e = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
			if e != nil {
				ok = false
				break
			}
		}
		if !ok {
			return "", nil, fmt.Errorf("interface counter unavailable: %s", n.Name)
		}
		uplink := routes[n.Name]
		if len(routes) == 0 {
			uplink = n.Flags&net.FlagUp != 0 && !strings.HasPrefix(n.Name, "veth") && !strings.HasPrefix(n.Name, "docker") && !strings.HasPrefix(n.Name, "br-") && !strings.HasPrefix(n.Name, "tun")
		}
		out = append(out, Raw{Name: n.Name, RX: rx, TX: tx, Uplink: uplink})
	}
	return boot, out, nil
}
