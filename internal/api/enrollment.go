package api

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed bootstrap/*
var bootstrapFiles embed.FS
var ticketPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var bootstrapDownloads = make(chan struct{}, 4)
var enrollmentLimit struct {
	sync.Mutex
	window time.Time
	count  int
}

type agentPackage struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (s *Server) release(platform string) (agentPackage, string, error) {
	var p agentPackage
	if platform != "linux-amd64" && platform != "linux-arm64" {
		return p, "", bad("supported platforms: linux-amd64, linux-arm64")
	}
	if !filepath.IsAbs(s.AgentReleaseDir) {
		return p, "", problem{503, "Agent installer is not configured on the control server"}
	}
	b, e := os.ReadFile(filepath.Join(s.AgentReleaseDir, "manifest.json"))
	if e != nil {
		return p, "", problem{503, "Agent release manifest unavailable"}
	}
	packages := map[string]agentPackage{}
	if json.Unmarshal(b, &packages) != nil {
		return p, "", problem{503, "invalid Agent release manifest"}
	}
	p, ok := packages[platform]
	if !ok || !ticketPattern.MatchString(p.SHA256) || p.Size < 1 || p.Size > 160<<20 {
		return p, "", problem{503, "verified Agent package unavailable"}
	}
	path := filepath.Join(s.AgentReleaseDir, platform+".zip")
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() != p.Size {
		return p, "", problem{503, "Agent package unavailable"}
	}
	return p, path, nil
}
func (s *Server) agentRelease(w http.ResponseWriter, r *http.Request) error {
	p, path, e := s.release(r.PathValue("platform"))
	if e != nil {
		return e
	}
	if r.URL.Query().Get("download") != "1" {
		send(w, 200, p)
		return nil
	}
	select {
	case bootstrapDownloads <- struct{}{}:
		defer func() { <-bootstrapDownloads }()
	default:
		return problem{429, "installer downloads busy; retry shortly"}
	}
	w.Header().Set("Content-Type", "application/zip")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Minute))
	http.ServeFile(w, r, path)
	return nil
}
func (s *Server) bootstrapScript(w http.ResponseWriter, r *http.Request) {
	name := "install.sh"
	if strings.HasSuffix(r.URL.Path, ".py") {
		name = "install.py"
	}
	b, _ := bootstrapFiles.ReadFile("bootstrap/" + name)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(b)
}
func (s *Server) enrollmentCreate(w http.ResponseWriter, r *http.Request) error {
	var active string
	if e := s.DB.QueryRow(r.Context(), `SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state=''`, tenant, r.PathValue("id")).Scan(&active); e != nil {
		return e
	}
	origin, e := s.managementOrigin(r)
	if e != nil {
		return e
	}
	var options struct {
		AllowHTTP bool `json:"allow_insecure_http"`
	}
	if e = decode(w, r, &options); e != nil {
		return e
	}
	insecure := !strings.HasPrefix(origin, "https://")
	if insecure && !options.AllowHTTP {
		return bad("HTTP 入口没有传输加密；请显式确认允许 HTTP 接入，或配置 HTTPS")
	}

	if _, _, e = s.release("linux-amd64"); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var host string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state='' FOR UPDATE", tenant, r.PathValue("id")).Scan(&host); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE agent_enrollments SET revoked_at=now() WHERE tenant_id=$1 AND host_id=$2 AND consumed_at IS NULL AND revoked_at IS NULL", tenant, host); e != nil {
		return e
	}
	ticket := ID() + ID()
	expires := time.Now().Add(15 * time.Minute)
	if _, e = tx.Exec(ctx, "INSERT INTO agent_enrollments(tenant_id,host_id,token_hash,expires_at) VALUES($1,$2,$3,$4)", tenant, host, Hash(ticket), expires); e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "agent.enrollment.create", host); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	// Origin is a validated HTTPS DNS origin; token alphabet is fixed. No user-supplied shell is accepted.
	transport := "https"
	if insecure {
		transport = "http,https"
	}
	command := "(f=$(mktemp) && curl --proto '=" + transport + "' --tlsv1.2 -fsS '" + origin + "/api/v1/agent/install.sh' -o \"$f\" && sh \"$f\" '" + origin + "' '" + ticket + "'" + func() string {
		if insecure {
			return " --allow-insecure-http"
		}
		return ""
	}() + "; rc=$?; rm -f \"$f\"; exit $rc)"
	send(w, 201, map[string]any{"command": command, "expires_at": expires, "state": "waiting", "platforms": []string{"linux-amd64", "linux-arm64"}})
	return nil
}
func (s *Server) enrollmentClaim(w http.ResponseWriter, r *http.Request) error {
	enrollmentLimit.Lock()
	if time.Since(enrollmentLimit.window) > time.Second {
		enrollmentLimit.window = time.Now()
		enrollmentLimit.count = 0
	}
	enrollmentLimit.count++
	limited := enrollmentLimit.count > 30
	enrollmentLimit.Unlock()
	if limited {
		w.Header().Set("Retry-After", "1")
		return problem{429, "registration rate limited"}
	}
	var b struct {
		Ticket string `json:"ticket"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if !ticketPattern.MatchString(b.Ticket) {
		return problem{401, "invalid or expired enrollment ticket"}
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var host string
	// Same host lock order as ticket generation prevents claim/regeneration races.
	e = tx.QueryRow(ctx, "SELECT host_id FROM agent_enrollments WHERE token_hash=$1 AND tenant_id=$2", Hash(b.Ticket), tenant).Scan(&host)
	if e == pgx.ErrNoRows {
		return problem{401, "invalid or expired enrollment ticket"}
	}
	if e != nil {
		return e
	}
	var locked string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state='' FOR UPDATE", tenant, host).Scan(&locked); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "UPDATE agent_enrollments SET consumed_at=now() WHERE token_hash=$1 AND tenant_id=$2 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>now()", Hash(b.Ticket), tenant)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return problem{401, "invalid or expired enrollment ticket"}
	}
	token := ID() + ID()
	if _, e = tx.Exec(ctx, "UPDATE hosts SET agent_hash=$3,last_seen=NULL WHERE tenant_id=$1 AND id=$2", tenant, host, Hash(token)); e != nil {
		return e
	}
	if e = audit(ctx, tx, "agent", "agent.enrollment.claim", host); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]string{"host_id": host, "agent_token": token})
	return nil
}
func (s *Server) enrollmentStatus(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT json_build_object('state',CASE WHEN e.consumed_at IS NULL AND e.revoked_at IS NULL AND e.expires_at>now() THEN 'waiting' WHEN h.last_seen>now()-interval '90 seconds' AND (e.consumed_at IS NULL OR h.last_seen>=e.consumed_at) THEN 'connected' WHEN e.revoked_at IS NOT NULL THEN 'revoked' WHEN e.consumed_at IS NOT NULL THEN 'installed' WHEN e.expires_at<=now() THEN 'expired' ELSE 'waiting' END,'expires_at',e.expires_at,'last_seen',h.last_seen) FROM hosts h LEFT JOIN LATERAL (SELECT * FROM agent_enrollments WHERE tenant_id=h.tenant_id AND host_id=h.id ORDER BY created_at DESC LIMIT 1) e ON true WHERE h.tenant_id=$1 AND h.id=$2`, tenant, r.PathValue("id")).Scan(&raw)
	if e != nil {
		return e
	}
	send(w, 200, raw)
	return nil
}
func (s *Server) enrollmentRevoke(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state='' FOR UPDATE", tenant, r.PathValue("id")).Scan(&id); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE agent_enrollments SET revoked_at=now() WHERE tenant_id=$1 AND host_id=$2 AND consumed_at IS NULL AND revoked_at IS NULL", tenant, id)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "agent.enrollment.revoke", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"revoked": true})
	return nil
}

// Verify packages when publishing them; exposed here for release tooling tests.
func packageDigest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
