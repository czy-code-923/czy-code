//go:build windows

package live

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

func runnerLock(root *os.Root) (func(), error) {
	p, e := syscall.UTF16PtrFromString(root.Name() + "\\runner.lock")
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return func() { syscall.CloseHandle(h) }, nil
}
func prepareChild(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

type jobLimits struct {
	ProcessTime int64
	JobTime     int64
	Flags       uint32
	MinWorking  uintptr
	MaxWorking  uintptr
	Active      uint32
	Affinity    uintptr
	Priority    uint32
	Scheduling  uint32
}
type ioCounters struct {
	ReadOps    uint64
	WriteOps   uint64
	OtherOps   uint64
	ReadBytes  uint64
	WriteBytes uint64
	OtherBytes uint64
}
type extendedLimits struct {
	Basic         jobLimits
	IO            ioCounters
	ProcessMemory uintptr
	JobMemory     uintptr
	PeakProcess   uintptr
	PeakJob       uintptr
}

func guardChild(c *exec.Cmd) (func(), error) {
	h, _, e := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if h == 0 {
		return nil, e
	}
	release := func() { syscall.CloseHandle(syscall.Handle(h)) }
	info := extendedLimits{}
	info.Basic.Flags = 0x2000
	ok, _, e := kernel.NewProc("SetInformationJobObject").Call(h, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		release()
		return nil, e
	}
	process, e := syscall.OpenProcess(0x0100|0x0001, false, uint32(c.Process.Pid))
	if e != nil {
		release()
		return nil, e
	}
	defer syscall.CloseHandle(process)
	ok, _, e = kernel.NewProc("AssignProcessToJobObject").Call(h, uintptr(process))
	if ok == 0 {
		release()
		return nil, e
	}
	return release, nil
}
func portString(p int) string { return strconv.Itoa(p) }
