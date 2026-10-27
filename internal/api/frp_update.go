package api

import (
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/jackc/pgx/v5"
	"vps-control/internal/live"
)

// FRP listener reservations are changed only after a confirmed stop. A server with
// assigned clients retains its immutable connection contract until clients are moved.
func (s *Server) updateFRP(w http.ResponseWriter, r *http.Request, tx pgx.Tx, b liveInput, raw []byte, enabled bool) error {
	var spec live.Spec
	if e := json.Unmarshal(raw, &spec); e != nil {
		return e
	}
	id := r.PathValue("id")
	var rv int
	var state string
	if e := tx.QueryRow(r.Context(), `SELECT reported_version,state FROM live_services WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&rv, &state); e != nil {
		return e
	}
	if enabled || !(rv == 0 && b.Version == 1 || rv == b.Version && state == "stopped") {
		return problem{409, "请先停用 FRP 并等待 Agent 确认停止后再修改"}
	}
	var parent string
	if e := tx.QueryRow(r.Context(), `SELECT COALESCE(parent_id,'') FROM live_services WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&parent); e != nil {
		return e
	}
	if spec.Kind == "frps" {
		var used bool
		if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND parent_id=$2)`, tenant, id).Scan(&used); e != nil {
			return e
		}
		if used && (spec.Address != b.Address || spec.Port != b.Port || spec.Listen != b.Listen || !reflect.DeepEqual(spec.Tunnels, b.Tunnels) || b.Password != "" || b.RotateCredentials) {
			return problem{409, "此 FRP 服务端已有客户端，请先移动或删除客户端，再修改连接参数"}
		}
		if spec.Address != b.Address {
			cert, key, e := live.Certificate(b.Address)
			if e != nil {
				return bad(e.Error())
			}
			spec.Certificate = cert
			spec.PrivateKey = key
		}
		spec.Address = b.Address
		spec.Port = b.Port
		spec.Listen = b.Listen
		spec.Tunnels = b.Tunnels
		if b.Password != "" {
			spec.Password = b.Password
		}
		if b.RotateCredentials {
			spec.Password = live.Secret()
		}
	} else {
		parent = b.ParentID
		var sealed []byte
		if e := tx.QueryRow(r.Context(), `SELECT sealed FROM live_services WHERE tenant_id=$1 AND id=$2 AND kind='frps' AND delete_requested_at IS NULL`, tenant, parent).Scan(&sealed); e != nil {
			return e
		}
		plain, e := live.Open(s.MasterKey, sealed)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(plain, &spec); e != nil {
			return e
		}
		spec.Kind = "frpc"
		spec.PrivateKey = ""
		spec.Listen = "127.0.0.1"
		if len(b.Tunnels) > 0 {
			allowed := map[int]bool{}
			for _, t := range spec.Tunnels {
				allowed[t.RemotePort] = true
			}
			for _, t := range b.Tunnels {
				if !allowed[t.RemotePort] {
					return bad("client port must be reserved by parent server")
				}
			}
			spec.Tunnels = b.Tunnels
		}
	}
	if e := spec.Validate(); e != nil {
		return bad(e.Error())
	}
	if _, e := tx.Exec(r.Context(), `DELETE FROM live_ports WHERE tenant_id=$1 AND service_id=$2`, tenant, id); e != nil {
		return e
	}
	if _, e := tx.Exec(r.Context(), `DELETE FROM frp_assignments WHERE tenant_id=$1 AND client_id=$2`, tenant, id); e != nil {
		return e
	}
	if spec.Kind == "frps" {
		ports := []int{spec.Port}
		for _, t := range spec.Tunnels {
			ports = append(ports, t.RemotePort)
		}
		for _, port := range ports {
			var reserved bool
			if e := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM port_reservations WHERE tenant_id=$1 AND host_id=$2 AND port=$3)`, tenant, b.HostID, port).Scan(&reserved); e != nil {
				return e
			}
			if reserved {
				return problem{409, "port reserved by inventory"}
			}
			if _, e := tx.Exec(r.Context(), `INSERT INTO live_ports VALUES($1,$2,$3,$4)`, tenant, b.HostID, port, id); e != nil {
				return e
			}
		}
	} else {
		for _, t := range spec.Tunnels {
			if _, e := tx.Exec(r.Context(), `INSERT INTO frp_assignments VALUES($1,$2,$3,$4)`, tenant, parent, t.RemotePort, id); e != nil {
				return e
			}
		}
	}
	raw, _ = json.Marshal(spec)
	sealed, e := live.Seal(s.MasterKey, raw)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET name=$3,address=$4,port=$5,parent_id=NULLIF($6,''),sealed=$7,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2`, tenant, id, b.Name, spec.Address, spec.Port, parent, sealed); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "frp.update", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
