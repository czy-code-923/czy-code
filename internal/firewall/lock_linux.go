//go:build linux

package firewall

import (
	"context"
	"os"
	"syscall"
	"time"
)

// A kernel lock is released on process death, so a killed parent cannot block the rollback timer.
func (m *Manager) lock(ctx context.Context) (func(), error) {
	f, e := m.root.OpenFile("operation.lock", os.O_RDWR|os.O_CREATE, 0600)
	if e != nil {
		return nil, e
	}
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
