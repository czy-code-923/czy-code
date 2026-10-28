package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"vps-control/internal/live"
)

// The gateway overwrites X-Real-IP; direct peers cannot supply their own address.
func subscriptionPeer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	if peer.IsLoopback() {
		if ip, e := netip.ParseAddr(r.Header.Get("X-Real-IP")); e == nil && !ip.IsUnspecified() && ip.Zone() == "" {
			return ip.Unmap().String()
		}
	}
	return peer.Unmap().String()
}

// Compatibility mode records an observed subscription client, not a hardware
// identity. Limits apply to subscription downloads; copied configs cannot be
// counted as physical devices by VLESS/Trojan/Clash protocols.
func (s *Server) observeSubscription(w http.ResponseWriter, r *http.Request, user, client string) error {
	ip := subscriptionPeer(r)
	if ip == "" {
		return nil
	}
	hwid := r.Header.Get("X-Hwid")
	kind := "source_and_client"
	family := client
	if c, ok := clientByID(client); ok {
		family = c.Format
	}
	key := ip + "/" + family
	if subscriptionHWIDPattern.MatchString(hwid) {
		kind = "client_reported_hwid"
		key = hwid
	}
	fingerprint := Hash("observed-client/v1/" + user + "/" + key)
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return err
	}
	var limit int
	if err = tx.QueryRow(r.Context(), `SELECT `+deviceLimitSQL+` FROM proxy_users u WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now() FOR UPDATE`, tenant, user).Scan(&limit); err != nil {
		return problem{404, "subscription unavailable"}
	}
	var id string
	var enabled bool
	err = tx.QueryRow(r.Context(), `SELECT id,enabled FROM subscription_clients WHERE tenant_id=$1 AND user_id=$2 AND fingerprint=$3`, tenant, user, fingerprint).Scan(&id, &enabled)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && !enabled {
		return problem{403, "此订阅客户端记录已停用，请联系管理员；该记录是兼容识别，不是物理硬件证明"}
	}
	if err == pgx.ErrNoRows {
		var count, total int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE user_id=$2 AND enabled AND last_subscription_at>now()-interval '7 days'),count(*) FROM subscription_clients WHERE tenant_id=$1`, tenant, user).Scan(&count, &total); err != nil {
			return err
		}
		if limit > 0 && count >= limit {
			w.Header().Set("X-Starhub-Device-Error", "observed_client_limit")
			return problem{403, "已达到兼容订阅客户端名额；后台可删除旧记录。来源地址变化可能产生新记录，不等于精确物理设备数"}
		}
		if count >= 200 || total >= 10000 {
			if limit == 0 {
				return nil
			}
			return problem{409, "客户端记录容量已满，请清理旧记录"}
		}
		id = ID()
		if _, err = tx.Exec(r.Context(), `INSERT INTO subscription_clients(tenant_id,id,user_id,fingerprint,name,client_id,user_agent,source_ip,identity_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8::inet,$9)`, tenant, id, user, fingerprint, client+" · "+id[:6], client, clientMetadata(r.UserAgent(), 256), ip, kind); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE subscription_clients SET last_subscription_at=now(),user_agent=$3,source_ip=$4::inet,device_os=$5,os_version=$6,device_model=$7 WHERE tenant_id=$1 AND id=$2`, tenant, id, clientMetadata(r.UserAgent(), 256), ip, clientMetadata(r.Header.Get("X-Device-Os"), 64), clientMetadata(r.Header.Get("X-Ver-Os"), 64), clientMetadata(r.Header.Get("X-Device-Model"), 128)); err != nil {
		return err
	}
	w.Header().Set("X-Starhub-Device-Recognition", kind)
	return tx.Commit(r.Context())
}

