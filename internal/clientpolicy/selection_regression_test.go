package clientpolicy

import (
	"slices"
	"testing"
)

func TestEndpointFamiliesMappedAddressesAndDynamicRefresh(t *testing.T) {
	if AddressFamily("::ffff:192.0.2.1") != "ipv4" {
		t.Fatal("mapped IPv4 classified as IPv6")
	}
	p := Policy{Groups: []Group{{Name: "v4", Type: "select", Proxies: []string{"family:ipv4"}}, {Name: "fixed", Type: "select", Proxies: []string{"endpoint:one:ipv4"}}}, Rules: []string{"MATCH,v4"}}
	inventory := []Node{{ID: "one", Name: "one", Family: "ipv4", Tag: "one-v4"}, {ID: "one", Name: "one", Family: "ipv6", Tag: "one-v6"}}
	first, e := p.Resolve(inventory, nil)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(first.Groups[0].Proxies, []string{"one-v4"}) {
		t.Fatal(first.Groups)
	}
	inventory = append(inventory, Node{ID: "two", Name: "two", Family: "ipv4", Tag: "two-v4"})
	next, e := p.Resolve(inventory, nil)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(next.Groups[0].Proxies, []string{"one-v4", "two-v4"}) || !slices.Equal(next.Groups[1].Proxies, []string{"one-v4"}) {
		t.Fatal("dynamic or fixed endpoint selection stale", next.Groups)
	}
}
func TestRawNamesCannotBroadenAnExactExportTagOrAmbiguousNode(t *testing.T) {
	nodes := []Node{{ID: "one", Name: "same", Tag: "export-one", Family: "ipv4"}, {ID: "two", Name: "export-one", Tag: "export-two", Family: "ipv6"}, {ID: "three", Name: "same", Tag: "export-three", Family: "ipv6"}}
	p := Policy{Groups: []Group{{Name: "choice", Type: "select", Proxies: []string{"export-one"}}}, Rules: []string{"MATCH,choice"}}
	r, e := p.Resolve(nodes, nil)
	if e != nil || !slices.Equal(r.Groups[0].Proxies, []string{"export-one"}) {
		t.Fatal("exact tag expanded display name", r, e)
	}
	p.Groups[0].Proxies = []string{"same"}
	r, e = p.Resolve(nodes, nil)
	if e != nil || len(r.Groups) != 0 || !slices.Equal(r.Rules, []string{"MATCH,REJECT"}) {
		t.Fatal("ambiguous name broadened", r, e)
	}
	r, e = p.Resolve(nodes, map[string]string{"same": "one"})
	if e != nil || !slices.Equal(r.Groups[0].Proxies, []string{"export-one"}) {
		t.Fatal("stable binding lost", r, e)
	}
}
func TestGroupFreePolicyGetsValidFinalTarget(t *testing.T) {
	p := Policy{Rules: []string{"DOMAIN,example.test,DIRECT"}}
	r, e := p.Resolve([]Node{{ID: "one", Family: "ipv4", Tag: "one"}}, nil)
	if e != nil || len(r.Groups) != 1 || r.Groups[0].Name != "PROXY" || r.Rules[len(r.Rules)-1] != "MATCH,PROXY" {
		t.Fatal("implicit MATCH referenced missing group", r, e)
	}
}
func TestImportedAliasKeepsPreciseAddressAndPort(t *testing.T) {
	id := "abcdef1234567890"
	nodes := []Candidate{{ID: id, Name: "jp", Protocol: "shadowsocks", Addresses: []string{"192.0.2.1", "2001:db8::1"}, Port: 443}, {ID: "other", Name: "jp2", Protocol: "trojan", Addresses: []string{"192.0.2.2"}, Port: 443}}
	for _, c := range []struct {
		a    Alias
		want string
	}{
		{Alias{Name: "jp", Protocol: "SS", Port: 8443}, ""},
		{Alias{Name: "jp-v4", Protocol: "ss"}, "endpoint:" + id + ":ipv4"},
		{Alias{Name: "jp-v6", Protocol: "shadowsocks"}, "endpoint:" + id + ":ipv6"},
		{Alias{Name: "jp-abcdef12-ipv6", Protocol: "shadowsocks"}, "endpoint:" + id + ":ipv6"},
		{Alias{Name: "jp-abcdef12", Protocol: "shadowsocks"}, ""},
		{Alias{Name: "old", Protocol: "SS", Server: "::ffff:192.0.2.1", Port: 443}, "endpoint:" + id + ":ipv4"},
	} {
		got, _ := MatchAlias(c.a, nodes)
		if got != c.want {
			t.Fatalf("%+v got %s want %s", c.a, got, c.want)
		}
	}
}

func TestImportRejectsAmbiguousDuplicateServerNames(t *testing.T) {
	source := `proxies:
  - {name: same, type: ss, server: 192.0.2.1, port: 443}
  - {name: same, type: ss, server: 192.0.2.2, port: 443}
proxy-groups:
  - {name: choice, type: select, proxies: [same]}
rules: ["MATCH,choice"]`
	if _, err := Import(source); err == nil {
		t.Fatal("duplicate alias was allowed to map to a different server")
	}
}

func TestDynamicFamilyDoesNotMatchAnotherNodesDisplayName(t *testing.T) {
	p := Policy{Groups: []Group{{Name: "choice", Type: "select", Proxies: []string{"family:ipv4"}}}, Rules: []string{"MATCH,choice"}}
	r, err := p.Resolve([]Node{{ID: "v4", Name: "actual", Family: "ipv4", Tag: "v4"}, {ID: "v6", Name: "family:ipv4", Family: "ipv6", Tag: "v6"}}, nil)
	if err != nil || !slices.Equal(r.Groups[0].Proxies, []string{"v4"}) {
		t.Fatal("display name overrode an address-family selector", r, err)
	}
}
