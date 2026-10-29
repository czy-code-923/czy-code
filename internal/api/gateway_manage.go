package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"vps-control/internal/live"
)

type reverseInput struct {
	Domain        string         `json:"domain"`
	Upstream      string         `json:"upstream"`
	CertificateID string         `json:"certificate_id"`
	Enabled       bool           `json:"enabled"`
	Version       int            `json:"version"`
	Options       map[string]any `json:"options"`
}

func validateReverseInput(b reverseInput) error {
	u, e := url.Parse(b.Upstream)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" || !live.Address(u.Hostname()) || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Path, ";{}\\\r\n\t\"$ ") {
		return bad("上游需为合法 http(s)://公网域名或显式回环应用地址，不接受凭据、查询串或配置指令")
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		port, e = strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return bad("invalid upstream port")
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		if ip.IsLoopback() {
			if u.Scheme != "http" || port < 1024 {
				return bad("local upstream requires an unprivileged HTTP port")
			}
			for _, p := range []int{8080, 8091, 5173, 15432, 5432, 2375} {
				if p == port {
					return bad("reserved upstream port")
				}
			}
		} else if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return bad("private upstream denied")
		}
	}
	for key, value := range b.Options {
		switch key {
		case "connect_timeout", "read_timeout", "body_limit_mib":
			n, ok := value.(float64)
			max := 600.0
			if key == "connect_timeout" {
				max = 60
			}
			if key == "body_limit_mib" {
				max = 64
			}
			if !ok || n < 1 || n > max || n != float64(int(n)) {
				return bad("invalid reverse proxy numeric option")
			}
		case "websocket":
			if _, ok := value.(bool); !ok {
				return bad("websocket must be boolean")
			}
		case "host_header":
			v, ok := value.(string)
			if !ok || !validDomain(v) {
				return bad("host_header must be a DNS domain")
			}
		default:
			return bad("unsupported reverse option: " + key)
		}
	}
	return nil
}
func (s *Server) reverseManage(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT p.*,'nginx' engine FROM reverse_proxies p WHERE tenant_id=$1 AND ($2='' OR id=$2) ORDER BY domain LIMIT 100)x`, tenant, id).Scan(&raw)
		if e != nil {
			return e
		}
		if id != "" {
			var a []map[string]any
			json.Unmarshal(raw, &a)
			if len(a) == 0 {
				return pgx.ErrNoRows
			}
			send(w, 200, a[0])
		} else {
			send(w, 200, raw)
		}
		return nil
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if r.Method == "DELETE" {
		tag, e := tx.Exec(r.Context(), `UPDATE reverse_proxies SET enabled=false,deleted_at=COALESCE(deleted_at,now()),version=version+CASE WHEN deleted_at IS NULL THEN 1 ELSE 0 END,state='pending' WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if e = audit(r.Context(), tx, "admin", "reverse-proxy.delete-request", id); e != nil {
			return e
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
		send(w, 202, map[string]bool{"pending_cleanup": true})
		return nil
	}
	var b reverseInput
	if e = decode(w, r, &b); e != nil {
		return e
	}
	b.Domain = strings.ToLower(strings.TrimSpace(b.Domain))
	if !validDomain(b.Domain) {
		return bad("valid proxy domain required")
	}
	if e = validateReverseInput(b); e != nil {
		return e
	}
	var reserved bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM site_settings WHERE tenant_id=$1 AND (domain=$2 OR share_domain=$2))`, tenant, b.Domain).Scan(&reserved); e != nil {
		return e
	}
	if reserved {
		return bad("管理和订阅域名不能被反代覆盖")
	}
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM reverse_proxies WHERE tenant_id=$1 AND id<>$2 AND (domain=$3 OR applied_domain=$3 OR $3=ANY(cleanup_domains)))`, tenant, id, b.Domain).Scan(&reserved); e != nil {
		return e
	}
	if reserved {
		return problem{409, "此域名仍由其他反代占用或等待清理"}
	}
	var domain, state string
	if e = tx.QueryRow(r.Context(), `SELECT domain,state FROM managed_certificates WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, b.CertificateID).Scan(&domain, &state); e != nil {
		return e
	}
	if domain != b.Domain || b.Enabled && state != "ready" {
		return bad("启用前需要域名匹配且就绪的证书")
	}
	options, _ := json.Marshal(b.Options)
	if b.Options == nil {
		options = []byte(`{}`)
	}
	status := 200
	if r.Method == "POST" {
		id = ID()
		status = 201
		_, e = tx.Exec(r.Context(), `INSERT INTO reverse_proxies(tenant_id,id,domain,upstream,certificate_id,enabled,options) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, id, b.Domain, b.Upstream, b.CertificateID, b.Enabled, options)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, e = tx.Exec(r.Context(), `UPDATE reverse_proxies SET cleanup_domains=CASE WHEN domain<>$3 THEN array_append(cleanup_domains,domain) ELSE cleanup_domains END,domain=$3,upstream=$4,certificate_id=$5,enabled=$6,options=$7,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2 AND version=$8 AND deleted_at IS NULL AND cardinality(cleanup_domains)<16`, tenant, id, b.Domain, b.Upstream, b.CertificateID, b.Enabled, options, b.Version)
		if e == nil && tag.RowsAffected() == 0 {
			return problem{409, "configuration changed; refresh"}
		}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "reverse-proxy.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, status, map[string]string{"id": id})
	return nil
}

