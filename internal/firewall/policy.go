// Package firewall compiles a small typed policy into an isolated nftables INPUT table.
// No API-provided native nft statements, includes, commands, forwarding or NAT are accepted.
package firewall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"vps-control/internal/manifest"
)

type Rule struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	EndPort  int    `json:"end_port,omitempty"`
	Source   string `json:"source"`
	Action   string `json:"action"`
}
type Policy struct {
	Name          string   `json:"name"`
	SSHPort       int      `json:"ssh_port"`
	AdminCIDRs    []string `json:"admin_cidrs"`
	DefaultAction string   `json:"default_action"`
	Rules         []Rule   `json:"rules"`
}
type Export struct {
	HostID string `json:"host_id"`
	Policy Policy `json:"policy"`
}

func Parse(raw []byte) (Export, error) {
	var e Export
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, errors.New("invalid firewall export")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return e, errors.New("trailing firewall content")
	}
	if !manifest.ValidID(e.HostID) {
		return e, errors.New("invalid host ID")
	}
	return e, e.Policy.Validate()
}
func prefix(s string) (netip.Prefix, error) {
	p, e := netip.ParsePrefix(s)
	if e != nil || p.Addr().Is4In6() || p.Addr().Zone() != "" {
		return p, errors.New("source must be a canonical IPv4/IPv6 CIDR")
	}
	if p != p.Masked() {
		return p, errors.New("CIDR contains host bits")
	}
	return p, nil
}
func (p Policy) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 100 || p.SSHPort < 1 || p.SSHPort > 65535 || p.SSHPort == 8080 || p.SSHPort == 5173 || (p.DefaultAction != "drop" && p.DefaultAction != "accept") {
		return errors.New("invalid name, SSH port or default action")
	}
	if len(p.AdminCIDRs) < 1 || len(p.AdminCIDRs) > 16 || len(p.Rules) > 128 {
		return errors.New("1-16 admin CIDRs and at most 128 rules required")
	}
	seen := map[string]bool{}
	for _, cidr := range p.AdminCIDRs {
		v, e := prefix(cidr)
		if e != nil || v.Bits() == 0 {
			return errors.New("admin CIDR must be specific; /0 is forbidden")
		}
		if seen[cidr] {
			return errors.New("duplicate admin CIDR")
		}
		seen[cidr] = true
	}
	seen = map[string]bool{}
	for _, r := range p.Rules {
		end := r.EndPort
		if end == 0 {
			end = r.Port
		}
		if len([]rune(r.Name)) > 100 || r.Port < 1 || end < r.Port || end > 65535 || (r.Protocol != "tcp" && r.Protocol != "udp") || (r.Action != "accept" && r.Action != "drop") {
			return errors.New("invalid firewall rule")
		}
		if _, e := prefix(r.Source); e != nil {
			return e
		}
		// Management is handled by immutable safeguards; never publish web management on any external address.
		for _, protected := range []int{p.SSHPort, 22, 8080, 5173} {
			if r.Port <= protected && end >= protected {
				return fmt.Errorf("port %d is protected; use the SSH safeguard, not a user rule", protected)
			}
		}
		key := fmt.Sprintf("%s/%d/%d/%s", r.Protocol, r.Port, end, r.Source)
		if seen[key] {
			return errors.New("duplicate or contradictory rule")
		}
		seen[key] = true
	}
	return nil
}
func Table(host string) string { return "harbor_" + host }
func Compile(e Export) (string, error) {
	if !manifest.ValidID(e.HostID) {
		return "", errors.New("invalid host ID")
	}
	if err := e.Policy.Validate(); err != nil {
		return "", err
	}
	p := e.Policy
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s {\n  comment \"harbor-managed-v1\"\n  chain input {\n    type filter hook input priority 10; policy accept;\n    iifname \"lo\" accept\n    ct state established,related accept\n    ct state invalid drop\n    meta l4proto icmp accept\n    meta l4proto ipv6-icmp accept\n    udp sport 67 udp dport 68 accept\n    udp sport 547 udp dport 546 accept\n", Table(e.HostID))
	for _, cidr := range p.AdminCIDRs {
		v, _ := prefix(cidr)
		family := "ip"
		if v.Addr().Is6() {
			family = "ip6"
		}
		fmt.Fprintf(&b, "    %s saddr %s tcp dport %d accept\n", family, cidr, p.SSHPort)
	}
	// Web management stays loopback-only even when the default action is accept.
	b.WriteString("    tcp dport { 8080, 5173 } drop\n")
	// SSH must not fall through to a permissive default for other source addresses.
	fmt.Fprintf(&b, "    tcp dport %d drop\n", p.SSHPort)
	if p.SSHPort != 22 {
		b.WriteString("    tcp dport 22 drop\n")
	}
	// User deny rules precede allows, avoiding accidental shadowing by a broad allow.
	for _, action := range []string{"drop", "accept"} {
		for _, r := range p.Rules {
			if r.Action != action {
				continue
			}
			v, _ := prefix(r.Source)
			family := "ip"
			if v.Addr().Is6() {
				family = "ip6"
			}
			port := fmt.Sprint(r.Port)
			if r.EndPort > r.Port {
				port = fmt.Sprintf("%d-%d", r.Port, r.EndPort)
			}
			fmt.Fprintf(&b, "    %s saddr %s %s dport %s %s\n", family, r.Source, r.Protocol, port, action)
		}
	}
	fmt.Fprintf(&b, "    %s\n  }\n}\n", p.DefaultAction)
	return b.String(), nil
}
func (p Policy) AllowsPeer(peer string) bool {
	a, e := netip.ParseAddr(peer)
	if e != nil {
		return false
	}
	for _, cidr := range p.AdminCIDRs {
		v, e := prefix(cidr)
		if e == nil && v.Contains(a) {
			return true
		}
	}
	return false
}