func (s *Server) appendObservedClients(r *http.Request, user string, raw json.RawMessage) (json.RawMessage, error) {
	var observed json.RawMessage
	err := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT id,user_id,name,client_id,enabled,version,created_at,last_subscription_at,last_connected_at,user_agent,host(source_ip) source_ip,identity_kind,device_os,os_version,device_model,'observed_client' registration_source,'subscription_downloads' enforcement FROM subscription_clients WHERE tenant_id=$1 AND user_id=$2 AND last_subscription_at>now()-interval '7 days' ORDER BY last_subscription_at DESC LIMIT 200)x`, tenant, user).Scan(&observed)
	if err != nil {
		return nil, err
	}
	var a, b []json.RawMessage
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(observed, &b); err != nil {
		return nil, err
	}
	return json.Marshal(append(b, a...))
}
func (s *Server) observedClientAction(w http.ResponseWriter, r *http.Request, user, id string) (bool, error) {
	var exists bool
	if err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM subscription_clients WHERE tenant_id=$1 AND user_id=$2 AND id=$3)`, tenant, user, id).Scan(&exists); err != nil {
		return true, err
	}
	if !exists {
		return false, nil
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return true, err
	}
	defer tx.Rollback(r.Context())
	if err = graphLock(r, tx); err != nil {
		return true, err
	}
	if r.Method == "DELETE" {
		_, err = tx.Exec(r.Context(), `DELETE FROM subscription_clients WHERE tenant_id=$1 AND user_id=$2 AND id=$3`, tenant, user, id)
	} else {
		var b struct {
			Name    string `json:"name"`
			Client  string `json:"client_id"`
			Enabled *bool  `json:"enabled"`
			Version int    `json:"version"`
		}
		if err = decode(w, r, &b); err != nil {
			return true, err
		}
		if b.Enabled == nil || len([]rune(strings.TrimSpace(b.Name))) < 1 || len([]rune(b.Name)) > 100 {
			return true, bad("name and enabled required")
		}
		tag, e := tx.Exec(r.Context(), `UPDATE subscription_clients SET name=$4,enabled=$5,version=version+1 WHERE tenant_id=$1 AND user_id=$2 AND id=$3 AND version=$6`, tenant, user, id, b.Name, *b.Enabled, b.Version)
		err = e
		if err == nil && tag.RowsAffected() == 0 {
			return true, problem{409, "客户端记录已变化，请刷新"}
		}
	}
	if err != nil {
		return true, err
	}
	if err = checkDevicePolicies(r.Context(), tx); err != nil {
		return true, err
	}
	if err = audit(r.Context(), tx, "admin", "client-observation."+strings.ToLower(r.Method), id); err != nil {
		return true, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return true, err
	}
	send(w, 200, map[string]bool{"saved": true})
	return true, nil
}

func (s *Server) userSubscription(w http.ResponseWriter, r *http.Request) error {
	user := r.PathValue("id")
	var b struct{}
	if e := decode(w, r, &b); e != nil {
		return e
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
	var name string
	var expiry time.Time
	if e = tx.QueryRow(r.Context(), `SELECT name,expires_at FROM proxy_users WHERE tenant_id=$1 AND id=$2 AND enabled AND expires_at>now() FOR UPDATE`, tenant, user).Scan(&name, &expiry); e != nil {
		return e
	}
	var id string
	var sealed []byte
	e = tx.QueryRow(r.Context(), `SELECT id,sealed_token FROM subscriptions WHERE tenant_id=$1 AND user_id=$2 AND device_id IS NULL AND revoked_at IS NULL AND expires_at>now() AND sealed_token IS NOT NULL ORDER BY created_at LIMIT 1 FOR UPDATE`, tenant, user).Scan(&id, &sealed)
	reused := e == nil
	var token string
	if e == pgx.ErrNoRows {
		var n int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, tenant).Scan(&n); e != nil {
			return e
		}
		if n >= 200 {
			return bad("subscription capacity reached")
		}
		id = ID()
		token = ID() + ID()
		sealed, e = live.Seal(s.MasterKey, []byte(token))
		if e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO subscriptions(tenant_id,id,name,token_hash,expires_at,user_id,sealed_token) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, id, name+"的订阅", Hash(token), expiry, user, sealed); e != nil {
			return e
		}
		if e = audit(r.Context(), tx, "admin", "subscription.user-create", id); e != nil {
			return e
		}
	} else if e != nil {
		return e
	} else {
		plain, err := live.Open(s.MasterKey, sealed)
		if err != nil {
			return err
		}
		token = string(plain)
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": id, "url": origin + "/s/" + token, "reused": reused})
	return nil
}
