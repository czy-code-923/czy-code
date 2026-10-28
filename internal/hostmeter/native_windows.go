//go:build windows

package hostmeter

import (
	"fmt"
	"net"
	"strconv"
	"syscall"
	"unsafe"
)

// MIB_IF_ROW2: field order follows the Windows SDK, with Go native alignment.
type ifRow2 struct {
	Luid                                                                                                                                                                                                                                                                                    uint64
	Index                                                                                                                                                                                                                                                                                   uint32
	Guid                                                                                                                                                                                                                                                                                    [16]byte
	Alias, Description                                                                                                                                                                                                                                                                      [257]uint16
	PhysicalLength                                                                                                                                                                                                                                                                          uint32
	Physical, Permanent                                                                                                                                                                                                                                                                     [32]byte
	Mtu, Type, Tunnel, Media, PhysicalMedium, Access, Direction                                                                                                                                                                                                                             uint32
	Flags                                                                                                                                                                                                                                                                                   byte
	Oper, Admin, MediaState                                                                                                                                                                                                                                                                 uint32
	NetworkGuid                                                                                                                                                                                                                                                                             [16]byte
	ConnectionType                                                                                                                                                                                                                                                                          uint32
	TransmitSpeed, ReceiveSpeed, InOctets, InUcastPkts, InNUcastPkts, InDiscards, InErrors, InUnknown, InUcastOctets, InMulticastOctets, InBroadcastOctets, OutOctets, OutUcastPkts, OutNUcastPkts, OutDiscards, OutErrors, OutUcastOctets, OutMulticastOctets, OutBroadcastOctets, OutQLen uint64
}

var ipHelper = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetIfEntry2")
var bootInfo = syscall.NewLazyDLL("ntdll.dll").NewProc("NtQuerySystemInformation")

func readNative() (string, []Raw, error) {
	// Read-only boot time is used only to distinguish counter epochs. If this
	// optional native ABI is unavailable, fail visibly rather than invent a baseline.
	var info [48]byte
	var length uint32
	s, _, _ := bootInfo.Call(3, uintptr(unsafe.Pointer(&info[0])), 48, uintptr(unsafe.Pointer(&length)))
	if s != 0 {
		return "", nil, fmt.Errorf("Windows boot epoch unavailable")
	}
	boot := strconv.FormatUint(*(*uint64)(unsafe.Pointer(&info[0])), 10)
	links, e := net.Interfaces()
	if e != nil {
		return "", nil, e
	}
	out := []Raw{}
	for _, n := range links {
		if n.Flags&net.FlagLoopback != 0 {
			continue
		}
		row := ifRow2{Index: uint32(n.Index)}
		if unsafe.Sizeof(row) != 1352 || unsafe.Offsetof(row.InOctets) != 1208 || unsafe.Offsetof(row.OutOctets) != 1280 {
			return "", nil, fmt.Errorf("unsupported Windows interface ABI")
		}
		code, _, _ := ipHelper.Call(uintptr(unsafe.Pointer(&row)))
		if code != 0 {
			return "", nil, fmt.Errorf("Windows interface counter unavailable")
		}
		if row.InOctets > 9000000000000000 || row.OutOctets > 9000000000000000 {
			return "", nil, fmt.Errorf("interface counter capacity exceeded")
		}
		out = append(out, Raw{Name: n.Name, RX: int64(row.InOctets), TX: int64(row.OutOctets), Uplink: row.Oper == 1 && row.Flags&1 != 0 && (row.Type == 6 || row.Type == 71)})
	}
	return boot, out, nil
}
