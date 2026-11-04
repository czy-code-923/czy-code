package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) trafficDisplayUnit(ctx context.Context) (string, error) {
	var unit string
	err := s.DB.QueryRow(ctx, `SELECT traffic_display_unit FROM tenants WHERE id=$1`, tenant).Scan(&unit)
	return unit, err
}

func (s *Server) displaySettings(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		unit, err := s.trafficDisplayUnit(r.Context())
		if err != nil {
			return err
		}
		send(w, 200, map[string]string{"traffic_unit": unit})
		return nil
	}
	var in struct {
		Unit string `json:"traffic_unit"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Unit != "GB" && in.Unit != "GiB" && in.Unit != "auto" {
		return bad("traffic_unit must be GB, GiB or auto")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `UPDATE tenants SET traffic_display_unit=$2 WHERE id=$1`, tenant, in.Unit); err != nil {
		return err
	}
	if err = audit(r.Context(), tx, "admin", "display.traffic-unit", in.Unit); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]string{"traffic_unit": in.Unit})
	return nil
}

func trafficBytes(n int64, unit string) string {
	size := float64(1e9)
	if unit == "GiB" {
		size = 1 << 30
	}
	if unit == "auto" {
		size = 1
		for _, item := range []struct {
			name string
			size float64
		}{{"PB", 1e15}, {"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}} {
			if float64(n) >= item.size {
				return fmt.Sprintf("%.2f %s", float64(n)/item.size, item.name)
			}
		}
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n) / size
	if n > 0 && v < .01 {
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 9, 64), "0"), ".") + " " + unit
	}
	return fmt.Sprintf("%.2f %s", v, unit)
}

func (s *Server) trafficSummary(ctx context.Context, command string) (string, error) {
	unit, err := s.trafficDisplayUnit(ctx)
	if err != nil {
		return "", err
	}
	query := `SELECT name,used_bytes,quota_bytes,multiplier_milli,billing_mode FROM proxy_users WHERE tenant_id=$1 ORDER BY name LIMIT 30`
	if command == "hosts" {
		query = `SELECT h.name,COALESCE(t.used_bytes,0),COALESCE(t.quota_bytes,0),COALESCE(t.display_unit,'GiB'),COALESCE(h.last_seen>now()-interval '90 seconds',false),COALESCE(t.last_sample_at>now()-interval '35 seconds',false) FROM hosts h LEFT JOIN host_traffic t ON(t.tenant_id,t.host_id)=(h.tenant_id,h.id) WHERE h.tenant_id=$1 AND h.deleted_at IS NULL ORDER BY h.name LIMIT 30`
	}
	rows, err := s.DB.Query(ctx, query, tenant)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	lines := []string{"星枢 · " + command + "（最多 30 条）"}
	for rows.Next() {
		var name, mode string
		var used, quota int64
		var milli int
		var online, fresh bool
		if command == "hosts" {
			if err = rows.Scan(&name, &used, &quota, &mode, &online, &fresh); err != nil {
				return "", err
			}
			state, sample := "离线/未接入", "断报/待采样"
			if online {
				state = "在线"
			}
			if fresh {
				sample = "最新采样"
			}
			remaining := "不限额"
			if quota > 0 {
				remaining = trafficBytes(max(0, quota-used), mode)
			}
			lines = append(lines, fmt.Sprintf("%s · %s · 套餐已用 %s · 剩余 %s · %s", name, state, trafficBytes(used, mode), remaining, sample))
		} else {
			if err = rows.Scan(&name, &used, &quota, &milli, &mode); err != nil {
				return "", err
			}
			limit := "不限额"
			if quota > 0 {
				limit = trafficBytes(quota, unit)
			}
			lines = append(lines, fmt.Sprintf("%s · %s / %s · 倍率 %gx · %s", name, trafficBytes(used, unit), limit, float64(milli)/1000, mode))
		}
	}
	return strings.Join(lines, "\n"), rows.Err()
}