func (s *Server) certificateManage(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	id := r.PathValue("id")
	var oldDomain, provider, email, state string
	var version int
	var credentials []byte
	if e = tx.QueryRow(r.Context(), `SELECT domain,provider,email,state,version,sealed_credentials FROM managed_certificates WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, id).Scan(&oldDomain, &provider, &email, &state, &version, &credentials); e != nil {
		return e
	}
	var refs bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND certificate_id=$2) OR EXISTS(SELECT 1 FROM reverse_proxies WHERE tenant_id=$1 AND certificate_id=$2)`, tenant, id).Scan(&refs); e != nil {
		return e
	}
	if r.Method == "DELETE" {
		if refs {
			return problem{409, "证书仍被节点或反代引用，请先解除引用；反代删除需等待网关确认"}
		}
		_, e = tx.Exec(r.Context(), `UPDATE managed_certificates SET deleted_at=now(),version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id)
	} else {
		var b struct {
			Domain      string `json:"domain"`
			Provider    string `json:"provider"`
			Email       string `json:"email"`
			DNSToken    string `json:"dns_token"`
			Certificate string `json:"certificate"`
			PrivateKey  string `json:"private_key"`
			Version     int    `json:"version"`
		}
		if e = decode(w, r, &b); e != nil {
			return e
		}
		b.Domain = strings.ToLower(strings.TrimSpace(b.Domain))
		if b.Version != 0 && b.Version != version {
			return problem{409, "certificate changed; refresh"}
		}
		if !validDomain(b.Domain) {
			return bad("valid domain required")
		}
		if b.Domain != oldDomain && refs {
			return problem{409, "域名变更前请先解除节点和反代引用"}
		}
		if b.Domain != oldDomain {
			tag, err := tx.Exec(r.Context(), `UPDATE managed_certificates SET cleanup_domains=array_append(cleanup_domains,domain) WHERE tenant_id=$1 AND id=$2 AND cardinality(cleanup_domains)<16`, tenant, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return problem{409, "请等待网关清理历史域名后再修改"}
			}
		}
		if b.Provider == "manual" {
			if (b.Certificate == "") != (b.PrivateKey == "") || b.Certificate == "" && (provider != "manual" || state != "ready") {
				return bad("需同时提供证书与私钥；仅已就绪手动证书允许留空保留")
			}
			if b.DNSToken != "" {
				return bad("manual certificate does not use a DNS token")
			}
			if b.Certificate == "" && b.Domain != oldDomain {
				return bad("新域名需要匹配的证书和私钥")
			}
			_, e = tx.Exec(r.Context(), `UPDATE managed_certificates SET domain=$3,provider='manual',email='',sealed_credentials=NULL,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id, b.Domain)
			if e == nil && b.Certificate != "" {
				e = s.saveManagedCertificate(r, tx, id, b.Domain, certificatePair{b.Certificate, b.PrivateKey})
			}
		} else {
			address, err := mail.ParseAddress(b.Email)
			if err != nil || address.Address != b.Email || len(b.Email) > 200 || (b.Provider != "http-01" && b.Provider != "cloudflare") {
				return bad("valid email and ACME provider required")
			}
			if b.Provider == "cloudflare" {
				if b.DNSToken != "" {
					if len(b.DNSToken) < 20 || len(b.DNSToken) > 256 || strings.ContainsAny(b.DNSToken, "\r\n \t") {
						return bad("invalid scoped DNS token")
					}
					credentials, e = live.Seal(s.MasterKey, []byte(b.DNSToken))
				} else if provider != "cloudflare" || len(credentials) == 0 {
					return bad("DNS token required")
				}
			} else {
				credentials = nil
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE managed_certificates SET domain=$3,provider=$4,email=$5,sealed_credentials=$6,state='pending',result='',last_attempt=NULL,version=version+1,sealed=CASE WHEN domain=$3 THEN sealed ELSE NULL END WHERE tenant_id=$1 AND id=$2`, tenant, id, b.Domain, b.Provider, b.Email, credentials)
			}
		}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "certificate.manage", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	status := 200
	if r.Method == "DELETE" {
		status = 202
	}
	send(w, status, map[string]string{"id": id})
	return nil
}
