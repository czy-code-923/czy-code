//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var safePath = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

func secureBinary(path string) error {
	if !safePath.MatchString(path) || !filepath.IsAbs(path) {
		return fmt.Errorf("trusted absolute executable path required")
	}
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	info, e := os.Stat(real)
	if e != nil {
		return e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("executable must be root-owned, regular and not group/world writable")
	}
	for _, chain := range []string{filepath.Dir(real), filepath.Dir(path)} {
		for parent := chain; ; parent = filepath.Dir(parent) {
			v, e := os.Stat(parent)
			if e != nil {
				return e
			}
			st, ok := v.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != 0 || !v.IsDir() || v.Mode().Perm()&0022 != 0 {
				return fmt.Errorf("executable parents must be root-owned and not group/world writable")
			}
			if parent == "/" {
				break
			}
		}
	}
	return nil
}
func securePaths(state, nft string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("firewall kernel operations require local Linux root")
	}
	if e := secureBinary(nft); e != nil {
		return e
	}
	if !safePath.MatchString(state) || filepath.Clean(state) != state {
		return fmt.Errorf("private absolute state path required")
	}
	if e := os.MkdirAll(state, 0700); e != nil {
		return e
	}
	for p := state; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("state path and its parents must be root-owned, not symlinks or group/world writable")
		}
		if p == state && info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("state directory must be private (0700)")
		}
		if p == "/" {
			break
		}
	}
	return nil
}
