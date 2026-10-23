package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
	"vps-control/internal/live"
)

func (s *Server) liveReady() error {
	if len(s.MasterKey) != 32 {
		return problem{503, "live services require a persistent 32-byte MASTER_KEY"}
	}
	return nil
}
func (s *Server) site(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var domain string
		https := true
		e := s.DB.QueryRow(r.Context(), "SELECT domain,https FROM site_settings WHERE tenant_id=$1", tenant).Scan(&domain, &https)
		if e != nil && e != pgx.ErrNoRows {
			return e
		}
		var actualDomain *string
		var actualHTTPS *bool
		var state *string
		var checked *time.Time
		_ = s.DB.QueryRow(r.Context(), "SELECT gateway_domain,gateway_https,gateway_state,gateway_checked_at FROM site_settings WHERE tenant_id=$1", tenant).Scan(&actualDomain, &actualHTTPS, &state, &checked)
		send(w, 200, map[string]any{"domain": domain, "https": https, "gateway_domain": actualDomain, "gateway_https": actualHTTPS, "gateway_state": state, "gateway_checked_at": checked, "live_enabled": len(s.MasterKey) == 32})
		return nil
	}
	var b struct {
		Domain string `json:"domain"`
		HTTPS  *bool  `json:"https"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.HTTPS == nil {
		return bad("https is required")
	}
	b.Domain = strings.ToLower(strings.TrimSpace(b.Domain))
	if !live.Address(b.Domain) || strings.Contains(b.Domain, ":") {
		return bad("valid DNS domain or IPv4 required; IPv6 endpoints belong on live services")
	}
	scheme := "https"
	if !*b.HTTPS {
		scheme = "http"
	}
	if !live.Origin(scheme + "://" + b.Domain) {
		return bad("invalid public domain")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "INSERT INTO site_settings(tenant_id,domain,https) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET domain=excluded.domain,https=excluded.https,updated_at=now()", tenant, b.Domain, *b.HTTPS); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "site.update", tenant); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"domain": b.Domain, "https": *b.HTTPS, "gateway_change_required": true})
	return nil
}
func (s *Server) liveList(w http.ResponseWriter, r *http.Request) error {
	var b json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,host_id,name,kind,engine,address,port,parent_id,next_id,enabled,version,reported_version,state,reported_at,created_at,(reported_at>now()-interval '45 seconds' AND version=reported_version AND state='running' AND enabled) AS online FROM live_services WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200) x`, tenant).Scan(&b)
	if e != nil {
		return e
	}
	send(w, 200, b)
	return nil
}
func (s *Server) loadSpec(r *http.Request, id string) (live.Spec, error) {
	var sealed []byte
	e := s.DB.QueryRow(r.Context(), "SELECT sealed FROM live_services WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&sealed)
	if e != nil {
		return live.Spec{}, e
	}
	raw, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return live.Spec{}, e
	}
	var spec live.Spec
	e = json.Unmarshal(raw, &spec)
	return spec, e
}
func (s *Server) liveCreate(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		Engine      string        `json:"engine"`
		Name        string        `json:"name"`
		HostID      string        `json:"host_id"`
		Kind        string        `json:"kind"`
		Address     string        `json:"address"`
		Listen      string        `json:"listen"`
		Port        int           `json:"port"`
		ParentID    string        `json:"parent_id"`
		NextID      string        `json:"next_id"`
		Tunnels     []live.Tunnel `json:"tunnels"`
		ServerName  string        `json:"server_name"`
		Certificate string        `json:"certificate"`
		PrivateKey  string        `json:"private_key"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(strings.TrimSpace(b.Name)) == 0 || len([]rune(b.Name)) > 100 {
		return bad("name required (max 100 characters)")
	}
	if b.Listen == "" {
		b.Listen = "127.0.0.1"
	}
	spec := live.Spec{Kind: b.Kind, Listen: b.Listen, Address: b.Address, Port: b.Port, Password: live.Secret(), Tunnels: b.Tunnels}
	spec.Engine = b.Engine
	if b.Engine != "" && !live.Proxy(b.Kind) {
		return bad("engine selection only valid for proxy templates")
	}
	if live.TLSProxy(b.Kind) {
		spec.UUID = live.UUID()
		spec.ServerName = b.ServerName
		spec.Certificate = b.Certificate
		spec.PrivateKey = b.PrivateKey
	} else if b.ServerName != "" || b.Certificate != "" || b.PrivateKey != "" {
		return bad("TLS fields only valid for TLS proxy templates")
	}
	if b.Kind == "frpc" {
		if b.NextID != "" {
			return bad("FRP client cannot contain route peers")
		}
		parent, e := s.loadSpec(r, b.ParentID)
		if e != nil {
			return e
		}
		if parent.Kind != "frps" {
			return bad("FRP client needs an FRP server")
		}
		spec = parent
		spec.Kind = "frpc"
		spec.PrivateKey = ""
		if len(b.Tunnels) > 0 {
			allowed := map[int]bool{}
			for _, t := range parent.Tunnels {
				allowed[t.RemotePort] = true
			}
			for _, t := range b.Tunnels {
				if !allowed[t.RemotePort] {
					return bad("client tunnel remote port is not reserved by parent server")
				}
			}
			spec.Tunnels = b.Tunnels
		}
		spec.Listen = "127.0.0.1"
	} else if b.ParentID != "" {
		return bad("parent only valid for FRP clients")
	}
	if b.Kind == "frps" {
		var e error
		spec.Certificate, spec.PrivateKey, e = live.Certificate(spec.Address)
		if e != nil {
			return bad(e.Error())
		}
	}
	if b.NextID != "" {
		if b.Kind != "shadowsocks" {
			return bad("route peer requires shadowsocks")
		}
		next, e := s.loadSpec(r, b.NextID)
		if e != nil {
			return e
		}
		if next.Kind != "shadowsocks" {
			return bad("invalid route peer")
		}
		var depth int
		if e := s.DB.QueryRow(r.Context(), `WITH RECURSIVE chain AS (SELECT id,next_id,1 AS depth FROM live_services WHERE tenant_id=$1 AND id=$2 UNION ALL SELECT l.id,l.next_id,c.depth+1 FROM live_services l JOIN chain c ON l.id=c.next_id WHERE l.tenant_id=$1 AND c.depth<9) SELECT COALESCE(max(depth),0) FROM chain`, tenant, b.NextID).Scan(&depth); e != nil {
			return e
		}
		if depth >= 8 {
			return bad("at most 8 live route hops")
		}
		spec.Peer = &live.Peer{Address: next.Address, Port: next.Port, Password: next.Password}
	}
	if e := spec.Validate(); e != nil {
		return bad(e.Error())
	}
	if live.TLSProxy(spec.Kind) {
		if e := spec.VerifyPublicCertificate(); e != nil {
			return bad(e.Error())
		}
	}
	raw, _ := json.Marshal(spec)
	sealed, e := live.Seal(s.MasterKey, raw)
	if e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var host string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, b.HostID).Scan(&host); e != nil {
		return e
	}
	if live.Proxy(spec.Kind) && spec.Kind != "shadowsocks" {
		var version string
		if e = tx.QueryRow(ctx, "SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2", tenant, host).Scan(&version); e != nil {
			return e
		}
		if !live.ProtocolCapable(version) {
			return problem{409, "upgrade this host to Agent 0.6.0 before creating protocol services"}
		}
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM live_services WHERE tenant_id=$1 AND host_id=$2", tenant, host).Scan(&count); e != nil {
		return e
	}
	if count >= 64 {
		return bad("at most 64 live services per host")
	}
	id := ID()
	_, e = tx.Exec(ctx, "INSERT INTO live_services(tenant_id,id,host_id,name,kind,address,port,parent_id,next_id,sealed,engine) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11)", tenant, id, host, b.Name, spec.Kind, spec.Address, spec.Port, b.ParentID, b.NextID, sealed, map[bool]string{true: "xray", false: "sing-box"}[spec.Engine == "xray"])
	if e != nil {
		return e
	}
	if spec.Kind != "frpc" {
		ports := []int{spec.Port}
		if spec.Kind == "frps" {
			for _, t := range spec.Tunnels {
				ports = append(ports, t.RemotePort)
			}
		}
		for _, p := range ports {
			var exists bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM port_reservations WHERE tenant_id=$1 AND host_id=$2 AND port=$3)", tenant, host, p).Scan(&exists); e != nil {
				return e
			}
			if exists {
				return problem{409, "port already reserved by an inventory instance"}
			}
			if _, e = tx.Exec(ctx, "INSERT INTO live_ports VALUES($1,$2,$3,$4)", tenant, host, p, id); e != nil {
				return e
			}
		}
	}
	if spec.Kind == "frpc" {
		for _, t := range spec.Tunnels {
			if _, e = tx.Exec(ctx, "INSERT INTO frp_assignments VALUES($1,$2,$3,$4)", tenant, b.ParentID, t.RemotePort, id); e != nil {
				return e
			}
		}
	}
	if e = audit(ctx, tx, "admin", "live.create", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 201, map[string]any{"id": id, "enabled": false})
	return nil
}
func (s *Server) liveToggle(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Enabled == nil {
		return bad("enabled is required")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if *b.Enabled {
		var kind, version string
		if e = tx.QueryRow(r.Context(), "SELECT l.kind,h.agent_version FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.id=$2", tenant, r.PathValue("id")).Scan(&kind, &version); e != nil {
			return e
		}
		if live.Proxy(kind) && kind != "shadowsocks" && !live.ProtocolCapable(version) {
			return problem{409, "upgrade this host to protocol-capable Agent 0.6.0 before activation"}
		}
	}
	tag, e := tx.Exec(r.Context(), "UPDATE live_services SET enabled=$3,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2 AND enabled<>$3", tenant, r.PathValue("id"), *b.Enabled)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		e = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND id=$2)", tenant, r.PathValue("id")).Scan(&exists)
		if e != nil {
			return e
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}
	if e = audit(r.Context(), tx, "admin", "live.toggle", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"enabled": *b.Enabled})
	return nil
}

type liveWork struct {
	ID      string    `json:"id"`
	Version int       `json:"version"`
	Enabled bool      `json:"enabled"`
	Spec    live.Spec `json:"spec"`
}
type liveReport struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	State   string `json:"state"`
}

func (s *Server) liveSync(w http.ResponseWriter, r *http.Request, host string) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		Reports []liveReport `json:"reports"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(b.Reports) > 64 {
		return bad("too many reports")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	for _, v := range b.Reports {
		if v.State != "running" && v.State != "stopped" && v.State != "failed" && v.State != "pending" {
			return bad("invalid live state")
		}
		tag, e := tx.Exec(r.Context(), "UPDATE live_services SET reported_version=$4,state=$5,reported_at=now() WHERE tenant_id=$1 AND host_id=$2 AND id=$3 AND version=$4 AND (enabled=($5<>'stopped'))", tenant, host, v.ID, v.Version, v.State)
		if e != nil {
			return e
		}
		_ = tag
	}
	var agentVersion string
	if e = tx.QueryRow(r.Context(), "SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2", tenant, host).Scan(&agentVersion); e != nil {
		return e
	}
	rows, e := tx.Query(r.Context(), "SELECT id,version,enabled,sealed FROM live_services WHERE tenant_id=$1 AND host_id=$2 AND (kind IN ('shadowsocks','frps','frpc') OR $3) ORDER BY created_at LIMIT 64", tenant, host, live.ProtocolCapable(agentVersion))
	if e != nil {
		return e
	}
	work := []liveWork{}
	for rows.Next() {
		var v liveWork
		var sealed []byte
		if e = rows.Scan(&v.ID, &v.Version, &v.Enabled, &sealed); e != nil {
			rows.Close()
			return e
		}
		raw, e := live.Open(s.MasterKey, sealed)
		if e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal(raw, &v.Spec); e != nil {
			rows.Close()
			return e
		}
		work = append(work, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, work)
	return nil
}
func (s *Server) publicOrigin(r *http.Request) (string, error) {
	var domain string
	var https bool
	e := s.DB.QueryRow(r.Context(), "SELECT domain,https FROM site_settings WHERE tenant_id=$1", tenant).Scan(&domain, &https)
	if e != nil {
		return "", e
	}
	prefix := "https://"
	if !https {
		prefix = "http://"
	}
	if !live.Origin(prefix + domain) {
		return "", bad("configure site domain first")
	}
	return prefix + domain, nil
}
func (s *Server) subscriptionList(w http.ResponseWriter, r *http.Request) error {
	var b json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,expires_at,revoked_at,created_at FROM subscriptions WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200) x`, tenant).Scan(&b)
	if e != nil {
		return e
	}
	send(w, 200, b)
	return nil
}
func (s *Server) subscriptionCreate(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		Name       string   `json:"name"`
		ServiceIDs []string `json:"service_ids"`
		Days       int      `json:"days"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Days < 1 || b.Days > 365 || len(b.ServiceIDs) < 1 || len(b.ServiceIDs) > 64 || strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 {
		return bad("name, 1-64 services and expiry 1-365 days required")
	}
	origin, e := s.publicOrigin(r)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	id, token := ID(), ID()+ID()
	_, e = tx.Exec(r.Context(), "INSERT INTO subscriptions(tenant_id,id,name,token_hash,expires_at) VALUES($1,$2,$3,$4,$5)", tenant, id, b.Name, Hash(token), time.Now().Add(time.Duration(b.Days)*24*time.Hour))
	if e != nil {
		return e
	}
	for _, service := range b.ServiceIDs {
		var kind string
		if e = tx.QueryRow(r.Context(), "SELECT kind FROM live_services WHERE tenant_id=$1 AND id=$2", tenant, service).Scan(&kind); e != nil {
			return e
		}
		if !live.Proxy(kind) {
			return bad("only encrypted proxy endpoints are subscribable")
		}
		if _, e = tx.Exec(r.Context(), "INSERT INTO subscription_services VALUES($1,$2,$3)", tenant, id, service); e != nil {
			return e
		}
	}
	if e = audit(r.Context(), tx, "admin", "subscription.create", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 201, map[string]string{"id": id, "url": origin + "/s/" + token, "notice": "URL shown once; contains proxy credentials. Protect it. Revocation stops downloads, not already imported credentials."})
	return nil
}
func (s *Server) subscriptionRevoke(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	tag, e := tx.Exec(r.Context(), "UPDATE subscriptions SET revoked_at=COALESCE(revoked_at,now()) WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e = audit(r.Context(), tx, "admin", "subscription.revoke", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"revoked": true})
	return nil
}

func (s *Server) subscriptionRotate(w http.ResponseWriter, r *http.Request) error {
	origin, e := s.publicOrigin(r)
	if e != nil {
		return e
	}
	token := ID() + ID()
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	tag, e := tx.Exec(r.Context(), "UPDATE subscriptions SET token_hash=$3 WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()", tenant, r.PathValue("id"), Hash(token))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e = audit(r.Context(), tx, "admin", "subscription.rotate", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"url": origin + "/s/" + token})
	return nil
}

func (s *Server) gatewayStatus(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Domain string `json:"domain"`
		HTTPS  bool   `json:"https"`
		State  string `json:"state"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if !live.Address(b.Domain) || (b.State != "active" && b.State != "error") {
		return bad("invalid gateway status")
	}
	_, e := s.DB.Exec(r.Context(), "UPDATE site_settings SET gateway_domain=$2,gateway_https=$3,gateway_state=$4,gateway_checked_at=now() WHERE tenant_id=$1", tenant, b.Domain, b.HTTPS, b.State)
	if e != nil {
		return e
	}
	send(w, 200, map[string]bool{"recorded": true})
	return nil
}

func (s *Server) liveTunnels(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	spec, e := s.loadSpec(r, r.PathValue("id"))
	if e != nil {
		return e
	}
	if spec.Kind != "frps" {
		return bad("FRP server required")
	}
	send(w, 200, spec.Tunnels)
	return nil
}
