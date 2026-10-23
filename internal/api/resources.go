package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Mutable inventory labels are separate from immutable engine configuration versions.
func (s *Server) updateResource(w http.ResponseWriter, r *http.Request) error {
	kind := r.PathValue("kind")
	tables := map[string]string{"hosts": "hosts", "nodes": "nodes", "instances": "instances", "tunnels": "tunnels", "routes": "route_plans"}
	table, ok := tables[kind]
	if !ok {
		return problem{404, "unknown editable collection"}
	}
	var raw map[string]json.RawMessage
	if e := decode(w, r, &raw); e != nil {
		return e
	}
	allowed := "name"
	if kind == "hosts" {
		allowed += " provider address"
	}
	if kind == "nodes" {
		allowed += " engine"
	}
	if len(raw) == 0 {
		return bad("empty update")
	}
	updates := map[string]string{}
	for key, value := range raw {
		if !allowedField(allowed, key) {
			return bad("topology fields are immutable; create a new resource or config version")
		}
		var v string
		if e := json.Unmarshal(value, &v); e != nil {
			return bad("update values must be strings")
		}
		v = strings.TrimSpace(v)
		if v == "" || len([]rune(v)) > 255 {
			return bad("invalid update value")
		}
		if key == "name" && len([]rune(v)) > 100 {
			return bad("name too long")
		}
		if key == "engine" && v != "sing-box" && v != "frp" && v != "xray" {
			return bad("unsupported engine")
		}
		updates[key] = v
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var locked string
	if e = tx.QueryRow(ctx, "SELECT id FROM "+table+" WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, r.PathValue("id")).Scan(&locked); e != nil {
		return e
	}
	if engine, ok := updates["engine"]; ok {
		var old string
		var count int
		if e = tx.QueryRow(ctx, "SELECT engine FROM nodes WHERE tenant_id=$1 AND id=$2", tenant, locked).Scan(&old); e != nil {
			return e
		}
		if engine != old {
			if e = tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM instances WHERE tenant_id=$1 AND node_id=$2)+(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND node_id=$2)", tenant, locked).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return problem{409, "engine is immutable while instances reference this node"}
			}
		}
	}
	for key, value := range updates {
		if _, e = tx.Exec(ctx, "UPDATE "+table+" SET "+key+"=$3 WHERE tenant_id=$1 AND id=$2", tenant, locked, value); e != nil {
			return e
		}
	}
	if e = audit(ctx, tx, "admin", "update."+kind, locked); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": locked})
	return nil
}
func (s *Server) deleteResource(w http.ResponseWriter, r *http.Request) error {
	kind := r.PathValue("kind")
	tables := map[string]string{"hosts": "hosts", "nodes": "nodes", "instances": "instances", "tunnels": "tunnels", "routes": "route_plans"}
	table, ok := tables[kind]
	if !ok {
		return problem{404, "unknown removable collection"}
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	// Tunnels lock their parent first, matching config generation and tunnel creation.
	if kind == "tunnels" {
		var parent string
		if e = tx.QueryRow(ctx, "SELECT i.id FROM instances i JOIN tunnels t ON (i.tenant_id,i.id)=(t.tenant_id,t.instance_id) WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF i", tenant, id).Scan(&parent); e != nil {
			return e
		}
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM configs WHERE tenant_id=$1 AND instance_id=$2", tenant, parent).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return problem{409, "tunnel ports are retained for immutable config history"}
		}
	}
	var locked string
	if e = tx.QueryRow(ctx, "SELECT id FROM "+table+" WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, id).Scan(&locked); e != nil {
		return e
	}
	switch kind {
	case "instances":
		var dependent bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM configs WHERE tenant_id=$1 AND instance_id=$2) OR EXISTS(SELECT 1 FROM tunnels WHERE tenant_id=$1 AND instance_id=$2) OR EXISTS(SELECT 1 FROM route_hops WHERE tenant_id=$1 AND instance_id=$2)", tenant, id).Scan(&dependent); e != nil {
			return e
		}
		if dependent {
			return problem{409, "instance has config history, tunnels or routes; cannot delete"}
		}
		if _, e = tx.Exec(ctx, "DELETE FROM port_reservations WHERE tenant_id=$1 AND instance_id=$2", tenant, id); e != nil {
			return e
		}
	case "tunnels":
		if _, e = tx.Exec(ctx, "DELETE FROM port_reservations WHERE tenant_id=$1 AND tunnel_id=$2", tenant, id); e != nil {
			return e
		}
	case "routes":
		if _, e = tx.Exec(ctx, "DELETE FROM route_hops WHERE tenant_id=$1 AND route_id=$2", tenant, id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 AND id=$2", tenant, id); e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "delete."+kind, id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
