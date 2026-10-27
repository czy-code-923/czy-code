package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"vps-control/internal/manifest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const tenant = "self" // Never accepted from client input. Replace with membership resolution before multi-tenant release.
type Server struct {
	DB              *pgxpool.Pool
	MasterKey       []byte
	AdminToken      string
	AgentReleaseDir string
	telegramClient  *http.Client
	keyRates        keyLimiter
}
type problem struct {
	status  int
	message string
}

func (p problem) Error() string { return p.message }
func bad(s string) error        { return problem{400, s} }
func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeBounded(w, r, v, 64<<10)
}
func decodeBounded(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("invalid JSON or unsupported fields")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad("one JSON object required")
	}
	return nil
}

type endpoint func(http.ResponseWriter, *http.Request) error

func wrap(f endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if e := f(w, r); e != nil {
			status, msg := 500, "internal error"
			var p problem
			var db *pgconn.PgError
			if errors.As(e, &p) {
				status, msg = p.status, p.message
			} else if errors.Is(e, pgx.ErrNoRows) {
				status, msg = 404, "resource not found"
			} else if errors.As(e, &db) && (db.Code == "23505" || db.Code == "23503" || db.Code == "23514") {
				status, msg = 409, "resource conflict or invalid reference"
			} else {
				slog.Error("request failed", "error", e)
			}
			if status == 429 && w.Header().Get("Retry-After") == "" {
				w.Header().Set("Retry-After", "60")
			}
			send(w, status, map[string]string{"error": msg})
		}
	}
}
func (s *Server) admin(f endpoint) http.HandlerFunc {
	return wrap(func(w http.ResponseWriter, r *http.Request) error {
		identity, e := s.authenticateAPI(r)
		if e != nil {
			return e
		}
		return f(w, r.WithContext(context.WithValue(r.Context(), identityContext{}, identity)))
	})
}
func (s *Server) agent(f func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc {
	return wrap(func(w http.ResponseWriter, r *http.Request) error {
		host := r.Header.Get("X-Host-ID")
		var hash string
		e := s.DB.QueryRow(r.Context(), "SELECT agent_hash FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL", tenant, host).Scan(&hash)
		if e == pgx.ErrNoRows && r.URL.Path == "/api/v1/agent/retirement/ack" {
			e = s.DB.QueryRow(r.Context(), `SELECT agent_hash FROM host_retirement_receipts WHERE tenant_id=$1 AND host_id=$2`, tenant, host).Scan(&hash)
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if e != nil || !strings.HasPrefix(header, "Bearer ") || !equal(Hash(token), hash) {
			return problem{401, "unauthorized agent"}
		}
		return f(w, r, host)
	})
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /api/v1/configuration/inspect", s.admin(s.inspectConfiguration))
	m.HandleFunc("GET /api/v1/notifications/telegram", s.admin(s.notifications))
	m.HandleFunc("PUT /api/v1/notifications/telegram", s.admin(s.notifications))
	m.HandleFunc("DELETE /api/v1/notifications/telegram", s.admin(s.notifications))
	m.HandleFunc("POST /api/v1/notifications/telegram/test", s.admin(s.notificationTest))
	m.HandleFunc("GET /api/v1/notifications/log", s.admin(s.notificationLog))
	m.HandleFunc("GET /api/v1/hosts/{id}/retirement", s.admin(s.retirement))
	m.HandleFunc("POST /api/v1/hosts/{id}/retirement", s.admin(s.retirement))
	m.HandleFunc("POST /api/v1/agent/retirement/{action}", s.agent(s.retirementAgent))
	m.HandleFunc("GET /api/v1/client-catalog", s.admin(s.clientCatalog))
	m.HandleFunc("GET /api/v1/client-policies", s.admin(s.policies))
	m.HandleFunc("POST /api/v1/client-policies", s.admin(s.policies))
	m.HandleFunc("PUT /api/v1/client-policies/{id}", s.admin(s.policies))
	m.HandleFunc("DELETE /api/v1/client-policies/{id}", s.admin(s.policies))
	m.HandleFunc("POST /api/v1/client-policies/parse", s.admin(s.parsePolicy))
	m.HandleFunc("GET /api/v1/client-policies/default", s.admin(s.defaultPolicy))
	m.HandleFunc("POST /api/v1/client-policies/import", s.admin(s.importPolicy))
	m.HandleFunc("POST /api/v1/client-policies/preview", s.admin(s.previewPolicy))
	m.HandleFunc("GET /api/v1/branding", wrap(s.branding))
	m.HandleFunc("GET /api/v1/subscriptions/{id}/{view}", s.admin(s.inspectSubscription))
	m.HandleFunc("GET /api/v1/session", s.admin(s.apiSession))
	m.HandleFunc("GET /api/v1/api-capabilities", s.admin(s.apiCapabilities))
	m.HandleFunc("GET /api/v1/openapi", s.admin(s.openapi))
	m.HandleFunc("GET /api/v1/api-keys", s.admin(s.keys))
	m.HandleFunc("POST /api/v1/api-keys", s.admin(s.keys))
	m.HandleFunc("PUT /api/v1/api-keys/{id}", s.admin(s.keys))
	m.HandleFunc("DELETE /api/v1/api-keys/{id}", s.admin(s.keys))
	m.HandleFunc("POST /api/v1/live/batch", s.admin(s.liveBatch))
	m.HandleFunc("GET /api/v1/maintenance/trials", s.admin(s.trials))
	m.HandleFunc("POST /api/v1/maintenance/trials", s.admin(s.trials))
	m.HandleFunc("GET /api/v1/dashboard", s.admin(s.dashboard))
	m.HandleFunc("GET /api/v1/access-records", s.admin(s.accessRecords))
	m.HandleFunc("POST /api/v1/proxy-users/{id}/reset-traffic", s.admin(s.resetTraffic))
	m.HandleFunc("GET /api/v1/agent/telemetry", s.agent(s.telemetry))
	m.HandleFunc("POST /api/v1/agent/telemetry/baseline", s.agent(s.telemetry))
	m.HandleFunc("POST /api/v1/agent/telemetry", s.agent(s.telemetry))
	m.HandleFunc("DELETE /api/v1/proxy-users/{id}", s.admin(s.removeManaged))
	m.HandleFunc("DELETE /api/v1/user-groups/{id}", s.admin(s.removeManaged))
	m.HandleFunc("DELETE /api/v1/client-profiles/{id}", s.admin(s.removeManaged))
	m.HandleFunc("DELETE /api/v1/subscriptions/{id}", s.admin(s.removeManaged))
	m.HandleFunc("DELETE /api/v1/live/{id}", s.admin(s.liveDelete))
	m.HandleFunc("PUT /api/v1/subscriptions/{id}", s.admin(s.subscriptionUpdate))
	m.HandleFunc("GET /api/v1/subscriptions/{id}/link", s.admin(s.subscriptionLink))
	m.HandleFunc("GET /api/v1/proxy-users", s.admin(s.users))
	m.HandleFunc("POST /api/v1/proxy-users", s.admin(s.users))
	m.HandleFunc("PUT /api/v1/proxy-users/{id}", s.admin(s.users))
	m.HandleFunc("GET /api/v1/user-groups", s.admin(s.groups))
	m.HandleFunc("POST /api/v1/user-groups", s.admin(s.groups))
	m.HandleFunc("PUT /api/v1/user-groups/{id}", s.admin(s.groups))
	m.HandleFunc("GET /api/v1/client-profiles", s.admin(s.clientProfiles))
	m.HandleFunc("POST /api/v1/client-profiles", s.admin(s.clientProfiles))
	m.HandleFunc("PUT /api/v1/client-profiles/{id}", s.admin(s.clientProfiles))
	m.HandleFunc("GET /api/v1/hosts/{id}/kernels", s.admin(s.kernels))
	m.HandleFunc("GET /api/v1/hosts/{id}/kernel-config", s.admin(s.kernelConfig))
	m.HandleFunc("GET /api/v1/engine-catalog", s.admin(s.engineCatalog))
	m.HandleFunc("GET /api/v1/protocol-templates", s.admin(s.protocolTemplates))
	m.HandleFunc("POST /api/v1/hosts/{id}/enrollment", s.admin(s.enrollmentCreate))
	m.HandleFunc("GET /api/v1/hosts/{id}/enrollment", s.admin(s.enrollmentStatus))
	m.HandleFunc("POST /api/v1/hosts/{id}/enrollment/revoke", s.admin(s.enrollmentRevoke))
	m.HandleFunc("POST /api/v1/agent/enroll", wrap(s.enrollmentClaim))
	m.HandleFunc("GET /api/v1/agent/install.sh", s.bootstrapScript)
	m.HandleFunc("GET /api/v1/agent/install.py", s.bootstrapScript)
	m.HandleFunc("GET /api/v1/agent/releases/{platform}", wrap(s.agentRelease))
	m.HandleFunc("GET /api/v1/site", s.admin(s.site))
	m.HandleFunc("PUT /api/v1/site", s.admin(s.site))
	m.HandleFunc("POST /api/v1/site/gateway-status", s.admin(s.gatewayStatus))
	m.HandleFunc("GET /api/v1/live/{id}", s.admin(s.liveDetail))
	m.HandleFunc("PUT /api/v1/live/{id}", s.admin(s.liveUpdate))
	m.HandleFunc("GET /api/v1/certificates", s.admin(s.certificates))
	m.HandleFunc("PUT /api/v1/certificates/{id}", s.admin(s.certificates))
	m.HandleFunc("POST /api/v1/certificates", s.admin(s.certificates))
	m.HandleFunc("POST /api/v1/certificates/{id}/retry", s.admin(s.certificateRetry))
	m.HandleFunc("POST /api/v1/gateway/work", s.admin(s.gatewayWork))
	m.HandleFunc("POST /api/v1/gateway/report", s.admin(s.gatewayReport))
	m.HandleFunc("GET /api/v1/reverse-proxies", s.admin(s.reverseProxies))
	m.HandleFunc("POST /api/v1/reverse-proxies", s.admin(s.reverseProxies))
	m.HandleFunc("PUT /api/v1/reverse-proxies/{id}", s.admin(s.reverseProxies))
	m.HandleFunc("GET /api/v1/live", s.admin(s.liveList))
	m.HandleFunc("POST /api/v1/live", s.admin(s.liveCreate))
	m.HandleFunc("POST /api/v1/live/{id}/toggle", s.admin(s.liveToggle))
	m.HandleFunc("GET /api/v1/live/{id}/tunnels", s.admin(s.liveTunnels))
	m.HandleFunc("GET /api/v1/subscriptions", s.admin(s.subscriptionList))
	m.HandleFunc("POST /api/v1/subscriptions", s.admin(s.subscriptionCreate))
	m.HandleFunc("PUT /api/v1/subscriptions/{id}/services", s.admin(s.subscriptionServices))
	m.HandleFunc("POST /api/v1/subscriptions/{id}/revoke", s.admin(s.subscriptionRevoke))
	m.HandleFunc("POST /api/v1/subscriptions/{id}/rotate", s.admin(s.subscriptionRotate))
	m.HandleFunc("POST /api/v1/agent/live/sync", s.agent(s.liveSync))
	m.HandleFunc("GET /healthz", wrap(func(w http.ResponseWriter, r *http.Request) error {
		ctx, c := context.WithTimeout(r.Context(), 2*time.Second)
		defer c()
		if e := s.DB.Ping(ctx); e != nil {
			return problem{503, "database unavailable"}
		}
		send(w, 200, map[string]string{"status": "ok", "mode": "control-only"})
		return nil
	}))
	m.HandleFunc("GET /api/v1/{kind}", s.admin(s.list))
	m.HandleFunc("POST /api/v1/{kind}", s.admin(s.create))
	m.HandleFunc("POST /api/v1/instances/{id}/configs", s.admin(s.config))
	m.HandleFunc("POST /api/v1/instances/{id}/deploy", s.admin(s.deploy))
	m.HandleFunc("POST /api/v1/instances/{id}/rollback", s.admin(s.deploy))
	m.HandleFunc("GET /api/v1/configs/{id}/preview", s.admin(s.preview))
	m.HandleFunc("POST /api/v1/hosts/{id}/rotate-agent-token", s.admin(s.rotate))
	m.HandleFunc("POST /api/v1/agent/heartbeat", s.agent(s.heartbeat))
	m.HandleFunc("POST /api/v1/agent/claim", s.agent(s.claim))
	m.HandleFunc("POST /api/v1/agent/tasks/{id}/complete", s.agent(s.complete))
	m.HandleFunc("POST /api/v1/agent/tasks/{id}/renew", s.agent(s.renew))
	m.HandleFunc("GET /api/v1/snapshot", s.admin(s.snapshot))
	m.HandleFunc("POST /api/v1/hosts/{id}/firewalls", s.admin(s.createFirewall))
	m.HandleFunc("GET /api/v1/firewalls/{id}/preview", s.admin(s.firewallPreview))
	m.HandleFunc("POST /api/v1/firewalls/{id}/archive", s.admin(s.archiveFirewall))
	m.HandleFunc("PUT /api/v1/firewalls/{id}", s.admin(s.editFirewall))
	m.HandleFunc("DELETE /api/v1/firewalls/{id}", s.admin(s.deleteFirewall))
	m.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.admin(s.cancelTask))
	m.HandleFunc("PATCH /api/v1/{kind}/{id}", s.admin(s.updateResource))
	m.HandleFunc("DELETE /api/v1/{kind}/{id}", s.admin(s.deleteResource))
	return s.cors(m)
}
func audit(ctx context.Context, tx pgx.Tx, actor, action, id string) error {
	_, e := tx.Exec(ctx, "INSERT INTO audit(tenant_id,actor,action,resource_id) VALUES($1,$2,$3,$4)", tenant, actorContext(ctx, actor), action, id)
	return e
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) error {
	// SQL identifiers are selected solely from this fixed allowlist.
	tables := map[string]string{"hosts": "SELECT tenant_id,id,name,provider,address,last_seen,COALESCE(last_seen>now()-interval '90 seconds',false) online,agent_mode,agent_version,platform,reported_addresses,retire_state,retire_requested_at,cleanup_capable,created_at FROM hosts", "nodes": "SELECT * FROM nodes", "instances": "SELECT * FROM instances", "tunnels": "SELECT * FROM tunnels", "configs": "SELECT * FROM configs", "tasks": "SELECT tenant_id,id,instance_id,config_id,request_key,state,attempts,lease_until,result,created_at,updated_at,mode,operation,artifact_checksum FROM tasks", "audit": "SELECT * FROM audit", "routes": "SELECT * FROM route_plans"}
	tables["firewalls"] = "SELECT tenant_id,id,host_id,name,version,checksum,archived_at,deleted_at,created_at FROM firewall_policies"
	q, ok := tables[r.PathValue("kind")]
	if !ok {
		return problem{404, "unknown collection"}
	}
	limit, offset := 200, 0
	if value := r.URL.Query().Get("limit"); value != "" {
		v, e := strconv.Atoi(value)
		if e != nil || v < 1 || v > 200 {
			return bad("limit must be 1-200")
		}
		limit = v
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		v, e := strconv.Atoi(value)
		if e != nil || v < 0 || v > 1000000 {
			return bad("invalid offset")
		}
		offset = v
	}
	filter := ""
	if r.PathValue("kind") == "hosts" {
		filter = " AND deleted_at IS NULL"
	}
	if r.PathValue("kind") == "firewalls" {
		filter = " AND deleted_at IS NULL"
		if r.URL.Query().Get("trash") == "1" {
			filter = " AND deleted_at IS NOT NULL"
		}
	}
	var data json.RawMessage
	e := s.DB.QueryRow(r.Context(), "SELECT COALESCE(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM ("+q+" WHERE tenant_id=$1"+filter+" ORDER BY created_at DESC LIMIT $2 OFFSET $3) x", tenant, limit, offset).Scan(&data)
	if e != nil {
		return e
	}
	send(w, 200, data)
	return nil
}

type input struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Address    string   `json:"address"`
	Engine     string   `json:"engine"`
	HostID     string   `json:"host_id"`
	NodeID     string   `json:"node_id"`
	ListenPort int      `json:"listen_port"`
	InstanceID string   `json:"instance_id"`
	LocalHost  string   `json:"local_host"`
	LocalPort  int      `json:"local_port"`
	RemotePort int      `json:"remote_port"`
	Hops       []string `json:"hops"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) error {
	kind := r.PathValue("kind")
	allowed := map[string]string{"hosts": "name provider address", "nodes": "name engine", "instances": "name host_id node_id listen_port", "tunnels": "name instance_id local_host local_port remote_port", "routes": "name hops"}
	keys, ok := allowed[kind]
	if !ok {
		return problem{404, "unknown writable collection"}
	}
	var raw map[string]json.RawMessage
	if e := decode(w, r, &raw); e != nil {
		return e
	}
	for key := range raw {
		if !allowedField(keys, key) {
			return bad("unsupported field for " + kind + ": " + key)
		}
	}
	var b input
	encoded, _ := json.Marshal(raw)
	if e := json.Unmarshal(encoded, &b); e != nil {
		return bad("invalid resource field types")
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len([]rune(b.Name)) > 100 {
		return bad("name must contain 1-100 characters")
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id := ID()
	result := map[string]any{"id": id}
	switch kind {
	case "hosts":
		if b.Provider == "" {
			b.Provider = "self"
		}
		if b.Address == "" {
			b.Address = "outbound-agent"
		}
		if len(b.Address) > 255 || len(b.Provider) > 100 {
			return bad("provider or inventory address too long")
		}
		token := ID() + ID()
		_, e = tx.Exec(ctx, "INSERT INTO hosts(tenant_id,id,name,provider,address,agent_hash) VALUES($1,$2,$3,$4,$5,$6)", tenant, id, b.Name, b.Provider, b.Address, Hash(token))
		result["agent_token"] = token
	case "nodes":
		if b.Engine != "sing-box" && b.Engine != "frp" && b.Engine != "xray" {
			return bad("engine must be sing-box or frp")
		}
		_, e = tx.Exec(ctx, "INSERT INTO nodes(tenant_id,id,name,engine) VALUES($1,$2,$3,$4)", tenant, id, b.Name, b.Engine)
	case "instances":
		var lockedHost string
		if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND retire_state='' FOR UPDATE", tenant, b.HostID).Scan(&lockedHost); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return problem{409, "host reference does not exist"}
			}
			return e
		}
		var liveConflict bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM live_ports WHERE tenant_id=$1 AND host_id=$2 AND port=$3)", tenant, b.HostID, b.ListenPort).Scan(&liveConflict); e != nil {
			return e
		}
		if liveConflict {
			return problem{409, "port reserved by live service"}
		}
		_, e = tx.Exec(ctx, "INSERT INTO instances(tenant_id,id,name,host_id,node_id,listen_port) VALUES($1,$2,$3,$4,$5,$6)", tenant, id, b.Name, b.HostID, b.NodeID, b.ListenPort)
		if e == nil {
			_, e = tx.Exec(ctx, "INSERT INTO port_reservations(tenant_id,host_id,port,instance_id) VALUES($1,$2,$3,$4)", tenant, b.HostID, b.ListenPort, id)
		}
	case "tunnels":
		var engine string
		e = tx.QueryRow(ctx, "SELECT n.engine FROM instances i JOIN nodes n ON (n.tenant_id,n.id)=(i.tenant_id,i.node_id) WHERE i.tenant_id=$1 AND i.id=$2 FOR UPDATE OF i", tenant, b.InstanceID).Scan(&engine)
		if e != nil {
			return e
		}
		if engine != "frp" {
			return bad("tunnels require an FRP instance")
		}
		if b.LocalHost == "" {
			b.LocalHost = "127.0.0.1"
		}
		var tunnelHost string
		if e = tx.QueryRow(ctx, "SELECT h.id FROM hosts h JOIN instances i ON (h.tenant_id,h.id)=(i.tenant_id,i.host_id) WHERE i.tenant_id=$1 AND i.id=$2 FOR UPDATE OF h", tenant, b.InstanceID).Scan(&tunnelHost); e != nil {
			return e
		}
		var conflict bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM live_ports WHERE tenant_id=$1 AND host_id=$2 AND port=$3)", tenant, tunnelHost, b.RemotePort).Scan(&conflict); e != nil {
			return e
		}
		if conflict {
			return problem{409, "port reserved by live service"}
		}
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM tunnels WHERE tenant_id=$1 AND instance_id=$2", tenant, b.InstanceID).Scan(&count); e != nil {
			return e
		}
		if count >= 64 {
			return bad("at most 64 tunnels per instance")
		}
		_, e = tx.Exec(ctx, "INSERT INTO tunnels(tenant_id,id,name,instance_id,local_host,local_port,remote_port) VALUES($1,$2,$3,$4,$5,$6,$7)", tenant, id, b.Name, b.InstanceID, b.LocalHost, b.LocalPort, b.RemotePort)
		if e == nil {
			_, e = tx.Exec(ctx, "INSERT INTO port_reservations(tenant_id,host_id,port,instance_id,tunnel_id) SELECT tenant_id,host_id,$3,id,$4 FROM instances WHERE tenant_id=$1 AND id=$2", tenant, b.InstanceID, b.RemotePort, id)
		}
	case "routes":
		if len(b.Hops) < 2 || len(b.Hops) > 8 {
			return bad("route needs 2-8 distinct sing-box instance IDs")
		}
		seen := map[string]bool{}
		for _, hop := range b.Hops {
			if seen[hop] {
				return bad("cyclic or repeated hop")
			}
			seen[hop] = true
			var engine string
			if e = tx.QueryRow(ctx, "SELECT n.engine FROM instances i JOIN nodes n ON (n.tenant_id,n.id)=(i.tenant_id,i.node_id) WHERE i.tenant_id=$1 AND i.id=$2", tenant, hop).Scan(&engine); e != nil {
				return e
			}
			if engine != "sing-box" {
				return bad("route hops require sing-box")
			}
		}
		hops, _ := json.Marshal(b.Hops)
		_, e = tx.Exec(ctx, "INSERT INTO route_plans(tenant_id,id,name,hops) VALUES($1,$2,$3,$4)", tenant, id, b.Name, hops)
		if e == nil {
			for n, hop := range b.Hops {
				if _, e = tx.Exec(ctx, "INSERT INTO route_hops(tenant_id,route_id,position,instance_id) VALUES($1,$2,$3,$4)", tenant, id, n+1, hop); e != nil {
					break
				}
			}
		}
	default:
		return problem{404, "unknown writable collection"}
	}
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "create."+kind, id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 201, result)
	return nil
}
func (s *Server) config(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Outbound string `json:"outbound"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Outbound != "direct" && b.Outbound != "block" {
		return bad("outbound must be direct or block")
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	instance := r.PathValue("id")
	var port int
	var engine string
	e = tx.QueryRow(ctx, "SELECT i.listen_port,n.engine FROM instances i JOIN nodes n ON (n.tenant_id,n.id)=(i.tenant_id,i.node_id) WHERE i.tenant_id=$1 AND i.id=$2 FOR UPDATE OF i", tenant, instance).Scan(&port, &engine)
	if e != nil {
		return e
	}
	// A typed simulation manifest, deliberately not arbitrary native config or executable input.
	spec := map[string]any{"schema": 1, "mode": "simulation", "engine": engine, "listen": "127.0.0.1", "port": port, "outbound": b.Outbound}
	if engine == "frp" {
		var tunnels json.RawMessage
		e = tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(jsonb_build_object('local_host',local_host,'local_port',local_port,'remote_port',remote_port) ORDER BY remote_port,id),'[]'::jsonb) FROM tunnels WHERE tenant_id=$1 AND instance_id=$2", tenant, instance).Scan(&tunnels)
		if e != nil {
			return e
		}
		spec["tunnels"] = tunnels
	}
	raw, _ := json.Marshal(spec)
	raw, e = manifest.Canonical(raw)
	if e != nil {
		return e
	}
	if _, e = manifest.Parse(raw); e != nil {
		return bad(e.Error())
	}
	id := ID()
	var version int
	e = tx.QueryRow(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM configs WHERE tenant_id=$1 AND instance_id=$2", tenant, instance).Scan(&version)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO configs(tenant_id,id,instance_id,version,spec,checksum) VALUES($1,$2,$3,$4,$5,$6)", tenant, id, instance, version, raw, Hash(string(raw)))
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "config.create", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 201, map[string]any{"id": id, "version": version, "spec": spec, "checksum": Hash(string(raw))})
	return nil
}
func (s *Server) deploy(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		ConfigID string `json:"config_id"`
		Mode     string `json:"mode"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Mode == "" {
		b.Mode = "simulation"
	}
	if b.Mode != "simulation" && b.Mode != "staging" {
		return bad("mode must be simulation or staging")
	}
	operation := "deploy"
	if strings.HasSuffix(r.URL.Path, "/rollback") {
		operation = "rollback"
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		return bad("Idempotency-Key must contain 8-128 bytes")
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Serializes requests sharing a key, including requests targeting different instances.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", tenant+key); e != nil {
		return e
	}
	var existing, instance, config, mode, op string
	e = tx.QueryRow(ctx, "SELECT id,instance_id,config_id,mode,operation FROM tasks WHERE tenant_id=$1 AND request_key=$2", tenant, key).Scan(&existing, &instance, &config, &mode, &op)
	if e == nil {
		if instance != r.PathValue("id") || config != b.ConfigID || mode != b.Mode || op != operation {
			return problem{409, "idempotency key reused with different request"}
		}
		send(w, 200, map[string]string{"id": existing})
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	// Lock the instance before validating rollback history and creating an active task.
	var current *string
	column := "applied_config_id"
	if b.Mode == "staging" {
		column = "staged_config_id"
	}
	if e = tx.QueryRow(ctx, "SELECT "+column+" FROM instances WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, r.PathValue("id")).Scan(&current); e != nil {
		return e
	}
	if operation == "rollback" {
		var known bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks WHERE tenant_id=$1 AND instance_id=$2 AND config_id=$3 AND mode=$4 AND state='succeeded')", tenant, r.PathValue("id"), b.ConfigID, b.Mode).Scan(&known); e != nil {
			return e
		}
		if !known || current == nil || *current == b.ConfigID {
			return problem{409, "rollback needs a different, previously successful config in this mode"}
		}
	}
	id := ID()
	_, e = tx.Exec(ctx, "INSERT INTO tasks(tenant_id,id,instance_id,config_id,request_key,mode,operation) VALUES($1,$2,$3,$4,$5,$6,$7)", tenant, id, r.PathValue("id"), b.ConfigID, key, b.Mode, operation)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, "admin", "deploy.queued", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 202, map[string]string{"id": id})
	return nil
}

func allowedField(keys, key string) bool {
	for _, field := range strings.Fields(keys) {
		if field == key {
			return true
		}
	}
	return false
}
