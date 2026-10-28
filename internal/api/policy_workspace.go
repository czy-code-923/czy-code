package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"slices"
	"strings"
	"vps-control/internal/clientpolicy"
	"vps-control/internal/live"
)

const legacyDefaultSourceSHA = "5bac8a5edb37b4e89c4a4406c82efb8f3099e429d4d43c0ad7ff438ef2defbfc"

// Only the byte-exact, uncustomized factory template is eligible for migration.
// User policies and deliberately deleted templates are preserved.
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
	var oldID, oldSource string
	e = tx.QueryRow(ctx, `SELECT id,source FROM client_policies WHERE tenant_id=$1 AND user_id IS NULL AND client_id='all' AND name='Mihomo Max 默认模板' AND bindings='{}'::jsonb FOR UPDATE`, tenant).Scan(&oldID, &oldSource)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == nil && fmt.Sprintf("%x", sha256.Sum256([]byte(oldSource))) == legacyDefaultSourceSHA {
		if _, e = tx.Exec(ctx, `UPDATE client_policies SET source=$3,version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, oldID, source); e != nil {
			return e
		}
		if e = audit(ctx, tx, "system", "client-policy.factory-migrate", oldID); e != nil {
			return e
		}
	}
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
	send(w, 200, map[string]any{"name": "Mihomo Max 默认模板", "policy": p, "source": source, "native_options": map[string]any{}, "notes": []string{"基于提供的 mihomo_max.yaml：21 组、186 条规则，基础/DNS/策略组/规则独立编辑", "不含虚构协议或节点；按实际已授权节点的 IPv4/IPv6 端点选择", "默认关闭 LAN/TUN，DNS/控制器使用回环监听；respect-rules 时关闭 prefer-h3"}})
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
	aliases := map[string]bool{}
	for _, g := range result.Policy.Groups {
		known[g.Name] = true
	}
	for _, a := range result.Aliases {
		aliases[a.Name] = true
		ref, reason := clientpolicy.MatchAlias(a, nodes)
		mappings = append(mappings, map[string]string{"name": a.Name, "protocol": a.Protocol, "reference": ref, "match_reason": reason})
		if ref != "" {
			known[a.Name] = true
			result.Warnings = append(result.Warnings, "节点别名 "+a.Name+" → "+ref)
		}
	}
	unresolved := []string{}
	for _, ref := range result.Policy.References() {
		if known[ref] || clientpolicy.Dynamic(ref) {
			continue
		}
		if !aliases[ref] {
			aliases[ref] = true
			matched, reason := clientpolicy.MatchAlias(clientpolicy.Alias{Name: ref}, nodes)
			mappings = append(mappings, map[string]string{"name": ref, "protocol": "", "reference": matched, "match_reason": reason})
			if matched != "" {
				known[ref] = true
				continue
			}
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
		User   string                     `json:"user_id"`
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
	rows, e := s.DB.Query(r.Context(), `SELECT id,name,kind,host_id,address,sealed FROM live_services l WHERE tenant_id=$1 AND delete_requested_at IS NULL AND kind NOT IN ('frps','frpc') AND ($2='' OR EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=$2)) ORDER BY created_at LIMIT 128`, tenant, b.User)
	if e != nil {
		return e
	}
	defer rows.Close()
	nodes := []clientpolicy.Node{}
	outs := []any{}
	for rows.Next() {
		var n clientpolicy.Node
		var address string
		var sealed []byte
		if e = rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Host, &address, &sealed); e != nil {
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
		addresses := spec.EndpointAddresses
		if len(addresses) == 0 {
			addresses = []string{address}
		}
		for _, address := range addresses {
			endpoint := n
			endpoint.Tag = n.Name + "-" + n.ID[:8] + "-" + clientpolicy.AddressFamily(address)
			endpoint.Family = clientpolicy.AddressFamily(address)
			nodes = append(nodes, endpoint)
			outs = append(outs, map[string]any{"tag": endpoint.Tag, "name": endpoint.Tag, "type": "socks", "protocol": "socks", "server": "127.0.0.1", "server_port": 9, "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 9}}}})
		}
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
