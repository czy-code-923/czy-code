package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"vps-control/internal/billing"
	"vps-control/internal/live"
	"vps-control/internal/manifest"

	"github.com/jackc/pgx/v5"
)

type commerceSelection struct {
	Groups    []string `json:"group_ids"`
	Protocols []string `json:"protocols"`
}

func validCommerceSelection(b commerceSelection) error {
	if len(b.Groups) < 1 || len(b.Groups) > 8 || len(b.Protocols) < 1 || len(b.Protocols) > 32 {
		return bad("select 1–8 commercial groups and explicit protocols")
	}
	seen := map[string]bool{}
	for _, id := range b.Groups {
		if !manifest.ValidID(id) || seen[id] {
			return bad("invalid or duplicate commercial group")
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, p := range b.Protocols {
		if !live.Proxy(p) || seen[p] {
			return bad("invalid or duplicate proxy protocol")
		}
		seen[p] = true
	}
	return nil
}
func (s *Server) commerceNodes(ctx context.Context, tx pgx.Tx, b commerceSelection) (string, error) {
	if e := validCommerceSelection(b); e != nil {
		return "", e
	}
	var groups int
	if e := tx.QueryRow(ctx, "SELECT count(*) FROM user_groups WHERE tenant_id=$1 AND id=ANY($2)", tenant, b.Groups).Scan(&groups); e != nil {
		return "", e
	}
	if groups != len(b.Groups) {
		return "用户组不存在", nil
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT l.kind,l.managed_users,h.agent_version,(h.last_seen>now()-interval '90 seconds') AND l.enabled AND l.state='running' AND l.version=l.reported_version AND l.reported_at>now()-interval '45 seconds' AND EXISTS(SELECT 1 FROM telemetry_status t WHERE t.tenant_id=l.tenant_id AND t.host_id=l.host_id AND t.engine=l.engine AND t.state='collecting' AND t.reported_at>now()-interval '30 seconds') ready FROM live_services l JOIN user_group_services gs ON(gs.tenant_id,gs.service_id)=(l.tenant_id,l.id) JOIN hosts h ON(h.tenant_id,h.id)=(l.tenant_id,l.host_id) WHERE l.tenant_id=$1 AND gs.group_id=ANY($2) AND l.delete_requested_at IS NULL`, tenant, b.Groups)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	allowed := 0
	for rows.Next() {
		var kind, version string
		var managed bool
		var ready *bool
		if e = rows.Scan(&kind, &managed, &version, &ready); e != nil {
			return "", e
		}
		if !managed {
			return "组中包含共享密钥或非独立身份节点", nil
		}
		if !slices.Contains(b.Protocols, kind) {
			continue
		}
		if !live.MeterCapable(version) {
			return "Agent 不支持个人计量", nil
		}
		if ready == nil || !*ready {
			return "节点配置或个人计量尚未就绪", nil
		}
		allowed++
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	if allowed == 0 {
		return "没有符合所选协议的可计费节点", nil
	}
	return "", nil
}
func (s *Server) commerceValidate(w http.ResponseWriter, r *http.Request) error {
	var b commerceSelection
	if e := decode(w, r, &b); e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	reason, e := s.commerceNodes(r.Context(), tx, b)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"ready": reason == "", "reason": reason})
	return nil
}
func (s *Server) commerceGroups(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT g.id,g.name,COALESCE((SELECT jsonb_agg(jsonb_build_object('id',l.id,'name',l.name,'protocol',l.kind,'managed_users',l.managed_users,'engine',l.engine,'enabled',l.enabled)) FROM user_group_services gs JOIN live_services l ON(l.tenant_id,l.id)=(gs.tenant_id,gs.service_id) WHERE gs.tenant_id=g.tenant_id AND gs.group_id=g.id AND l.delete_requested_at IS NULL),'[]') services FROM user_groups g WHERE tenant_id=$1 ORDER BY name LIMIT 200)x`, tenant).Scan(&raw)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"feature": "commerce_v1", "groups": raw})
	return nil
}
func calendarMonths(t time.Time, n int, day int) time.Time {
	t = t.In(billingLocation)
	start := time.Date(t.Year(), t.Month()+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), billingLocation)
	last := time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, billingLocation).Day()
	return time.Date(start.Year(), start.Month(), min(day, last), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), billingLocation)
}

