package api

import (
	"net/http/httptest"
	"testing"
	"vps-control/internal/live"
)

func TestHoppingReservationsAndAgentGate(t *testing.T) {
	s := testControlDB(t)
	h := api018{t, s}
	host := obj018(h.call("POST", "hosts", map[string]any{"name": "hopping-test", "address": "192.0.2.1"}, 201, nil))
	hostID := host["id"].(string)
	h.call("POST", "agent/heartbeat", map[string]any{"version": "0.23.13", "platform": "linux/amd64"}, 200, host)
	node := obj018(h.call("POST", "live", map[string]any{"name": "initial", "host_id": hostID, "kind": "shadowsocks", "address": "192.0.2.1", "listen": "0.0.0.0", "port": 24000}, 201, nil))
	id := node["id"].(string)
	r := httptest.NewRequest("POST", "/", nil)
	spec := live.Spec{Kind: "hysteria2", Port: 24000, ProtocolOptions: live.ProtocolOptions{HopPorts: "30000-30100"}}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(r.Context())
	if requireHopping(r, tx, hostID, spec) == nil {
		t.Fatal("old Agent accepted")
	}
	if _, e = tx.Exec(r.Context(), "UPDATE hosts SET agent_version='0.23.14' WHERE tenant_id=$1 AND id=$2", tenant, hostID); e != nil {
		t.Fatal(e)
	}
	if e = requireHopping(r, tx, hostID, spec); e != nil {
		t.Fatal(e)
	}
	if e = reserveLivePorts(r, tx, hostID, id, spec); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM live_ports WHERE tenant_id=$1 AND service_id=$2", tenant, id).Scan(&count); e != nil || count != 102 {
		t.Fatal(count, e)
	}
	if e = reserveLivePorts(r, tx, hostID, "other", live.Spec{Port: 30020}); e == nil {
		t.Fatal("hop range collision accepted")
	}
	spec.HopPorts = "31000-31001"
	if e = reserveLivePorts(r, tx, hostID, id, spec); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM live_ports WHERE tenant_id=$1 AND service_id=$2 AND port BETWEEN 30000 AND 30100", tenant, id).Scan(&count); e != nil || count != 0 {
		t.Fatal("obsolete reservation survived", count, e)
	}
	if _, e = tx.Exec(r.Context(), "UPDATE hosts SET platform='windows/amd64' WHERE tenant_id=$1 AND id=$2", tenant, hostID); e != nil {
		t.Fatal(e)
	}
	if requireHopping(r, tx, hostID, spec) == nil {
		t.Fatal("Windows server hopping accepted")
	}
}
