package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"vps-control/internal/live"
)

func requireFRPVersion(r *http.Request, tx pgx.Tx, host string, spec live.Spec) error {
	if !hasFRPUDP(spec) {
		return nil
	}
	var version string
	if e := tx.QueryRow(r.Context(), `SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2`, tenant, host).Scan(&version); e != nil {
		return e
	}
	if !engineUpdateCapable(version) {
		return problem{409, "FRP UDP requires Agent 0.17 or newer"}
	}
	return nil
}

func hasFRPUDP(spec live.Spec) bool {
	for _, tunnel := range spec.Tunnels {
		if tunnel.Type == "udp" {
			return true
		}
	}
	return false
}
