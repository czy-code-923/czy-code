// Package manifest defines the only configuration language accepted from the control plane.
// No paths, URLs, commands, plugins, system proxy settings or arbitrary native fields are supported.
package manifest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

var identifier = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidID(s string) bool { return identifier.MatchString(s) }

type Tunnel struct {
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
}
type Spec struct {
	Schema   int      `json:"schema"`
	Mode     string   `json:"mode"`
	Engine   string   `json:"engine"`
	Listen   string   `json:"listen"`
	Port     int      `json:"port"`
	Outbound string   `json:"outbound"`
	Tunnels  []Tunnel `json:"tunnels,omitempty"`
}

func Parse(raw []byte) (Spec, error) {
	var s Spec
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&s); e != nil {
		return s, errors.New("invalid typed manifest")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return s, errors.New("trailing manifest data")
	}
	return s, s.Validate()
}
func (s Spec) Validate() error {
	if s.Schema != 1 || s.Mode != "simulation" || s.Listen != "127.0.0.1" || s.Port < 1024 || s.Port > 65535 || (s.Engine != "sing-box" && s.Engine != "frp") || (s.Outbound != "direct" && s.Outbound != "block") {
		return errors.New("unsupported manifest fields")
	}
	if s.Engine == "sing-box" && len(s.Tunnels) > 0 {
		return errors.New("sing-box cannot contain FRP tunnels")
	}
	if len(s.Tunnels) > 64 {
		return errors.New("at most 64 tunnels supported")
	}
	seen := map[int]bool{s.Port: true}
	for _, t := range s.Tunnels {
		if (t.LocalHost != "127.0.0.1" && t.LocalHost != "::1") || t.LocalPort < 1 || t.LocalPort > 65535 || t.RemotePort < 1024 || t.RemotePort > 65535 || seen[t.RemotePort] {
			return errors.New("invalid or conflicting tunnel")
		}
		seen[t.RemotePort] = true
	}
	return nil
}
func Canonical(raw []byte) ([]byte, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		return nil, e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return json.Marshal(v)
}
func Digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func Verify(raw []byte, sum string) (Spec, error) {
	s, e := Parse(raw)
	if e != nil {
		return s, e
	}
	b, e := Canonical(raw)
	if e != nil {
		return s, e
	}
	if Digest(b) != sum {
		return s, errors.New("manifest checksum mismatch")
	}
	return s, nil
}

type Artifact struct {
	Name    string          `json:"name"`
	Engine  string          `json:"engine"`
	Content json.RawMessage `json:"content"`
}

// Render produces loopback-only laboratory files. FRP emits a paired local server/client,
// NOT a claim that the current data model describes remote FRP client placement.
func Render(s Spec, instanceID, secret string) ([]Artifact, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	if !ValidID(instanceID) || len(secret) < 32 {
		return nil, errors.New("valid instance ID and local engine secret required")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("harbor-engine-v1/" + instanceID))
	password := hex.EncodeToString(mac.Sum(nil))
	artifact := func(name, engine string, v any) Artifact {
		raw, _ := json.MarshalIndent(v, "", "  ")
		return Artifact{Name: name, Engine: engine, Content: raw}
	}
	if s.Engine == "sing-box" {
		route := map[string]any{"final": "direct"}
		if s.Outbound == "block" {
			route["rules"] = []any{map[string]any{"action": "reject"}}
		}
		return []Artifact{artifact("sing-box.json", "sing-box", map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "tag": "local", "listen": "127.0.0.1", "listen_port": s.Port, "users": []any{map[string]string{"username": "harbor", "password": password}}, "set_system_proxy": false}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": route})}, nil
	}
	allow := []any{}
	proxies := []any{}
	// With no tunnels, allow one inert reserved server port. An empty allowPorts in FRP means ALL ports.
	if len(s.Tunnels) == 0 {
		allow = append(allow, map[string]int{"single": s.Port})
	}
	for n, t := range s.Tunnels {
		allow = append(allow, map[string]int{"single": t.RemotePort})
		proxies = append(proxies, map[string]any{"name": fmt.Sprintf("harbor-%d", n+1), "type": "tcp", "localIP": t.LocalHost, "localPort": t.LocalPort, "remotePort": t.RemotePort})
	}
	return []Artifact{
		artifact("frps.json", "frps", map[string]any{"bindAddr": "127.0.0.1", "bindPort": s.Port, "proxyBindAddr": "127.0.0.1", "allowPorts": allow, "auth": map[string]any{"method": "token", "token": password}, "transport": map[string]any{"tls": map[string]any{"force": true}}}),
		artifact("frpc.json", "frpc", map[string]any{"serverAddr": "127.0.0.1", "serverPort": s.Port, "auth": map[string]any{"method": "token", "token": password}, "transport": map[string]any{"tls": map[string]any{"enable": true}}, "proxies": proxies}),
	}, nil
}
