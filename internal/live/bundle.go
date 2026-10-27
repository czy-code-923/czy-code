package live

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"vps-control/internal/manifest"
)

// BundleWork makes a single process per host/kernel. FRP has independent lifecycle.
// Both control-plane preview and Agent use this compiler, with no arbitrary native JSON.
func BundleWork(work []Work) []Work {
	result := []Work{}
	groups := map[string][]Work{}
	for _, w := range work {
		if !Proxy(w.Spec.Kind) {
			result = append(result, w)
			continue
		}
		engine := w.Spec.Engine
		if engine == "" {
			engine = "sing-box"
		}
		groups[engine] = append(groups[engine], w)
	}
	for _, engine := range []string{"sing-box", "xray"} {
		members := groups[engine]
		if len(members) == 0 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		enabled := false
		for _, m := range members {
			enabled = enabled || m.Enabled
		}
		id := manifest.Digest([]byte("harbor-kernel-" + engine))[:32]
		result = append(result, Work{ID: id, Version: 1, Enabled: enabled, Spec: Spec{Engine: engine}, Members: members})
	}
	return result
}
func (w Work) Render() (string, map[string][]byte, error) {
	if len(w.Members) == 0 {
		return w.Spec.Render()
	}
	if len(w.Members) > 64 {
		return "", nil, errors.New("too many inbounds")
	}
	files := map[string][]byte{}
	ins, outs, rules := []any{}, []any{}, []any{}
	seen := map[string]bool{}
	for _, m := range w.Members {
		if len(m.Members) > 0 || !manifest.ValidID(m.ID) || seen[m.ID] {
			return "", nil, errors.New("invalid bundle member")
		}
		seen[m.ID] = true
		if !m.Enabled {
			continue
		}
		engine, part, e := m.Spec.Render()
		if e != nil {
			return "", nil, e
		}
		if engine != w.Spec.Engine {
			return "", nil, errors.New("bundle engine mismatch")
		}
		var cfg map[string]any
		if e = json.Unmarshal(part["config.json"], &cfg); e != nil {
			return "", nil, e
		}
		rename := map[string]string{"entry": "in-" + m.ID, "egress": "out-" + m.ID, "egress-block": "block-" + m.ID}
		for name, body := range part {
			if name != "config.json" {
				newName := m.ID + "-" + name
				rename[name] = newName
				files[newName] = body
			}
		}
		var replace func(any) any
		replace = func(v any) any {
			switch a := v.(type) {
			case string:
				if n, ok := rename[a]; ok {
					return n
				}
			case map[string]any:
				for k, x := range a {
					a[k] = replace(x)
				}
			case []any:
				for i, x := range a {
					a[i] = replace(x)
				}
			}
			return v
		}
		replace(cfg)
		ins = append(ins, cfg["inbounds"].([]any)...)
		outs = append(outs, cfg["outbounds"].([]any)...)
		if engine == "xray" {
			if routing, ok := cfg["routing"].(map[string]any); ok {
				if extra, ok := routing["rules"].([]any); ok {
					for _, item := range extra {
						rule := item.(map[string]any)
						rule["inboundTag"] = []string{rename["entry"]}
						rules = append(rules, rule)
					}
				}
			}
		} else {
			if route, ok := cfg["route"].(map[string]any); ok {
				if extra, ok := route["rules"].([]any); ok {
					for _, item := range extra {
						rule := item.(map[string]any)
						rule["inbound"] = []string{rename["entry"]}
						rules = append(rules, rule)
					}
				}
			}
		}
		if engine == "xray" {
			rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{rename["entry"]}, "outboundTag": rename["egress"]})
		} else {
			rules = append(rules, map[string]any{"inbound": []string{rename["entry"]}, "action": "route", "outbound": rename["egress"]})
		}
	}
	cfg := map[string]any{"inbounds": ins, "outbounds": outs}
	needsDNS := false
	for _, m := range w.Members {
		if m.Enabled && len(m.Spec.EgressFamilies) > 0 {
			needsDNS = true
		}
	}
	if needsDNS {
		if w.Spec.Engine == "xray" {
			cfg["dns"] = map[string]any{"servers": []string{"localhost"}}
		} else {
			cfg["dns"] = map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "host-dns"}}}
		}
	}
	if w.Spec.Engine == "xray" {
		cfg["log"] = map[string]any{"loglevel": "warning"}
		cfg["routing"] = map[string]any{"rules": rules}
	} else {
		cfg["log"] = map[string]any{"level": "warn"}
		cfg["route"] = map[string]any{"rules": rules}
	}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	files["config.json"] = raw
	return w.Spec.Engine, files, e
}
func (w Work) validate() error {
	if len(w.Members) == 0 {
		return w.Spec.Validate()
	}
	_, _, e := w.Render()
	return e
}

// Revision fingerprints contain credentials only as a SHA-256 digest.
func (w Work) Revision() string { raw, _ := json.Marshal(w); return manifest.Digest(raw) }
func BundleCapable(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	values := []int{}
	for _, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 {
			return false
		}
		values = append(values, n)
	}
	return values[0] > 0 || values[1] >= 8
}

type KernelReport struct {
	Engine   string `json:"engine"`
	Checksum string `json:"checksum"`
	State    string `json:"state"`
}

func (r *Runner) KernelReports() []KernelReport {
	result := []KernelReport{}
	for _, p := range r.processes {
		if len(p.members) > 0 {
			result = append(result, KernelReport{p.engine, p.sum, p.state})
		}
	}
	return result
}
