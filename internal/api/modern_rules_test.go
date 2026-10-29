package api

import (
	"net/http/httptest"
	"testing"
)

func TestModernRuleSetHTTPClientCompatibility(t *testing.T) {
	config := map[string]any{"outbounds": []any{map[string]any{"tag": "proxy", "type": "direct"}}, "route": map[string]any{"final": "proxy", "rule_set": []any{map[string]any{"tag": "test", "download_detour": "proxy"}}}}
	set := object(array(object(config["route"])["rule_set"])[0])
	r := httptest.NewRequest("GET", "/", nil)
	old, _ := clientByID("sing-box")
	if modernSingBox(r, old) {
		t.Fatal("unknown version must preserve old configuration")
	}
	r.Header.Set("User-Agent", "SFA/1.14.4")
	if !modernSingBox(r, old) {
		t.Fatal("1.14 must use the new client")
	}
	r.Header.Set("User-Agent", "SFA/1.13.0")
	if modernSingBox(r, old) {
		t.Fatal("1.13 must keep legacy downloader")
	}
	modernRuleSetHTTPClients(config)
	if set["download_detour"] != nil || object(set["http_client"])["detour"] != "proxy" {
		t.Fatal(set)
	}
	if err := validateClientGraph("sing-box", config); err != nil {
		t.Fatal(err)
	}
	object(set["http_client"])["detour"] = "missing"
	if validateClientGraph("sing-box", config) == nil {
		t.Fatal("missing HTTP client outbound was accepted")
	}
}
