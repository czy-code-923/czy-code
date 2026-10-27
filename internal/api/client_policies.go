package api

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"vps-control/internal/clientpolicy"
	"vps-control/internal/live"
)

type clientInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Full   bool   `json:"full_config"`
	Note   string `json:"note"`
}

var clients = []clientInfo{
	{"shadowrocket-config", "Shadowrocket 分流配置（配合节点订阅）", "shadowrocket-config", true, "转为 PROXY/DIRECT/REJECT 规则；节点另导入，不保留 Mihomo 交互组"},
	{"naive-proxy", "NaiveProxy 专用 JSON", "naive", false, "仅一个 Naive 节点，不携带策略组；连接地址必须匹配证书域名"},
	{"mihomo", "Mihomo / Clash Meta", "mihomo", true, "原生策略组与规则"},
	{"clash-verge", "Clash Verge Rev", "mihomo", true, "Mihomo YAML"},
	{"stash", "Stash", "mihomo", true, "使用 Clash 风格配置；协议能力取决于客户端版本"},
	{"sing-box", "sing-box", "sing-box", true, "selector/urltest 与原生路由规则"},
	{"hiddify", "Hiddify", "sing-box", true, "完整 sing-box JSON；请使用兼容内核版本"},
	{"nekobox", "NekoBox for Android", "sing-box", true, "完整 sing-box JSON；旧内核可能不支持新增协议"},
	{"sfa", "SFA / SFI", "sing-box", true, "sing-box 原生配置"},
	{"xray", "Xray", "xray", true, "select 使用默认成员，url-test 转换为 leastPing；不等同于 GUI 交互组"},
	{"v2rayng", "v2rayNG", "xray", true, "完整自定义 JSON 订阅，包含 inbounds/outbounds/routing"},
	{"v2rayn", "v2rayN", "xray", true, "完整 Xray 自定义配置"},
	{"shadowrocket", "Shadowrocket 节点列表", "shadowrocket", false, "URI 列表不携带策略组或分流规则"},
	{"v2rayng-uri", "v2rayNG 节点列表", "base64", false, "只导入兼容节点；策略规则需完整 JSON 模式"},
	{"nekobox-uri", "NekoBox 节点列表", "base64", false, "URI 列表不携带策略规则"},
	{"base64", "通用 Base64 节点列表", "base64", false, "不携带策略组或规则"},
}

func clientByID(id string) (clientInfo, bool) {
	if id == "clash" {
		id = "mihomo"
	}
	for _, c := range clients {
		if c.ID == strings.ToLower(id) {
			return c, true
		}
	}
	return clientInfo{}, false
}
func (s *Server) clientCatalog(w http.ResponseWriter, r *http.Request) error {
	send(w, 200, clients)
	return nil
}

type policyInput struct {
	ID      string                     `json:"id"`
	Name    string                     `json:"name"`
	UserID  string                     `json:"user_id"`
	Client  string                     `json:"client_id"`
	Source  string                     `json:"source"`
	Native  map[string]json.RawMessage `json:"native_options"`
	Version int                        `json:"version"`
}

