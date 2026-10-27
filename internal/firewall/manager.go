package firewall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
	"vps-control/internal/manifest"
)

type Backend interface {
	Snapshot(context.Context, string) (string, bool, error)
	Check(context.Context, string) error
	Apply(context.Context, string) error
}
type Scheduler interface {
	Arm(context.Context, string, time.Duration) error
}
type Journal struct {
	ID        string    `json:"id"`
	HostID    string    `json:"host_id"`
	Original  string    `json:"original"`
	Existed   bool      `json:"existed"`
	Status    string    `json:"status"`
	Deadline  time.Time `json:"deadline"`
	TokenHash string    `json:"token_hash"`
}
type Receipt struct {
	ID           string    `json:"id"`
	ConfirmToken string    `json:"confirm_token"`
	Deadline     time.Time `json:"deadline"`
}
type Manager struct {
	root      *os.Root
	backend   Backend
	scheduler Scheduler
}

func OpenManager(dir string, b Backend, s Scheduler) (*Manager, error) {
	if !filepath.IsAbs(dir) || b == nil {
		return nil, errors.New("absolute private state directory and backend required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	r, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	return &Manager{root: r, backend: b, scheduler: s}, nil
}
func (m *Manager) Close() error { return m.root.Close() }
func (m *Manager) save(j Journal) error {
	b, _ := json.MarshalIndent(j, "", "  ")
	name := "journal-" + j.ID + ".json"
	f, e := m.root.OpenFile(name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return m.root.Rename(name+".tmp", name)
}
func (m *Manager) read(id string) (Journal, error) {
	var j Journal
	if !manifest.ValidID(id) {
		return j, errors.New("invalid transaction ID")
	}
	b, e := m.root.ReadFile("journal-" + id + ".json")
	if e != nil {
		return j, e
	}
	if len(b) > 512<<10 {
		return j, errors.New("oversized journal")
	}
	e = json.Unmarshal(b, &j)
	if e != nil || j.ID != id || !manifest.ValidID(j.HostID) {
		return j, errors.New("invalid journal")
	}
	return j, nil
}
func replace(host, body string, exists bool) string {
	prefix := ""
	if exists {
		prefix = "delete table inet " + Table(host) + "\n"
	}
	return prefix + body
}
func (m *Manager) Start(ctx context.Context, p Export, peer string, window time.Duration) (Receipt, error) {
	var receipt Receipt
	body, e := Compile(p)
	if e != nil {
		return receipt, e
	}
	if !p.Policy.AllowsPeer(peer) {
		return receipt, errors.New("SSH peer is not protected by admin CIDRs")
	}
	if window < 30*time.Second || window > 300*time.Second || m.scheduler == nil {
		return receipt, errors.New("30-300 second independent rollback scheduler required")
	}
	unlock, e := m.lock(ctx)
	if e != nil {
		return receipt, e
	}
	defer unlock()
	if active, e := m.root.ReadFile("active"); e == nil {
		j, e := m.read(string(active))
		if e != nil {
			return receipt, e
		}
		if j.Status == "pending" {
			return receipt, errors.New("pending firewall transaction must be confirmed or rolled back first")
		}
	} else if !os.IsNotExist(e) {
		return receipt, e
	}
	old, existed, e := m.backend.Snapshot(ctx, p.HostID)
	if e != nil {
		return receipt, e
	}
	batch := replace(p.HostID, body, existed)
	if e = m.backend.Check(ctx, batch); e != nil {
		return receipt, e
	}
	random := make([]byte, 32)
	if _, e = rand.Read(random); e != nil {
		return receipt, e
	}
	id := hex.EncodeToString(random[:16])
	token := hex.EncodeToString(random[16:])
	j := Journal{ID: id, HostID: p.HostID, Original: old, Existed: existed, Status: "pending", Deadline: time.Now().Add(window), TokenHash: manifest.Digest([]byte(token))}
	if e = m.save(j); e != nil {
		return receipt, e
	}
	if e = m.root.WriteFile("active", []byte(id), 0600); e != nil {
		return receipt, e
	}
	// Arm and verify an independent systemd timer BEFORE changing the kernel rules.
	if e = m.scheduler.Arm(ctx, id, window); e != nil {
		j.Status = "aborted"
		m.save(j)
		return receipt, errors.New("rollback timer could not be armed; no firewall changes made")
	}
	if e = m.backend.Apply(ctx, batch); e != nil {
		recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.restore(recovery, j); err != nil {
			return receipt, errors.New("apply failed; rollback remains armed, inspect local journal")
		}
		return receipt, errors.New("apply failed and previous owned table restored")
	}
	return Receipt{ID: id, ConfirmToken: token, Deadline: j.Deadline}, nil
}
func (m *Manager) Confirm(ctx context.Context, id, token string) error {
	unlock, e := m.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	j, e := m.read(id)
	if e != nil {
		return e
	}
	if j.Status != "pending" || time.Now().After(j.Deadline) || manifest.Digest([]byte(token)) != j.TokenHash {
		return errors.New("invalid/expired confirmation or transaction no longer pending")
	}
	j.Status = "confirmed"
	return m.save(j)
}
func (m *Manager) restore(ctx context.Context, j Journal) error {
	_, exists, e := m.backend.Snapshot(ctx, j.HostID)
	if e != nil {
		return e
	}
	body := ""
	if j.Existed {
		body = j.Original
	}
	batch := replace(j.HostID, body, exists)
	if batch != "" {
		if e = m.backend.Check(ctx, batch); e != nil {
			return e
		}
		if e = m.backend.Apply(ctx, batch); e != nil {
			return e
		}
	}
	j.Status = "rolled_back"
	return m.save(j)
}
func (m *Manager) Rollback(ctx context.Context, id string) error {
	unlock, e := m.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	j, e := m.read(id)
	if e != nil {
		return e
	}
	if j.Status != "pending" {
		return nil
	}
	return m.restore(ctx, j)
}

// StartRemoval removes only this host's owned table, with the same independent
// rollback timer as apply. Provider, Docker and unrelated nft tables are untouched.
func (m *Manager) StartRemoval(ctx context.Context, host string, window time.Duration) (Receipt, error) {
	var receipt Receipt
	if !manifest.ValidID(host) || window < 30*time.Second || window > 300*time.Second || m.scheduler == nil {
		return receipt, errors.New("valid host and 30-300 second rollback scheduler required")
	}
	unlock, e := m.lock(ctx)
	if e != nil {
		return receipt, e
	}
	defer unlock()
	if raw, e := m.root.ReadFile("active"); e == nil {
		j, e := m.read(string(raw))
		if e != nil {
			return receipt, e
		}
		if j.Status == "pending" {
			return receipt, errors.New("confirm or cancel the pending transaction first")
		}
	} else if !os.IsNotExist(e) {
		return receipt, e
	}
	old, exists, e := m.backend.Snapshot(ctx, host)
	if e != nil {
		return receipt, e
	}
	if !exists {
		return receipt, errors.New("owned firewall table is not applied")
	}
	batch := "delete table inet " + Table(host) + "\n"
	if e = m.backend.Check(ctx, batch); e != nil {
		return receipt, e
	}
	random := make([]byte, 32)
	if _, e = rand.Read(random); e != nil {
		return receipt, e
	}
	id := hex.EncodeToString(random[:16])
	token := hex.EncodeToString(random[16:])
	j := Journal{ID: id, HostID: host, Original: old, Existed: true, Status: "pending", Deadline: time.Now().Add(window), TokenHash: manifest.Digest([]byte(token))}
	if e = m.save(j); e != nil {
		return receipt, e
	}
	if e = m.root.WriteFile("active", []byte(id), 0600); e != nil {
		return receipt, e
	}
	if e = m.scheduler.Arm(ctx, id, window); e != nil {
		j.Status = "aborted"
		_ = m.save(j)
		return receipt, errors.New("rollback timer could not be armed; no changes made")
	}
	if e = m.backend.Apply(ctx, batch); e != nil {
		recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e = m.restore(recovery, j); e != nil {
			return receipt, errors.New("removal failed; rollback remains armed")
		}
		return receipt, errors.New("removal failed; original restored")
	}
	return Receipt{ID: id, ConfirmToken: token, Deadline: j.Deadline}, nil
}
