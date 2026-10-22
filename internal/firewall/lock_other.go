//go:build !linux

package firewall

import (
	"context"
	"os"
	"time"
)

// For compiler/fake-backend tests only. Native kernel operations are refused on this platform.
func (m *Manager) lock(ctx context.Context) (func(), error) {
	for {
		f, e := m.root.OpenFile("operation.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			f.Close()
			return func() { m.root.Remove("operation.lock") }, nil
		}
		if !os.IsExist(e) {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
