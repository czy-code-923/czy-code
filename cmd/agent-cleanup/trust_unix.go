//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func trustedManifest(path string) error {
	if os.Geteuid() != 0 {
		return errors.New("cleanup requires root")
	}
	for p := path; ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil {
			return e
		}
		st, ok := s.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || s.Mode().Perm()&0022 != 0 || s.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe root manifest or parent")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
