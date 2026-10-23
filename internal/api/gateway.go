package api

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"vps-control/internal/live"
)

type certificatePair struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

func certificateExpiry(value string) (time.Time, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return time.Time{}, pgx.ErrNoRows
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return time.Time{}, e
	}
	return cert.NotAfter, nil
}
func validDomain(d string) bool {
	return live.Address(d) && net.ParseIP(d) == nil && strings.Contains(d, ".") && !strings.ContainsAny(d, "/:*\r\n") && d == strings.ToLower(d)
}
func (s *Server) certificates(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	if r.Method == "GET" {
		var b json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,domain,provider,email,state,expires_at,last_attempt,result FROM managed_certificates WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200)x`, tenant).Scan(&b)
		if e != nil {
			return e
		}
		send(w, 200, b)
		return nil
	}
	var b struct {
		Domain      string `json:"domain"`
		Provider    string `json:"provider"`
		Email       string `json:"email"`
		DNSToken    string `json:"dns_token"`
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"private_key"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	b.Domain = strings.ToLower(strings.TrimSpace(b.Domain))
	if b.Provider == "manual" || r.Method == "PUT" {
		if !validDomain(b.Domain) || b.Provider != "manual" || b.DNSToken != "" {
			return bad("valid domain and manual certificate provider required")
		}
		tx, e := s.DB.Begin(r.Context())
		if e != nil {
			return e
		}
		defer tx.Rollback(r.Context())
		if _, e = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(717071)"); e != nil {
			return e
		}
		id := r.PathValue("id")
		status := 200
		if r.Method == "POST" {
			id = ID()
			status = 201
			if _, e = tx.Exec(r.Context(), "INSERT INTO managed_certificates(tenant_id,id,domain,provider,email) VALUES($1,$2,$3,'manual','')", tenant, id, b.Domain); e != nil {
				return e
			}
		} else {
			var domain, provider string
			if e = tx.QueryRow(r.Context(), "SELECT domain,provider FROM managed_certificates WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, id).Scan(&domain, &provider); e != nil {
				return e
			}
			if domain != b.Domain || provider != "manual" {
				return bad("only the same manually managed domain can be updated")
			}
		}
		if e = s.saveManagedCertificate(r, tx, id, b.Domain, certificatePair{b.Certificate, b.PrivateKey}); e != nil {
			return e
		}
		if e = audit(r.Context(), tx, "admin", "certificate.import", id); e != nil {
			return e
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
		send(w, status, map[string]string{"id": id, "state": "ready"})
		return nil
	}
	address, e := mail.ParseAddress(b.Email)
	if !validDomain(b.Domain) || e != nil || address.Address != b.Email || len(b.Email) > 200 || (b.Provider != "http-01" && b.Provider != "cloudflare") {
		return bad("valid domain, email and http-01/cloudflare provider required")
	}
	if b.Provider == "cloudflare" && (len(b.DNSToken) < 20 || len(b.DNSToken) > 256 || strings.ContainsAny(b.DNSToken, "\r\n \t")) {
		return bad("scoped Cloudflare DNS API token required")
	}
	if b.Provider == "http-01" && b.DNSToken != "" {
		return bad("HTTP challenge does not use a DNS token")
	}
	secret, e := live.Seal(s.MasterKey, []byte(b.DNSToken))
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	id := ID()
	if _, e = tx.Exec(r.Context(), "INSERT INTO managed_certificates(tenant_id,id,domain,provider,email,sealed_credentials) VALUES($1,$2,$3,$4,$5,$6)", tenant, id, b.Domain, b.Provider, b.Email, secret); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "certificate.request", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 201, map[string]any{"id": id, "state": "pending"})
	return nil
}
func (s *Server) certificateRetry(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	tag, e := tx.Exec(r.Context(), "UPDATE managed_certificates SET state='pending',result='' WHERE tenant_id=$1 AND id=$2 AND provider<>'manual' AND state IN ('ready','failed') AND (last_attempt IS NULL OR last_attempt<now()-interval '5 minutes')", tenant, r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return problem{409, "request running, missing, or retry interval under 5 minutes"}
	}
	if e = audit(r.Context(), tx, "admin", "certificate.retry", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"queued": true})
	return nil
}
func (s *Server) gatewayWork(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	// One bounded worker owns a claim for fifteen minutes. Secrets only go to this authenticated local root helper.
	rows, e := tx.Query(r.Context(), `UPDATE managed_certificates SET state='running',last_attempt=now() WHERE (tenant_id,id) IN (SELECT tenant_id,id FROM managed_certificates WHERE tenant_id=$1 AND provider<>'manual' AND (state='pending' OR state='running' AND last_attempt<now()-interval '15 minutes' OR state='ready' AND expires_at<now()+interval '30 days' AND last_attempt<now()-interval '24 hours') ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 4) RETURNING id,domain,provider,email,sealed_credentials`, tenant)
	if e != nil {
		return e
	}
	jobs := []map[string]any{}
	for rows.Next() {
		var id, domain, provider, email string
		var sealed []byte
		if e = rows.Scan(&id, &domain, &provider, &email, &sealed); e != nil {
			rows.Close()
			return e
		}
		raw, e := live.Open(s.MasterKey, sealed)
		if e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, map[string]any{"id": id, "domain": domain, "provider": provider, "email": email, "dns_token": string(raw)})
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	var proxies json.RawMessage
	if e = tx.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT p.*,c.domain AS certificate_domain,c.state AS certificate_state,encode(c.sealed,'base64') AS sealed FROM reverse_proxies p JOIN managed_certificates c ON (p.tenant_id,p.certificate_id)=(c.tenant_id,c.id) WHERE p.tenant_id=$1 ORDER BY p.id LIMIT 100)x`, tenant).Scan(&proxies); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	var decoded []map[string]any
	if e = json.Unmarshal(proxies, &decoded); e != nil {
		return e
	}
	for _, proxy := range decoded {
		if encoded, ok := proxy["sealed"].(string); ok && proxy["enabled"] == true && proxy["certificate_state"] == "ready" {
			sealed, e := base64.StdEncoding.DecodeString(encoded)
			if e != nil {
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
			proxy["certificate"] = pair.Certificate
			proxy["private_key"] = pair.PrivateKey
		}
		delete(proxy, "sealed")
	}
	send(w, 200, map[string]any{"certificates": jobs, "reverse_proxies": decoded})
	return nil
}
func (s *Server) gatewayReport(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var b struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		State       string `json:"state"`
		Version     int    `json:"version"`
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"private_key"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.Kind == "reverse-proxy" {
		if b.State != "active" && b.State != "disabled" && b.State != "failed" {
			return bad("invalid state")
		}
		_, e := s.DB.Exec(r.Context(), "UPDATE reverse_proxies SET state=$3,reported_version=$4 WHERE tenant_id=$1 AND id=$2 AND version=$4", tenant, b.ID, b.State, b.Version)
		if e != nil {
			return e
		}
		send(w, 200, map[string]bool{"recorded": true})
		return nil
	}
	if b.Kind != "certificate" {
		return bad("invalid report kind")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(717071)"); e != nil {
		return e
	}
	var domain string
	if e = tx.QueryRow(r.Context(), "SELECT domain FROM managed_certificates WHERE tenant_id=$1 AND id=$2 AND state='running' FOR UPDATE", tenant, b.ID).Scan(&domain); e != nil {
		return e
	}
	if b.State == "failed" {
		_, e = tx.Exec(r.Context(), "UPDATE managed_certificates SET state='failed',result='ACME failed; check DNS, challenge reachability and private helper journal' WHERE tenant_id=$1 AND id=$2", tenant, b.ID)
	} else if b.State == "ready" {
		e = s.saveManagedCertificate(r, tx, b.ID, domain, certificatePair{b.Certificate, b.PrivateKey})
	} else {
		return bad("invalid certificate state")
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "gateway", "certificate."+b.State, b.ID); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"recorded": true})
	return nil
}
func (s *Server) reverseProxies(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), "SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT * FROM reverse_proxies WHERE tenant_id=$1 ORDER BY domain LIMIT 100)x", tenant).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	var b struct {
		Domain        string `json:"domain"`
		Upstream      string `json:"upstream"`
		CertificateID string `json:"certificate_id"`
		Enabled       bool   `json:"enabled"`
		Version       int    `json:"version"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	b.Domain = strings.ToLower(b.Domain)
	u, e := url.Parse(b.Upstream)
	if e != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
		return bad("upstream must be http://127.0.0.1:port or http://[::1]:port, including FRP local endpoints")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1024 || port > 65535 || port == 8080 || port == 8091 || port == 5173 || port == 15432 || port == 5432 || port == 2375 {
		return bad("invalid or reserved upstream port")
	}
	if !validDomain(b.Domain) {
		return bad("valid DNS domain required")
	}
	var management string
	if e = s.DB.QueryRow(r.Context(), "SELECT domain FROM site_settings WHERE tenant_id=$1", tenant).Scan(&management); e != nil {
		return e
	}
	if b.Domain == management {
		return bad("reverse proxy cannot replace management domain")
	}
	var certDomain, state string
	if e = s.DB.QueryRow(r.Context(), "SELECT domain,state FROM managed_certificates WHERE tenant_id=$1 AND id=$2", tenant, b.CertificateID).Scan(&certDomain, &state); e != nil {
		return e
	}
	if certDomain != b.Domain || b.Enabled && state != "ready" {
		return bad("matching ready certificate required before activation")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	status := 200
	if r.Method == "POST" {
		id = ID()
		status = 201
		_, e = tx.Exec(r.Context(), "INSERT INTO reverse_proxies(tenant_id,id,domain,upstream,certificate_id,enabled) VALUES($1,$2,$3,$4,$5,$6)", tenant, id, b.Domain, b.Upstream, b.CertificateID, b.Enabled)
	} else {
		var current string
		if e = tx.QueryRow(r.Context(), "SELECT domain FROM reverse_proxies WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, id).Scan(&current); e != nil {
			return e
		}
		if current != b.Domain {
			return bad("domain immutable; create another proxy")
		}
		result, err := tx.Exec(r.Context(), "UPDATE reverse_proxies SET upstream=$3,certificate_id=$4,enabled=$5,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2 AND version=$6", tenant, id, b.Upstream, b.CertificateID, b.Enabled, b.Version)
		e = err
		if e == nil && result.RowsAffected() == 0 {
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

func (s *Server) saveManagedCertificate(r *http.Request, tx pgx.Tx, id, domain string, pair certificatePair) error {
	var e error
	spec := live.Spec{Kind: "trojan", Address: domain, Listen: "127.0.0.1", Port: 25000, Password: live.Secret(), ServerName: domain, Certificate: pair.Certificate, PrivateKey: pair.PrivateKey}
	if e = spec.Validate(); e != nil {
		return bad("invalid certificate/key")
	}
	if e = spec.VerifyPublicCertificate(); e != nil {
		return bad(e.Error())
	}
	expires, e := certificateExpiry(pair.Certificate)
	if e != nil {
		return bad("invalid certificate")
	}
	raw, _ := json.Marshal(certificatePair{pair.Certificate, pair.PrivateKey})
	sealed, e := live.Seal(s.MasterKey, raw)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "UPDATE managed_certificates SET sealed=$3,state='ready',expires_at=$4,result='' WHERE tenant_id=$1 AND id=$2", tenant, id, sealed, expires); e != nil {
		return e
	}
	rows, e := tx.Query(r.Context(), "SELECT id,sealed FROM live_services WHERE tenant_id=$1 AND certificate_id=$2", tenant, id)
	if e != nil {
		return e
	}
	type change struct {
		id     string
		sealed []byte
	}
	changes := []change{}
	for rows.Next() {
		var id string
		var sealed []byte
		if e = rows.Scan(&id, &sealed); e != nil {
			rows.Close()
			return e
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
		if spec.ServerName != domain {
			rows.Close()
			return bad("certificate binding mismatch")
		}
		spec.Certificate = pair.Certificate
		spec.PrivateKey = pair.PrivateKey
		raw, _ = json.Marshal(spec)
		sealed, e = live.Seal(s.MasterKey, raw)
		if e != nil {
			rows.Close()
			return e
		}
		changes = append(changes, change{id, sealed})
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, v := range changes {
		if _, e = tx.Exec(r.Context(), "UPDATE live_services SET sealed=$3,version=version+1,state='pending' WHERE tenant_id=$1 AND id=$2", tenant, v.id, v.sealed); e != nil {
			return e
		}
		if e = s.bumpParents(r, tx, v.id); e != nil {
			return e
		}
	}
	return nil
}
