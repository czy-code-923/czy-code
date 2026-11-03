package api

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"vps-control/internal/live"
)

func requireHopping(r *http.Request, tx pgx.Tx, host string, spec live.Spec) error {
	if !spec.RequiresHopping() {
		return nil
	}
	var version, platform string
	if err := tx.QueryRow(r.Context(), "SELECT agent_version,platform FROM hosts WHERE tenant_id=$1 AND id=$2", tenant, host).Scan(&version, &platform); err != nil {
		return err
	}
	if !live.HoppingCapable(version) || !strings.HasPrefix(platform, "linux/") {
		return problem{409, "端口跳跃需要 Linux Agent 0.23.14+；在连接主机中生成端口跳跃助手安装命令"}
	}
	return nil
}

func reserveLivePorts(r *http.Request, tx pgx.Tx, host, id string, spec live.Spec) error {
	ports := []int32{}
	for _, p := range spec.ReservedPorts() {
		ports = append(ports, int32(p))
	}
	var collision bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM port_reservations WHERE tenant_id=$1 AND host_id=$2 AND port=ANY($3::int[])) OR EXISTS(SELECT 1 FROM live_ports WHERE tenant_id=$1 AND host_id=$2 AND service_id<>$4 AND port=ANY($3::int[]))`, tenant, host, ports, id).Scan(&collision); err != nil {
		return err
	}
	if collision {
		return problem{409, "监听 / 跳跃端口与已有主机部署或库存端口冲突"}
	}
	if _, err := tx.Exec(r.Context(), "DELETE FROM live_ports WHERE tenant_id=$1 AND service_id=$2", tenant, id); err != nil {
		return err
	}
	_, err := tx.Exec(r.Context(), "INSERT INTO live_ports SELECT $1,$2,p,$4 FROM unnest($3::int[]) p", tenant, host, ports, id)
	return err
}
