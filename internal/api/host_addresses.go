package api

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"slices"

	"github.com/jackc/pgx/v5"
	"vps-control/internal/live"
)

func hostAddressMode(address, mode string) (string, error) {
	if mode == "" {
		mode = "manual"
		if _, err := netip.ParseAddr(address); err == nil || address == "outbound-agent" {
			mode = "auto"
		}
	}
	if mode == "manual" && address == "outbound-agent" {
		return "", bad("固定手动地址不能为空")
	}
	if mode != "auto" && mode != "manual" {
		return "", bad("主机地址模式只能为 auto 或 manual")
	}
	return mode, nil
}

func usableHostIP(value string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(value)
	a = a.Unmap()
	return a, err == nil && a.Zone() == "" && !a.IsUnspecified() && !a.IsMulticast() && !a.IsLoopback() && !a.IsLinkLocalUnicast()
}

// Keep an existing address while it is still reported. Never replace a public
// address with a Docker/private address after an incomplete interface report.
func reportedPrimary(old string, addresses []string) string {
	if slices.Contains(addresses, old) {
		return old
	}
	previous, hadIP := usableHostIP(old)
	best, rank := old, 0
	for _, value := range addresses {
		a, ok := usableHostIP(value)
		if !ok || hadIP && (previous.Is4() != a.Is4() || !previous.IsPrivate() && a.IsPrivate()) {
			continue
		}
		r := 1
		if a.Is4() {
			r = 2
		}
		if !a.IsPrivate() {
			r += 2
		}
		if r > rank {
			best, rank = a.String(), r
		}
	}
	return best
}

// Only published interface addresses and the exact previous inventory IP follow
// the host. Custom domains and independent/manual connection addresses stay put.
func reconcileHostSpec(spec *live.Spec, old, current string, addresses []string) bool {
	if !live.Proxy(spec.Kind) {
		return false
	}
	before, _ := json.Marshal([]any{spec.Address, spec.EndpointAddresses})
	known := append(slices.Clone(addresses), current)
	for i, value := range spec.EndpointAddresses {
		a, ok := usableHostIP(value)
		if b, valid := usableHostIP(current); ok && valid && value == old && current != old && a.Is4() == b.Is4() && a.IsPrivate() == b.IsPrivate() {
			spec.EndpointAddresses[i] = current
			continue
		}
		if slices.Contains(known, value) {
			continue
		}
		if !ok {
			continue
		}
		candidates := []string{}
		for _, candidate := range known {
			b, valid := usableHostIP(candidate)
			if valid && a.Is4() == b.Is4() && a.IsPrivate() == b.IsPrivate() && !slices.Contains(candidates, b.String()) {
				candidates = append(candidates, b.String())
			}
		}
		// Multiple public addresses are ambiguous; let the administrator choose.
		if len(candidates) == 1 {
			spec.EndpointAddresses[i] = candidates[0]
		}
	}
	if len(spec.EndpointAddresses) > 0 {
		spec.Address = spec.EndpointAddresses[0]
	} else if a, ok := usableHostIP(old); ok && spec.Address == old && current != old {
		if b, valid := usableHostIP(current); !valid || a.Is4() != b.Is4() || a.IsPrivate() != b.IsPrivate() {
			return false
		}
		spec.Address = current
	}
	after, _ := json.Marshal([]any{spec.Address, spec.EndpointAddresses})
	return string(before) != string(after)
}

// Call under graphLock, in the same transaction as the host address change.
func (s *Server) syncHostAddresses(r *http.Request, tx pgx.Tx, host, old, current string, addresses []string) error {
	rows, err := tx.Query(r.Context(), `SELECT id,sealed FROM live_services WHERE tenant_id=$1 AND host_id=$2 AND delete_requested_at IS NULL AND kind NOT IN ('frps','frpc') ORDER BY id FOR UPDATE`, tenant, host)
	if err != nil {
		return err
	}
	type update struct {
		id, address string
		sealed      []byte
	}
	updates := []update{}
	for rows.Next() {
		var id string
		var sealed []byte
		if err = rows.Scan(&id, &sealed); err != nil {
			rows.Close()
			return err
		}
		raw, e := live.Open(s.MasterKey, sealed)
		if e != nil {
			rows.Close()
			return e
		}
		var spec live.Spec
		if e = json.Unmarshal(raw, &spec); e != nil {
			rows.Close()
			return e
		}
		if !reconcileHostSpec(&spec, old, current, addresses) {
			continue
		}
		raw, e = json.Marshal(spec)
		if e == nil {
			sealed, e = live.Seal(s.MasterKey, raw)
		}
		if e != nil {
			rows.Close()
			return e
		}
		updates = append(updates, update{id, spec.Address, sealed})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err = tx.Exec(r.Context(), `UPDATE live_services SET address=$3,sealed=$4,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2`, tenant, u.id, u.address, u.sealed); err != nil {
			return err
		}
		if err = s.bumpParents(r, tx, u.id); err != nil {
			return err
		}
		if err = audit(r.Context(), tx, "control", "live.address-sync", u.id); err != nil {
			return err
		}
	}
	return nil
}
