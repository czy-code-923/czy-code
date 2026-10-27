package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
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
		var title string
		var origins json.RawMessage
		if e = s.DB.QueryRow(r.Context(), `SELECT title,api_origins FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&title, &origins); e != nil && e != pgx.ErrNoRows {
			return e
		}
		if title == "" {
			title = "星枢"
		}
		if len(origins) == 0 {
			origins = json.RawMessage(`[]`)
		}
		var shareGatewayDomain, shareGatewayState string
		var shareGatewayChecked *time.Time
		if e = s.DB.QueryRow(r.Context(), `SELECT share_gateway_domain,share_gateway_state,share_gateway_checked_at FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&shareGatewayDomain, &shareGatewayState, &shareGatewayChecked); e != nil && e != pgx.ErrNoRows {
			return e
		}
		var ipOrigin, shareDomain string
		var shareHTTPS bool
		if e = s.DB.QueryRow(r.Context(), `SELECT ip_origin,share_domain,share_https FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&ipOrigin, &shareDomain, &shareHTTPS); e != nil && e != pgx.ErrNoRows {
			return e
		}
		send(w, 200, map[string]any{"share_gateway_domain": shareGatewayDomain, "share_gateway_state": shareGatewayState, "share_gateway_checked_at": shareGatewayChecked, "ip_origin": ipOrigin, "share_domain": shareDomain, "share_https": shareHTTPS, "title": title, "api_origins": origins, "domain": domain, "https": https, "gateway_domain": actualDomain, "gateway_https": actualHTTPS, "gateway_state": state, "gateway_checked_at": checked, "live_enabled": len(s.MasterKey) == 32})
		return nil
	}
	var b struct {
		Domain      string    `json:"domain"`
		IPOrigin    *string   `json:"ip_origin"`
		ShareDomain *string   `json:"share_domain"`
		ShareHTTPS  *bool     `json:"share_https"`
		HTTPS       *bool     `json:"https"`
		Title       *string   `json:"title"`
		Origins     *[]string `json:"api_origins"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.HTTPS == nil {
		return bad("https is required")
	}
	b.Domain = strings.ToLower(strings.TrimSpace(b.Domain))
	if b.Domain != "" && (!live.Address(b.Domain) || strings.Contains(b.Domain, ":")) {
		return bad("valid DNS domain or IPv4 required; IPv6 endpoints belong on live services")
	}
	var reserved bool
	if e := s.DB.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM reverse_proxies WHERE tenant_id=$1 AND domain=$2)", tenant, b.Domain).Scan(&reserved); e != nil {
		return e
	}
	if reserved {
		return problem{409, "domain is reserved by a reverse proxy"}
	}
	scheme := "https"
	if !*b.HTTPS {
		scheme = "http"
	}
	if b.Domain != "" && !live.Origin(scheme+"://"+b.Domain) {
		return bad("invalid public domain")
	}
	if b.IPOrigin != nil {
		v := strings.TrimRight(strings.TrimSpace(*b.IPOrigin), "/")
		u, e := url.Parse(v)
		if e != nil || !live.Origin(v) {
			return bad("IP 访问入口应为 http(s)://IP:端口")
		}
		if _, e = netip.ParseAddr(u.Hostname()); e != nil {
			return bad("IP 入口必须填写 IP 地址")
		}
		b.IPOrigin = &v
	}
	if b.ShareDomain != nil {
		v := strings.ToLower(strings.TrimSpace(*b.ShareDomain))
		if v != "" && (!live.Address(v) || strings.Contains(v, ":")) {
			return bad("订阅域名格式无效")
		}
		b.ShareDomain = &v
		var used bool
		if e := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM reverse_proxies WHERE tenant_id=$1 AND domain=$2)`, tenant, v).Scan(&used); e != nil {
			return e
		}
		if used {
			return problem{409, "订阅域名已用于反向代理"}
		}
	}
	if b.Title != nil {
		value := strings.TrimSpace(*b.Title)
		if value == "" || len([]rune(value)) > 30 {
			return bad("site title must contain 1-30 characters")
		}
		for _, c := range value {
			if unicode.IsControl(c) {
				return bad("invalid site title")
			}
		}
		b.Title = &value
	}
	if b.Origins != nil {
		if len(*b.Origins) > 8 {
			return bad("at most eight API origins")
		}
		values := []string{}
		seen := map[string]bool{}
		for _, origin := range *b.Origins {
			normalized, ok := normalizedAPIOrigin(origin)
			if !ok {
				return bad("API origin requires exact HTTPS origin, or HTTP loopback for development")
			}
			if !seen[normalized] {
				values = append(values, normalized)
				seen[normalized] = true
			}
		}
		b.Origins = &values
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "INSERT INTO site_settings(tenant_id,domain,https) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET domain=excluded.domain,https=excluded.https,updated_at=now()", tenant, b.Domain, *b.HTTPS); e != nil {
		return e
	}
	if b.Title != nil {
		if _, e = tx.Exec(r.Context(), `UPDATE site_settings SET title=$2 WHERE tenant_id=$1`, tenant, *b.Title); e != nil {
			return e
		}
	}
	if b.Origins != nil {
		raw, _ := json.Marshal(*b.Origins)
		if _, e = tx.Exec(r.Context(), `UPDATE site_settings SET api_origins=$2 WHERE tenant_id=$1`, tenant, raw); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(r.Context(), `UPDATE site_settings SET ip_origin=COALESCE($2,ip_origin),share_domain=COALESCE($3,share_domain),share_https=COALESCE($4,share_https) WHERE tenant_id=$1`, tenant, b.IPOrigin, b.ShareDomain, b.ShareHTTPS); e != nil {
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
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,host_id,node_id,certificate_id,name,kind,engine,address,port,parent_id,next_id,managed_users,enabled,version,reported_version,state,reported_at,created_at,delete_requested_at,(reported_at>now()-interval '45 seconds' AND version=reported_version AND state='running' AND enabled) AS online FROM live_services WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200) x`, tenant).Scan(&b)
	if e != nil {
		return e
	}
	send(w, 200, b)
	return nil
}
func (s *Server) loadSpec(r *http.Request, id string) (live.Spec, error) {
	var sealed []byte
	e := s.DB.QueryRow(r.Context(), "SELECT l.sealed FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.id=$2 AND h.retire_state='' AND h.deleted_at IS NULL", tenant, id).Scan(&sealed)
	if e != nil {
		return live.Spec{}, e
	}
	raw, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return live.Spec{}, e
	}
	var spec live.Spec
	e = json.Unmarshal(raw, &spec)
	if spec.Outbound != nil {
		spec.Outbound.StripServerSecrets()
	}
	return spec, e
}

