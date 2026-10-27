package clientpolicy

import "testing"

func TestImportedAliasRequiresPreciseIdentity(t *testing.T) {
	nodes := []Candidate{{"a", "same", "trojan", []string{"192.0.2.1", "2001:db8::1"}, 443}, {"b", "same", "trojan", []string{"192.0.2.2"}, 443}, {"c", "same", "vless", []string{"192.0.2.1"}, 443}}
	for _, c := range []struct {
		alias Alias
		want  string
	}{
		{Alias{Name: "same", Protocol: "trojan"}, ""},
		{Alias{Name: "old", Protocol: "trojan", Server: "192.0.2.1", Port: 443}, "endpoint:a:ipv4"},
		{Alias{Name: "same", Protocol: "trojan", Server: "192.0.2.9", Port: 443}, ""},
		{Alias{Name: "old", Protocol: "trojan", Server: "[2001:0db8::1]", Port: 443}, "endpoint:a:ipv6"},
		{Alias{Name: "same", Protocol: "trojan", Server: "192.0.2.1", Port: 8443}, ""},
	} {
		if got, _ := MatchAlias(c.alias, nodes); got != c.want {
			t.Fatalf("%+v got %s want %s", c.alias, got, c.want)
		}
	}
}
