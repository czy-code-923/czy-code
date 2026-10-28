package live

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"vps-control/internal/manifest"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
)

type connectionUsage struct {
	Up, Down      int64
	Service, User string
	Closed        bool
	Observation   *AccessObservation
}
type kernelMeter struct {
	mu             sync.Mutex
	engine, secret string
	port           int
	cancel         context.CancelFunc
	done           chan struct{}
	store          *usageStore
	closed         []string
	connections    map[string]connectionUsage
	native         map[string]int64
	ready          bool
	rpc            *grpc.ClientConn
}
type usageStore struct {
	mu       sync.Mutex
	Counters map[string]TrafficCounter    `json:"counters"`
	Access   map[string]AccessObservation `json:"access"`
}

func newUsageStore() *usageStore {
	return &usageStore{Counters: map[string]TrafficCounter{}, Access: map[string]AccessObservation{}}
}
func (s *usageStore) add(engine, kind, id string, up, down int64) {
	if !manifest.ValidID(id) || up < 0 || down < 0 || up+down == 0 {
		return
	}
	key := manifest.Digest([]byte(engine + "/" + kind + "/" + id))[:32]
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Counters) >= 2000 {
		if _, ok := s.Counters[key]; !ok {
			return
		}
	}
	v := s.Counters[key]
	v.ID = key
	v.Kind = kind
	v.Subject = id
	v.Up += up
	v.Down += down
	s.Counters[key] = v
}
func newObservation(key, user, service, source, dest, domain, network string) *AccessObservation {
	if !manifest.ValidID(service) {
		return nil
	}

	ip, _, e := net.SplitHostPort(source)
	if e != nil {
		return nil
	}
	if address, err := netip.ParseAddr(ip); err != nil || address.Zone() != "" || address.IsUnspecified() {
		return nil
	}
	host, port, e := net.SplitHostPort(dest)
	if e != nil {
		return nil
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil
	}
	if domain != "" {
		host = domain
	}
	if !Address(host) {
		return nil
	}
	if !manifest.ValidID(user) {
		user = ""
	}
	if network != "tcp" && network != "udp" {
		return nil
	}
	return &AccessObservation{ID: manifest.Digest([]byte(key))[:32], User: user, Service: service, Source: ip, Destination: host, Port: n, Network: network, ObservedAt: time.Now().UTC()}
}
func (s *usageStore) enqueue(v AccessObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.Access[v.ID]
	if !exists && len(s.Access) >= 2000 {
		return
	}
	if exists {
		v.QueuedAt = old.QueuedAt
	} else {
		v.QueuedAt = time.Now().UTC()
	}
	s.Access[v.ID] = v
}
func (s *usageStore) observe(key, user, service, source, dest, domain, network string) {
	if v := newObservation(key, user, service, source, dest, domain, network); v != nil {
		s.enqueue(*v)
	}
}

