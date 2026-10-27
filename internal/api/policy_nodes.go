package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/clientpolicy"
	"vps-control/internal/live"
)

func (s *Server) policyNodes(w http.ResponseWriter, r *http.Request) error {
	rows, e := s.DB.Query(r.Context(), `SELECT l.id,l.name,l.host_id,l.kind,l.engine,l.address,l.enabled,l.state,l.sealed,(l.enabled AND l.state='running' AND l.version=l.reported_version AND l.reported_at>now()-interval '45 seconds') FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.delete_requested_at IS NULL AND h.retire_state='' AND h.deleted_at IS NULL AND l.kind NOT IN ('frpc','frps') AND ($2='' OR EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=$2)) ORDER BY l.created_at LIMIT 200`, tenant, r.URL.Query().Get("user_id"))
	if e != nil {
		return e
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, host, kind, engine, address, state string
		var enabled bool
		var online *bool
		var sealed []byte
		if e = rows.Scan(&id, &name, &host, &kind, &engine, &address, &enabled, &state, &sealed, &online); e != nil {
			return e
		}
		raw, err := live.Open(s.MasterKey, sealed)
		if err != nil {
			return err
		}
		var spec live.Spec
		if e = json.Unmarshal(raw, &spec); e != nil {
			return e
		}
		addresses := spec.EndpointAddresses
		if len(addresses) == 0 {
			addresses = []string{address}
		}
		for _, a := range addresses {
			family := clientpolicy.AddressFamily(a)
			suffix := map[string]string{"ipv4": "v4", "ipv6": "v6", "dns": "域名"}[family]
			result = append(result, map[string]any{"id": id, "name": name + "-" + suffix, "node_name": name, "kind": kind, "engine": engine, "host_id": host, "address": a, "family": family, "reference": clientpolicy.EndpointReference(id, family), "state": state, "enabled": enabled, "online": online})
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	send(w, 200, result)
	return nil
}

func prunePolicyNodes(r *http.Request, tx pgx.Tx, ids map[string]bool) error {
	rows, e := tx.Query(r.Context(), `SELECT id,source,bindings FROM client_policies WHERE tenant_id=$1 FOR UPDATE`, tenant)
	if e != nil {
		return e
	}
	type item struct {
		id, source string
		bindings   []byte
	}
	items := []item{}
	for rows.Next() {
		var v item
		if e = rows.Scan(&v.id, &v.source, &v.bindings); e != nil {
			rows.Close()
			return e
		}
		items = append(items, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, v := range items {
		p, err := clientpolicy.Parse(v.source)
		if err != nil {
			return err
		}
		bindings := map[string]string{}
		if err = json.Unmarshal(v.bindings, &bindings); err != nil {
			return err
		}
		if !p.RemoveNodes(ids, bindings) {
			continue
		}
		source, err := marshalPolicyYAML(p)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(bindings)
		if _, err = tx.Exec(r.Context(), `UPDATE client_policies SET source=$3,bindings=$4,version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, v.id, source, raw); err != nil {
			return err
		}
	}
	return nil
}
