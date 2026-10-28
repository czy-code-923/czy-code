package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

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

func TestShareProxyReplacesSpoofedPublicOrigin(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.Header.Get("X-Real-IP"))) }))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	for _, v := range []struct{ peer, header, want string }{{"192.0.2.8:1000", "203.0.113.8", "192.0.2.8"}, {"127.0.0.1:3000", "203.0.113.8", "203.0.113.8"}} {
		r := httptest.NewRequest("GET", "http://share.test/s/test", nil)
		r.RemoteAddr = v.peer
		r.Header.Set("X-Real-IP", v.header)
		w := httptest.NewRecorder()
		shareProxy(target).ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != v.want {
			t.Fatal("origin trust error", w.Code, w.Body.String())
		}
	}
}

func TestRecoveryOriginDoesNotGrantCrossOriginAccess(t *testing.T) {
	for _, v := range []struct {
		origin string
		want   bool
	}{{"http://192.0.2.1:18081", true}, {"http://evil.example", false}, {"https://192.0.2.1:18081", false}, {"http://192.0.2.1:18081/path", false}, {"null", false}} {
		r := httptest.NewRequest("POST", "http://192.0.2.1:18081/api/v1/hosts", nil)
		r.Header.Set("Origin", v.origin)
		if sameWebOrigin(r) != v.want {
			t.Fatal(v)
		}
	}
}
