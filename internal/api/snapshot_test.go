package api

import "testing"

func TestConditionalSnapshotTags(t *testing.T) {
	for _, v := range []string{`"one"`, `W/"one"`, `W/"two", W/"one"`, `*`} {
		if !matchesETag(v, `"one"`) {
			t.Fatal(v)
		}
	}
	if matchesETag(`"two"`, `"one"`) {
		t.Fatal("mismatched tag accepted")
	}
}
func TestFieldAllowlistUsesExactTokens(t *testing.T) {
	if allowedField("name provider address", "name provider") || allowedField("name provider address", "name;drop") {
		t.Fatal("non-field admitted")
	}
	if !allowedField("name provider address", "name") {
		t.Fatal("valid field refused")
	}
}
