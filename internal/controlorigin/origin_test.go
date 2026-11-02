package controlorigin

import "testing"

func TestNormalizeOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://a/b", "https://u:p@a", "https://a?x", "https://a#x", "https://a?", "https://a:0", "https://a:65536", "https://a..b", "https://a';echo bad"} {
		if _, err := Normalize(raw, false); err == nil {
			t.Error("accepted", raw)
		}
	}
	for _, raw := range []string{"https://example.com", "https://[2001:db8::1]:443", "https://192.0.2.1:8443"} {
		if _, err := Normalize(raw, false); err != nil {
			t.Error(raw, err)
		}
	}
	if Proof(TokenHash("one"), "nonce", "https://panel.example") == Proof(TokenHash("two"), "nonce", "https://panel.example") || Proof("key", "one", "https://panel.example") == Proof("key", "two", "https://panel.example") {
		t.Fatal("proof does not bind identity and nonce")
	}
}
