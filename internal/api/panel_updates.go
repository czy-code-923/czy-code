package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"vps-control/internal/buildinfo"
)

var panelVersion = regexp.MustCompile(`^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$`)
var updateStates = []string{"downloading", "verifying", "backing_up", "applying", "checking", "succeeded", "rolled_back", "failed"}

func (s *Server) panelUpdaterReady(r *http.Request) bool {
	if s.DB == nil {
		return false
	}
	var ready bool
	err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM panel_updater WHERE tenant_id=$1 AND last_seen>now()-interval '45 seconds')`, tenant).Scan(&ready)
	return err == nil && ready
}

func (s *Server) panelUpdates(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var rows json.RawMessage
		if err := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT id,from_version,target_version,state,detail,created_at,updated_at FROM panel_updates WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 20)x`, tenant).Scan(&rows); err != nil {
			return err
		}
		send(w, 200, map[string]any{"helper_ready": s.panelUpdaterReady(r), "current": buildinfo.Version, "items": rows})
		return nil
	}
	var b struct {
		Version string `json:"version"`
		Current string `json:"current"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !panelVersion.MatchString(b.Version) || b.Current != buildinfo.Version || !panelNewer(b.Version, buildinfo.Version) {
		return problem{409, "版本已变化或目标不是更新版本，请重新检查"}
	}
	if !s.panelUpdaterReady(r) {
		return problem{409, "本机更新服务未就绪，请先安装或检查更新服务"}
	}
	release, err := s.latestPanelRelease(r)
	if err != nil {
		return err
	}
	if release.Tag != "v"+b.Version+"-distribution" {
		return problem{409, "最新正式版已变化，请重新检查"}
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM panel_updates WHERE tenant_id=$1 AND state NOT IN ('succeeded','rolled_back','failed'))`, tenant).Scan(&active); err != nil {
		return err
	}
	if active {
		return problem{409, "已有更新任务，请等待其结束"}
	}
	id := ID()
	if _, err = tx.Exec(r.Context(), `INSERT INTO panel_updates(tenant_id,id,from_version,target_version) VALUES($1,$2,$3,$4)`, tenant, id, buildinfo.Version, b.Version); err != nil {
		return err
	}
	if err = audit(r.Context(), tx, "admin", "panel.update-request", id); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 202, map[string]any{"id": id, "state": "queued"})
	return nil
}

// These routes require the root token and a direct loopback peer. They cannot
// be delegated to API keys or called through the public web/reverse proxy.
func updaterLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback() && r.Header.Get("X-Real-IP") == "" && r.Header.Get("X-Forwarded-For") == ""
}
func (s *Server) panelWorker(w http.ResponseWriter, r *http.Request) error {
	if !updaterLoopback(r) {
		return problem{403, "local updater only"}
	}
	switch r.PathValue("action") {
	case "heartbeat":
		var b struct {
			Layout   string `json:"layout"`
			Platform string `json:"platform"`
		}
		if err := decode(w, r, &b); err != nil {
			return err
		}
		if !slices.Contains([]string{"legacy", "quickstart"}, b.Layout) || !slices.Contains([]string{"linux-amd64", "linux-arm64"}, b.Platform) {
			return bad("unsupported updater installation")
		}
		if _, err := s.DB.Exec(r.Context(), `INSERT INTO panel_updater(tenant_id,layout,platform) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET layout=excluded.layout,platform=excluded.platform,last_seen=now()`, tenant, b.Layout, b.Platform); err != nil {
			return err
		}
		send(w, 200, map[string]bool{"saved": true})
		return nil
	case "claim":
		var b struct{}
		if err := decode(w, r, &b); err != nil {
			return err
		}
		var raw json.RawMessage
		err := s.DB.QueryRow(r.Context(), `WITH candidate AS(SELECT id FROM panel_updates WHERE tenant_id=$1 AND state='queued' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE panel_updates p SET state='downloading',updated_at=now() FROM candidate c WHERE p.tenant_id=$1 AND p.id=c.id RETURNING jsonb_build_object('id',p.id,'from_version',p.from_version,'target_version',p.target_version)`, tenant).Scan(&raw)
		if err == pgx.ErrNoRows {
			send(w, 200, map[string]any{"job": nil})
			return nil
		}
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"job": raw})
		return nil
	case "report":
		var b struct {
			ID     string `json:"id"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		}
		if err := decode(w, r, &b); err != nil {
			return err
		}
		if len(b.ID) != 32 || !slices.Contains(updateStates, b.State) || len(b.Detail) > 300 || strings.ContainsAny(b.Detail, "\r\n") {
			return bad("invalid updater report")
		}
		tag, err := s.DB.Exec(r.Context(), `UPDATE panel_updates SET state=$3,detail=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND state NOT IN ('succeeded','rolled_back','failed')`, tenant, b.ID, b.State, b.Detail)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return problem{409, "update job is already terminal or missing"}
		}
		send(w, 200, map[string]bool{"saved": true})
		return nil
	}
	return problem{404, "unknown updater action"}
}
