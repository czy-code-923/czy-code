package live

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"vps-control/internal/executor"
)

func TestNativeFRPUDPForwarding(t *testing.T) {
	binaries := map[string]executor.Binary{}
	for name, key := range map[string]string{"frps": "TEST_FRPS_BINARY", "frpc": "TEST_FRPC_BINARY"} {
		path := os.Getenv(key)
		if path == "" {
			t.Skip("native FRP binaries required")
		}
		sum, e := executor.FileDigest(path)
		if e != nil {
			t.Fatal(e)
		}
		binaries[name] = executor.Binary{Path: path, SHA256: sum}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	echo, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, a, e := echo.ReadFrom(buf)
			if e != nil {
				return
			}
			echo.WriteTo(buf[:n], a)
		}
	}()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	probe, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	remote := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	cert, key, e := Certificate("127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	server := Spec{Kind: "frps", Listen: "127.0.0.1", Address: "127.0.0.1", Port: port, Password: Secret(), Certificate: cert, PrivateKey: key, Tunnels: []Tunnel{{Name: "udp-fixture", Type: "udp", LocalHost: "127.0.0.1", LocalPort: echo.LocalAddr().(*net.UDPAddr).Port, RemotePort: remote}}}
	client := server
	client.Kind = "frpc"
	client.PrivateKey = ""
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	runner, e := NewRunner(dir, binaries)
	if e != nil {
		t.Fatal(e)
	}
	defer runner.Close()
	if e = runner.Reconcile(context.Background(), []Work{{ID: strings.Repeat("a", 32), Version: 1, Enabled: true, Spec: server}, {ID: strings.Repeat("b", 32), Version: 1, Enabled: true, Spec: client}}); e != nil {
		t.Fatal(e)
	}
	connection, e := net.Dial("udp", net.JoinHostPort("127.0.0.1", portString(remote)))
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Close()
	buf := make([]byte, 32)
	for i := 0; i < 30; i++ {
		connection.SetDeadline(time.Now().Add(300 * time.Millisecond))
		connection.Write([]byte("frp-udp"))
		n, e := connection.Read(buf)
		if e == nil && string(buf[:n]) == "frp-udp" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("FRP UDP did not reach the internal echo service")
}
