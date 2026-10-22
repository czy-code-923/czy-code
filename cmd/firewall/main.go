// Standalone root-only Linux helper. The API and unprivileged Agent cannot invoke it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"vps-control/internal/firewall"
)

func main() {
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: harbor-firewall preview|check|apply|confirm|rollback [flags]")
	}
	op := os.Args[1]
	flags := flag.NewFlagSet(op, flag.ContinueOnError)
	file := flags.String("file", "", "typed exported policy JSON")
	state := flags.String("state-dir", "/var/lib/harbor-firewall", "private root-owned state directory")
	nft := flags.String("nft", "/usr/sbin/nft", "trusted absolute nft path")
	peer := flags.String("ssh-peer", "", "current SSH source IP")
	window := flags.Int("timeout", 60, "rollback timeout seconds (30-300)")
	id := flags.String("id", "", "transaction ID")
	token := flags.String("token", "", "one-time confirm token")
	if e := flags.Parse(os.Args[2:]); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if op == "preview" || op == "check" || op == "apply" {
		f, e := os.Open(*file)
		if e != nil {
			return e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if e != nil {
			return e
		}
		if len(b) > 64<<10 {
			return fmt.Errorf("policy file too large")
		}
		p, e := firewall.Parse(b)
		if e != nil {
			return e
		}
		script, e := firewall.Compile(p)
		if e != nil {
			return e
		}
		if op == "preview" {
			fmt.Print(script)
			return nil
		}
		real, e := filepath.EvalSymlinks(*nft)
		if e != nil {
			return e
		}
		*nft = real
		if e = securePaths(*state, *nft); e != nil {
			return e
		}
		native := firewall.Native{Path: *nft}
		if op == "check" {
			_, exists, e := native.Snapshot(ctx, p.HostID)
			if e != nil {
				return e
			}
			if exists {
				script = "delete table inet " + firewall.Table(p.HostID) + "\n" + script
			}
			return native.Check(ctx, script)
		}
		if connection := os.Getenv("SSH_CONNECTION"); connection != "" {
			fields := strings.Fields(connection)
			if len(fields) != 4 || fields[0] != *peer || fmt.Sprint(p.Policy.SSHPort) != fields[3] {
				return fmt.Errorf("SSH peer/port does not match the current SSH connection")
			}
		}
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		executable, e = filepath.EvalSymlinks(executable)
		if e != nil {
			return e
		}
		if e = secureBinary(executable); e != nil {
			return e
		}
		m, e := firewall.OpenManager(*state, native, firewall.SystemdScheduler{Executable: executable, StateDir: *state, NftPath: *nft})
		if e != nil {
			return e
		}
		defer m.Close()
		receipt, e := m.Start(ctx, p, *peer, time.Duration(*window)*time.Second)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(receipt)
	}
	if op != "confirm" && op != "rollback" {
		return fmt.Errorf("unsupported operation")
	}
	if e := securePaths(*state, *nft); e != nil {
		return e
	}
	m, e := firewall.OpenManager(filepath.Clean(*state), firewall.Native{Path: *nft}, nil)
	if e != nil {
		return e
	}
	defer m.Close()
	if op == "confirm" {
		return m.Confirm(ctx, *id, *token)
	}
	return m.Rollback(ctx, *id)
}
