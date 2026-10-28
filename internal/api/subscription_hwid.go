package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
	"vps-control/internal/live"

	"github.com/jackc/pgx/v5"
)

// Compatibility contract documented by Remnawave/Happ. This is a client-reported
// installation identifier, never proof of physical hardware or an IP fingerprint.
// https://github.com/remnawave/panel/blob/main/docs/features/hwid-device-limit.md
var subscriptionHWIDPattern = regexp.MustCompile(`^[a-zA-Z0-9=-]{10,64}$`)

type subscriptionInspectionKey struct{}

func clientMetadata(value string, limit int) string {
	value = strings.Map(func(c rune) rune {
		if c < 32 || c == 127 {
			return -1
		}
		return c
	}, strings.TrimSpace(value))
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

// Register under the same graph lock as user/group changes, so simultaneous
// downloads cannot overbook a user's limit or deploy stale permissions.
func (s *Server) subscriptionDevice(w http.ResponseWriter, r *http.Request, user, client string, limit int) (string, error) {
	if limit > 0 {
		w.Header().Set("X-Hwid-Active", "true")
	}
	hwid := r.Header.Get("X-Hwid")
	if !subscriptionHWIDPattern.MatchString(hwid) {
		if limit > 0 {
			w.Header().Set("X-Hwid-Not-Supported", "true")
			w.Header().Set("X-Starhub-Device-Error", "hwid_required")
			return "", problem{409, "设备限制已开启，当前客户端未回传有效设备标识 x-hwid；请启用客户端 HWID 功能或使用支持的客户端。普通 Clash/Mihomo 不会自动提供硬件标识"}
		}
		return "", nil
	}
	hash := Hash("subscription-hwid/v1/" + user + "/" + hwid)
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return "", err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return "", err
	}
	// Re-read enabled/expiry/limits inside the lock, rather than trusting a request
	// that raced with an administrator lowering the limit or disabling the user.
	if err = tx.QueryRow(r.Context(), `SELECT `+deviceLimitSQL+` FROM proxy_users u WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now() FOR UPDATE`, tenant, user).Scan(&limit); err != nil {
		return "", problem{404, "subscription unavailable"}
	}
	var id string
	var enabled bool
	err = tx.QueryRow(r.Context(), `SELECT id,enabled FROM user_devices WHERE tenant_id=$1 AND user_id=$2 AND hwid_hash=$3`, tenant, user, hash).Scan(&id, &enabled)
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}
	if err == nil && !enabled {
		w.Header().Set("X-Starhub-Device-Error", "device_disabled")
		return "", problem{403, "此客户端设备已被停用，请联系管理员"}
	}
	if err == pgx.ErrNoRows {
		var active, total, workspaceActive, workspaceTotal int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE user_id=$2 AND enabled),count(*) FILTER(WHERE user_id=$2),count(*) FILTER(WHERE enabled),count(*) FROM user_devices WHERE tenant_id=$1`, tenant, user).Scan(&active, &total, &workspaceActive, &workspaceTotal); err != nil {
			return "", err
		}
		if limit > 0 && active >= limit {
			w.Header().Set("X-Hwid-Max-Devices-Reached", "true")
			w.Header().Set("X-Hwid-Limit", "true")
			w.Header().Set("X-Starhub-Device-Error", "device_limit_reached")
			return "", problem{403, "已达到用户允许的客户端设备数量，请在后台移除或停用旧设备后重新更新订阅"}
		}
		if active >= 20 || total >= 200 || workspaceActive >= 400 || workspaceTotal >= 2000 {
			return "", problem{409, "客户端设备登记容量已满，请联系管理员清理旧设备"}
		}
		var managed bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) JOIN live_services l ON(l.tenant_id,l.id)=(gs.tenant_id,gs.service_id) WHERE gm.tenant_id=$1 AND gm.user_id=$2 AND l.managed_users AND l.delete_requested_at IS NULL)`, tenant, user).Scan(&managed); err != nil {
			return "", err
		}
		if !managed {
			return "", problem{422, "当前用户未获授权独立身份协议；SS/Snell 共享密钥不能执行逐用户设备限制"}
		}
		id = ID()
		raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
		sealed, err := live.Seal(s.MasterKey, raw)
		if err != nil {
			return "", err
		}
		name := "自动客户端 · " + id[:8]
		if _, err = tx.Exec(r.Context(), `INSERT INTO user_devices(tenant_id,id,user_id,name,client_id,sealed,registration_source,hwid_hash,last_subscription_at) VALUES($1,$2,$3,$4,$5,$6,'subscription_hwid',$7,now())`, tenant, id, user, name, client, sealed, hash); err != nil {
			return "", err
		}
		if err = checkDevicePolicies(r.Context(), tx); err != nil {
			return "", err
		}
		if err = bumpDeviceUser(r.Context(), tx, user); err != nil {
			return "", err
		}
		if err = audit(r.Context(), tx, "subscriber", "user.device-auto-register", id); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE user_devices SET last_subscription_at=now(),user_agent=$3,device_os=$4,os_version=$5,device_model=$6,client_id=$7 WHERE tenant_id=$1 AND id=$2`, tenant, id, clientMetadata(r.UserAgent(), 256), clientMetadata(r.Header.Get("X-Device-Os"), 64), clientMetadata(r.Header.Get("X-Ver-Os"), 64), clientMetadata(r.Header.Get("X-Device-Model"), 128), client); err != nil {
		return "", err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return "", err
	}
	w.Header().Set("X-Starhub-Device-Recognition", "client-reported-hwid")
	return id, nil
}

// A successful subscription must not silently cache an incomplete snapshot while
// an online Agent is still applying newly granted user/device credentials.
func (s *Server) subscriptionPending(ctx context.Context, user, device string) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM live_services l JOIN hosts h ON(l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.enabled AND l.delete_requested_at IS NULL AND h.last_seen>now()-interval '90 seconds' AND l.state<>'failed' AND (l.state='pending' OR l.version<>l.reported_version) AND EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gm.tenant_id=l.tenant_id AND gm.user_id=$2 AND gs.service_id=l.id) AND ($3='' OR (l.managed_users AND EXISTS(SELECT 1 FROM user_devices d WHERE d.tenant_id=l.tenant_id AND d.id=$3 AND (d.all_nodes OR d.service_ids ? l.id))))`, tenant, user, device).Scan(&count)
	return count, err
}
