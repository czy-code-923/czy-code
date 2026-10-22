package main

import (
	"strings"
	"testing"
)

func TestGatewayIsolation(t *testing.T) {
	s, e := render("vps.example.com", false, "", "", []string{"203.0.113.5/32", "2001:db8::1/128"})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"access_log off;", "127.0.0.1:8091", "location ^~ /api/v1/agent/", "deny all;", "server_name vps.example.com"} {
		if !strings.Contains(s, v) {
			t.Fatal(v)
		}
	}
	if _, e = render("a; include secret;", false, "", "", []string{"127.0.0.1/32"}); e == nil {
		t.Fatal("injection")
	}
	if _, e = render("a.test", false, "", "", []string{"0.0.0.0/0"}); e == nil {
		t.Fatal("public management")
	}
	if _, e = render("a.test", true, "/missing/cert", "/missing/key", []string{"127.0.0.1/32"}); e == nil {
		t.Fatal("TLS without certificate")
	}
}
