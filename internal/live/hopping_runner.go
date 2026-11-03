package live

import (
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"time"
	"vps-control/internal/manifest"
)

type hopRule struct {
	ID     string `json:"id"`
	Listen string `json:"listen"`
	Port   int    `json:"port"`
	Ports  string `json:"ports"`
}

func workHops(w Work) []hopRule {
	if !w.Enabled {
		return nil
	}
	if len(w.Members) > 0 {
		result := []hopRule{}
		for _, m := range w.Members {
			result = append(result, workHops(m)...)
		}
		return result
	}
	if w.Spec.HopPorts == "" {
		return nil
	}
	return []hopRule{{w.ID, w.Spec.Listen, w.Spec.Port, w.Spec.HopPorts}}
}
func (r *Runner) syncHopping() {
	if runtime.GOOS != "linux" {
		return
	}
	rules := []hopRule{}
	for _, p := range r.processes {
		if p.cmd != nil {
			rules = append(rules, p.hops...)
		}
	}
	if len(rules) == 0 && r.hopRevision == "" {
		if _, e := r.root.Stat("port-hopping.json"); os.IsNotExist(e) {
			return
		}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	raw, _ := json.Marshal(rules)
	revision := manifest.Digest(raw)
	if len(rules) == 0 && r.hopRevision == revision && r.hoppingReady() {
		if r.root.Remove("port-hopping.json") == nil {
			r.hopRevision = ""
		}
		return
	}
	request, _ := json.Marshal(map[string]any{"revision": revision, "updated_at": time.Now().Unix(), "rules": rules})
	f, e := r.root.OpenFile("port-hopping.json.tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		r.hopRevision = "unavailable"
		return
	}
	_, e = f.Write(request)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = r.root.Rename("port-hopping.json.tmp", "port-hopping.json")
	}
	if e != nil {
		r.hopRevision = "unavailable"
		return
	}
	r.hopRevision = revision
}
func (r *Runner) hoppingReady() bool {
	if r.hopRevision == "" {
		return true
	}
	if runtime.GOOS != "linux" {
		return false
	}
	raw, e := r.root.ReadFile("port-hopping.status")
	if e != nil || len(raw) > 2048 {
		return false
	}
	var status struct {
		Revision string `json:"revision"`
		Updated  int64  `json:"updated_at"`
		Ready    bool   `json:"ready"`
	}
	if json.Unmarshal(raw, &status) != nil {
		return false
	}
	age := time.Now().Unix() - status.Updated
	return status.Ready && status.Revision == r.hopRevision && age >= 0 && age <= 30
}