type commerceGrantRequest struct {
	External      string     `json:"external_id"`
	Order         string     `json:"order_id"`
	Name          string     `json:"name"`
	Months        int        `json:"months"`
	Quota         int64      `json:"quota_bytes"`
	Groups        []string   `json:"group_ids"`
	Protocols     []string   `json:"protocols"`
	Max           int        `json:"max_devices"`
	Billing       string     `json:"billing_mode"`
	Multiplier    int        `json:"multiplier_milli"`
	SeedExpiry    time.Time  `json:"-"`
	SeedUsed      int64      `json:"-"`
	SeedResetDay  int        `json:"-"`
	SeedNextReset *time.Time `json:"-"`
}

func validCommerceGrant(b commerceGrantRequest) error {
	if !strings.HasPrefix(b.External, "portal:") || !manifest.ValidID(strings.TrimPrefix(b.External, "portal:")) || !manifest.ValidID(b.Order) || strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || b.Months < 1 || b.Months > 12 || b.Quota < 1 || b.Quota > 9000000000000000 || b.Max < 0 || b.Max > 20 || !billing.ValidMode(b.Billing) || b.Multiplier < 1 || b.Multiplier > 100000 {
		return bad("invalid commercial entitlement")
	}
	selection := commerceSelection{b.Groups, b.Protocols}
	if e := validCommerceSelection(selection); e != nil {
		return e
	}
	return nil
}
func (s *Server) commerceGrant(w http.ResponseWriter, r *http.Request) error {
	var b commerceGrantRequest
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := validCommerceGrant(b); e != nil {
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
	response, e := s.commerceGrantTx(r, tx, b)
	if e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, response)
	return nil
}
func (s *Server) commerceGrantTx(r *http.Request, tx pgx.Tx, b commerceGrantRequest) (json.RawMessage, error) {
	var e error
	selection := commerceSelection{b.Groups, b.Protocols}
	requestHash := Hash(string(mustJSON(b)))
	var oldHash string
	var previous json.RawMessage
	e = tx.QueryRow(r.Context(), "SELECT request_hash,response FROM commerce_entitlements WHERE tenant_id=$1 AND order_id=$2", tenant, b.Order).Scan(&oldHash, &previous)
	if e == nil {
		if oldHash != requestHash {
			return nil, problem{409, "order ID was used with different entitlement fields"}
		}
		return previous, nil
	}
	if e != pgx.ErrNoRows {
		return nil, e
	}
	if b.SeedExpiry.IsZero() {
		reason, e := s.commerceNodes(r.Context(), tx, selection)
		if e != nil {
			return nil, e
		}
		if reason != "" {
			return nil, problem{409, reason}
		}
	}
	var id string
	var expires time.Time
	var commercial bool
	var anchorDay int
	e = tx.QueryRow(r.Context(), "SELECT id,expires_at,commercial_only,reset_day FROM proxy_users WHERE tenant_id=$1 AND external_id=$2 FOR UPDATE", tenant, b.External).Scan(&id, &expires, &commercial, &anchorDay)
	fresh := e == pgx.ErrNoRows
	now := time.Now()
	restartCycle := fresh || expires.Before(now)
	if restartCycle {
		anchorDay = now.In(billingLocation).Day()
	}
	if e != nil && !fresh {
		return nil, e
	}
	if !fresh && !commercial {
		return nil, problem{409, "external ID belongs to a non-commercial user"}
	}
	if fresh {
		var count int
		if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM proxy_users WHERE tenant_id=$1", tenant).Scan(&count); e != nil {
			return nil, e
		}
		if count >= 500 {
			return nil, problem{409, "control-plane user capacity reached"}
		}
		id = ID()
		expires = now
		raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
		sealed, e := live.Seal(s.MasterKey, raw)
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO proxy_users(tenant_id,id,name,enabled,expires_at,sealed,external_id,commercial_only,legacy_credentials,reset_strategy,reset_day) VALUES($1,$2,$3,true,$4,$5,$6,true,false,'rolling_monthly',$7)`, tenant, id, b.Name, expires, sealed, b.External, now.In(billingLocation).Day()); e != nil {
			return nil, e
		}
	}
	if expires.Before(now) {
		expires = now
	}
	expires = calendarMonths(expires, b.Months, anchorDay)
	if !b.SeedExpiry.IsZero() {
		if !fresh {
			return nil, problem{409, "migration identity already exists"}
		}
		expires = b.SeedExpiry
	}
	if expires.After(now.AddDate(10, 0, 0)) {
		return nil, bad("maximum commercial entitlement horizon is ten years")
	}
	if restartCycle && !fresh {
		if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET reset_day=$3 WHERE tenant_id=$1 AND id=$2", tenant, id, anchorDay); e != nil {
			return nil, e
		}
	}
	if _, e = tx.Exec(r.Context(), `UPDATE proxy_users SET name=$3,enabled=true,expires_at=$4,quota_bytes=$5,commercial_only=true,commercial_protocols=$6,max_devices=$7,device_limit_mode='compatible',billing_mode=$8,multiplier_milli=$9,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id, b.Name, expires, b.Quota, mustJSON(b.Protocols), b.Max, b.Billing, b.Multiplier); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(r.Context(), "UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND managed_users AND id IN(SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gm.tenant_id=$1 AND gm.user_id=$2)", tenant, id); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM user_group_members WHERE tenant_id=$1 AND user_id=$2", tenant, id); e != nil {
		return nil, e
	}
	for _, group := range b.Groups {
		if _, e = tx.Exec(r.Context(), "INSERT INTO user_group_members(tenant_id,group_id,user_id) VALUES($1,$2,$3)", tenant, group, id); e != nil {
			return nil, e
		}
	}
	if _, e = tx.Exec(r.Context(), "UPDATE user_groups SET version=version+1 WHERE tenant_id=$1 AND id=ANY($2)", tenant, b.Groups); e != nil {
		return nil, e
	}
	if e = configureCycle(r.Context(), tx, id, nil, restartCycle); e != nil {
		return nil, e
	}
	if !b.SeedExpiry.IsZero() {
		if _, e = tx.Exec(r.Context(), "UPDATE proxy_users SET used_bytes=$3,billed_download_bytes=$3,reset_day=$4,next_reset_at=$5 WHERE tenant_id=$1 AND id=$2", tenant, id, b.SeedUsed, b.SeedResetDay, b.SeedNextReset); e != nil {
			return nil, e
		}
	}
	var subscription string
	var tokenSealed []byte
	e = tx.QueryRow(r.Context(), "SELECT id FROM subscriptions WHERE tenant_id=$1 AND user_id=$2 AND device_id IS NULL AND independent_identity AND revoked_at IS NULL ORDER BY created_at LIMIT 1 FOR UPDATE", tenant, id).Scan(&subscription)
	if e == pgx.ErrNoRows {
		subscription = ID()
		token := ID() + ID()
		tokenSealed, e = live.Seal(s.MasterKey, []byte(token))
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO subscriptions(tenant_id,id,name,token_hash,sealed_token,user_id,expires_at,independent_identity) VALUES($1,$2,$3,$4,$5,$6,$7,true)`, tenant, subscription, b.Name+"的订阅", Hash(token), tokenSealed, id, expires); e != nil {
			return nil, e
		}
		if _, _, e = s.ensureSubscriptionIdentity(r.Context(), tx, subscription, id, ""); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(r.Context(), "UPDATE subscriptions SET expires_at=$3,version=version+1 WHERE tenant_id=$1 AND id=$2", tenant, subscription, expires); e != nil {
		return nil, e
	}
	if e = bumpDeviceUser(r.Context(), tx, id); e != nil {
		return nil, e
	}
	if e = checkDevicePolicies(r.Context(), tx); e != nil {
		return nil, e
	}
	if e = s.checkMeterHosts(r, tx, id, ""); e != nil {
		return nil, e
	}
	response := map[string]any{"user_id": id, "subscription_id": subscription, "operation_id": b.Order, "expires_at": expires}
	if _, e = tx.Exec(r.Context(), "INSERT INTO commerce_entitlements(tenant_id,order_id,external_id,request_hash,response) VALUES($1,$2,$3,$4,$5)", tenant, b.Order, b.External, requestHash, mustJSON(response)); e != nil {
		return nil, e
	}
	if e = audit(r.Context(), tx, "integration", "commerce.grant", b.Order); e != nil {
		return nil, e
	}
	return mustJSON(response), nil
}

func (s *Server) commerceState(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		External string `json:"external_id"`
		Enabled  bool   `json:"enabled"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if b.External != "portal:"+r.PathValue("id") || !manifest.ValidID(r.PathValue("id")) {
		return bad("invalid commerce account")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var id string
	if e = tx.QueryRow(r.Context(), "UPDATE proxy_users SET enabled=$3,version=version+1 WHERE tenant_id=$1 AND external_id=$2 AND commercial_only RETURNING id", tenant, b.External, b.Enabled).Scan(&id); e != nil {
		return e
	}
	if e = bumpDeviceUser(r.Context(), tx, id); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "integration", "commerce.state", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"saved": true})
	return nil
}