type liveInput struct {
	EndpointAddresses *[]string `json:"endpoint_addresses"`
	EgressFamilies    *[]string `json:"egress_families"`
	live.ProtocolOptions
	ClearHysteriaObfs bool          `json:"clear_hysteria_obfs"`
	ClearHY2Obfs      bool          `json:"clear_hy2_obfs"`
	Reality           *live.Reality `json:"reality"`
	Flow              string        `json:"flow"`
	ALPN              []string      `json:"alpn"`
	Congestion        string        `json:"congestion"`
	WSHost            string        `json:"ws_host"`
	ServiceName       string        `json:"service_name"`
	ManagedUsers      bool          `json:"managed_users"`
	Password          string        `json:"password"`
	Username          string        `json:"username"`
	UUID              string        `json:"uuid"`
	Engine            string        `json:"engine"`
	Name              string        `json:"name"`
	HostID            string        `json:"host_id"`
	Kind              string        `json:"kind"`
	Address           string        `json:"address"`
	Listen            string        `json:"listen"`
	Port              int           `json:"port"`
	ParentID          string        `json:"parent_id"`
	NextID            string        `json:"next_id"`
	Tunnels           []live.Tunnel `json:"tunnels"`
	ServerName        string        `json:"server_name"`
	Certificate       string        `json:"certificate"`
	PrivateKey        string        `json:"private_key"`
	Version           int           `json:"version"`
	NodeID            string        `json:"node_id"`
	CertificateID     string        `json:"certificate_id"`
	Transport         string        `json:"transport"`
	Path              string        `json:"path"`
	TLSMode           string        `json:"tls_mode"`
	RotateCredentials bool          `json:"rotate_credentials"`
}

