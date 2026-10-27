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
		{"", "", "", "sing-box"},
		{"", "sing-box/1.14", "mihomo", "sing-box"},
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
