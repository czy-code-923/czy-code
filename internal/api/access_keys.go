package api

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"vps-control/internal/buildinfo"
)

type apiIdentity struct {
	ID      string
	Scopes  []string
	Expires time.Time
	Root    bool
}
type identityContext struct{}
type scopeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Sensitive bool   `json:"sensitive"`
}

var apiScopes = []scopeInfo{
	{"commerce:read", "��ѯ��ҵ�ɼƷѽڵ���", false}, {"commerce:write", "��ͨ�����Ѻ���ͣ��ҵ�û�Ȩ��", true},
	{"devices:read", "�鿴�û��豸��ͻ���Ԫ����", true}, {"devices:write", "�Ǽǡ������������û��豸", true}, {"devices:links", "��ȡ�豸ר������ƾ��", true},
	{"kernels:read", "�鿴�ٷ��ں˰汾�����״̬", false}, {"kernels:update", "��װ���л��ٷ��ں˰汾", true},
	{"notifications:read", "�鿴֪ͨ��������", true}, {"notifications:write", "��֪ͨ����������Ȩ��ѯ", true},
	{"hosts:uninstall", "ж��Զ�� Agent ��ɾ��������������", true}, {"hosts:read", "�鿴����", false}, {"hosts:write", "�޸���ɾ������", true}, {"hosts:enroll", "����������ƾ��", true},
	{"nodes:read", "�鿴Э�����ں�����", false}, {"nodes:write", "�޸ġ���������ͣЭ��", true},
	{"users:read", "�鿴�û�", false}, {"users:write", "�޸��û������", true}, {"groups:read", "�鿴�û���", false}, {"groups:write", "�޸��û���Ȩ", true},
	{"usage:read", "�鿴����ͳ��", false}, {"usage:reset", "�����û�����", true}, {"access:read", "�鿴��Դ IP �����Ŀ��", true},
	{"subscriptions:read", "�鿴�������������", false}, {"subscriptions:write", "�޸��볷������", true}, {"subscriptions:links", "��ȡ�������ӺͿͻ���ƾ��", true},
	{"profiles:read", "�鿴�ͻ���ģ��", false}, {"profiles:write", "�޸Ŀͻ���ģ��", true},
	{"site:read", "�鿴վ������", false}, {"site:write", "�޸�������HTTPS �������Դ", true},
	{"certificates:read", "�鿴֤��״̬", false}, {"certificates:write", "����֤��", true}, {"reverse:read", "�鿴����", false}, {"reverse:write", "��������", true},
	{"firewall:read", "�鿴����ǽ����", false}, {"firewall:write", "��������ǽ����", true}, {"audit:read", "�鿴���", true},
	{"inventory:read", "�鿴�߼������汾", false}, {"inventory:write", "�޸ĸ߼����������", true}, {"tasks:read", "�鿴��������", false}, {"tasks:write", "���л�ȡ�����޲�������", true},
}
var keyPattern = regexp.MustCompile(`^xs_[a-f0-9]{64}$`)

type rateWindow struct {
	Start time.Time
	Count int
}
type keyLimiter struct {
	sync.Mutex
	Windows map[string]rateWindow
}

