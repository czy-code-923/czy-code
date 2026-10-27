package api

import (
	"bytes"
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredEndpointInventoryAndDeletionCleanup(t *testing.T) {
	s := testControlDB(t)
	handler := s.Handler()
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	object := func(raw []byte) map[string]any { var v map[string]any; json.Unmarshal(raw, &v); return v }
	host := object(call("POST", "hosts", map[string]any{"name": "host", "provider": "fixture", "address": "192.0.2.1"}, 201))["id"].(string)
	if _, e := s.DB.Exec(context.Background(), `UPDATE hosts SET agent_version='0.17.0',last_seen=now(),reported_addresses='["192.0.2.1","2001:db8::1"]' WHERE id=$1`, host); e != nil {
		t.Fatal(e)
	}
	node := object(call("POST", "live", map[string]any{"name": "entry", "kind": "shadowsocks", "engine": "sing-box", "host_id": host, "listen": "::", "address": "192.0.2.1", "port": 25300, "endpoint_addresses": []string{"192.0.2.1", "2001:db8::1"}}, 201))["id"].(string)
	var endpoints []map[string]any
	json.Unmarshal(call("GET", "client-policies/nodes", nil, 200), &endpoints)
	if len(endpoints) != 2 || endpoints[0]["reference"] != "endpoint:"+node+":ipv4" || endpoints[1]["reference"] != "endpoint:"+node+":ipv6" {
		t.Fatal(endpoints)
	}
	source := "proxy-groups:\n- name: choice\n  type: select\n  proxies: [endpoint:" + node + ":ipv4]\nrules: [MATCH,choice]\n"
	source = strings.Replace(source, "rules: [MATCH,choice]", "rules: ['MATCH,choice']", 1)
	policy := object(call("POST", "client-policies", map[string]any{"name": "manual", "client_id": "all", "source": source}, 200))["id"].(string)
	if _, e := s.DB.Exec(context.Background(), `INSERT INTO site_settings(tenant_id,domain) VALUES('self','')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(context.Background(), `UPDATE live_services SET enabled=true,state='running',reported_version=version,reported_at=now() WHERE id=$1`, node); e != nil {
		t.Fatal(e)
	}
	sub := object(call("POST", "subscriptions", map[string]any{"name": "endpoint selection", "days": 1, "service_ids": []string{node}}, 201))
	share := httptest.NewRecorder()
	s.ShareHandler().ServeHTTP(share, httptest.NewRequest("GET", sub["url"].(string)+"?format=mihomo", nil))
	if share.Code != 200 {
		t.Fatalf("share %d %s", share.Code, share.Body)
	}
	var config map[string]any
	if e := yaml.Unmarshal(share.Body.Bytes(), &config); e != nil {
		t.Fatal(e)
	}
	groups := config["proxy-groups"].([]any)
	if len(groups) != 1 {
		t.Fatal("unrequested PROXY group", groups)
	}
	members := groups[0].(map[string]any)["proxies"].([]any)
	if len(members) != 1 {
		t.Fatal("single IPv4 endpoint expanded into both families", members)
	}
	found := false
	for _, raw := range config["proxies"].([]any) {
		p := raw.(map[string]any)
		if p["name"] == members[0] {
			found = p["server"] == "192.0.2.1"
		}
	}
	if !found {
		t.Fatal("selected endpoint was not IPv4", members)
	}
	// Stopped acknowledgement allows this fixture to exercise actual deletion.
	s.DB.Exec(context.Background(), `UPDATE live_services SET enabled=false,state='stopped' WHERE id=$1`, node)
	call("DELETE", "live/"+node, nil, 200)
	var saved string
	var version int
	if e := s.DB.QueryRow(context.Background(), `SELECT source,version FROM client_policies WHERE id=$1`, policy).Scan(&saved, &version); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(saved, node) || !strings.Contains(saved, "REJECT") || version != 2 {
		t.Fatal(saved, version)
	}
	json.Unmarshal(call("GET", "client-policies/nodes", nil, 200), &endpoints)
	if len(endpoints) != 0 {
		t.Fatal("deleted endpoint still offered", endpoints)
	}
}
