package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"vps-control/internal/live"
)

func (s *Server) checkChain(r *http.Request, tx pgx.Tx, id, nextID string) error {
	seen := map[string]bool{id: true}
	depth := 0
	for nextID != "" {
		if seen[nextID] || depth >= 7 {
			return bad("chain cycle or more than 8 hops")
		}
		seen[nextID] = true
		depth++
		var kind string
		if e := tx.QueryRow(r.Context(), "SELECT kind,COALESCE(next_id,'') FROM live_services WHERE tenant_id=$1 AND id=$2 AND delete_requested_at IS NULL", tenant, nextID).Scan(&kind, &nextID); e != nil {
			return e
		}
		if !live.Proxy(kind) {
			return bad("chain requires proxy nodes")
		}
	}
	return nil
}
func (s *Server) ensureNode(r *http.Request, tx pgx.Tx, id, name string, spec live.Spec) (string, error) {
	engine := spec.Engine
	if engine == "" {
		engine = "sing-box"
	}
	if !live.Proxy(spec.Kind) {
		engine = "frp"
	}
	if id == "" {
		id = ID()
		_, e := tx.Exec(r.Context(), "INSERT INTO nodes(tenant_id,id,name,engine) VALUES($1,$2,$3,$4)", tenant, id, name, engine)
		return id, e
	}
	var actual string
	e := tx.QueryRow(r.Context(), "SELECT engine FROM nodes WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, id).Scan(&actual)
	if e != nil {
		return "", e
	}
	if actual != engine {
		return "", bad("logical node kernel must match deployment kernel")
	}
	return id, nil
}
func (s *Server) applyCertificate(r *http.Request, id string, b *liveInput) error {
	var sealed []byte
	if e := s.DB.QueryRow(r.Context(), "SELECT sealed FROM managed_certificates WHERE tenant_id=$1 AND id=$2 AND state='ready' AND expires_at>now()", tenant, id).Scan(&sealed); e != nil {
		return e
	}
	raw, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return e
	}
	var pair certificatePair
	if e = json.Unmarshal(raw, &pair); e != nil {
		return e
	}
	b.Certificate = pair.Certificate
	b.PrivateKey = pair.PrivateKey
	return nil
}
func redactLive(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			switch strings.ToLower(k) {
			case "password", "uuid", "id", "psk", "key", "token", "privatekey", "private_key", "user_key", "userkey", "hy2_obfs_password", "hysteria_obfs_password", "auth_str", "auth", "proxy":
				v[k] = "[已隐藏]"
			default:
				v[k] = redactLive(x)
			}
		}
	case []any:
		for i, x := range v {
			v[i] = redactLive(x)
		}
	}
	return value
}
func (s *Server) liveDetail(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	spec, e := s.loadSpec(r, r.PathValue("id"))
	if e != nil {
		return e
	}
	var name, host, node, cert, next, parent string
	var version int
	if e = s.DB.QueryRow(r.Context(), "SELECT name,host_id,COALESCE(node_id,''),COALESCE(certificate_id,''),COALESCE(next_id,''),COALESCE(parent_id,''),version FROM live_services WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id")).Scan(&name, &host, &node, &cert, &next, &parent, &version); e != nil {
		return e
	}
	engine, files, e := spec.Render()
	if e != nil {
		return e
	}
	var preview any
	if e = json.Unmarshal(files["config.json"], &preview); e != nil {
		return e
	}
	spec.HY2Obfs = ""
	spec.HysteriaObfs = ""
	spec.Password = ""
	spec.UUID = ""
	spec.PrivateKey = ""
	spec.Reality = spec.PublicReality()
	if spec.Peer != nil {
		spec.Peer.Password = ""
	}
	if spec.Outbound != nil {
		spec.Outbound.Password = ""
		spec.Outbound.UUID = ""
		spec.Outbound.StripServerSecrets()
	}
	send(w, 200, map[string]any{"id": r.PathValue("id"), "name": name, "host_id": host, "node_id": node, "certificate_id": cert, "next_id": next, "parent_id": parent, "version": version, "spec": spec, "engine": engine, "rendered": redactLive(preview)})
	return nil
}
func (s *Server) liveUpdate(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b liveInput
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || b.Version < 1 {
		return bad("name and expected version required")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(717071)"); e != nil {
		return e
	}
	var sealed []byte
	var version int
	var host, kind string
	var enabled bool
	if e = tx.QueryRow(r.Context(), "SELECT sealed,version,host_id,kind,enabled FROM live_services WHERE tenant_id=$1 AND id=$2 AND delete_requested_at IS NULL FOR UPDATE", tenant, r.PathValue("id")).Scan(&sealed, &version, &host, &kind, &enabled); e != nil {
		return e
	}
	if version != b.Version {
		return problem{409, "configuration changed; refresh before saving"}
	}
	if b.Kind != kind || b.HostID != host {
		return bad("host and protocol are immutable; create a replacement deployment")
	}
	if !live.Proxy(kind) {
		plain, e := live.Open(s.MasterKey, sealed)
		if e != nil {
			return e
		}
		return s.updateFRP(w, r, tx, b, plain, enabled)
	}
	raw, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return e
	}
	var spec live.Spec
	if e = json.Unmarshal(raw, &spec); e != nil {
		return e
	}
	if b.ClearHysteriaObfs {
		b.HysteriaObfs = ""
	}
	if !b.ClearHysteriaObfs && b.HysteriaObfs == "" && b.Kind == "hysteria" {
		b.HysteriaObfs = spec.HysteriaObfs
	}
	if b.ClearHY2Obfs {
		b.HY2Obfs = ""
	}
	if !b.ClearHY2Obfs && b.HY2Obfs == "" && b.Kind == "hysteria2" {
		b.HY2Obfs = spec.HY2Obfs
	}
	spec.Engine = b.Engine
	spec.Listen = b.Listen
	spec.Port = b.Port
	spec.Address = b.Address
	if e := s.applyNetwork(r, tx, b, &spec); e != nil {
		return e
	}
	spec.Transport = b.Transport
	spec.Path = b.Path
	spec.TLSMode = b.TLSMode
	if e := applySecurity(&spec, b); e != nil {
		return e
	}
	spec.ServerName = b.ServerName
	spec.Peer = nil
	spec.Outbound = nil
	if b.RotateCredentials && (b.Password != "" || b.UUID != "") {
		return bad("choose random rotation or manual credentials")
	}
	if b.Password != "" {
		spec.Password = b.Password
	}
	if b.UUID != "" {
		if kind != "vless" && kind != "vmess" && kind != "tuic" {
			return bad("UUID not applicable")
		}
		spec.UUID = b.UUID
	}
	if b.RotateCredentials {
		spec.Password = live.Secret()
		if kind == "shadowsocks" {
			spec.Password = live.CipherSecret(spec.SSMethod())
		}
		if kind == "vless" || kind == "tuic" || kind == "vmess" {
			spec.UUID = live.UUID()
		}
	}
	if b.CertificateID != "" {
		if e = s.applyCertificate(r, b.CertificateID, &b); e != nil {
			return e
		}
	}
	if b.Certificate != "" || b.PrivateKey != "" {
		if b.Certificate == "" || b.PrivateKey == "" {
			return bad("certificate and key must be replaced together")
		}
		spec.Certificate = b.Certificate
		spec.PrivateKey = b.PrivateKey
	}
	if !spec.UsesTLS() {
		spec.Certificate = ""
		spec.PrivateKey = ""
		if !spec.IsReality() {
			spec.ServerName = ""
		}
	}
	if e = s.checkChain(r, tx, r.PathValue("id"), b.NextID); e != nil {
		return e
	}
	if b.NextID != "" {
		next, e := s.loadSpec(r, b.NextID)
		if e != nil {
			return e
		}
		next.Outbound = nil
		next.Peer = nil
		next.StripServerSecrets()
		spec.Outbound = &next
	}
	if e = spec.Validate(); e != nil {
		return bad(e.Error())
	}
	if spec.UsesTLS() {
		if e = spec.VerifyPublicCertificate(); e != nil {
			return bad(e.Error())
		}
	}
	var av string
	if e = tx.QueryRow(r.Context(), "SELECT agent_version FROM hosts WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, host).Scan(&av); e != nil {
		return e
	}
	if spec.RequiresV19() && !live.V19Capable(av) {
		return problem{409, "upgrade host to Agent 0.19.0 for native listener selection"}
	}
	if spec.RequiresV13() && !live.V13Capable(av) {
		return problem{409, "upgrade host to Agent 0.13.0 for this protocol/transport"}
	}
	if spec.RequiresOptions() && !live.OptionsCapable(av) {
		return problem{409, "upgrade host to Agent 0.9.0 for protocol options"}
	}
	if spec.ManagedUsers && spec.SharedUserProtocol() && !live.ManagedSharedCapable(av) {
		return problem{409, "独立 SS/Snell 用户计量需要先升级主机 Agent 到 0.23.0"}
	}
	if (spec.ManagedUsers || spec.IsReality() || spec.Transport == "grpc") && !live.BundleCapable(av) {
		return problem{409, "upgrade host to Agent 0.8.0 first"}
	}
	if !live.AdvancedCapable(av) {
		return problem{409, "upgrade host to Agent 0.7.0 before editing configuration"}
	}
	if !spec.ManagedUsers && !sharedGroupProtocol(spec) {
		var grouped bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_group_services WHERE tenant_id=$1 AND service_id=$2)`, tenant, r.PathValue("id")).Scan(&grouped); e != nil {
			return e
		}
		if grouped {
			return bad("remove inbound from user groups before disabling managed users")
		}
	}
	// An upstream edit must not make existing Xray dependents impossible to render.
	rows, e := tx.Query(r.Context(), "SELECT l.sealed,h.agent_version FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.next_id=$2", tenant, r.PathValue("id"))
	if e != nil {
		return e
	}
	for rows.Next() {
		var data []byte
		var parentVersion string
		if e = rows.Scan(&data, &parentVersion); e != nil {
			rows.Close()
			return e
		}
		if spec.RequiresV19() && !live.V19Capable(parentVersion) {
			rows.Close()

			return problem{409, "upgrade host to Agent 0.19.0 for native listener selection"}
		}
		if spec.RequiresV13() && !live.V13Capable(parentVersion) {
			rows.Close()
			return problem{409, "upgrade dependent chain hosts to Agent 0.13.0"}
		}
		if spec.RequiresOptions() && !live.OptionsCapable(parentVersion) {
			rows.Close()
			return problem{409, "upgrade dependent chain hosts to Agent 0.9.0 for protocol options"}
		}
		if !live.AdvancedCapable(parentVersion) {
			rows.Close()
			return problem{409, "upgrade dependent chain hosts to Agent 0.7.0 before editing upstream"}
		}
		plain, e := live.Open(s.MasterKey, data)
		if e != nil {
			rows.Close()
			return e
		}
		var parent live.Spec
		if e = json.Unmarshal(plain, &parent); e != nil {
			rows.Close()
			return e
		}
		next := spec
		next.Outbound = nil
		next.StripServerSecrets()
		parent.Peer = nil
		parent.Outbound = &next
		if e = parent.Validate(); e != nil {
			rows.Close()
			return bad("upstream edit incompatible with existing chain")
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	node, e := s.ensureNode(r, tx, b.NodeID, b.Name, spec)
	if e != nil {
		return e
	}
	var collision bool
	if e = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM port_reservations WHERE tenant_id=$1 AND host_id=$2 AND port=$3)", tenant, host, spec.Port).Scan(&collision); e != nil {
		return e
	}
	if collision {
		return problem{409, "port reserved by inventory instance"}
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM live_ports WHERE tenant_id=$1 AND service_id=$2", tenant, r.PathValue("id")); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "INSERT INTO live_ports VALUES($1,$2,$3,$4)", tenant, host, spec.Port, r.PathValue("id")); e != nil {
		return e
	}
	raw, _ = json.Marshal(spec)
	sealed, e = live.Seal(s.MasterKey, raw)
	if e != nil {
		return e
	}
	_, e = tx.Exec(r.Context(), "UPDATE live_services SET name=$3,node_id=$4,engine=$5,address=$6,port=$7,next_id=NULLIF($8,''),certificate_id=NULLIF($9,''),sealed=$10,managed_users=$11,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2", tenant, r.PathValue("id"), b.Name, node, map[bool]string{true: "xray", false: "sing-box"}[spec.Engine == "xray"], spec.Address, spec.Port, b.NextID, b.CertificateID, sealed, spec.ManagedUsers)
	if e != nil {
		return e
	}
	if e = s.bumpParents(r, tx, r.PathValue("id")); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "live.update", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"version": version + 1, "enabled": enabled})
	return nil
}
func (s *Server) bumpParents(r *http.Request, tx pgx.Tx, id string) error {
	_, e := tx.Exec(r.Context(), `WITH RECURSIVE parents AS (SELECT id,next_id,1 depth FROM live_services WHERE tenant_id=$1 AND next_id=$2 UNION ALL SELECT l.id,l.next_id,p.depth+1 FROM live_services l JOIN parents p ON l.next_id=p.id WHERE l.tenant_id=$1 AND p.depth<8) UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id IN (SELECT id FROM parents)`, tenant, id)
	return e
}
