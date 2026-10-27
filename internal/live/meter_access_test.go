package live

import (
	"strings"
	"testing"
)

func TestAccessVariantsStripNetworkAndURLPath(t *testing.T) {
	for _, line := range []string{
		"from tcp:127.0.0.1:12345 accepted tcp:127.0.0.1:443 [in-" + strings.Repeat("a", 32) + " -> out]",
		"from 127.0.0.1:12345 accepted //127.0.0.1:443 [in-" + strings.Repeat("a", 32) + " -> out]",
		"from 127.0.0.1:12345 accepted https://127.0.0.1/private?secret=value [in-" + strings.Repeat("a", 32) + " -> out]",
	} {
		store := newUsageStore()
		w := &accessWriter{meter: &kernelMeter{store: store, secret: "test"}}
		w.line(line)
		if len(store.Access) != 1 {
			t.Fatal("valid native access metadata was dropped")
		}
		for _, v := range store.Access {
			if v.Source != "127.0.0.1" || v.Destination != "127.0.0.1" || v.Port != 443 || strings.Contains(v.Destination, "secret") {
				t.Fatal("URL path/user data retained", v)
			}
		}
	}
}
