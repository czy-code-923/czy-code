package clientpolicy

import("testing";"slices")
func TestEndpointSelectionDoesNotBroadenAddressFamily(t *testing.T){
 p:=Policy{Groups:[]Group{{Name:"choice",Type:"select",Proxies:[]string{"endpoint:one:ipv4"}}},Rules:[]string{"MATCH,choice"}}
 nodes:=[]Node{{ID:"one",Family:"ipv4",Tag:"trojan-v4"},{ID:"one",Family:"ipv6",Tag:"trojan-v6"}}
 r,e:=p.Resolve(nodes,nil);if e!=nil||len(r.Groups)!=1||!slices.Equal(r.Groups[0].Proxies,[]string{"trojan-v4"}){t.Fatal(r,e)}
 p.Groups[0].Proxies=[]string{"endpoint:one:ipv6"};r,e=p.Resolve(nodes,nil);if e!=nil||!slices.Equal(r.Groups[0].Proxies,[]string{"trojan-v6"}){t.Fatal(r,e)}
 p.Groups[0].Proxies=[]string{"node:one"};r,e=p.Resolve(nodes,nil);if e!=nil||len(r.Groups[0].Proxies)!=2{t.Fatal("legacy whole-node references",r,e)}
 p.Groups[0].Proxies=[]string{"PROXY"};r,e=p.Resolve(nodes,nil);if e!=nil||r.Groups[len(r.Groups)-1].Name!="PROXY"{t.Fatal("required generated proxy must be last",r,e)}
}
func TestDeletedNodeReferencesAreRemovedWithoutGrantingAnotherNode(t *testing.T){
 p:=Policy{Groups:[]Group{{Name:"manual",Type:"select",Proxies:[]string{"endpoint:one:ipv4"}},{Name:"dynamic",Type:"select",Proxies:[]string{"ALL"}}},Rules:[]string{"DOMAIN,example.test,endpoint:one:ipv6","MATCH,manual"}}
 if !p.RemoveNodes(map[string]bool{"one":true},map[string]string{}){t.Fatal("not changed")};if p.Groups[0].Proxies[0]!="REJECT"||p.Groups[1].Proxies[0]!="ALL"||p.Rules[0]!="DOMAIN,example.test,REJECT"{t.Fatal(p)}
}
