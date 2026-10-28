package api

import (
	"testing"
	"vps-control/internal/clientpolicy"
)

func TestPolicyFragmentRecognizesStableEndpointWithoutProxySecrets(t *testing.T) {
	f := newSubscriptionFixture(t, 0)
	alias := "original-" + f.node[:8] + "-ipv4"
	out := obj018(f.h.call("POST", "client-policies/import", map[string]any{"source": "proxy-groups:\n  - name: Select\n    type: select\n    proxies: [" + alias + "]\nrules: ['MATCH,Select']\n"}, 200, nil))
	mappings := out["mappings"].([]any)
	if len(mappings) != 1 || mappings[0].(map[string]any)["reference"] != clientpolicy.EndpointReference(f.node, "ipv4") || len(out["unresolved"].([]any)) != 0 {
		t.Fatal("stable fragment alias not recognized", out)
	}
}
