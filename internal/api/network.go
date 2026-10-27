package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/netip"
	"slices"
	"vps-control/internal/live"
)

func (s *Server) applyNetwork(r *http.Request, tx pgx.Tx, b liveInput, spec *live.Spec) error {
	if b.Kind == "frpc" {
		return nil
	}
	var address, version string
	var raw []byte
	var retiring string
	if e := tx.QueryRow(r.Context(), `SELECT address,reported_addresses,agent_version,retire_state FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, b.HostID).Scan(&address, &raw, &version, &retiring); e != nil {
		return e
	}
	if retiring != "" {
		return problem{409, "此主机正在卸载，不能修改部署"}
	}
	ips := []string{}
	_ = json.Unmarshal(raw, &ips)
	ips = append([]string{address}, ips...)
	known := map[string]bool{}
	families := map[string]bool{}
	for _, ip := range ips {
		if a, e := netip.ParseAddr(ip); e == nil && a.Zone() == "" && !a.IsLoopback() && !a.IsUnspecified() && !a.IsLinkLocalUnicast() {
			known[a.String()] = true
			if a.Is4() {
				families["ipv4"] = true
			} else {
				families["ipv6"] = true
			}
		}
	}
	if spec.Address == "" {
		for _, ip := range ips {
			if known[ip] {
				spec.Address = ip
				break
			}
		}
		if spec.Address == "" {
			return bad("主机尚未回报 IP，请手动填写连接地址")
		}
	}
	if b.EndpointAddresses != nil {
		spec.EndpointAddresses = *b.EndpointAddresses
		for _, ip := range spec.EndpointAddresses {
			if !known[ip] {
				return bad("所选发布 IP 不属于此主机，请刷新地址或使用手动连接地址")
			}
		}
		if len(spec.EndpointAddresses) > 0 {
			spec.Address = spec.EndpointAddresses[0]
		}
	}
	if b.EgressFamilies != nil {
		spec.EgressFamilies = *b.EgressFamilies
	}
	if len(spec.EgressFamilies) > 0 {
		if !live.NetworkCapable(version) {
			return problem{409, "出站 IP 策略需要 Agent 0.12.0，请先升级主机 Agent"}
		}
		for _, f := range spec.EgressFamilies {
			if !families[f] || !slices.Contains([]string{"ipv4", "ipv6"}, f) {
				return bad("主机未回报所选 IP 类型；无 IPv6 时不能选择 IPv6 出站")
			}
		}
	}
	return nil
}