func (s *Server) policies(w http.ResponseWriter, r *http.Request) error {
	if r.Method == "GET" {
		var raw json.RawMessage
		e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,name,COALESCE(user_id,'') user_id,client_id,source,bindings,native_options,version,updated_at FROM client_policies WHERE tenant_id=$1 ORDER BY updated_at DESC LIMIT 500) x`, tenant).Scan(&raw)
		if e != nil {
			return e
		}
		send(w, 200, raw)
		return nil
	}
	id := r.PathValue("id")
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if e = graphLock(r, tx); e != nil {
		return e
	}
	if r.Method == "DELETE" {
		tag, e := tx.Exec(r.Context(), `DELETE FROM client_policies WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if e = audit(r.Context(), tx, "admin", "client-policy.delete", id); e != nil {
			return e
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
		send(w, 200, map[string]bool{"deleted": true})
		return nil
	}
	var b policyInput
	if e = decodeBounded(w, r, &b, 128<<10); e != nil {
		return e
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len([]rune(b.Name)) > 100 {
		return bad("请输入策略名称，最多 100 字")
	}
	if b.Client == "" {
		b.Client = "all"
	}
	if _, ok := clientByID(b.Client); !ok && b.Client != "all" {
		return bad("未知客户端")
	}
	policy, e := clientpolicy.Parse(b.Source)
	if e != nil {
		return bad(e.Error())
	}
	if len(b.Native) > 4 {
		return bad("原生覆盖只支持 mihomo/sing-box/xray/shadowrocket-config")
	}
	for format, raw := range b.Native {
		if !validPolicyNativeFormat(format) {
			return bad("未知原生覆盖格式")
		}
		if e = validateClientOptions(format, raw); e != nil {
			return e
		}
	}
	if b.Native == nil {
		b.Native = map[string]json.RawMessage{}
	}
	previous := map[string]string{}
	if id != "" {
		var raw []byte
		if e = tx.QueryRow(r.Context(), `SELECT bindings FROM client_policies WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&raw); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &previous); e != nil {
			return e
		}
	}
	groups := map[string]bool{"PROXY": true}
	for _, g := range policy.Groups {
		groups[g.Name] = true
	}
	rows, e := tx.Query(r.Context(), `SELECT id,name FROM live_services WHERE tenant_id=$1 AND delete_requested_at IS NULL AND kind NOT IN ('frps','frpc')`, tenant)
	if e != nil {
		return e
	}
	names := map[string][]string{}
	ids := map[string]bool{}
	for rows.Next() {
		var node, name string
		if e = rows.Scan(&node, &name); e != nil {
			rows.Close()
			return e
		}
		names[name] = append(names[name], node)
		ids[node] = true
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	bindings := map[string]string{}
	for _, ref := range policy.References() {
		if groups[ref] || clientpolicy.Dynamic(ref) {
			continue
		}
		if old := previous[ref]; old != "" && ids[old] {
			bindings[ref] = old
			continue
		}
		if len(names[ref]) != 1 {
			return bad("节点名称不存在或重名，请从列表选择稳定的节点引用: " + ref)
		}
		bindings[ref] = names[ref][0]
	}
	rawBindings, _ := json.Marshal(bindings)
	rawNative, _ := json.Marshal(b.Native)
	if id == "" {
		var n int
		if e = tx.QueryRow(r.Context(), `SELECT count(*) FROM client_policies WHERE tenant_id=$1`, tenant).Scan(&n); e != nil {
			return e
		}
		if n >= 500 {
			return bad("最多 500 个用户/客户端策略")
		}
		id = ID()
		_, e = tx.Exec(r.Context(), `INSERT INTO client_policies(tenant_id,id,name,user_id,client_id,source,bindings,native_options) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8)`, tenant, id, b.Name, b.UserID, b.Client, b.Source, rawBindings, rawNative)
	} else {
		tag, err := tx.Exec(r.Context(), `UPDATE client_policies SET name=$3,user_id=NULLIF($4,''),client_id=$5,source=$6,bindings=$7,native_options=$8,version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND version=$9`, tenant, id, b.Name, b.UserID, b.Client, b.Source, rawBindings, rawNative, b.Version)
		e = err
		if e == nil && tag.RowsAffected() == 0 {
			return problem{409, "策略已修改，请刷新后重试"}
		}
	}
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "client-policy.save", id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, map[string]string{"id": id})
	return nil
}
func (s *Server) parsePolicy(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Source string               `json:"source"`
		Policy *clientpolicy.Policy `json:"policy"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	var p clientpolicy.Policy
	var e error
	if b.Policy != nil {
		p = *b.Policy
		e = p.Validate()
	} else {
		p, e = clientpolicy.Parse(b.Source)
	}
	if e != nil {
		return bad(e.Error())
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	var generic any
	_ = json.Unmarshal(raw, &generic)
	source, e := marshalPolicyYAML(p)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"policy": generic, "source": source})
	return nil
}
func (s *Server) selectedPolicy(r *http.Request, user, client string) (*clientpolicy.Policy, map[string]string, map[string]json.RawMessage, string, error) {
	var source, id string
	var bindingsRaw, nativeRaw []byte
	e := s.DB.QueryRow(r.Context(), `SELECT id,source,bindings,native_options FROM client_policies WHERE tenant_id=$1 AND (user_id=$2 OR user_id IS NULL) AND client_id IN ($3,'all') ORDER BY (user_id IS NOT NULL) DESC,(client_id=$3) DESC LIMIT 1`, tenant, user, client).Scan(&id, &source, &bindingsRaw, &nativeRaw)
	if e == pgx.ErrNoRows {
		return nil, nil, nil, "", nil
	}
	if e != nil {
		return nil, nil, nil, "", e
	}
	p, e := clientpolicy.Parse(source)
	if e != nil {
		return nil, nil, nil, "", e
	}
	bindings := map[string]string{}
	native := map[string]json.RawMessage{}
	if e = json.Unmarshal(bindingsRaw, &bindings); e != nil {
		return nil, nil, nil, "", e
	}
	if e = json.Unmarshal(nativeRaw, &native); e != nil {
		return nil, nil, nil, "", e
	}
	return &p, bindings, native, id, nil
}
func (s *Server) applyClientPolicy(r *http.Request, user, client, format string, nodes []clientpolicy.Node, base map[string]any) (map[string]any, []string, error) {
	p, bindings, native, _, e := s.selectedPolicy(r, user, client)
	if e != nil || p == nil {
		return base, nil, e
	}
	resolved, e := p.Resolve(nodes, bindings)
	if e != nil {
		return nil, nil, e
	}
	base, warnings, e := resolved.Apply(format, base)
	if e != nil {
		return nil, nil, e
	}
	if raw := native[format]; len(raw) > 0 {
		base, e = mergeClientOptions(base, raw)
	}
	return base, warnings, e
}
func policyFamily(address string) string {
	if strings.Contains(address, ":") {
		return "ipv6"
	}
	for _, r := range address {
		if (r < '0' || r > '9') && r != '.' {
			return "dns"
		}
	}
	return "ipv4"
}
func selectedClient(r *http.Request, profile string) (clientInfo, error) {
	id := r.URL.Query().Get("client")
	if id == "" {
		id = r.URL.Query().Get("format")
	}
	if id == "" {
		id = profile
	}
	if id == "" {
		ua := strings.ToLower(r.UserAgent())
		switch {
		case strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo"):
			id = "mihomo"
		case strings.Contains(ua, "v2rayng"):
			id = "v2rayng"
		case strings.Contains(ua, "shadowrocket"):
			id = "shadowrocket"
		case strings.Contains(ua, "hiddify"):
			id = "hiddify"
		case strings.Contains(ua, "nekobox"):
			id = "nekobox"
		default:
			id = "sing-box"
		}
	}
	if c, ok := clientByID(id); ok {
		return c, nil
	}
	return clientInfo{}, bad(fmt.Sprint("不支持的客户端: ", id))
}
func sharedGroupProtocol(spec live.Spec) bool {
	return !spec.ManagedUsers && (spec.Kind == "snell" || spec.Kind == "shadowsocks")
}