// Only explicit routes are delegable. Unknown routes and key administration stay root-only.
func requiredScopes(r *http.Request) []string {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	a := strings.Split(p, "/")
	write := r.Method != "GET" && r.Method != "HEAD"
	mode := ":read"
	if write {
		mode = ":write"
	}
	switch a[0] {
	case "commerce":
		if len(a) > 1 && a[1] == "validate" {
			return []string{"commerce:read"}
		}
		return []string{"commerce" + mode}
	case "configuration":
		return []string{"nodes:write"}
	case "notifications":
		return []string{"notifications" + mode}
	case "session", "api-capabilities", "openapi":
		if !write {
			return []string{}
		}
	case "hosts":
		if len(a) > 2 && a[2] == "traffic" {
			if len(a) > 3 {
				return []string{"usage:reset"}
			}
			if write {
				return []string{"hosts:write"}
			}
			return []string{"usage:read"}
		}
		if len(a) > 2 && a[2] == "engine-updates" {
			if write {
				return []string{"kernels:update"}
			}
			return []string{"kernels:read"}
		}
		if write && (r.Method == "DELETE" || len(a) > 2 && a[2] == "retirement") {
			return []string{"hosts:uninstall"}
		}
		if len(a) > 2 && (a[2] == "enrollment" || a[2] == "rotate-agent-token" || a[2] == "repair-command") {
			return []string{"hosts:enroll"}
		}
		if len(a) > 2 && (a[2] == "kernels" || a[2] == "kernel-config") {
			return []string{"nodes:read"}
		}
		if len(a) > 2 && a[2] == "firewalls" {
			return []string{"firewall:write"}
		}
		if write && len(a) == 1 {
			return []string{"hosts:write", "hosts:enroll"}
		}
		return []string{"hosts" + mode}
	case "live", "protocol-templates", "engine-catalog":
		return []string{"nodes" + mode}
	case "engine-releases":
		return []string{"kernels:read"}
	case "gateway":
		if len(a) == 2 && a[1] == "nginx" && !write {
			return []string{"reverse:read"}
		}
	case "proxy-users":
		if len(a) > 2 && a[2] == "subscription" {
			return []string{"users:read", "subscriptions:write", "subscriptions:links"}
		}
		if len(a) > 2 && a[2] == "devices" {
			if len(a) > 4 {
				return []string{"devices:links"}
			}
			if r.Method == "POST" {
				return []string{"devices:write", "devices:links"}
			}
			return []string{"devices" + mode}
		}
		if len(a) > 2 && a[2] == "reset-traffic" {
			return []string{"usage:reset"}
		}
		return []string{"users" + mode}
	case "user-groups":
		return []string{"groups" + mode}
	case "dashboard":
		return []string{"usage:read"}
	case "access-records":
		return []string{"access:read"}
	case "subscriptions":
		if len(a) > 2 && (a[2] == "link" || a[2] == "config" || a[2] == "share" || a[2] == "content" || a[2] == "nodes" || a[2] == "qr") {
			return []string{"subscriptions:links"}
		}
		if len(a) > 2 && a[2] == "usage" {
			return []string{"subscriptions:read"}
		}
		if r.Method == "POST" && (len(a) == 1 || (len(a) > 2 && a[2] == "rotate")) {
			return []string{"subscriptions:write", "subscriptions:links"}
		}
		return []string{"subscriptions" + mode}
	case "client-profiles", "client-policies", "client-catalog":
		return []string{"profiles" + mode}
	case "site", "display-settings":
		if len(a) == 1 {
			return []string{"site" + mode}
		}
	case "certificates":
		return []string{"certificates" + mode}
	case "reverse-proxies":
		return []string{"reverse" + mode}
	case "firewalls":
		return []string{"firewall" + mode}
	case "audit":
		return []string{"audit:read"}
	case "tasks":
		return []string{"tasks" + mode}
	case "nodes", "instances", "tunnels", "routes", "configs":
		if len(a) > 2 && (a[2] == "deploy" || a[2] == "rollback") {
			return []string{"tasks:write"}
		}
		return []string{"inventory" + mode}
	}
	return nil
}

