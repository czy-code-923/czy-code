package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

type Request struct {
	ID      string `json:"id"`
	Engine  string `json:"engine"`
	Version string `json:"version"`
}
type Report struct {
	Request
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}
type Candidate struct {
	Request
	Binary executor.Binary
}
type managerState struct {
	Overrides map[string]executor.Binary `json:"overrides"`
	Reports   []Report                   `json:"reports"`
}
type Manager struct {
	mu        sync.Mutex
	dir       string
	state     managerState
	busy      bool
	candidate chan Candidate
}

func NewManager(dir string) (*Manager, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("absolute private engine state directory required")
	}
	m := &Manager{dir: filepath.Join(dir, "engine-versions"), candidate: make(chan Candidate, 1), state: managerState{Overrides: map[string]executor.Binary{}, Reports: []Report{}}}
	if e := os.MkdirAll(m.dir, 0700); e != nil {
		return nil, e
	}
	raw, e := os.ReadFile(filepath.Join(m.dir, "active.json"))
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &m.state); e != nil {
		return nil, e
	}
	for name, b := range m.state.Overrides {
		relative, e := filepath.Rel(m.dir, b.Path)
		if Repository(name) == "" || e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("invalid persisted engine path")
		}
		hash, e := executor.FileDigest(b.Path)
		if e != nil || hash != b.SHA256 {
			return nil, errors.New("persisted engine digest mismatch")
		}
	}
	for i := range m.state.Reports {
		if m.state.Reports[i].State != "succeeded" && m.state.Reports[i].State != "failed" {
			m.state.Reports[i].State = "failed"
			m.state.Reports[i].Error = "Agent restarted before update completed; retry with a new request"
		}
	}
	return m, nil
}
func (m *Manager) Overrides() map[string]executor.Binary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]executor.Binary{}
	for k, v := range m.state.Overrides {
		out[k] = v
	}
	return out
}
func (m *Manager) Reports() []Report {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Report{}, m.state.Reports...)
}
func (m *Manager) save() error {
	raw, e := json.Marshal(m.state)
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(m.dir)
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := root.OpenFile("active.json.new", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return root.Rename("active.json.new", "active.json")
}
func (m *Manager) Submit(ctx context.Context, r Request) {
	m.mu.Lock()
	if m.busy || !manifest.ValidID(r.ID) || Repository(r.Engine) == "" || !ValidVersion(r.Version) || r.Version == "latest" {
		m.mu.Unlock()
		return
	}
	for _, v := range m.state.Reports {
		if v.ID == r.ID {
			m.mu.Unlock()
			return
		}
	}
	m.busy = true
	m.state.Reports = append(m.state.Reports, Report{Request: r, State: "downloading"})
	if len(m.state.Reports) > 20 {
		m.state.Reports = m.state.Reports[len(m.state.Reports)-20:]
	}
	if e := m.save(); e != nil {
		m.busy = false
		m.state.Reports[len(m.state.Reports)-1].State = "failed"
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	go func() {
		b, e := Stage(ctx, m.dir, r.Engine, r.Version, runtime.GOOS+"-"+runtime.GOARCH)
		if e != nil {
			m.Finish(r, executor.Binary{}, "official release download or verification failed")
			return
		}
		m.mu.Lock()
		for i := range m.state.Reports {
			if m.state.Reports[i].ID == r.ID {
				m.state.Reports[i].State = "validating"
			}
		}
		m.save()
		m.mu.Unlock()
		select {
		case m.candidate <- Candidate{r, b}:
		case <-ctx.Done():
			m.Finish(r, executor.Binary{}, "Agent stopped before activation")
		}
	}()
}
func (m *Manager) Pending() (Candidate, bool) {
	select {
	case c := <-m.candidate:
		return c, true
	default:
		return Candidate{}, false
	}
}

// A successful save is part of activation. The caller must restore its previous
// binary when saving the active version fails.
func (m *Manager) Finish(r Request, b executor.Binary, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, had := m.state.Overrides[r.Engine]
	if message == "" {
		m.state.Overrides[r.Engine] = b
	}
	for i := range m.state.Reports {
		if m.state.Reports[i].ID == r.ID {
			m.state.Reports[i].State = "succeeded"
			m.state.Reports[i].Error = ""
			if message != "" {
				m.state.Reports[i].State = "failed"
				m.state.Reports[i].Error = message
			}
		}
	}
	m.busy = false
	if e := m.save(); e != nil {
		if had {
			m.state.Overrides[r.Engine] = old
		} else {
			delete(m.state.Overrides, r.Engine)
		}
		for i := range m.state.Reports {
			if m.state.Reports[i].ID == r.ID {
				m.state.Reports[i].State = "failed"
				m.state.Reports[i].Error = "Cannot persist selected version; reverted"
			}
		}
		return e
	}
	if message == "" {
		m.prune(r.Engine, old.Path)
	}
	return nil
}

func (m *Manager) prune(name, previous string) {
	base := filepath.Join(m.dir, name)
	entries, e := os.ReadDir(base)
	if e != nil {
		return
	}
	type version struct {
		name     string
		modified int64
	}
	versions := []version{}
	for _, entry := range entries {
		if !entry.IsDir() || !tagPattern.MatchString(entry.Name()) {
			continue
		}
		info, e := entry.Info()
		if e == nil {
			versions = append(versions, version{entry.Name(), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].modified > versions[j].modified })
	keep := map[string]bool{}
	for _, path := range []string{m.state.Overrides[name].Path, previous} {
		relative, e := filepath.Rel(base, filepath.Dir(path))
		if e == nil && relative != "." && relative != ".." && !strings.Contains(relative, string(filepath.Separator)) {
			keep[relative] = true
		}
	}
	for _, v := range versions {
		if len(keep) >= 3 {
			break
		}
		keep[v.name] = true
	}
	root, e := os.OpenRoot(base)
	if e != nil {
		return
	}
	defer root.Close()
	for _, v := range versions {
		if !keep[v.name] {
			root.RemoveAll(v.name)
		}
	}
}
