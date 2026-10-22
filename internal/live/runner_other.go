//go:build !windows && !linux

package live

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

func runnerLock(root *os.Root) (func(), error) {
	return nil, errors.New("live runner supports Linux and Windows only")
}
func prepareChild(c *exec.Cmd)               {}
func guardChild(c *exec.Cmd) (func(), error) { return nil, errors.New("unsupported platform") }
func portString(p int) string                { return strconv.Itoa(p) }