func (s *Server) authenticateAPI(r *http.Request) (apiIdentity, error) {
	header := r.Header.Get("Authorization")
	if equal(header, "Bearer "+s.AdminToken) {
		return apiIdentity{Root: true, ID: "admin"}, nil
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if !strings.HasPrefix(header, "Bearer ") || !keyPattern.MatchString(token) {
		return apiIdentity{}, problem{401, "unauthorized"}
	}
	var identity apiIdentity
	var rate int
	e := s.DB.QueryRow(r.Context(), `SELECT id,scopes,expires_at,rate_limit FROM api_keys WHERE tenant_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>now()`, tenant, Hash(token)).Scan(&identity.ID, &identity.Scopes, &identity.Expires, &rate)
	if e == pgx.ErrNoRows {
		return identity, problem{401, "unauthorized"}
	}
	if e != nil {
		return identity, e
	}
	required := requiredScopes(r)
	if required == nil {
		return identity, problem{403, "this operation requires the root administrator"}
	}
	for _, scope := range required {
		if !slices.Contains(identity.Scopes, scope) {
			return identity, problem{403, "missing API permission: " + scope}
		}
	}
	now := time.Now()
	s.keyRates.Lock()
	if s.keyRates.Windows == nil {
		s.keyRates.Windows = map[string]rateWindow{}
	}
	if len(s.keyRates.Windows) > 500 {
		for id, v := range s.keyRates.Windows {
			if now.Sub(v.Start) >= time.Minute {
				delete(s.keyRates.Windows, id)
			}
		}
	}
	v := s.keyRates.Windows[identity.ID]
	if now.Sub(v.Start) >= time.Minute {
		v = rateWindow{Start: now}
	}
	v.Count++
	s.keyRates.Windows[identity.ID] = v
	s.keyRates.Unlock()
	if v.Count > rate {
		return identity, problem{429, "API key rate limit reached"}
	}
	_, e = s.DB.Exec(r.Context(), `UPDATE api_keys SET last_used_at=now() WHERE tenant_id=$1 AND id=$2 AND (last_used_at IS NULL OR last_used_at<now()-interval '1 minute')`, tenant, identity.ID)
	return identity, e
}

func (s *Server) apiSession(w http.ResponseWriter, r *http.Request) error {
	identity, _ := r.Context().Value(identityContext{}).(apiIdentity)
	send(w, 200, map[string]any{"root": identity.Root, "key_id": identity.ID, "scopes": identity.Scopes, "expires_at": identity.Expires, "version": buildinfo.Version})
	return nil
}
func (s *Server) apiCapabilities(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, map[string]any{"version": buildinfo.Version, "scopes": apiScopes, "openapi": "/api/v1/openapi", "features": []string{"commerce_v1", "os_host_traffic", "host_quota_calibration", "user_billing_modes", "user_traffic_multiplier", "device_credential_slots", "compatible_subscription_observations", "canonical_user_subscription", "access_metadata_pagination", "policy_workspace", "client_configuration_import", "shadowrocket_rules", "protocol_layers", "telegram_notifications", "configuration_inspect", "firewall_versions", "protocol_batch", "rolling_monthly", "client_json_templates", "subscription_usage", "scoped_api_keys", "client_policies", "dual_stack_endpoints", "host_retirement", "independent_share_domain"}})
	return nil
}
func (s *Server) keys(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,scopes,expires_at,revoked_at,last_used_at,created_at,version,rate_limit FROM api_keys WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200) x`, tenant).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
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
	id := r.PathValue("id")
	if r.Method == "DELETE" {
		tag, e := tx.Exec(r.Context(), `DELETE FROM api_keys WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if e = audit(r.Context(), tx, "admin", "api-key.delete", id); e != nil {
			return e
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
		send(w, 200, map[string]bool{"revoked": true, "deleted": true})
		return nil
	}
	var b struct {
		Name    string    `json:"name"`
		Scopes  []string  `json:"scopes"`
		Expires time.Time `json:"expires_at"`
		Rate    int       `json:"rate_limit"`
		Version int       `json:"version"`
	}
	if e = decode(w, r, &b); e != nil {
		return e
	}
	if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || len(b.Scopes) == 0 || len(b.Scopes) > len(apiScopes) || b.Expires.IsZero() || b.Expires.After(time.Now().AddDate(3, 0, 0)) || !b.Expires.After(time.Now()) {
		return bad("name, explicit permissions and expiry within three years required")
	}
	seen := map[string]bool{}
	for _, v := range b.Scopes {
		if seen[v] || !slices.ContainsFunc(apiScopes, func(s scopeInfo) bool { return s.ID == v }) {
			return bad("unknown or duplicate API permission")
		}
		seen[v] = true
	}
	if b.Rate == 0 {
		b.Rate = 120
	}
	if b.Rate < 1 || b.Rate > 600 {
		return bad("rate_limit must be 1-600 requests per minute")
	}
	token := ""
	if id == "" {
		var n int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND revoked_at IS NULL AND expires_at>now()`, tenant).Scan(&n); e != nil {
			return e
		}
		if n >= 100 {
			return bad("at most 100 active API keys")
		}
		id = ID()
		token = "xs_" + ID() + ID()
		_, e = tx.Exec(r.Context(), `INSERT INTO api_keys(tenant_id,id,name,token_hash,scopes,expires_at,rate_limit) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, id, b.Name, Hash(token), b.Scopes, b.Expires, b.Rate)
	} else {
		tag, err := tx.Exec(r.Context(), `UPDATE api_keys SET name=$3,scopes=$4,expires_at=$5,rate_limit=$6,version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$7 AND revoked_at IS NULL`, tenant, id, b.Name, b.Scopes, b.Expires, b.Rate, b.Version)
		e = err
		if e == nil && tag.RowsAffected() == 0 {
			return problem{409, "API key changed or was revoked; refresh before saving"}
		}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "api-key.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	response := map[string]any{"id": id}
	if token != "" {
		response["token"] = token
	}
	send(w, 201, response)
	return nil
}

func actorContext(ctx context.Context, actor string) string {
	if actor == "admin" {
		if id, ok := ctx.Value(identityContext{}).(apiIdentity); ok && !id.Root {
			return "api-key:" + id.ID
		}
	}
	return actor
}