func (m *kernelMeter) processSB(raw []byte) error {
	fields, e := pbFields(raw)
	if e != nil {
		return e
	}
	for _, f := range fields {
		if f.tag != 1 {
			continue
		}
		event, e := pbFields(f.bytes)
		if e != nil {
			return e
		}
		var id string
		var conn []byte
		var typ uint64
		var du, dd int64
		for _, x := range event {
			switch x.tag {
			case 1:
				typ = x.number
			case 2:
				id = string(x.bytes)
			case 3:
				conn = x.bytes
			case 4:
				du = int64(x.number)
			case 5:
				dd = int64(x.number)
			}
		}
		if id == "" {
			continue
		}
		previous, exists := m.connections[id]
		v := previous
		if len(conn) > 0 {
			parts, e := pbFields(conn)
			if e != nil {
				return e
			}
			var source, dest, domain, network string
			for _, x := range parts {
				switch x.tag {
				case 2:
					v.Service = strings.TrimPrefix(string(x.bytes), "in-")
				case 5:
					network = string(x.bytes)
				case 6:
					source = string(x.bytes)
				case 7:
					dest = string(x.bytes)
				case 8:
					domain = string(x.bytes)
				case 10:
					v.User = string(x.bytes)
				case 13:
					v.Closed = x.number > 0
				case 16:
					v.Up = int64(x.number)
				case 17:
					v.Down = int64(x.number)
				}
			}
			if v.Observation == nil {
				v.Observation = newObservation(m.secret+id, v.User, v.Service, source, dest, domain, network)
			}
		} else if exists && typ == 1 {
			v.Up += du
			v.Down += dd
		} else {
			continue
		}
		if v.Up >= previous.Up && v.Down >= previous.Down {
			up, down := v.Up-previous.Up, v.Down-previous.Down
			m.store.add(m.engine, "service", v.Service, up, down)
			m.store.add(m.engine, "user", v.User, up, down)
		}
		if typ == 2 {
			v.Closed = true
		}
		if v.Observation != nil {
			obs := *v.Observation
			obs.Up = v.Up
			obs.Down = v.Down
			obs.BytesKnown = true
			obs.Closed = v.Closed
			m.store.enqueue(obs)
		}
		if v.Closed && !previous.Closed {
			m.closed = append(m.closed, id)
			if len(m.closed) > 2000 {
				old := m.closed[0]
				m.closed = m.closed[1:]
				if old != id {
					delete(m.connections, old)
				}
			}
		}
		if len(m.connections) >= 20000 && !exists {
			return errors.New("connection accounting capacity reached")
		}
		m.connections[id] = v
	}
	return nil
}
func (m *kernelMeter) subscribe(ctx context.Context) {
	defer close(m.done)
	for ctx.Err() == nil {
		stream, e := m.rpc.NewStream(secretContext(ctx, m.secret), &grpc.StreamDesc{ServerStreams: true}, "/daemon.StartedService/SubscribeConnections")
		if e == nil {
			request := protowire.AppendTag(nil, 1, protowire.VarintType)
			request = protowire.AppendVarint(request, uint64(time.Second))
			e = stream.SendMsg(request)
			if e == nil {
				e = stream.CloseSend()
			}
			for e == nil {
				var raw []byte
				e = stream.RecvMsg(&raw)
				if e != nil {
					break
				}
				m.mu.Lock()
				e = m.processSB(raw)
				m.ready = e == nil
				m.mu.Unlock()
			}
		}
		m.mu.Lock()
		m.ready = false
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (m *kernelMeter) poll(ctx context.Context) bool {
	guard, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if m.engine == "sing-box" {
		var reply []byte
		e := m.rpc.Invoke(secretContext(guard, m.secret), "/daemon.StartedService/GetVersion", []byte{}, &reply)
		m.mu.Lock()
		defer m.mu.Unlock()
		return e == nil && m.ready
	}
	var reply []byte
	if e := m.rpc.Invoke(guard, "/xray.app.stats.command.StatsService/QueryStats", []byte{}, &reply); e != nil {
		return false
	}
	fields, e := pbFields(reply)
	if e != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range fields {
		if f.tag != 1 {
			continue
		}
		parts, e := pbFields(f.bytes)
		if e != nil {
			return false
		}
		var name string
		var value int64
		for _, v := range parts {
			if v.tag == 1 {
				name = string(v.bytes)
			}
			if v.tag == 2 {
				value = int64(v.number)
			}
		}
		tags := strings.Split(name, ">>>")
		if len(tags) != 4 || value < 0 {
			continue
		}
		kind, id := tags[0], tags[1]
		if kind == "inbound" {
			kind = "service"
			id = strings.TrimPrefix(id, "in-")
		}
		if kind != "user" && kind != "service" {
			continue
		}
		previous := m.native[name]
		if value < previous {
			continue
		}
		delta := value - previous
		m.native[name] = value
		if tags[3] == "uplink" {
			m.store.add(m.engine, kind, id, delta, 0)
		} else if tags[3] == "downlink" {
			m.store.add(m.engine, kind, id, 0, delta)
		}
	}
	return true
}
func (m *kernelMeter) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if m.engine == "sing-box" {
		var reply []byte
		_ = m.rpc.Invoke(secretContext(ctx, m.secret), "/daemon.StartedService/CloseAllConnections", []byte{}, &reply)
		time.Sleep(30 * time.Millisecond)
	} else {
		m.poll(ctx)
	}
	m.cancel()
	if m.done != nil {
		<-m.done
	}
	m.rpc.Close()
}

// Meter listeners and secrets are generated locally and never exposed to the public gateway.
func prepareMeter(engine string, files map[string][]byte, store *usageStore) (*kernelMeter, error) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var cfg map[string]any
	if e = json.Unmarshal(files["config.json"], &cfg); e != nil {
		return nil, e
	}
	m := &kernelMeter{engine: engine, secret: Secret(), port: port, store: store, connections: map[string]connectionUsage{}, native: map[string]int64{}}
	if engine == "sing-box" {
		cfg["services"] = []any{map[string]any{"type": "api", "tag": "harbor-meter", "listen": "127.0.0.1", "listen_port": port, "secret": m.secret, "dashboard": false, "access_control_allow_origin": []string{"https://harbor.invalid"}}}
	}
	if engine == "xray" {
		cfg["api"] = map[string]any{"tag": "harbor-meter", "listen": net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "services": []string{"StatsService"}}
		cfg["stats"] = map[string]any{}
		cfg["policy"] = map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true}}
		cfg["log"] = map[string]any{"loglevel": "warning", "access": "", "error": "none"}
	}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return nil, e
	}
	files["config.json"] = raw
	return m, nil
}
func (m *kernelMeter) start() error {
	var e error
	m.rpc, e = rpcConnection(m.port)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.engine == "sing-box" {
		m.done = make(chan struct{})
		go m.subscribe(ctx)
	}
	return nil
}
