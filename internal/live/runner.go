package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

type Work struct {
	Members []Work `json:"members,omitempty"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Enabled bool   `json:"enabled"`
	Spec    Spec   `json:"spec"`
}
type Report struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	State   string `json:"state"`
}
type process struct {
	members  []Work
	engine   string
	version  int
	sum      string
	cmd      *exec.Cmd
	done     chan error
	started  time.Time
	retry    time.Time
	attempts int
	state    string
	release  func()
}
type Runner struct {
	root      *os.Root
	binary    map[string]executor.Binary
	processes map[string]*process
	unlock    func()
}

func NewRunner(dir string, binaries map[string]executor.Binary) (*Runner, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("absolute private LIVE_STATE_DIR required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("private state directory required")
	}
	if runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("LIVE_STATE_DIR permissions must be 0700")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	unlock, e := runnerLock(root)
	if e != nil {
		root.Close()
		return nil, e
	}
	return &Runner{root: root, binary: binaries, processes: map[string]*process{}, unlock: unlock}, nil
}
func stopProcess(p *process) {
	if p.cmd != nil {
		p.cmd.Process.Kill()
		<-p.done
		p.cmd = nil
	}
	if p.release != nil {
		p.release()
		p.release = nil
	}
}
func (r *Runner) Close() {
	for _, p := range r.processes {
		stopProcess(p)
	}
	r.unlock()
	r.root.Close()
}
func (r *Runner) Reports() []Report {
	reports := []Report{}
	for id, p := range r.processes {
		if p.cmd != nil {
			select {
			case <-p.done:
				p.cmd = nil
				if p.release != nil {
					p.release()
					p.release = nil
				}
				p.state = "failed"
				p.attempts++
				p.retry = time.Now().Add(time.Duration(min(p.attempts*10, 60)) * time.Second)
			default:
				if time.Since(p.started) > 2*time.Second {
					p.state = "running"
					p.attempts = 0
				}
			}
		}
		if len(p.members) > 0 {
			for _, m := range p.members {
				state := p.state
				if !m.Enabled {
					state = "stopped"
				}
				reports = append(reports, Report{m.ID, m.Version, state})
			}
		} else {
			reports = append(reports, Report{id, p.version, p.state})
		}
	}
	return reports
}
func (r *Runner) Reconcile(ctx context.Context, work []Work) error {
	if len(work) > 64 {
		return errors.New("too many live services")
	}
	seen := map[string]bool{}
	for _, w := range work {
		if !manifest.ValidID(w.ID) || w.Version < 1 || seen[w.ID] {
			return errors.New("invalid service identity")
		}
		seen[w.ID] = true
	}
	for id, p := range r.processes {
		if !seen[id] {
			stopProcess(p)
			delete(r.processes, id)
		}
	}
	for _, w := range work {
		raw, _ := json.Marshal(w)
		sum := manifest.Digest(raw)
		p := r.processes[w.ID]
		if p == nil {
			p = &process{state: "pending"}
			r.processes[w.ID] = p
		}
		if !w.Enabled {
			stopProcess(p)
			p.version = w.Version
			p.state = "stopped"
			p.members = w.Members
			p.sum = sum
			p.engine = w.Spec.Engine
			continue
		}
		// One expired certificate must not block other instances or prevent a stop request.
		if e := w.validate(); e != nil {
			if len(w.Members) > 0 {
				stopProcess(p)
				p.members = w.Members
				p.sum = sum
				p.engine = w.Spec.Engine
				p.state = "failed"
			}
			if p.cmd == nil {
				p.version = w.Version
				p.state = "failed"
			}
			continue
		}
		if p.cmd != nil && p.sum == sum {
			p.version = w.Version
			p.members = w.Members
			continue
		}
		if p.cmd == nil && time.Now().Before(p.retry) {
			continue
		}
		if p.cmd == nil {
			p.version = w.Version
			p.state = "failed"
		}
		p.retry = time.Now().Add(30 * time.Second)
		engine, files, e := w.Render()
		if e != nil {
			continue
		}
		dir := w.ID + "/" + sum
		if e = r.root.MkdirAll(dir, 0700); e != nil {
			continue
		}
		valid := true
		for name, body := range files {
			f, e := r.root.OpenFile(dir+"/"+name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if e != nil {
				valid = false
				break
			}
			_, e = f.Write(body)
			if e == nil {
				e = f.Sync()
			}
			ce := f.Close()
			if e != nil || ce != nil {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		abs := filepath.Join(r.root.Name(), filepath.FromSlash(dir))
		v := executor.NativeValidator{Binaries: r.binary}
		if e = v.Check(ctx, abs, []manifest.Artifact{{Name: "config.json", Engine: engine, Content: files["config.json"]}}); e != nil {
			if len(w.Members) > 0 {
				stopProcess(p)
				p.members = w.Members
				p.sum = sum
				p.engine = w.Spec.Engine
				p.state = "failed"
			}
			continue
		}
		// Native candidate must pass before interrupting a previously running revision.
		stopProcess(p)
		p.version = w.Version
		p.state = "failed"
		p.members = w.Members
		p.engine = engine
		// Port preflight never changes network settings. Actual bind remains the authority.
		if len(w.Members) == 0 && w.Spec.Kind != "frpc" {
			ln, e := net.Listen("tcp", net.JoinHostPort(w.Spec.Listen, portString(w.Spec.Port)))
			if e != nil {
				continue
			}
			ln.Close()
			if w.Spec.Kind == "shadowsocks" || w.Spec.Kind == "hysteria2" || w.Spec.Kind == "tuic" || w.Spec.Kind == "snell" {
				conn, e := net.ListenPacket("udp", net.JoinHostPort(w.Spec.Listen, portString(w.Spec.Port)))
				if e != nil {
					continue
				}
				conn.Close()
			}
		}
		args := []string{"-c", "config.json"}
		if engine == "xray" {
			args = []string{"run", "-config", "config.json"}
		}
		if engine == "sing-box" {
			args = append([]string{"run"}, args...)
		}
		cmd := exec.Command(r.binary[engine].Path, args...)
		cmd.Dir = abs
		cmd.Env = []string{}
		if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
			cmd.Env = append(cmd.Env, "SystemRoot="+systemRoot)
		}
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		prepareChild(cmd)
		if e = cmd.Start(); e != nil {
			continue
		}
		release, e := guardChild(cmd)
		if e != nil {
			cmd.Process.Kill()
			cmd.Wait()
			continue
		}
		p.cmd = cmd
		p.release = release
		p.done = make(chan error, 1)
		go func(done chan error, c *exec.Cmd) { done <- c.Wait() }(p.done, cmd)
		p.sum = sum
		p.started = time.Now()
		p.state = "pending"
		r.prune(w.ID, sum)
	}
	return nil
}
