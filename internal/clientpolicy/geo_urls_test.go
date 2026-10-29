package clientpolicy

import (
	"strings"
	"testing"
)

func TestGeoIPTelegramUsesPublishedServiceDataset(t *testing.T) {
	base := map[string]any{"route": map[string]any{"final": "direct"}}
	attachRuleSets(base, []string{"GEOIP,telegram,DIRECT,no-resolve", "GEOSITE,cn,DIRECT", "GEOIP,CN,DIRECT"})
	sets := obj(base["route"])["rule_set"].([]any)
	if len(sets) != 3 {
		t.Fatalf("expected three rule sets, got %d", len(sets))
	}
	if got := obj(sets[0])["url"].(string); !strings.HasSuffix(got, "/geo/geoip/telegram.srs") || !strings.Contains(got, "MetaCubeX/meta-rules-dat") {
		t.Fatal(got)
	}
	if got := obj(sets[1])["url"].(string); !strings.HasSuffix(got, "/geosite-cn.srs") {
		t.Fatal(got)
	}
	if got := obj(sets[2])["url"].(string); !strings.HasSuffix(got, "/geo/geoip/cn.srs") {
		t.Fatal(got)
	}
}
