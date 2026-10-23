package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
	"vps-control/internal/live"
)

func (s *Server) users(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	if r.Method == "GET" {
		var data json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,enabled,expires_at,created_at,(enabled AND expires_at>now()) active FROM proxy_users WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 500) x`, tenant).Scan(&data)
		if e != nil {
			return e
		}
		send(w, 200, data)
		return nil
	}
	var b struct {
		Name    string    `json:"name"`
		Enabled *bool     `json:"enabled"`
		Expires time.Time `json:"expires_at"`
		Rotate  bool      `json:"rotate_credentials"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || b.Enabled == nil || b.Expires.IsZero() || b.Expires.After(time.Now().AddDate(10, 0, 0)) {
		return bad("name, enabled and expiry required (maximum 10 years)")
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	fresh := id == ""
	if fresh {
		id = ID()
		var n int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM proxy_users WHERE tenant_id=$1`, tenant).Scan(&n); e != nil {
			return e
		}
		if n >= 500 {
			return bad("at most 500 users")
		}
	}
	var sealed []byte
	if fresh || b.Rotate {
		raw, _ := json.Marshal(live.Account{Name: id, UUID: live.UUID(), Password: live.Secret()})
		sealed, e = live.Seal(s.MasterKey, raw)
		if e != nil {
			return e
		}
	}
	if fresh {
		_, e = tx.Exec(r.Context(), `INSERT INTO proxy_users(tenant_id,id,name,enabled,expires_at,sealed) VALUES($1,$2,$3,$4,$5,$6)`, tenant, id, b.Name, *b.Enabled, b.Expires, sealed)
	} else {
		var exists string
		e = tx.QueryRow(r.Context(), `UPDATE proxy_users SET name=$3,enabled=$4,expires_at=$5,sealed=COALESCE($6,sealed) WHERE tenant_id=$1 AND id=$2 RETURNING id`, tenant, id, b.Name, *b.Enabled, b.Expires, sealed).Scan(&exists)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id IN (SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=$1 AND gm.user_id=$2)`, tenant, id); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "proxy-user.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
func (s *Server) groups(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var data json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT g.id,g.name,g.version,COALESCE((SELECT jsonb_agg(user_id) FROM user_group_members WHERE tenant_id=g.tenant_id AND group_id=g.id),'[]') user_ids,COALESCE((SELECT jsonb_agg(service_id) FROM user_group_services WHERE tenant_id=g.tenant_id AND group_id=g.id),'[]') service_ids FROM user_groups g WHERE tenant_id=$1 ORDER BY name LIMIT 200) x`, tenant).Scan(&data)
		if e != nil {
			return e
		}
		send(w, 200, data)
		return nil
	}
	var b struct {
		Name     string   `json:"name"`
		Users    []string `json:"user_ids"`
		Services []string `json:"service_ids"`
		Version  int      `json:"version"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 100 || len(b.Users) > 500 || len(b.Services) > 64 {
		return bad("name, at most 500 users and 64 inbounds required")
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
	if id == "" {
		id = ID()
		_, e = tx.Exec(r.Context(), `INSERT INTO user_groups(tenant_id,id,name) VALUES($1,$2,$3)`, tenant, id, b.Name)
	} else {
		var actual int
		e = tx.QueryRow(r.Context(), `SELECT version FROM user_groups WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&actual)
		if e != nil {
			return e
		}
		if actual != b.Version {
			return problem{409, "group changed; refresh"}
		}
		_, e = tx.Exec(r.Context(), `UPDATE user_groups SET name=$3,version=version+1 WHERE tenant_id=$1 AND id=$2`, tenant, id, b.Name)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id IN (SELECT service_id FROM user_group_services WHERE tenant_id=$1 AND group_id=$2)`, tenant, id); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM user_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, id); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `DELETE FROM user_group_services WHERE tenant_id=$1 AND group_id=$2`, tenant, id); e != nil {
		return e
	}
	for _, user := range b.Users {
		if _, e = tx.Exec(r.Context(), `INSERT INTO user_group_members VALUES($1,$2,$3)`, tenant, id, user); e != nil {
			return e
		}
	}
	for _, service := range b.Services {
		var sealed []byte
		var av string
		e = tx.QueryRow(r.Context(), `SELECT l.sealed,h.agent_version FROM live_services l JOIN hosts h ON (l.tenant_id,l.host_id)=(h.tenant_id,h.id) WHERE l.tenant_id=$1 AND l.id=$2`, tenant, service).Scan(&sealed, &av)
		if e != nil {
			return e
		}
		raw, err := live.Open(s.MasterKey, sealed)
		if err != nil {
			return err
		}
		var spec live.Spec
		if e = json.Unmarshal(raw, &spec); e != nil {
			return e
		}
		if !spec.ManagedUsers || !live.BundleCapable(av) {
			return bad("group inbounds must enable managed users on Agent 0.8.0+")
		}
		if _, e = tx.Exec(r.Context(), `INSERT INTO user_group_services VALUES($1,$2,$3)`, tenant, id, service); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(r.Context(), `UPDATE live_services SET version=version+1,state='pending' WHERE tenant_id=$1 AND id=ANY($2)`, tenant, b.Services); e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "proxy-group.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
func (s *Server) materializeAccounts(r *http.Request, tx pgx.Tx, id string, spec *live.Spec) error {
	if !spec.ManagedUsers {
		return nil
	}
	spec.Accounts = []live.Account{}
	rows, e := tx.Query(r.Context(), `SELECT DISTINCT u.id,u.sealed FROM proxy_users u JOIN user_group_members m ON (u.tenant_id,u.id)=(m.tenant_id,m.user_id) JOIN user_group_services g ON (g.tenant_id,g.group_id)=(m.tenant_id,m.group_id) WHERE u.tenant_id=$1 AND g.service_id=$2 AND u.enabled AND u.expires_at>now() ORDER BY u.id LIMIT 500`, tenant, id)
	if e != nil {
		return e
	}
	for rows.Next() {
		var user string
		var sealed []byte
		if e = rows.Scan(&user, &sealed); e != nil {
			rows.Close()
			return e
		}
		raw, err := live.Open(s.MasterKey, sealed)
		if err != nil {
			rows.Close()
			return err
		}
		var a live.Account
		if e = json.Unmarshal(raw, &a); e != nil {
			rows.Close()
			return e
		}
		a.Name = user
		spec.Accounts = append(spec.Accounts, a)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	// A reserved machine identity is admitted only when another managed inbound uses this next hop.
	var chain bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM live_services WHERE tenant_id=$1 AND next_id=$2)`, tenant, id).Scan(&chain); e != nil {
		return e
	}
	if chain {
		spec.Accounts = append(spec.Accounts, live.Account{Name: "harbor-chain", UUID: spec.UUID, Password: spec.Password})
	}
	return nil
}
