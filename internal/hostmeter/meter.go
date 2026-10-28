// Package hostmeter records OS interface bytes independently of proxy counters.
package hostmeter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Raw struct {
	Name   string
	RX, TX int64
	Uplink bool
}
type Interface struct {
	Name   string `json:"name"`
	RX     int64  `json:"rx"`
	TX     int64  `json:"tx"`
	Uplink bool   `json:"uplink"`
}
type Report struct {
	Epoch      string      `json:"epoch"`
	Started    time.Time   `json:"started_at"`
	At         time.Time   `json:"sampled_at"`
	Interfaces []Interface `json:"interfaces"`
	Gaps       int64       `json:"gaps"`
	State      string      `json:"state"`
}
type entry struct {
	RawRX, RawTX, RX, TX int64
	Uplink               bool
}
type state struct {
	Epoch       string
	Started, At time.Time
	Boot        string
	Entries     map[string]entry
	Gaps        int64
}
type Meter struct {
	dir   string
	state state
	read  func() (string, []Raw, error)
}

func New(dir string) (*Meter, error) { return newMeter(dir, readNative) }
func newMeter(dir string, reader func() (string, []Raw, error)) (*Meter, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("absolute private host meter directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	m := &Meter{dir: dir, read: reader}
	f, e := os.Open(filepath.Join(dir, "host-traffic.json"))
	var b []byte
	if e == nil {
		b, e = io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if len(b) > 1<<20 {
			return nil, errors.New("host meter state exceeds capacity")
		}
	}
	if e == nil {
		if e = json.Unmarshal(b, &m.state); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if m.state.Epoch == "" {
		var id [16]byte
		rand.Read(id[:])
		m.state = state{Epoch: hex.EncodeToString(id[:]), Started: time.Now().UTC(), Entries: map[string]entry{}}
	}
	if m.state.Entries == nil || len(m.state.Entries) > 64 {
		return nil, errors.New("invalid host meter state")
	}
	return m, nil
}
func (m *Meter) Snapshot(now time.Time) (Report, error) {
	boot, rows, e := m.read()
	if e != nil {
		return Report{}, e
	}
	if len(rows) > 64 {
		return Report{}, errors.New("interface capacity exceeded")
	}
	next := m.state
	next.Entries = make(map[string]entry, len(m.state.Entries))
	for k, v := range m.state.Entries {
		next.Entries[k] = v
	}
	reboot := next.Boot != "" && next.Boot != boot
	if reboot || !next.At.IsZero() && now.Sub(next.At) > 30*time.Second {
		next.Gaps++
	}
	for _, r := range rows {
		if r.RX < 0 || r.TX < 0 || r.Name == "" || len(r.Name) > 128 {
			return Report{}, errors.New("invalid OS interface counters")
		}
		old, exists := next.Entries[r.Name]
		v := old
		v.Uplink = r.Uplink
		if exists {
			if reboot || r.RX < old.RawRX || r.TX < old.RawTX {
				v.RX += r.RX
				v.TX += r.TX
				if !reboot {
					next.Gaps++
				}
			} else {
				v.RX += r.RX - old.RawRX
				v.TX += r.TX - old.RawTX
			}
		} else if len(next.Entries) >= 64 {
			return Report{}, errors.New("durable interface capacity reached")
		}
		v.RawRX = r.RX
		v.RawTX = r.TX
		next.Entries[r.Name] = v
	}
	next.At = now.UTC()
	next.Boot = boot
	raw, e := json.Marshal(next)
	if e != nil {
		return Report{}, e
	}
	root, e := os.OpenRoot(m.dir)
	if e != nil {
		return Report{}, e
	}
	defer root.Close()
	f, e := root.OpenFile("host-traffic.json.new", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return Report{}, e
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
		return Report{}, e
	}
	if e = root.Rename("host-traffic.json.new", "host-traffic.json"); e != nil {
		return Report{}, e
	}
	m.state = next
	out := Report{Epoch: next.Epoch, Started: next.Started, At: next.At, Gaps: next.Gaps, State: "collecting", Interfaces: []Interface{}}
	for _, r := range rows {
		v := next.Entries[r.Name]
		out.Interfaces = append(out.Interfaces, Interface{Name: r.Name, RX: v.RX, TX: v.TX, Uplink: r.Uplink})
	}
	sort.Slice(out.Interfaces, func(i, j int) bool { return out.Interfaces[i].Name < out.Interfaces[j].Name })
	return out, nil
}