type liveCreated struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Port    int    `json:"port"`
	NodeID  string `json:"node_id"`
	Engine  string `json:"engine"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) liveCreate(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b liveInput
	if e := decode(w, r, &b); e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var created liveCreated
	if e = s.createLiveTx(r, tx, b, &created); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 201, created)
	return nil
}
func (s *Server) createLiveTx(r *http.Request, tx pgx.Tx, b liveInput, created *liveCreated) error {
	if len(strings.TrimSpace(b.Name)) == 0 || len([]rune(b.Name)) > 100 {
		return bad("name required (max 100 characters)")
	}
	if b.Listen == "" {
		b.Listen = "127.0.0.1"
	}
	spec := live.Spec{Kind: b.Kind, Listen: b.Listen, Address: b.Address, Port: b.Port, Password: live.Secret(), Tunnels: b.Tunnels}
	if e := s.applyNetwork(r, tx, b, &spec); e != nil {
		return e
	}
	spec.Engine = b.Engine
	spec.Transport = b.Transport
	spec.Path = b.Path
	spec.TLSMode = b.TLSMode
	if e := applySecurity(&spec, b); e != nil {
		return e
	}
	if b.CertificateID != "" {
		if e := s.applyCertificate(r, b.CertificateID, &b); e != nil {
			return e
		}
	}
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
			allowed := map[int]string{}
			for _, t := range parent.Tunnels {
				kind := t.Type
				if kind == "" {
					kind = "tcp"
				}
				allowed[t.RemotePort] = kind
			}
			for _, t := range b.Tunnels {
				kind := t.Type
				if kind == "" {
					kind = "tcp"
				}
				if allowed[t.RemotePort] != kind {
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
		if !live.Proxy(spec.Kind) {
			return bad("only proxy nodes support a next hop")
		}
		next, e := s.loadSpec(r, b.NextID)
		if e != nil {
			return e
		}
		next.EndpointAddresses = nil
		next.EgressFamilies = nil
		next.Outbound = nil
		next.Peer = nil
		next.StripServerSecrets()
		spec.Outbound = &next
	}

	if b.Kind == "shadowsocks" && b.Password == "" {
		spec.Password = live.CipherSecret(spec.SSMethod())
	}
	if b.Password != "" {
		spec.Password = b.Password
	}
	if b.UUID != "" {
		if b.Kind != "vless" && b.Kind != "vmess" && b.Kind != "tuic" {
			return bad("UUID not applicable")
		}
		spec.UUID = b.UUID
	}
	if e := spec.Validate(); e != nil {
		return bad(e.Error())
	}
	if e := requireFRPVersion(r, tx, b.HostID, spec); e != nil {
		return e
	}
	if spec.UsesTLS() {
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
	if b.ParentID != "" {
		var parent string
		if e = tx.QueryRow(ctx, `SELECT id FROM live_services WHERE tenant_id=$1 AND id=$2 AND delete_requested_at IS NULL`, tenant, b.ParentID).Scan(&parent); e != nil {
			return e
		}
	}
	if e = s.checkChain(r, tx, "", b.NextID); e != nil {
		return e
	}
	var host string
	if e = tx.QueryRow(ctx, "SELECT id FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", tenant, b.HostID).Scan(&host); e != nil {
		return e
	}
	if live.Proxy(spec.Kind) && (spec.Kind != "shadowsocks" || spec.RequiresAdvanced()) {
		var version string
		if e = tx.QueryRow(ctx, "SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2", tenant, host).Scan(&version); e != nil {
			return e
		}
		if spec.RequiresV13() && !live.V13Capable(version) {
			return problem{409, "upgrade host to Agent 0.13.0 for this protocol/transport"}
		}
		if spec.RequiresOptions() && !live.OptionsCapable(version) {
			return problem{409, "upgrade host to Agent 0.9.0 for protocol options"}
		}
		if (spec.ManagedUsers || spec.IsReality() || spec.Transport == "grpc") && !live.BundleCapable(version) {
			return problem{409, "upgrade host to Agent 0.8.0 for REALITY, gRPC and users"}
		}
		if !live.ProtocolCapable(version) || spec.RequiresAdvanced() && !live.AdvancedCapable(version) {
			return problem{409, "upgrade this host to Agent 0.7.0 before creating protocol services"}
		}
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM live_services WHERE tenant_id=$1 AND host_id=$2", tenant, host).Scan(&count); e != nil {
		return e
	}
	if count >= 64 {
		return bad("at most 64 live services per host")
	}
	nodeID, e := s.ensureNode(r, tx, b.NodeID, b.Name, spec)
	if e != nil {
		return e
	}
	id := ID()
	_, e = tx.Exec(ctx, "INSERT INTO live_services(tenant_id,id,host_id,name,kind,address,port,parent_id,next_id,sealed,engine,node_id,certificate_id,managed_users) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,NULLIF($13,''),$14)", tenant, id, host, b.Name, spec.Kind, spec.Address, spec.Port, b.ParentID, b.NextID, sealed, map[bool]string{true: "xray", false: "sing-box"}[spec.Engine == "xray"], nodeID, b.CertificateID, spec.ManagedUsers)
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
	*created = liveCreated{ID: id, Kind: spec.Kind, Port: spec.Port, NodeID: nodeID, Engine: spec.Engine}
	return nil
}
func (s *Server) liveToggle(w http.ResponseWriter, r *http.Request) error {
	var deleting bool
	if e := s.DB.QueryRow(r.Context(), `SELECT (delete_requested_at IS NOT NULL OR EXISTS(SELECT 1 FROM hosts h WHERE (h.tenant_id,h.id)=(live_services.tenant_id,live_services.host_id) AND h.retire_state<>'')) FROM live_services WHERE tenant_id=$1 AND id=$2`, tenant, r.PathValue("id")).Scan(&deleting); e != nil {
		return e
	}
	if deleting {
		return problem{409, "配置正在删除，等待 Agent 停止回执"}
	}

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
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if e = tx.QueryRow(r.Context(), `SELECT (delete_requested_at IS NOT NULL OR EXISTS(SELECT 1 FROM hosts h WHERE (h.tenant_id,h.id)=(live_services.tenant_id,live_services.host_id) AND h.retire_state<>'')) FROM live_services WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, r.PathValue("id")).Scan(&deleting); e != nil {
		return e
	}
	if deleting {
		return problem{409, "配置正在删除，等待 Agent 停止回执"}
	}
	if *b.Enabled {
		var kind, version string
		if e = tx.QueryRow(r.Context(), "SELECT l.kind,h.agent_version FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.id=$2", tenant, r.PathValue("id")).Scan(&kind, &version); e != nil {
			return e
		}
		if live.BundleCapable(version) {
			spec, err := s.loadSpec(r, r.PathValue("id"))
			if err != nil {
				return err
			}
			if hasFRPUDP(spec) && !engineUpdateCapable(version) {
				return problem{409, "FRP UDP requires Agent 0.17 or newer"}
			}
			if spec.RequiresV13() && !live.V13Capable(version) {
				return problem{409, "upgrade host to Agent 0.13.0 for this protocol/transport"}
			}
			if spec.RequiresOptions() && !live.OptionsCapable(version) {
				return problem{409, "upgrade host to Agent 0.9.0 for protocol options"}
			}
			if err = spec.Validate(); err != nil {
				return bad(err.Error())
			}
			engine := spec.Engine
			if engine == "" {
				engine = "sing-box"
			}
			if spec.Kind == "frps" || spec.Kind == "frpc" {
				engine = spec.Kind
			}
			var ready bool
			if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM host_kernels WHERE tenant_id=$1 AND host_id=(SELECT host_id FROM live_services WHERE tenant_id=$1 AND id=$2) AND engine=$3 AND available AND checked_at>now()-interval '45 seconds')`, tenant, r.PathValue("id"), engine).Scan(&ready); e != nil {
				return e
			}
			if !ready {
				return problem{409, "host kernel is unavailable or stale; verify Agent runtime status first"}
			}
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
	NextID  string    `json:"-"`
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
		Reports       []liveReport        `json:"reports"`
		BundleMode    bool                `json:"bundle_mode"`
		Capabilities  []live.Capability   `json:"capabilities"`
		KernelReports []live.KernelReport `json:"kernel_reports"`
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
	if e = graphLock(r, tx); e != nil {
		return e
	}
	for _, c := range b.Capabilities {
		if c.Engine != "sing-box" && c.Engine != "xray" && c.Engine != "frps" && c.Engine != "frpc" || len(c.Version) > 120 {
			return bad("invalid kernel report")
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO host_kernels(tenant_id,host_id,engine,available,version) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,host_id,engine) DO UPDATE SET available=excluded.available,version=excluded.version,checked_at=now()`, tenant, host, c.Engine, c.Available, c.Version); e != nil {
			return e
		}
	}
	if len(b.Capabilities) > 4 || len(b.KernelReports) > 2 {
		return bad("too many kernel reports")
	}
	for _, k := range b.KernelReports {
		if k.Engine != "sing-box" && k.Engine != "xray" || len(k.Checksum) != 64 || (k.State != "pending" && k.State != "failed" && k.State != "running" && k.State != "stopped") {
			continue
		}
		if _, e = tx.Exec(r.Context(), `UPDATE host_kernels SET applied_checksum=$4,state=$5,reported_at=now() WHERE tenant_id=$1 AND host_id=$2 AND engine=$3 AND desired_checksum=$4`, tenant, host, k.Engine, k.Checksum, k.State); e != nil {
			return e
		}
	}
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
	// Never release a live port until the Agent has acknowledged this stop version.
	rowsToDelete, e := tx.Query(r.Context(), `SELECT id FROM live_services WHERE tenant_id=$1 AND host_id=$2 AND delete_requested_at IS NOT NULL AND NOT enabled AND state='stopped' AND reported_version=version`, tenant, host)
	if e != nil {
		return e
	}
	pending := []string{}
	for rowsToDelete.Next() {
		var id string
		if e = rowsToDelete.Scan(&id); e != nil {
			rowsToDelete.Close()
			return e
		}
		pending = append(pending, id)
	}
	rowsToDelete.Close()
	if e = rowsToDelete.Err(); e != nil {
		return e
	}
	for _, id := range pending {
		if e = purgeLive(r, tx, id); e != nil {
			return e
		}
	}
	var agentVersion string
	if e = tx.QueryRow(r.Context(), "SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2", tenant, host).Scan(&agentVersion); e != nil {
		return e
	}
	rows, e := tx.Query(r.Context(), "SELECT id,version,enabled,sealed,COALESCE(next_id,'') FROM live_services WHERE tenant_id=$1 AND host_id=$2 AND (kind IN ('shadowsocks','frps','frpc') OR $3) ORDER BY created_at LIMIT 64", tenant, host, live.ProtocolCapable(agentVersion))
	if e != nil {
		return e
	}
	work := []liveWork{}
	for rows.Next() {
		var v liveWork
		var sealed []byte
		if e = rows.Scan(&v.ID, &v.Version, &v.Enabled, &sealed, &v.NextID); e != nil {
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
		if hasFRPUDP(v.Spec) && !engineUpdateCapable(agentVersion) {
			v.Enabled = false
		}
		if v.Spec.RequiresV13() && !live.V13Capable(agentVersion) {
			v.Enabled = false
		}
		if v.Spec.RequiresOptions() && !live.OptionsCapable(agentVersion) {
			v.Enabled = false
		}
		if v.Spec.RequiresAdvanced() && !live.AdvancedCapable(agentVersion) {
			continue
		}
		if (v.Spec.ManagedUsers || v.Spec.IsReality() || v.Spec.Transport == "grpc") && (!b.BundleMode || !live.BundleCapable(agentVersion)) {
			v.Enabled = false
		}

		v.Spec.EndpointAddresses = nil
		if len(v.Spec.EgressFamilies) > 0 && !live.NetworkCapable(agentVersion) {
			v.Enabled = false
		}
		work = append(work, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for i := range work {
		if work[i].NextID != "" && live.AdvancedCapable(agentVersion) {
			var nextID string
			var sealed []byte
			if e = tx.QueryRow(r.Context(), "SELECT n.sealed FROM live_services l JOIN live_services n ON (l.tenant_id,l.next_id)=(n.tenant_id,n.id) WHERE l.tenant_id=$1 AND l.id=$2", tenant, work[i].ID).Scan(&sealed); e != nil {
				return e
			}
			_ = nextID
			raw, e := live.Open(s.MasterKey, sealed)
			if e != nil {
				return e
			}
			var next live.Spec
			if e = json.Unmarshal(raw, &next); e != nil {
				return e
			}
			if next.RequiresV13() && !live.V13Capable(agentVersion) {
				work[i].Enabled = false
			}
			next.EndpointAddresses = nil
			next.EgressFamilies = nil
			next.Outbound = nil
			next.Peer = nil
			next.StripServerSecrets()
			work[i].Spec.Outbound = &next
			work[i].Spec.Peer = nil
		}
	}
	compiled := []live.Work{}
	for i := range work {
		if e = s.materializeAccounts(r, tx, work[i].ID, &work[i].Spec); e != nil {
			return e
		}
		if live.Proxy(work[i].Spec.Kind) {
			if _, e = tx.Exec(r.Context(), `INSERT INTO traffic_subjects(tenant_id,host_id,subject_kind,subject_id) VALUES($1,$2,'service',$3) ON CONFLICT DO NOTHING`, tenant, host, work[i].ID); e != nil {
				return e
			}
			for _, a := range work[i].Spec.Accounts {
				if len(a.Name) == 32 {
					if _, e = tx.Exec(r.Context(), `INSERT INTO traffic_subjects(tenant_id,host_id,subject_kind,subject_id) VALUES($1,$2,'user',$3) ON CONFLICT DO NOTHING`, tenant, host, a.Name); e != nil {
						return e
					}
				}
			}
		}
		compiled = append(compiled, live.Work{ID: work[i].ID, Version: work[i].Version, Enabled: work[i].Enabled, Spec: work[i].Spec})
	}
	if b.BundleMode {
		compiled = live.BundleWork(compiled)
		for _, bundle := range compiled {
			if len(bundle.Members) == 0 {
				continue
			}
			raw, _ := json.Marshal(bundle)
			sealed, err := live.Seal(s.MasterKey, raw)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(r.Context(), `INSERT INTO kernel_revisions(tenant_id,host_id,engine,checksum,sealed) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, tenant, host, bundle.Spec.Engine, bundle.Revision(), sealed)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				if _, e = tx.Exec(r.Context(), `DELETE FROM kernel_revisions WHERE tenant_id=$1 AND host_id=$2 AND engine=$3 AND checksum NOT IN (SELECT checksum FROM kernel_revisions WHERE tenant_id=$1 AND host_id=$2 AND engine=$3 ORDER BY created_at DESC LIMIT 20)`, tenant, host, bundle.Spec.Engine); e != nil {
					return e
				}
			}
			if _, e = tx.Exec(r.Context(), `UPDATE host_kernels SET desired_checksum=$4 WHERE tenant_id=$1 AND host_id=$2 AND engine=$3`, tenant, host, bundle.Spec.Engine, bundle.Revision()); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	if b.BundleMode {
		send(w, 200, compiled)
	} else {
		send(w, 200, work)
	}
	return nil
}
func (s *Server) managementOrigin(r *http.Request) (string, error) { return s.siteOrigin(r, false) }
func (s *Server) publicOrigin(r *http.Request) (string, error)     { return s.siteOrigin(r, true) }
func (s *Server) siteOrigin(r *http.Request, share bool) (string, error) {
	var domain, ipOrigin, sd string
	var https, sh bool
	e := s.DB.QueryRow(r.Context(), `SELECT domain,https,ip_origin,share_domain,share_https FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&domain, &https, &ipOrigin, &sd, &sh)
	if e != nil {
		return "", e
	}
	if share && sd != "" && sd != domain {
		domain = sd
		https = sh
	}
	origin := ipOrigin
	if domain != "" {
		scheme := "http://"
		if https {
			scheme = "https://"
		}
		origin = scheme + domain
	}
	if !live.Origin(origin) {
		return "", bad("请配置 IP:端口或站点域名")
	}
	return origin, nil
}
func (s *Server) subscriptionList(w http.ResponseWriter, r *http.Request) error {
	var b json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,user_id,profile_id,expires_at,revoked_at,created_at,version,(revoked_at IS NULL AND expires_at>now()) active,COALESCE((SELECT jsonb_agg(service_id) FROM subscription_services ss WHERE ss.tenant_id=subscriptions.tenant_id AND ss.subscription_id=subscriptions.id),'[]') service_ids,CASE WHEN user_id IS NULL THEN 'shared' ELSE 'user' END mode FROM subscriptions WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200) x`, tenant).Scan(&b)
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
		UserID     string   `json:"user_id"`
		ProfileID  string   `json:"profile_id"`
		ServiceIDs []string `json:"service_ids"`
		Days       int      `json:"days"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Days < 1 || b.Days > 365 || (len(b.ServiceIDs) < 1 && b.UserID == "") || len(b.ServiceIDs) > 64 || strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 {
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
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if b.UserID != "" {
		var expires time.Time
		if e = tx.QueryRow(r.Context(), `SELECT expires_at FROM proxy_users WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now()`, tenant, b.UserID).Scan(&expires); e != nil {
			return e
		}
		b.ServiceIDs = nil
		rows, err := tx.Query(r.Context(), `SELECT DISTINCT s.service_id FROM user_group_services s JOIN user_group_members m USING(tenant_id,group_id) WHERE m.tenant_id=$1 AND m.user_id=$2 LIMIT 64`, tenant, b.UserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			b.ServiceIDs = append(b.ServiceIDs, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if len(b.ServiceIDs) == 0 {
			return bad("assign user to an authorized inbound group first")
		}
	}
	id, token := ID(), ID()+ID()
	sealedToken, e := live.Seal(s.MasterKey, []byte(token))
	if e != nil {
		return e
	}
	_, e = tx.Exec(r.Context(), "INSERT INTO subscriptions(tenant_id,id,name,token_hash,expires_at,user_id,profile_id,sealed_token) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8)", tenant, id, b.Name, Hash(token), time.Now().Add(time.Duration(b.Days)*24*time.Hour), b.UserID, b.ProfileID, sealedToken)
	if e != nil {
		return e
	}
	for _, service := range b.ServiceIDs {
		var kind string
		if e = tx.QueryRow(r.Context(), "SELECT kind FROM live_services WHERE tenant_id=$1 AND id=$2", tenant, service).Scan(&kind); e != nil {
			return e
		}
		if b.UserID == "" {
			spec, err := s.loadSpec(r, service)
			if err != nil {
				return err
			}
			if spec.ManagedUsers {
				return bad("managed inbounds require a user subscription")
			}
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
	send(w, 201, map[string]string{"id": id, "url": origin + "/s/" + token, "notice": "URL contains proxy credentials. Protect it. Revocation stops downloads, not already imported credentials."})
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
	sealedToken, e := live.Seal(s.MasterKey, []byte(token))
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	tag, e := tx.Exec(r.Context(), "UPDATE subscriptions SET token_hash=$3,sealed_token=$4 WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()", tenant, r.PathValue("id"), Hash(token), sealedToken)
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
		Role   string `json:"role"`
		Domain string `json:"domain"`
		HTTPS  bool   `json:"https"`
		State  string `json:"state"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if (b.Domain != "" && !live.Address(b.Domain)) || (b.State != "active" && b.State != "error" && !(b.Role == "subscription" && b.State == "inherited")) {
		return bad("invalid gateway status")
	}
	if b.Role == "subscription" {
		_, e := s.DB.Exec(r.Context(), `UPDATE site_settings SET share_gateway_domain=$2,share_gateway_state=$3,share_gateway_checked_at=now() WHERE tenant_id=$1`, tenant, b.Domain, b.State)
		if e != nil {
			return e
		}
		send(w, 200, map[string]bool{"recorded": true})
		return nil
	}
	if b.Role != "" && b.Role != "management" {
		return bad("unknown gateway role")
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
