package api

import (
	"context"
	"encoding/json"
	"net/http"
	"vps-control/internal/live"

	"github.com/jackc/pgx/v5"
)

// The caller holds the graph lock. The durable subject ID also attributes all
// protocol-specific credentials back to the user for billing and access logs.
func (s *Server) ensureSubscriptionIdentity(ctx context.Context, tx pgx.Tx, subscription, user, device string) ([]byte, bool, error) {
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT sealed FROM subscription_identities WHERE tenant_id=$1 AND subscription_id=$2 AND COALESCE(device_id,'')=$3`, tenant, subscription, device).Scan(&sealed)
	if err == nil {
		return sealed, false, nil
	}
	if err != pgx.ErrNoRows {
		return nil, false, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM subscription_identities WHERE tenant_id=$1 AND user_id=$2`, tenant, user).Scan(&count); err != nil {
		return nil, false, err
	}
	if count >= 200 {
		return nil, false, problem{409, "用户的订阅身份容量已满"}
	}
	id := ID()
	raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
	sealed, err = live.Seal(s.MasterKey, raw)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO subscription_identities(tenant_id,id,subscription_id,user_id,device_id,sealed) VALUES($1,$2,$3,$4,NULLIF($5,''),$6)`, tenant, id, subscription, user, device, sealed)
	return sealed, true, err
}

func (s *Server) subscriptionIdentity(r *http.Request, subscription, user, device string) ([]byte, error) {
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return nil, err
	}
	var valid bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM subscriptions s JOIN proxy_users u ON (u.tenant_id,u.id)=(s.tenant_id,s.user_id) WHERE s.tenant_id=$1 AND s.id=$2 AND s.user_id=$3 AND s.independent_identity AND s.revoked_at IS NULL AND s.expires_at>now() AND u.enabled AND u.expires_at>now())`, tenant, subscription, user).Scan(&valid); err != nil {
		return nil, err
	}
	if !valid {
		return nil, problem{404, "subscription unavailable"}
	}
	sealed, created, err := s.ensureSubscriptionIdentity(r.Context(), tx, subscription, user, device)
	if err != nil {
		return nil, err
	}
	if created {
		if err = bumpDeviceUser(r.Context(), tx, user); err != nil {
			return nil, err
		}
		if err = checkDevicePolicies(r.Context(), tx); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return nil, err
	}
	return sealed, nil
}

func (s *Server) migrateSubscriptionIdentity(w http.ResponseWriter, r *http.Request) error {
	var body struct{}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return err
	}
	var user string
	var device *string
	var independent bool
	err = tx.QueryRow(r.Context(), `SELECT COALESCE(user_id,''),device_id,independent_identity FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now() FOR UPDATE`, tenant, r.PathValue("id")).Scan(&user, &device, &independent)
	if err != nil {
		return err
	}
	if user == "" {
		return problem{409, "共享密钥订阅不能按用户或订阅单独撤销"}
	}
	if independent {
		return problem{409, "该订阅已使用独立身份"}
	}
	deviceID := ""
	if device != nil {
		deviceID = *device
	}
	if _, _, err = s.ensureSubscriptionIdentity(r.Context(), tx, r.PathValue("id"), user, deviceID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE subscriptions SET independent_identity=true,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, r.PathValue("id")); err != nil {
		return err
	}
	var legacy bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1 AND user_id=$2 AND device_id IS NULL AND NOT independent_identity AND revoked_at IS NULL AND expires_at>now())`, tenant, user).Scan(&legacy); err != nil {
		return err
	}
	if !legacy {
		if _, err = tx.Exec(r.Context(), `UPDATE proxy_users SET legacy_credentials=false WHERE tenant_id=$1 AND id=$2`, tenant, user); err != nil {
			return err
		}
	}
	if err = checkDevicePolicies(r.Context(), tx); err != nil {
		return err
	}
	if err = bumpDeviceUser(r.Context(), tx, user); err != nil {
		return err
	}
	if err = audit(r.Context(), tx, "admin", "subscription.identity-migrate", r.PathValue("id")); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"saved": true, "legacy_identity_still_active": legacy, "notice": "请更新客户端订阅；旧共享身份仅在其他旧订阅仍有效时继续保留"})
	return nil
}
