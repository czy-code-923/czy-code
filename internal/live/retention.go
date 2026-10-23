package live

import (
	"encoding/hex"
	"sort"
	"time"
)

// Private, ID-scoped state: retain current and one previous immutable revision.
func (r *Runner) prune(id, current string) {
	f, e := r.root.Open(id)
	if e != nil {
		return
	}
	items, e := f.ReadDir(-1)
	f.Close()
	if e != nil {
		return
	}
	type revision struct {
		name     string
		modified time.Time
	}
	old := []revision{}
	for _, item := range items {
		if !item.IsDir() || item.Name() == current || len(item.Name()) != 64 {
			continue
		}
		if _, e := hex.DecodeString(item.Name()); e != nil {
			continue
		}
		info, e := item.Info()
		if e == nil {
			old = append(old, revision{item.Name(), info.ModTime()})
		}
	}
	sort.Slice(old, func(i, j int) bool { return old[i].modified.After(old[j].modified) })
	for i := 1; i < len(old); i++ {
		_ = r.root.RemoveAll(id + "/" + old[i].name)
	}
}
