package api

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/live"
	"vps-control/internal/manifest"
)

// A batch is one host/core deployment transaction. No partially saved protocols or port leaks.
func (s *Server) liveBatch(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		Items      []liveInput `json:"items"`
		StartPort  int         `json:"start_port"`
		Enabled    bool        `json:"enabled"`
		RequestKey string      `json:"request_key"`
	}
	if e := decodeBounded(w, r, &b, 512<<10); e != nil {
		return e
	}
	if len(b.Items) < 1 || len(b.Items) > 8 {
		return bad("select 1-8 proxy protocols")
	}
	if b.StartPort == 0 {
		b.StartPort = 20000
	}
	if b.StartPort < 1024 || b.StartPort > 65535 {
		return bad("invalid starting port")
	}
	if b.RequestKey == "" {
		b.RequestKey = ID()
	}
	if !manifest.ValidID(b.RequestKey) {
		return bad("request_key must be 32 hexadecimal characters")
	}
	raw, _ := json.Marshal(b)
	requestHash := Hash(string(raw))
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var previous json.RawMessage
	var previousHash string
	e = tx.QueryRow(r.Context(), `SELECT request_hash,result FROM deployment_batches WHERE tenant_id=$1 AND request_key=$2`, tenant, b.RequestKey).Scan(&previousHash, &previous)
	if e == nil {
		if previousHash != requestHash {
			return problem{409, "request_key already used for another batch"}
		}
		send(w, 201, previous)
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	host, engine := b.Items[0].HostID, b.Items[0].Engine
	if engine == "" {
		engine = "sing-box"
	}
	var version string
	if e = tx.QueryRow(r.Context(), `SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, host).Scan(&version); e != nil {
		return e
	}
	if !live.BundleCapable(version) {
		return problem{409, "upgrade host to bundled Agent 0.8+ before batch configuration"}
	}
	if b.Enabled {
		var ready bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM host_kernels WHERE tenant_id=$1 AND host_id=$2 AND engine=$3 AND available AND checked_at>now()-interval '45 seconds')`, tenant, host, engine).Scan(&ready); e != nil {
			return e
		}
		if !ready {
			return problem{409, "host kernel is unavailable or stale"}
		}
	}
	seen := map[string]bool{}
	created := []liveCreated{}
	for i, item := range b.Items {
		core := item.Engine
		if core == "" {
			core = "sing-box"
		}
		if item.HostID != host || core != engine || !live.Proxy(item.Kind) || seen[item.Kind] {
			return bad("batch requires unique proxy protocols on one host/core")
		}
		seen[item.Kind] = true
		if item.Port == 0 {
			if e = tx.QueryRow(r.Context(), `SELECT p FROM generate_series($3::int,65535) p WHERE NOT EXISTS(SELECT 1 FROM live_ports WHERE tenant_id=$1 AND host_id=$2 AND port=p) AND NOT EXISTS(SELECT 1 FROM port_reservations WHERE tenant_id=$1 AND host_id=$2 AND port=p) LIMIT 1`, tenant, host, b.StartPort).Scan(&item.Port); e != nil {
				return problem{409, "no available protocol port"}
			}
		}
		var result liveCreated
		if e = s.createLiveTx(r, tx, item, &result); e != nil {
			return fmt.Errorf("protocol %d (%s): %w", i+1, item.Kind, e)
		}
		if b.Enabled {
			if _, e = tx.Exec(r.Context(), `UPDATE live_services SET enabled=true,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2`, tenant, result.ID); e != nil {
				return e
			}
			result.Enabled = true
		}
		created = append(created, result)
	}
	result := map[string]any{"items": created, "request_key": b.RequestKey, "enabled": b.Enabled}
	raw, _ = json.Marshal(result)
	if _, e = tx.Exec(r.Context(), `INSERT INTO deployment_batches(tenant_id,request_key,request_hash,result) VALUES($1,$2,$3,$4)`, tenant, b.RequestKey, requestHash, raw); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "live.batch-create", b.RequestKey); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 201, result)
	return nil
}
