package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"vps-control/internal/manifest"
)

type Native struct{ Path string }
type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 512<<10 {
		b.overflow = true
		return n, nil
	}
	b.Buffer.Write(p)
	return n, nil
}
func command(ctx context.Context, path, stdin string, args ...string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("absolute trusted program path required")
	}
	child, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	cmd.Stdin = strings.NewReader(stdin)
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = &bytes.Buffer{}
	if e := cmd.Run(); e != nil {
		return nil, errors.New("trusted firewall command failed")
	}
	if out.overflow {
		return nil, errors.New("firewall snapshot too large")
	}
	return out.Bytes(), nil
}
func (n Native) Snapshot(ctx context.Context, host string) (string, bool, error) {
	if !manifest.ValidID(host) {
		return "", false, errors.New("invalid host")
	}
	raw, e := command(ctx, n.Path, "", "-j", "list", "tables")
	if e != nil {
		return "", false, e
	}
	var listing struct {
		NFT []struct {
			Table *struct{ Family, Name string }
		} `json:"nftables"`
	}
	if e = json.Unmarshal(raw, &listing); e != nil {
		return "", false, e
	}
	exists := false
	for _, x := range listing.NFT {
		if x.Table != nil && x.Table.Family == "inet" && x.Table.Name == Table(host) {
			exists = true
		}
	}
	if !exists {
		return "", false, nil
	}
	raw, e = command(ctx, n.Path, "", "-j", "list", "table", "inet", Table(host))
	if e != nil {
		return "", false, e
	}
	var detail struct {
		NFT []struct {
			Table *struct{ Family, Name, Comment string }
		} `json:"nftables"`
	}
	if e = json.Unmarshal(raw, &detail); e != nil {
		return "", false, e
	}
	owned := false
	for _, x := range detail.NFT {
		if x.Table != nil && x.Table.Name == Table(host) && x.Table.Family == "inet" && x.Table.Comment == "harbor-managed-v1" {
			owned = true
		}
	}
	if !owned {
		return "", false, errors.New("existing table lacks Harbor ownership marker")
	}
	raw, e = command(ctx, n.Path, "", "-s", "-n", "list", "table", "inet", Table(host))
	return string(raw), true, e
}
func (n Native) Check(ctx context.Context, body string) error {
	_, e := command(ctx, n.Path, body, "--check", "-f", "-")
	return e
}
func (n Native) Apply(ctx context.Context, body string) error {
	_, e := command(ctx, n.Path, body, "-f", "-")
	return e
}

type SystemdScheduler struct{ Executable, StateDir, NftPath string }

func (s SystemdScheduler) Arm(ctx context.Context, id string, window time.Duration) error {
	if !manifest.ValidID(id) {
		return errors.New("invalid transaction ID")
	}
	unit := "harbor-fw-" + id
	_, e := command(ctx, "/usr/bin/systemd-run", "", "--quiet", "--unit="+unit, "--on-active="+fmt.Sprintf("%ds", int(window.Seconds())), "--timer-property=AccuracySec=1s", "--property=Type=exec", "--", s.Executable, "rollback", "--state-dir", s.StateDir, "--nft", s.NftPath, "--id", id)
	if e != nil {
		return e
	}
	out, e := command(ctx, "/usr/bin/systemctl", "", "is-active", unit+".timer")
	if e != nil || strings.TrimSpace(string(out)) != "active" {
		return errors.New("rollback timer not active")
	}
	return nil
}
