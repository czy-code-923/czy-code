package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"vps-control/internal/engine"
	"vps-control/internal/manifest"
)

func (s *Server) resolveOfficialRelease(ctx context.Context, name, version string) (engine.Release, error) {
	if s.resolveEngineRelease != nil {
		return s.resolveEngineRelease(ctx, name, version)
	}
	return engine.Resolve(ctx, name, version)
}

func (s *Server) engineRelease(w http.ResponseWriter, r *http.Request) error {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = "latest"
	}
	name := r.URL.Query().Get("engine")
	if engine.Repository(name) == "" || !engine.ValidVersion(version) {
		return bad("unsupported engine or version")
	}
	release, e := s.resolveOfficialRelease(r.Context(), name, version)
	if e != nil {
		return problem{502, e.Error()}
	}
	send(w, 200, map[string]any{"engine": name, "version": release.Tag, "name": release.Name, "published_at": release.Published, "prerelease": release.Prerelease, "official": true})
	return nil
}
func engineUpdateCapable(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major > 0 || minor >= 17
}
func (s *Server) engineUpdates(w http.ResponseWriter, r *http.Request) error {
	host := r.PathValue("id")
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,engine,version,state,error,created_at,updated_at FROM engine_updates WHERE tenant_id=$1 AND host_id=$2 ORDER BY created_at DESC LIMIT 30) x`, tenant, host).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	var body struct {
		Engine  string `json:"engine"`
		Version string `json:"version"`
	}
	if e := decode(w, r, &body); e != nil {
		return e
	}
	if body.Version == "" {
		body.Version = "latest"
	}
	if engine.Repository(body.Engine) == "" || !engine.ValidVersion(body.Version) {
		return bad("请选择 sing-box / Xray 与有效版本号")
	}
	var agent, platform string
	var online bool
	e := s.DB.QueryRow(r.Context(), `SELECT agent_version,platform,COALESCE(last_seen>now()-interval '45 seconds',false) FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state=''`, tenant, host).Scan(&agent, &platform, &online)
	if e != nil {
		return e
	}
	if !engineUpdateCapable(agent) {
		return problem{409, "请先升级此主机 Agent 至 0.17 或更新版本"}
	}
	if !online {
		return problem{409, "主机离线，暂不能切换内核"}
	}
	release, e := s.resolveOfficialRelease(r.Context(), body.Engine, body.Version)
	if e != nil {
		return problem{502, e.Error()}
	}
	if _, e = release.Asset(body.Engine, platform); e != nil {
		return bad(e.Error())
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var permitted bool
	if e = tx.QueryRow(r.Context(), `SELECT retire_state='' AND deleted_at IS NULL FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, host).Scan(&permitted); e != nil {
		return e
	}
	if !permitted {
		return problem{409, "host is retiring"}
	}
	var active bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM engine_updates WHERE tenant_id=$1 AND host_id=$2 AND state IN ('queued','downloading','validating'))`, tenant, host).Scan(&active); e != nil {
		return e
	}
	if active {
		return problem{409, "此主机已有内核更新任务，请等待完成"}
	}
	id := ID()
	if _, e = tx.Exec(r.Context(), `INSERT INTO engine_updates(tenant_id,id,host_id,engine,version) VALUES($1,$2,$3,$4,$5)`, tenant, id, host, body.Engine, release.Tag); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "engine.update-request", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 202, map[string]any{"id": id, "version": release.Tag, "state": "queued"})
	return nil
}
func (s *Server) engineSync(w http.ResponseWriter, r *http.Request, host string) error {
	var body struct {
		Reports []engine.Report `json:"reports"`
	}
	if e := decode(w, r, &body); e != nil {
		return e
	}
	if len(body.Reports) > 20 {
		return bad("too many engine reports")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var retiring bool
	if e = tx.QueryRow(r.Context(), `SELECT retire_state<>'' OR deleted_at IS NOT NULL FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, host).Scan(&retiring); e != nil {
		return e
	}
	if retiring {
		send(w, 200, []any{})
		return nil
	}
	for _, v := range body.Reports {
		if !manifest.ValidID(v.ID) || engine.Repository(v.Engine) == "" || !engine.ValidVersion(v.Version) || !strings.Contains("|downloading|validating|succeeded|failed|", "|"+v.State+"|") {
			return bad("invalid engine report")
		}
		message := ""
		if v.State == "failed" {
			message = "Agent 未能验证或启用此版本，原内核已保留或回退；请检查网络、版本兼容性与私有日志"
		}
		if _, e = tx.Exec(r.Context(), `UPDATE engine_updates SET state=$5,error=$6,updated_at=now() WHERE tenant_id=$1 AND host_id=$2 AND id=$3 AND engine=$4 AND version=$7 AND state NOT IN ('succeeded','failed')`, tenant, host, v.ID, v.Engine, v.State, message, v.Version); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(r.Context(), `UPDATE engine_updates SET state='failed',error='更新超时或 Agent 中断；当前已确认运行的版本保留' WHERE tenant_id=$1 AND host_id=$2 AND state IN ('downloading','validating') AND updated_at<now()-interval '15 minutes'`, tenant, host); e != nil {
		return e
	}
	rows, e := tx.Query(r.Context(), `SELECT id,engine,version FROM engine_updates WHERE tenant_id=$1 AND host_id=$2 AND state IN ('queued','downloading','validating') ORDER BY created_at LIMIT 1`, tenant, host)
	if e != nil {
		return e
	}
	out := []engine.Request{}
	for rows.Next() {
		var v engine.Request
		if e = rows.Scan(&v.ID, &v.Engine, &v.Version); e != nil {
			rows.Close()
			return e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, out)
	return nil
}
