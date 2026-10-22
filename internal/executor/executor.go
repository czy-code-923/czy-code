// Package executor validates and stages native configuration. It NEVER starts an engine,
// touches system service directories, installs software, changes firewall rules or invokes a shell.
package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"vps-control/internal/manifest"
)

type Task struct {
	ID         string          `json:"id"`
	InstanceID string          `json:"instance_id"`
	ConfigID   string          `json:"config_id"`
	Lease      string          `json:"lease_token"`
	Mode       string          `json:"mode"`
	Operation  string          `json:"operation"`
	Spec       json.RawMessage `json:"spec"`
	Checksum   string          `json:"checksum"`
}
type Binary struct {
	Path   string
	SHA256 string
}
type Validator interface {
	Check(context.Context, string, []manifest.Artifact) error
}
type NativeValidator struct{ Binaries map[string]Binary }

func (v NativeValidator) Check(ctx context.Context, dir string, files []manifest.Artifact) error {
	for _, file := range files {
		binary, ok := v.Binaries[file.Engine]
		if !ok || !filepath.IsAbs(binary.Path) || len(binary.SHA256) != 64 {
			return errors.New("engine binary and pinned SHA-256 required")
		}
		sum, e := FileDigest(binary.Path)
		if e != nil || sum != strings.ToLower(binary.SHA256) {
			return errors.New("engine binary checksum mismatch or unavailable")
		}
		var args []string
		switch file.Engine {
		case "sing-box":
			args = []string{"check", "-c", filepath.Join(dir, file.Name)}
		case "frps", "frpc":
			args = []string{"verify", "-c", filepath.Join(dir, file.Name)}
		default:
			return errors.New("unsupported engine validator")
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		cmd := exec.CommandContext(check, binary.Path, args...)
		cmd.Dir = dir
		cmd.Env = []string{}
		if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
			cmd.Env = append(cmd.Env, "SystemRoot="+systemRoot)
		}
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		e = cmd.Run()
		cancel()
		if e != nil {
			return errors.New("native engine configuration validation failed")
		}
	}
	return nil
}
func FileDigest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type Stager struct {
	root      *os.Root
	secret    string
	validator Validator
	mu        sync.Mutex
}
type Current struct {
	TaskID           string `json:"task_id"`
	ConfigID         string `json:"config_id"`
	ManifestChecksum string `json:"manifest_checksum"`
	ArtifactChecksum string `json:"artifact_checksum"`
	Revision         string `json:"revision"`
}

func Open(dir, secret string, v Validator) (*Stager, error) {
	if !filepath.IsAbs(dir) || len(secret) < 32 || v == nil {
		return nil, errors.New("absolute private STATE_DIR, 32-byte ENGINE_SECRET and validator required")
	}
	if e := os.MkdirAll(dir, 0o700); e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	lock, e := root.OpenFile(".agent.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if e != nil {
		root.Close()
		return nil, errors.New("STATE_DIR locked; stop other agent or inspect a stale lock")
	}
	fmt.Fprintln(lock, os.Getpid())
	lock.Close()
	return &Stager{root: root, secret: secret, validator: v}, nil
}
func (s *Stager) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root.Remove(".agent.lock")
	return s.root.Close()
}
func write(root *os.Root, name string, data []byte) error {
	// Temporary files never replace the previously published pointer until Sync/Close succeed.
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func (s *Stager) Stage(ctx context.Context, t Task, authorize func(context.Context) error) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Mode != "staging" || !manifest.ValidID(t.ID) || !manifest.ValidID(t.InstanceID) || !manifest.ValidID(t.ConfigID) || !manifest.ValidID(t.Lease) || authorize == nil {
		return "", errors.New("invalid staging identity or lease guard")
	}
	spec, e := manifest.Verify(t.Spec, t.Checksum)
	if e != nil {
		return "", e
	}
	files, e := manifest.Render(spec, t.InstanceID, s.secret)
	if e != nil {
		return "", e
	}
	encoded, _ := json.Marshal(files)
	sum := manifest.Digest(encoded)
	base := "instances/" + t.InstanceID
	revision := base + "/revisions/" + t.ID + "-" + t.Lease
	if e = s.root.MkdirAll(revision, 0o700); e != nil {
		return "", e
	}
	// A revision is never overwritten. A retry with the same lease must match byte-for-byte.
	for _, file := range files {
		path := revision + "/" + file.Name
		old, e := s.root.ReadFile(path)
		if e == nil {
			if string(old) != string(file.Content) {
				return "", errors.New("existing revision content differs")
			}
			continue
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		if e = write(s.root, path, file.Content); e != nil {
			return "", e
		}
	}
	if e = s.validator.Check(ctx, filepath.Join(s.root.Name(), filepath.FromSlash(revision)), files); e != nil {
		return "", e
	}
	if e = authorize(ctx); e != nil {
		return "", e
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	current := Current{TaskID: t.ID, ConfigID: t.ConfigID, ManifestChecksum: t.Checksum, ArtifactChecksum: sum, Revision: revision}
	pointer, _ := json.MarshalIndent(current, "", "  ")
	temp := base + "/.current-" + t.Lease + ".json"
	defer s.root.Remove(temp)
	if e = write(s.root, temp, pointer); e != nil {
		return "", e
	}
	if e = s.root.Rename(temp, base+"/current.json"); e != nil {
		return "", e
	}
	return sum, nil
}
