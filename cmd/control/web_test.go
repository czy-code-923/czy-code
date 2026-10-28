package main

import "testing"

func TestWebProxyUsesConfiguredListeners(t *testing.T) {
	for _, test := range []struct{ input, expected string }{{"127.0.0.1:18082", "http://127.0.0.1:18082"}, {"[::1]:18092", "http://[::1]:18092"}, {"0.0.0.0:8080", "http://127.0.0.1:8080"}, {"", "http://127.0.0.1:8080"}} {
		t.Setenv("HTTP_ADDR", test.input)
		u, err := localListenerURL("HTTP_ADDR", "127.0.0.1:8080")
		if err != nil || u.String() != test.expected {
			t.Fatalf("%q: %v %v", test.input, u, err)
		}
	}
	t.Setenv("SHARE_ADDR", "not-an-address")
	if _, err := localListenerURL("SHARE_ADDR", "127.0.0.1:8091"); err == nil {
		t.Fatal("invalid listener accepted")
	}
}
