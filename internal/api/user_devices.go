package api

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"slices"
	"strings"
	"time"
	"vps-control/internal/live"
	"vps-control/internal/manifest"
)

const deviceLimitSQL = `COALESCE(LEAST(NULLIF(u.max_devices,0),(SELECT min(NULLIF(g.max_devices,0)) FROM user_groups g JOIN user_group_members gm ON (g.tenant_id,g.id)=(gm.tenant_id,gm.group_id) WHERE gm.tenant_id=u.tenant_id AND gm.user_id=u.id)),0)`

func checkDevicePolicies(ctx context.Context, tx pgx.Tx) error {
	var invalid bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM proxy_users u WHERE tenant_id=$1 AND `+deviceLimitSQL+`>0 AND (SELECT count(*) FROM user_devices d WHERE d.tenant_id=u.tenant_id AND d.user_id=u.id AND d.enabled)>`+deviceLimitSQL+`)`, tenant).Scan(&invalid)
	if e != nil {
		return e
	}
	if invalid {
		return problem{409, "设备数超过新的用户/用户组限制，请先停用多余设备"}
	}
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM live_services l WHERE l.tenant_id=$1 AND l.managed_users AND (SELECT count(*) FROM (SELECT u.id FROM proxy_users u WHERE u.tenant_id=l.tenant_id AND u.enabled AND `+deviceLimitSQL+`=0 AND EXISTS(SELECT 1 FROM user_group_members gm JOIN user_group_services gs USING(tenant_id,group_id) WHERE gm.tenant_id=u.tenant_id AND gm.user_id=u.id AND gs.service_id=l.id) UNION SELECT d.id FROM user_devices d JOIN proxy_users u ON (u.tenant_id,u.id)=(d.tenant_id,d.user_id) WHERE d.tenant_id=l.tenant_id AND d.enabled AND (d.all_nodes OR d.service_ids ? l.id) AND u.enabled AND EXISTS(SELECT 1 FROM user_group_members gm JOIN user_group_services gs USING(tenant_id,group_id) WHERE gm.tenant_id=u.tenant_id AND gm.user_id=u.id AND gs.service_id=l.id)) identities)>500)`, tenant).Scan(&invalid)
	if e != nil {
		return e
	}
	if invalid {
		return problem{409, "此授权会使单个入站超过 500 个用户/设备身份，请拆分入站"}
	}
	return nil
}
func bumpDeviceUser(ctx context.Context, tx pgx.Tx, user string) error {
	_, e := tx.Exec(ctx, `UPDATE live_services SET version=version+1,state='pending' WHERE managed_users AND tenant_id=$1 AND id IN(SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gm.tenant_id=$1 AND gm.user_id=$2)`, tenant, user)
	return e
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) error {
	user, id := r.PathValue("id"), r.PathValue("device")
	if !manifest.ValidID(user) || id != "" && !manifest.ValidID(id) {
		return bad("invalid user or device")
	}
	if r.Method == "GET" {
		if strings.HasSuffix(r.URL.Path, "/link") {
			var token []byte
			var client string
			e := s.DB.QueryRow(r.Context(), `SELECT s.sealed_token,d.client_id FROM user_devices d JOIN subscriptions s ON (s.tenant_id,s.device_id)=(d.tenant_id,d.id) WHERE d.tenant_id=$1 AND d.user_id=$2 AND d.id=$3 AND d.enabled AND s.revoked_at IS NULL`, tenant, user, id).Scan(&token, &client)
			if e != nil {
				return e
			}
			plain, e := live.Open(s.MasterKey, token)
			if e != nil {
				return e
			}
			origin, e := s.publicOrigin(r)
			if e != nil {
				return e
			}
			send(w, 200, map[string]any{"url": origin + "/s/" + string(plain) + "?format=" + client})
			return nil
		}
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM(SELECT d.id,d.user_id,d.name,d.client_id,d.enabled,d.version,d.all_nodes,d.service_ids,d.created_at,d.last_subscription_at,d.last_connected_at,d.user_agent,host(d.last_source_ip) source_ip,s.id subscription_id,COALESCE((SELECT sum(c.uplink+c.downlink) FROM traffic_cursors c WHERE c.tenant_id=d.tenant_id AND c.subject_kind='user' AND c.subject_id=d.id),0) raw_lifetime_bytes FROM user_devices d LEFT JOIN subscriptions s ON(s.tenant_id,s.device_id)=(d.tenant_id,d.id) WHERE d.tenant_id=$1 AND d.user_id=$2 ORDER BY d.created_at DESC LIMIT 200)x`, tenant, user).Scan(&raw)
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
	var expires time.Time
	var userEnabled bool
	var limit int
	var userName string
	if e = tx.QueryRow(r.Context(), `SELECT u.expires_at,`+deviceLimitSQL+`,u.name,u.enabled FROM proxy_users u WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, user).Scan(&expires, &limit, &userName, &userEnabled); e != nil {
		return e
	}
	var in struct {
		Name     string    `json:"name"`
		Client   string    `json:"client_id"`
		Enabled  *bool     `json:"enabled"`
		Version  int       `json:"version"`
		AllNodes *bool     `json:"all_nodes"`
		Services *[]string `json:"service_ids"`
	}
	if r.Method != "DELETE" {
		if e = decode(w, r, &in); e != nil {
			return e
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Client == "" {
			in.Client = "mihomo"
		}
		if in.Name == "" || len([]rune(in.Name)) > 100 {
			return bad("device name required, at most 100 characters")
		}
		if client, ok := clientByID(in.Client); !ok {
			return bad("unknown device client format")
		} else {
			in.Client = client.ID
		}
	}
	fresh := r.Method == "POST"
	needSync := fresh || r.Method == "DELETE"
	var url string
	allNodes := true
	serviceIDs := []string{}
	if in.AllNodes != nil {
		allNodes = *in.AllNodes
	}
	if in.Services != nil {
		serviceIDs = *in.Services
	}
	if allNodes {
		serviceIDs = []string{}
	}
	if fresh {
		if e = validateDeviceServices(r.Context(), tx, user, allNodes, serviceIDs); e != nil {
			return e
		}
		if !userEnabled || !expires.After(time.Now()) {
			return bad("请先启用用户并设置有效的到期时间")
		}
		if in.Enabled != nil && !*in.Enabled {
			return bad("new devices start enabled; disable an existing device instead")
		}
		var managed bool
		if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) JOIN live_services l ON(l.tenant_id,l.id)=(gs.tenant_id,gs.service_id) WHERE gm.tenant_id=$1 AND gm.user_id=$2 AND l.managed_users AND l.delete_requested_at IS NULL)`, tenant, user).Scan(&managed); e != nil {
			return e
		}
		if !managed {
			return bad("请先授权支持独立身份的协议入站；SS/Snell 共享密钥不能提供用户设备限制")
		}
		var active, total int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE enabled),count(*) FROM user_devices WHERE tenant_id=$1 AND user_id=$2`, tenant, user).Scan(&active, &total); e != nil {
			return e
		}
		if active >= 20 || total >= 200 || limit > 0 && active >= limit {
			return problem{409, "用户的设备名额已满，请先停用或删除旧设备"}
		}
		var workspaceActive, workspaceTotal int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE enabled),count(*) FROM user_devices WHERE tenant_id=$1`, tenant).Scan(&workspaceActive, &workspaceTotal); e != nil {
			return e
		}
		if workspaceActive >= 400 || workspaceTotal >= 2000 {
			return problem{409, "当前工作区的设备容量已满"}
		}
		id = ID()
		raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
		sealed, err := live.Seal(s.MasterKey, raw)
		if err != nil {
			return err
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO user_devices(tenant_id,id,user_id,name,client_id,sealed,all_nodes,service_ids) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, id, user, in.Name, in.Client, sealed, allNodes, mustJSON(serviceIDs)); e != nil {
			return e
		}
		token := ID() + ID()
		sealedToken, err := live.Seal(s.MasterKey, []byte(token))
		if err != nil {
			return err
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO subscriptions(tenant_id,id,name,token_hash,expires_at,user_id,sealed_token,device_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, ID(), userName+" / "+in.Name, Hash(token), expires, user, sealedToken, id)
		if e != nil {
			return e
		}
		origin, err := s.publicOrigin(r)
		if err != nil {
			return err
		}
		url = origin + "/s/" + token + "?format=" + in.Client
	} else {
		var version int
		var enabled, oldAll bool
		var oldServices []byte
		if e = tx.QueryRow(r.Context(), `SELECT version,enabled,all_nodes,service_ids FROM user_devices WHERE tenant_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, tenant, user, id).Scan(&version, &enabled, &oldAll, &oldServices); e != nil {
			return e
		}
		if r.Method == "DELETE" {
			if _, e = tx.Exec(r.Context(), `DELETE FROM user_devices WHERE tenant_id=$1 AND user_id=$2 AND id=$3`, tenant, user, id); e != nil {
				return e
			}
			for _, table := range []string{"traffic_cursors", "traffic_subjects"} {
				if _, e = tx.Exec(r.Context(), `DELETE FROM `+table+` WHERE tenant_id=$1 AND subject_kind='user' AND subject_id=$2`, tenant, id); e != nil {
					return e
				}
			}
		} else {
			needSync = in.Enabled != nil && *in.Enabled != enabled
			if in.AllNodes == nil {
				allNodes = oldAll
			}
			if in.Services == nil {
				if e = json.Unmarshal(oldServices, &serviceIDs); e != nil {
					return e
				}
			}
			if allNodes {
				serviceIDs = []string{}
			}
			if in.AllNodes != nil || in.Services != nil {
				if e = validateDeviceServices(r.Context(), tx, user, allNodes, serviceIDs); e != nil {
					return e
				}
				if allNodes != oldAll || !equalDeviceServices(oldServices, serviceIDs) {
					needSync = true
				}
			}

			if in.Enabled == nil || version != in.Version {
				return problem{409, "device changed; read its current version"}
			}
			_, e = tx.Exec(r.Context(), `UPDATE user_devices SET name=$4,client_id=$5,enabled=$6,version=version+1,all_nodes=$7,service_ids=$8 WHERE tenant_id=$1 AND user_id=$2 AND id=$3`, tenant, user, id, in.Name, in.Client, *in.Enabled, allNodes, mustJSON(serviceIDs))
			if e != nil {
				return e
			}
			var n int
			if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM user_devices WHERE tenant_id=$1 AND enabled`, tenant).Scan(&n); e != nil {
				return e
			}
			if n > 400 {
				return problem{409, "工作区最多启用 400 个设备"}
			}
		}
	}
	if e = checkDevicePolicies(r.Context(), tx); e != nil {
		return e
	}
	if needSync {
		if e = bumpDeviceUser(r.Context(), tx, user); e != nil {
			return e
		}
	}
	if e = audit(r.Context(), tx, "admin", "user.device-"+strings.ToLower(r.Method), id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	status := 200
	if fresh {
		status = 201
	}
	send(w, status, map[string]any{"id": id, "url": url, "enforcement": "credential_slots", "note": "设备专属凭据可单独撤销；客户端名称/请求头为自报信息。同一凭据被复制时无法可靠区分物理设备。"})
	return nil
}

func (s *Server) rotateDeviceCredentials(ctx context.Context, tx pgx.Tx, user string) error {
	rows, e := tx.Query(ctx, `SELECT id FROM user_devices WHERE tenant_id=$1 AND user_id=$2`, tenant, user)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, id := range ids {
		raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
		sealed, err := live.Seal(s.MasterKey, raw)
		if err != nil {
			return err
		}
		if _, e = tx.Exec(ctx, `UPDATE user_devices SET sealed=$3,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id, sealed); e != nil {
			return e
		}
	}
	return nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func validateDeviceServices(ctx context.Context, tx pgx.Tx, user string, all bool, ids []string) error {
	if all {
		return nil
	}
	if len(ids) > 64 {
		return bad("at most 64 device inbounds")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !manifest.ValidID(id) || seen[id] {
			return bad("invalid or duplicate device inbound")
		}
		seen[id] = true
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) JOIN live_services l ON(l.tenant_id,l.id)=(gs.tenant_id,gs.service_id) WHERE gm.tenant_id=$1 AND gm.user_id=$2 AND l.id=$3 AND l.managed_users AND l.delete_requested_at IS NULL)`, tenant, user, id).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return bad("设备只能选择用户当前已获授权的独立身份入站")
		}
	}
	return nil
}

func equalDeviceServices(raw []byte, ids []string) bool {
	var previous []string
	if json.Unmarshal(raw, &previous) != nil {
		return false
	}
	next := slices.Clone(ids)
	slices.Sort(previous)
	slices.Sort(next)
	return slices.Equal(previous, next)
}
