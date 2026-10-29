package authconfig

import "testing"

func TestExampleTokensAreNotLoginCredentials(t *testing.T) {
	for _, v := range []string{"generate-at-least-32-random-bytes", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "change-me-change-me-change-me-change-me", " short "} {
		if ValidAdminToken(v) {
			t.Fatal("unsafe administrator token accepted")
		}
	}
	if !ValidAdminToken("01bc23de45fa678901bc23de45fa678901bc23de45fa678901bc23de45fa6789") {
		t.Fatal("random token rejected")
	}
}
