package api

import (
	"net/http/httptest"
	"testing"
)

func TestSubscriptionClientPrecedence(t *testing.T) {
	for _, c := range []struct{ query, ua, profile, want string }{
		{"", "clash-verge/v2.4.3", "sing-box", "mihomo"},
		{"?client=sing-box", "ClashVerge/2", "mihomo", "sing-box"},
		{"?format=mihomo", "Hiddify/2", "sing-box", "mihomo"},
		{"", "Mozilla/5.0", "mihomo", "mihomo"},
		{"", "", "", "base64"},
		{"", "sing-box/1.14", "mihomo", "sing-box"},
		{"", "Dart/3.8 (dart:io)", "sing-box", "base64"},
		{"", "NekoBox/1.4", "sing-box", "mihomo-nodes"},
		{"", "Hiddify/3", "mihomo", "sing-box"},
		{"", "Happ/fixture", "sing-box", "base64"},
		{"", "v2RayTun/fixture", "sing-box", "base64"},
		{"?client=mihomo", "Happ/fixture", "sing-box", "mihomo"},
		{"?client=clash-verge-rev", "", "", "mihomo"},
	} {
		r := httptest.NewRequest("GET", "http://localhost/s/test"+c.query, nil)
		r.Header.Set("User-Agent", c.ua)
		got, e := selectedClient(r, c.profile)
		if e != nil || got.Format != c.want {
			t.Fatalf("%+v got %+v %v", c, got, e)
		}
	}
}

func TestPolicyAddressFamilyUsesCanonicalIP(t *testing.T) {
	for address, family := range map[string]string{"::ffff:192.0.2.1": "ipv4", "2001:db8::1": "ipv6", "192.0.2.1": "ipv4", "node.example.test": "dns"} {
		if policyFamily(address) != family {
			t.Fatalf("%s family mismatch", address)
		}
	}
}
