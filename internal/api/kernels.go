package api

import (
	"encoding/json"
	"net/http"
	"vps-control/internal/live"
)

func (s *Server) kernels(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT k.*,(k.available AND k.checked_at>now()-interval '45 seconds') ready,(k.applied_checksum=k.desired_checksum AND k.desired_checksum<>'' AND k.reported_at>now()-interval '45 seconds') current,(SELECT count(*) FROM live_services l WHERE l.tenant_id=k.tenant_id AND l.host_id=k.host_id AND l.engine=k.engine AND l.kind NOT IN ('frps','frpc')) inbounds,(SELECT count(*) FROM kernel_revisions v WHERE v.tenant_id=k.tenant_id AND v.host_id=k.host_id AND v.engine=k.engine) revisions FROM host_kernels k WHERE k.tenant_id=$1 AND k.host_id=$2 ORDER BY k.engine) x`, tenant, r.PathValue("id")).Scan(&raw)
	if e != nil {
		return e
	}
	send(w, 200, raw)
	return nil
}
func (s *Server) kernelConfig(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	engine := r.URL.Query().Get("engine")
	if engine != "sing-box" && engine != "xray" {
		return bad("choose sing-box or xray")
	}
	var sealed []byte
	var checksum string
	e := s.DB.QueryRow(r.Context(), `SELECT sealed,checksum FROM kernel_revisions WHERE tenant_id=$1 AND host_id=$2 AND engine=$3 ORDER BY created_at DESC LIMIT 1`, tenant, r.PathValue("id"), engine).Scan(&sealed, &checksum)
	if e != nil {
		return e
	}
	raw, e := live.Open(s.MasterKey, sealed)
	if e != nil {
		return e
	}
	var bundle live.Work
	if e = json.Unmarshal(raw, &bundle); e != nil {
		return e
	}
	var preview any
	_, files, e := bundle.Render()
	if e != nil {
		return bad(e.Error())
	}
	if e = json.Unmarshal(files["config.json"], &preview); e != nil {
		return e
	}
	send(w, 200, map[string]any{"engine": engine, "checksum": checksum, "members": len(bundle.Members), "rendered": redactLive(preview)})
	return nil
}

type clientProfile struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Format    string          `json:"format"`
	Port      int             `json:"local_port"`
	Selection string          `json:"selection"`
	TestURL   string          `json:"test_url"`
	Extra     json.RawMessage `json:"extra_config,omitempty"`
	ExtraText *string         `json:"extra_text,omitempty"`
	Version   int             `json:"version"`
}

func (s *Server) clientProfiles(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,format,local_port,selection,test_url,extra_config,extra_source extra_text,version FROM client_profiles WHERE tenant_id=$1 ORDER BY name LIMIT 100) x`, tenant).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	var b clientProfile
	if e := decode(w, r, &b); e != nil {
		return e
	}
	client, clientOK := clientByID(b.Format)
	if b.Name == "" || len(b.Name) > 100 || !clientOK || b.Port < 1024 || b.Port > 65535 || (b.Selection != "select" && b.Selection != "urltest") {
		return bad("name, client format, local port and selection required")
	}
	if b.TestURL != "https://www.gstatic.com/generate_204" && b.TestURL != "https://cp.cloudflare.com/generate_204" {
		return bad("choose a supported connectivity test URL")
	}
	if b.ExtraText != nil {
		var e error
		b.Extra, e = parseClientText(*b.ExtraText)
		if e != nil {
			return e
		}
	}
	if len(b.Extra) == 0 {
		b.Extra = json.RawMessage(`{}`)
	}
	if e := validateClientOptions(client.Format, b.Extra); e != nil {
		return e
	}
	source := ""
	if b.ExtraText != nil {
		source = *b.ExtraText
	} else {
		pretty, e := json.MarshalIndent(b.Extra, "", "  ")
		if e != nil {
			return e
		}
		source = string(pretty)
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	if id == "" {
		id = ID()
		_, e = tx.Exec(r.Context(), `INSERT INTO client_profiles(tenant_id,id,name,format,local_port,selection,test_url,extra_config,extra_source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenant, id, b.Name, b.Format, b.Port, b.Selection, b.TestURL, b.Extra, source)
	} else {
		tag, err := tx.Exec(r.Context(), `UPDATE client_profiles SET name=$3,format=$4,local_port=$5,selection=$6,test_url=$7,extra_config=$8,extra_source=$9,version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$10`, tenant, id, b.Name, b.Format, b.Port, b.Selection, b.TestURL, b.Extra, source, b.Version)
		e = err
		if err == nil && tag.RowsAffected() == 0 {
			return problem{409, "client profile changed; refresh before saving"}
		}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "client-profile.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
