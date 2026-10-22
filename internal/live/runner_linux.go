//go:build linux

package live

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func runnerLock(root *os.Root) (func(), error) {
	f, e := root.OpenFile("runner.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return func() { f.Close() }, nil
}
func prepareChild(c *exec.Cmd)               { c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL} }
func guardChild(c *exec.Cmd) (func(), error) { return func() {}, nil }
func portString(p int) string                { return strconv.Itoa(p) }
