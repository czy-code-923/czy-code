package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
)

func (r *Runner) loadUsage() error {
	r.usage = newUsageStore()
	f, e := r.root.Open("traffic-state.json")
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if e != nil || len(raw) > 4<<20 {
		return errors.New("invalid traffic state")
	}
	if e = json.Unmarshal(raw, r.usage); e != nil {
		return e
	}
	if r.usage.Counters == nil {
		r.usage.Counters = map[string]TrafficCounter{}
	}
	if r.usage.Access == nil {
		r.usage.Access = map[string]AccessObservation{}
	}
	if len(r.usage.Counters) > 2000 || len(r.usage.Access) > 2000 {
		return errors.New("traffic state exceeds capacity")
	}
	return nil
}
func (r *Runner) saveUsage() error {
	r.usage.mu.Lock()
	raw, e := json.Marshal(r.usage)
	r.usage.mu.Unlock()
	if e != nil {
		return e
	}
	f, e := r.root.OpenFile("traffic-state.new", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return r.root.Rename("traffic-state.new", "traffic-state.json")
}

// The controller's authenticated baseline repairs a missing local ledger without resetting quotas.
func (r *Runner) SeedCounters(counters []TrafficCounter) error {
	if len(counters) > 2000 {
		return errors.New("traffic baseline exceeds capacity")
	}
	r.usage.mu.Lock()
	for _, v := range counters {
		old := r.usage.Counters[v.ID]
		if v.Up >= old.Up && v.Down >= old.Down {
			r.usage.Counters[v.ID] = v
		}
	}
	r.usage.mu.Unlock()
	return r.saveUsage()
}
func (r *Runner) TelemetrySnapshot(ctx context.Context) (TelemetryBatch, error) {
	b := TelemetryBatch{}
	for _, p := range r.processes {
		if p.meter == nil {
			continue
		}
		state := "unavailable"
		reason := "native statistics unavailable"
		if p.meter.poll(ctx) {
			state = "collecting"
			reason = ""
		}
		b.Status = append(b.Status, MeterStatus{Engine: p.engine, State: state, Error: reason})
	}
	if e := r.saveUsage(); e != nil {
		return b, e
	}
	r.usage.mu.Lock()
	defer r.usage.mu.Unlock()
	for _, v := range r.usage.Counters {
		b.Counters = append(b.Counters, v)
	}
	for _, v := range r.usage.Access {
		b.Access = append(b.Access, v)
	}
	sort.Slice(b.Access, func(i, j int) bool {
		if b.Access[i].QueuedAt.Equal(b.Access[j].QueuedAt) {
			return b.Access[i].ID < b.Access[j].ID
		}
		return b.Access[i].QueuedAt.Before(b.Access[j].QueuedAt)
	})
	if len(b.Access) > 100 {
		b.Access = b.Access[:100]
	}
	sort.Slice(b.Counters, func(i, j int) bool { return b.Counters[i].ID < b.Counters[j].ID })
	return b, nil
}
func (r *Runner) AckTelemetry(b TelemetryBatch, retired []string) error {
	r.usage.mu.Lock()
	for _, v := range b.Access {
		if current, ok := r.usage.Access[v.ID]; ok && current == v {
			delete(r.usage.Access, v.ID)
		}
	}
	for _, id := range retired {
		delete(r.usage.Counters, id)
	}
	r.usage.mu.Unlock()
	return r.saveUsage()
}
func (r *Runner) StopAll() {
	for _, p := range r.processes {
		stopProcess(p)
	}
}
