package clientpolicy

import (
	"slices"
	"testing"
)

func TestExportNamesAndStableSelectors(t *testing.T) {
	for _, v := range []struct{ addr, want string }{{"192.0.2.1", "snell_pro"}, {"example.test", "snell_pro"}, {"2001:db8::1", "snell_pro-v6"}, {"::ffff:192.0.2.1", "snell_pro"}} {
		if got := ExportName("snell_pro", v.addr); got != v.want {
			t.Fatal(got)
		}
	}
	id := "abcdef1234567890"
	nodes := []Node{{ID: id, Name: "snell_pro", Family: "ipv4", Tag: "snell_pro"}, {ID: id, Name: "snell_pro", Family: "ipv6", Tag: "snell_pro-v6"}}
	for _, ref := range []string{"endpoint:" + id + ":ipv6", "snell_pro-v6", "snell_pro-abcdef12-ipv6"} {
		p := Policy{Groups: []Group{{Name: "choice", Type: "select", Proxies: []string{ref}}}, Rules: []string{"MATCH,choice"}}
		r, e := p.Resolve(nodes, nil)
		if e != nil || !slices.Equal(r.Groups[0].Proxies, []string{"snell_pro-v6"}) {
			t.Fatal(ref, r, e)
		}
	}
	p := Policy{Groups: []Group{{Name: "choice", Type: "select", Proxies: []string{"alias"}}}, Rules: []string{"MATCH,choice"}}
	r, e := p.Resolve(nodes, map[string]string{"alias": "endpoint:" + id + ":ipv4"})
	if e != nil || !slices.Equal(r.Groups[0].Proxies, []string{"snell_pro"}) {
		t.Fatal(r, e)
	}
	candidates := []Candidate{{ID: id, Name: "snell_pro", Protocol: "snell", Addresses: []string{"192.0.2.1", "2001:db8::1"}}}
	for _, v := range []struct{ name, f string }{{"snell_pro", "ipv4"}, {"snell_pro-v6", "ipv6"}, {"snell_pro-abcdef12-ipv6", "ipv6"}} {
		got, _ := MatchAlias(Alias{Name: v.name}, candidates)
		if got != EndpointReference(id, v.f) {
			t.Fatal(v, got)
		}
	}
}
func TestExportNameCollisionsFailClearly(t *testing.T) {
	for _, nodes := range [][]Node{{{Tag: "same"}, {Tag: "same"}}, {{Tag: "DIRECT"}}, {{Tag: "bad,name"}}, {{Tag: "bad\nname"}}} {
		if ValidateExportNames(nodes) == nil {
			t.Fatal("invalid names accepted", nodes)
		}
	}
}
func TestDeletePrunesBoundEndpoint(t *testing.T) {
	p := Policy{Groups: []Group{{Name: "choice", Type: "select", Proxies: []string{"alias"}}}, Rules: []string{"MATCH,choice"}}
	b := map[string]string{"alias": "endpoint:one:ipv6"}
	if !p.RemoveNodes(map[string]bool{"one": true}, b) || len(b) != 0 || p.Groups[0].Proxies[0] != "REJECT" {
		t.Fatal(p, b)
	}
}
