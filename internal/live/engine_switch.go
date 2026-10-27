package live

import (
	"context"
	"errors"
	"path/filepath"
	"time"
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

func (r *Runner) Binary(engine string) executor.Binary { return r.binary[engine] }
func (r *Runner) SwitchBinary(ctx context.Context, engine string, binary executor.Binary) error {
	if engine != "sing-box" && engine != "xray" {
		return errors.New("unsupported update engine")
	}
	capabilities := Capabilities(map[string]executor.Binary{engine: binary})
	available := false
	for _, c := range capabilities {
		if c.Engine == engine && c.Available {
			available = true
		}
	}
	if !available {
		return errors.New("candidate engine version probe failed")
	}
	validator := executor.NativeValidator{Binaries: map[string]executor.Binary{engine: binary}}
	for id, p := range r.processes {
		if p.engine != engine || p.cmd == nil {
			continue
		}
		dir := filepath.Join(r.root.Name(), id, p.sum)
		file, e := r.root.ReadFile(filepath.Join(id, p.sum, "config.json"))
		if e != nil {
			return e
		}
		if e = validator.Check(ctx, dir, []manifest.Artifact{{Name: "config.json", Engine: engine, Content: file}}); e != nil {
			return errors.New("candidate engine rejects an active configuration; previous version retained")
		}
	}
	previous := r.binary[engine]
	reset := func() {
		for _, p := range r.processes {
			if p.engine == engine {
				stopProcess(p)
				p.retry = time.Time{}
				p.sum = ""
			}
		}
	}
	reset()
	r.binary[engine] = binary
	if e := r.Reconcile(ctx, r.lastWork); e == nil {
		timer := time.NewTimer(2500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		r.Reports()
		healthy := ctx.Err() == nil
		for _, w := range r.lastWork {
			selected := w.Spec.Engine
			if selected == "" && Proxy(w.Spec.Kind) {
				selected = "sing-box"
			}
			if !w.Enabled || selected != engine {
				continue
			}
			p := r.processes[w.ID]
			if p == nil || p.cmd == nil || p.state != "running" {
				healthy = false
			}
		}
		if healthy {
			return nil
		}
	}
	reset()
	r.binary[engine] = previous
	r.Reconcile(context.WithoutCancel(ctx), r.lastWork)
	return errors.New("candidate failed to start; previous engine restored")
}
