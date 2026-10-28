package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vps-control/internal/live"
)

// The same graph lock protects permission changes, topology edits and stop acknowledgements.
func graphLock(r *http.Request, tx pgx.Tx) error {
	_, e := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(717071)`)
	return e
}

func (s *Server) removeManaged(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	id, kind := r.PathValue("id"), r.PathValue("collection")
	if kind == "" {
		kind = strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")[0]
	}
	switch kind {
	case "proxy-users":
		if _, e = tx.Exec(r.Context(), `DELETE FROM traffic_cursors WHERE tenant_id=$1 AND subject_kind='user' AND (subject_id=$2 OR subject_id IN(SELECT id FROM user_devices WHERE tenant_id=$1 AND user_id=$2))`, tenant, id); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM traffic_subjects WHERE tenant_id=$1 AND subject_kind='user' AND (subject_id=$2 OR subject_id IN(SELECT id FROM user_devices WHERE tenant_id=$1 AND user_id=$2))`, tenant, id); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE managed_users AND tenant_id=$1 AND id IN (SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=$1 AND gm.user_id=$2)`, tenant, id); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE user_groups SET version=version+1 WHERE tenant_id=$1 AND id IN (SELECT group_id FROM user_group_members WHERE tenant_id=$1 AND user_id=$2)`, tenant, id); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscription_services WHERE tenant_id=$1 AND subscription_id IN (SELECT id FROM subscriptions WHERE tenant_id=$1 AND user_id=$2)`, tenant, id); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscriptions WHERE tenant_id=$1 AND user_id=$2`, tenant, id); e != nil {
			return e
		}
		t, err := tx.Exec(r.Context(), `DELETE FROM proxy_users WHERE tenant_id=$1 AND id=$2`, tenant, id)
		e = err
		if e == nil && t.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	case "user-groups":
		if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE managed_users AND tenant_id=$1 AND id IN (SELECT service_id FROM user_group_services WHERE tenant_id=$1 AND group_id=$2)`, tenant, id); e != nil {
			return e
		}
		t, err := tx.Exec(r.Context(), `DELETE FROM user_groups WHERE tenant_id=$1 AND id=$2`, tenant, id)
		e = err
		if e == nil && t.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	case "client-profiles":
		if _, e = tx.Exec(r.Context(), `UPDATE subscriptions SET profile_id=NULL WHERE tenant_id=$1 AND profile_id=$2`, tenant, id); e != nil {
			return e
		}
		t, err := tx.Exec(r.Context(), `DELETE FROM client_profiles WHERE tenant_id=$1 AND id=$2`, tenant, id)
		e = err
		if e == nil && t.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	case "subscriptions":
		var bound bool
		if e = tx.QueryRow(r.Context(), `SELECT device_id IS NOT NULL FROM subscriptions WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&bound); e != nil {
			return e
		}
		if bound {
			return problem{409, "请从用户设备管理删除此设备，以同时撤销连接凭据"}
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscription_services WHERE tenant_id=$1 AND subscription_id=$2`, tenant, id); e != nil {
			return e
		}
		t, err := tx.Exec(r.Context(), `DELETE FROM subscriptions WHERE tenant_id=$1 AND id=$2`, tenant, id)
		e = err
		if e == nil && t.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	default:
		return problem{404, "unknown collection"}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "delete."+kind, id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]any{"id": id, "deleted": true, "notice": "Permissions reconcile on the next Agent sync; disconnected Agents stop after their control timeout. Subscription deletion only revokes downloads."})
	return nil
}

func (s *Server) removeHost(w http.ResponseWriter, r *http.Request) error { return s.retirement(w, r) }

func purgeLive(r *http.Request, tx pgx.Tx, id string) error {
	for _, q := range []string{
		`DELETE FROM traffic_cursors WHERE tenant_id=$1 AND subject_kind='service' AND subject_id=$2`,
		`DELETE FROM traffic_subjects WHERE tenant_id=$1 AND subject_kind='service' AND subject_id=$2`,
		`UPDATE user_groups SET version=version+1 WHERE tenant_id=$1 AND id IN (SELECT group_id FROM user_group_services WHERE tenant_id=$1 AND service_id=$2)`,
		`DELETE FROM user_group_services WHERE tenant_id=$1 AND service_id=$2`,
		`DELETE FROM subscription_services WHERE tenant_id=$1 AND service_id=$2`,
		`DELETE FROM live_ports WHERE tenant_id=$1 AND service_id=$2`,
		`DELETE FROM frp_assignments WHERE tenant_id=$1 AND client_id=$2`,
		`DELETE FROM live_services WHERE tenant_id=$1 AND id=$2`,
	} {
		if _, e := tx.Exec(r.Context(), q, tenant, id); e != nil {
			return e
		}
	}
	return audit(r.Context(), tx, "control", "live.deleted", id)
}
func (s *Server) liveDelete(w http.ResponseWriter, r *http.Request) error {
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	id := r.PathValue("id")
	var enabled bool
	var v, rv int
	var state string
	if e = tx.QueryRow(r.Context(), `SELECT enabled,version,reported_version,state FROM live_services WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&enabled, &v, &rv, &state); e != nil {
		return e
	}
	var dependencies int
	if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM live_services WHERE tenant_id=$1 AND (next_id=$2 OR parent_id=$2)`, tenant, id).Scan(&dependencies); e != nil {
		return e
	}
	if dependencies > 0 {
		return problem{409, "此配置仍被链式出站或 FRP 客户端引用，请先修改或删除引用方"}
	}
	if e = prunePolicyNodes(r, tx, map[string]bool{id: true}); e != nil {
		return e
	}
	done := !enabled && ((state == "stopped" && v == rv) || (v == 1 && rv == 0))
	if done {
		e = purgeLive(r, tx, id)
	} else {
		_, e = tx.Exec(r.Context(), `UPDATE live_services SET enabled=false,version=CASE WHEN delete_requested_at IS NULL THEN version+1 ELSE version END,state='pending',delete_requested_at=COALESCE(delete_requested_at,now()) WHERE tenant_id=$1 AND id=$2`, tenant, id)
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "live.delete-request", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	status := 202
	if done {
		status = 200
	}
	send(w, status, map[string]bool{"deleted": done, "pending_stop": !done})
	return nil
}

func (s *Server) subscriptionUpdate(w http.ResponseWriter, r *http.Request) error {
	var bound bool
	if e := s.DB.QueryRow(r.Context(), `SELECT device_id IS NOT NULL FROM subscriptions WHERE tenant_id=$1 AND id=$2`, tenant, r.PathValue("id")).Scan(&bound); e != nil {
		return e
	}
	if bound {
		return problem{409, "设备订阅由设备管理维护，不能重新绑定用户或入口"}
	}

	var b struct {
		Name     string    `json:"name"`
		Profile  string    `json:"profile_id"`
		Expires  time.Time `json:"expires_at"`
		User     *string   `json:"user_id"`
		Services *[]string `json:"service_ids"`
		Version  int       `json:"version"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || b.Expires.IsZero() || b.Expires.After(time.Now().AddDate(1, 0, 0)) {
		return bad("name and expiry within one year required")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	var oldUser string
	var version int
	if e = tx.QueryRow(r.Context(), `SELECT COALESCE(user_id,''),version FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL FOR UPDATE`, tenant, r.PathValue("id")).Scan(&oldUser, &version); e != nil {
		return e
	}
	if (b.Version != 0 && b.Version != version) || ((b.User != nil || b.Services != nil) && b.Version == 0) {
		return problem{409, "subscription changed; refresh before saving"}
	}
	if b.User != nil || b.Services != nil {
		user := oldUser
		if b.User != nil {
			user = *b.User
		}
		if user != "" {
			var allowed bool
			if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM proxy_users u WHERE u.tenant_id=$1 AND u.id=$2 AND u.enabled AND u.expires_at>now() AND EXISTS(SELECT 1 FROM user_group_members m JOIN user_group_services gs USING(tenant_id,group_id) WHERE m.tenant_id=u.tenant_id AND m.user_id=u.id))`, tenant, user).Scan(&allowed); e != nil {
				return e
			}
			if !allowed {
				return bad("assign an active user to an authorized inbound group first")
			}
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM subscription_services WHERE tenant_id=$1 AND subscription_id=$2`, tenant, r.PathValue("id")); e != nil {
			return e
		}
		if user == "" {
			if b.Services == nil || len(*b.Services) < 1 || len(*b.Services) > 64 {
				return bad("select 1-64 shared proxy services")
			}
			seen := map[string]bool{}
			for _, id := range *b.Services {
				if seen[id] {
					return bad("duplicate shared service")
				}
				seen[id] = true
				var raw []byte
				var kind string
				if e = tx.QueryRow(r.Context(), `SELECT kind,sealed FROM live_services WHERE tenant_id=$1 AND id=$2 AND delete_requested_at IS NULL`, tenant, id).Scan(&kind, &raw); e != nil {
					return e
				}
				plain, err := live.Open(s.MasterKey, raw)
				if err != nil {
					return err
				}
				var spec live.Spec
				if err = json.Unmarshal(plain, &spec); err != nil {
					return err
				}
				if !live.Proxy(kind) || spec.ManagedUsers {
					return bad("shared subscriptions require unmanaged proxy inbounds")
				}
				if _, e = tx.Exec(r.Context(), `INSERT INTO subscription_services VALUES($1,$2,$3)`, tenant, r.PathValue("id"), id); e != nil {
					return e
				}
			}
		}
		if _, e = tx.Exec(r.Context(), `UPDATE subscriptions SET user_id=NULLIF($3,'') WHERE tenant_id=$1 AND id=$2`, tenant, r.PathValue("id"), user); e != nil {
			return e
		}
	}
	tag, e := tx.Exec(r.Context(), `UPDATE subscriptions SET name=$3,profile_id=NULLIF($4,''),expires_at=$5,version=version+1 WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL`, tenant, r.PathValue("id"), b.Name, b.Profile, b.Expires)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e = audit(r.Context(), tx, "admin", "subscription.update", r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]bool{"saved": true})
	return nil
}
func (s *Server) subscriptionLink(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	var sealed []byte
	if e := s.DB.QueryRow(r.Context(), `SELECT sealed_token FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`, tenant, r.PathValue("id")).Scan(&sealed); e != nil {
		return e
	}
	if len(sealed) == 0 {
		return problem{409, "旧订阅未保存可恢复链接，请先重置链接一次"}
	}
	token, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return e
	}
	origin, e := s.publicOrigin(r)
	if e != nil {
		return e
	}
	send(w, 200, map[string]string{"url": origin + "/s/" + string(token)})
	return nil
}
