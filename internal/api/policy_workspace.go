package api

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"slices"
	"strings"
	"vps-control/internal/clientpolicy"
	"vps-control/internal/live"
)

// Install the built-in template once for new workspaces. Existing global/user
// choices are preserved, and deliberately deleting it does not recreate it.
func (s *Server) EnsureDefaultClientPolicy(ctx context.Context) error {
	p, e := clientpolicy.Default()
	if e != nil {
		return e
	}
	source, e := marshalPolicyYAML(p)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var marker string
	e = tx.QueryRow(ctx, `INSERT INTO client_policy_bootstrap(tenant_id) VALUES($1) ON CONFLICT DO NOTHING RETURNING tenant_id`, tenant).Scan(&marker)
	if e == pgx.ErrNoRows {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO client_policies(tenant_id,id,name,client_id,source) VALUES($1,$2,'Mihomo Max 默认模板','all',$3) ON CONFLICT DO NOTHING`, tenant, ID(), source)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *Server) defaultPolicy(w http.ResponseWriter, r *http.Request) error {
	p, e := clientpolicy.Default()
	if e != nil {
		return e
	}
	source, e := marshalPolicyYAML(p)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"name": "Mihomo Max 默认模板", "policy": p, "source": source, "native_options": map[string]any{}, "notes": []string{"基于提供的 mihomo_max.yaml：21 组、186 条规则，基础/DNS/策略组/规则独立编辑", "不含节点凭据；节点按协议和 IPv4/IPv6 动态匹配已授权服务", "默认关闭 LAN/TUN，DNS/控制器使用回环监听；respect-rules 时关闭 prefer-h3"}})
	return nil
}
func (s *Server) importPolicy(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Source string `json:"source"`
	}
	if e := decodeBounded(w, r, &b, 160<<10); e != nil {
		return e
	}
	result, e := clientpolicy.Import(b.Source)
	if e != nil {
		return bad(e.Error())
	}
	rows, e := s.DB.Query(r.Context(), `SELECT id,name,kind,address,port,sealed FROM live_services WHERE tenant_id=$1 AND delete_requested_at IS NULL AND kind NOT IN ('frps','frpc')`, tenant)
	if e != nil {
		return e
	}
	defer rows.Close()
	nodes := []clientpolicy.Candidate{}
	for rows.Next() {
		var n clientpolicy.Candidate
		var address string
		var sealed []byte
		if e = rows.Scan(&n.ID, &n.Name, &n.Protocol, &address, &n.Port, &sealed); e != nil {
			return e
		}
		n.Addresses = []string{address}
		raw, err := live.Open(s.MasterKey, sealed)
		if err != nil {
			return err
		}
		var spec live.Spec
		if err = json.Unmarshal(raw, &spec); err != nil {
			return err
		}
		n.Addresses = append(n.Addresses, spec.EndpointAddresses...)
		nodes = append(nodes, n)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	mappings := []map[string]string{}
	known := map[string]bool{"PROXY": true}
	for _, g := range result.Policy.Groups {
		known[g.Name] = true
	}
	for _, a := range result.Aliases {
		ref, reason := clientpolicy.MatchAlias(a, nodes)
		mappings = append(mappings, map[string]string{"name": a.Name, "protocol": a.Protocol, "reference": ref, "match_reason": reason})
		if ref != "" {
			result.Warnings = append(result.Warnings, "节点别名 "+a.Name+" → "+ref)
		}
	}
	unresolved := []string{}
	for _, ref := range result.Policy.References() {
		if known[ref] || clientpolicy.Dynamic(ref) {
			continue
		}
		if !slices.Contains(unresolved, ref) {
			unresolved = append(unresolved, ref)
		}
	}
	source, e := marshalPolicyYAML(result.Policy)
	if e != nil {
		return e
	}
	send(w, 200, map[string]any{"format": result.Format, "policy": result.Policy, "source": source, "warnings": result.Warnings, "mappings": mappings, "unresolved": unresolved, "unsupported_rules": result.UnsupportedRules, "native_options": result.Native})
	return nil
}
func (s *Server) previewPolicy(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Source string                     `json:"source"`
		Client string                     `json:"client_id"`
		Native map[string]json.RawMessage `json:"native_options"`
	}
	if e := decodeBounded(w, r, &b, 160<<10); e != nil {
		return e
	}
	p, e := clientpolicy.Parse(b.Source)
	if e != nil {
		return bad(e.Error())
	}
	c, ok := clientByID(b.Client)
	if !ok || !c.Full {
		return bad("请选择支持分流配置的客户端")
	}
	rows, e := s.DB.Query(r.Context(), `SELECT id,name,kind,host_id FROM live_services WHERE tenant_id=$1 AND delete_requested_at IS NULL AND kind NOT IN ('frps','frpc') ORDER BY created_at LIMIT 128`, tenant)
	if e != nil {
		return e
	}
	defer rows.Close()
	nodes := []clientpolicy.Node{}
	outs := []any{}
	for rows.Next() {
		var n clientpolicy.Node
		if e = rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Host); e != nil {
			return e
		}
		n.Tag = n.Name + "-" + n.ID[:8]
		n.Family = "ipv4"
		nodes = append(nodes, n)
		outs = append(outs, map[string]any{"tag": n.Tag, "name": n.Tag, "type": "socks", "protocol": "socks", "server": "127.0.0.1", "server_port": 9, "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 9}}}})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	resolved, e := p.Resolve(nodes, nil)
	if e != nil {
		return bad(e.Error())
	}
	if c.Format == "shadowrocket-config" {
		text, warnings, e := resolved.Shadowrocket()
		if e != nil {
			return bad(e.Error())
		}
		send(w, 200, map[string]any{"preview_only": true, "text": text, "warnings": warnings, "nodes": nodes})
		return nil
	}
	base := map[string]any{"outbounds": outs, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "listen_port": 1080, "port": 1080, "type": "mixed", "protocol": "socks"}}}
	if c.Format == "mihomo" {
		base = map[string]any{"proxies": outs}
	}
	config, warnings, e := resolved.Apply(c.Format, base)
	if e != nil {
		return bad(e.Error())
	}
	if raw := b.Native[c.Format]; len(raw) > 0 {
		if e = validateClientOptions(c.Format, raw); e != nil {
			return e
		}
		config, e = mergeClientOptions(config, raw)
		if e != nil {
			return bad(e.Error())
		}
	}
	send(w, 200, map[string]any{"preview_only": true, "config": config, "warnings": warnings, "note": "仅编译预览，节点是本地不可用占位，不含身份信息；实际订阅仅包含该用户已授权且运行中的兼容节点"})
	return nil
}

// Canonical sections are stored separately, while the source remains an editable representation.
func validPolicyNativeFormat(format string) bool {
	return slices.Contains([]string{"mihomo", "sing-box", "xray", "shadowrocket-config"}, strings.ToLower(format))
}
