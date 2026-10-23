// Package live is the closed configuration language for opt-in running services.
package live

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Method = "2022-blake3-aes-256-gcm"

var label = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func Address(s string) bool {
	if a, e := netip.ParseAddr(s); e == nil {
		return a.Zone() == "" && !a.IsUnspecified() && !a.IsMulticast()
	}
	if len(s) > 253 || len(s) == 0 {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if !label.MatchString(p) {
			return false
		}
	}
	return true
}
func Origin(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.Host != "" && Address(u.Hostname()) && u.Port() == "" && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func Secret() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.StdEncoding.EncodeToString(b)
}
func Seal(key, plain []byte) ([]byte, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	n := make([]byte, g.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return g.Seal(n, n, plain, []byte("harbor-live-v1")), nil
}
func Open(key, data []byte) ([]byte, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if len(data) < g.NonceSize() {
		return nil, errors.New("invalid sealed configuration")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], []byte("harbor-live-v1"))
}

type Peer struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}
type Tunnel struct {
	Name       string `json:"name"`
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
}
type Spec struct {
	Engine      string   `json:"engine,omitempty"`
	UUID        string   `json:"uuid,omitempty"`
	ServerName  string   `json:"server_name,omitempty"`
	Kind        string   `json:"kind"`
	Listen      string   `json:"listen"`
	Port        int      `json:"port"`
	Address     string   `json:"address"`
	Password    string   `json:"password"`
	Peer        *Peer    `json:"peer,omitempty"`
	Tunnels     []Tunnel `json:"tunnels,omitempty"`
	Certificate string   `json:"certificate,omitempty"`
	PrivateKey  string   `json:"private_key,omitempty"`
}

func (s Spec) Validate() error {
	if s.Engine != "" && s.Engine != "sing-box" && !(s.Engine == "xray" && (s.Kind == "vless" || s.Kind == "trojan")) {
		return errors.New("unsupported engine/protocol combination")
	}
	if !Address(s.Address) || s.Port < 1024 || s.Port > 65535 {
		return errors.New("valid IPv4/IPv6/DNS address and port 1024-65535 required")
	}
	if s.Listen != "127.0.0.1" && s.Listen != "::1" && s.Listen != "0.0.0.0" && s.Listen != "::" {
		return errors.New("unsupported listen address")
	}
	if s.Port == 8080 || s.Port == 5173 || s.Port == 8091 {
		return errors.New("reserved management port")
	}
	if TLSProxy(s.Kind) || s.Kind == "snell" {
		return s.validateProxy()
	}
	if s.Kind != "shadowsocks" && s.Kind != "frps" && s.Kind != "frpc" {
		return errors.New("unsupported live service")
	}
	key, e := base64.StdEncoding.DecodeString(s.Password)
	if e != nil || len(key) != 32 {
		return errors.New("32-byte base64 credential required")
	}
	if s.Kind == "shadowsocks" {
		if len(s.Tunnels) > 0 || s.Certificate != "" || s.PrivateKey != "" {
			return errors.New("unexpected shadowsocks fields")
		}
		if s.Peer != nil {
			k, e := base64.StdEncoding.DecodeString(s.Peer.Password)
			if !Address(s.Peer.Address) || s.Peer.Port < 1024 || s.Peer.Port > 65535 || e != nil || len(k) != 32 {
				return errors.New("invalid route peer")
			}
		}
	} else {
		if s.Peer != nil || len(s.Tunnels) == 0 || len(s.Tunnels) > 64 {
			return errors.New("FRP needs 1-64 bounded tunnels")
		}
		cert, _ := pem.Decode([]byte(s.Certificate))
		if cert == nil || cert.Type != "CERTIFICATE" {
			return errors.New("FRP pinned TLS certificate required")
		}
		if _, e = x509.ParseCertificate(cert.Bytes); e != nil {
			return errors.New("invalid FRP certificate")
		}
		if s.Kind == "frps" {
			if _, e := tlsPair(s.Certificate, s.PrivateKey); e != nil {
				return e
			}
		} else if s.PrivateKey != "" {
			return errors.New("client must not receive server private key")
		}
		seen := map[int]bool{s.Port: true}
		names := map[string]bool{}
		for _, t := range s.Tunnels {
			if names[t.Name] || !label.MatchString(t.Name) || (t.LocalHost != "127.0.0.1" && t.LocalHost != "::1") || t.LocalPort < 1 || t.LocalPort > 65535 || t.RemotePort < 1024 || t.RemotePort > 65535 || seen[t.RemotePort] || t.RemotePort == 8080 || t.RemotePort == 8091 || t.RemotePort == 5173 {
				return errors.New("invalid or overlapping FRP tunnel")
			}
			seen[t.RemotePort] = true
			names[t.Name] = true
		}
	}
	return nil
}
func Certificate(address string) (string, string, error) {
	if !Address(address) {
		return "", "", errors.New("invalid certificate address")
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return "", "", e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return "", "", e
	}
	t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Harbor FRP"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(address); ip != nil {
		t.IPAddresses = []net.IP{ip}
	} else {
		t.DNSNames = []string{address}
	}
	der, e := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	if e != nil {
		return "", "", e
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), nil
}

// Render accepts only fixed protocol fields; all file names are chosen locally.
func (s Spec) Render() (string, map[string][]byte, error) {
	if e := s.Validate(); e != nil {
		return "", nil, e
	}
	files := map[string][]byte{}
	if TLSProxy(s.Kind) || s.Kind == "snell" {
		return s.renderProxy()
	}
	engine := "sing-box"
	var config any
	if s.Kind == "shadowsocks" {
		out := map[string]any{"type": "direct", "tag": "egress"}
		if s.Peer != nil {
			out = map[string]any{"type": "shadowsocks", "tag": "egress", "server": s.Peer.Address, "server_port": s.Peer.Port, "method": Method, "password": s.Peer.Password}
		}
		config = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "shadowsocks", "tag": "entry", "listen": s.Listen, "listen_port": s.Port, "method": Method, "password": s.Password}}, "outbounds": []any{out}, "route": map[string]any{"final": "egress"}}
	} else {
		files["tls.crt"] = []byte(s.Certificate)
		auth := map[string]any{"method": "token", "token": s.Password}
		if s.Kind == "frps" {
			engine = "frps"
			files["tls.key"] = []byte(s.PrivateKey)
			allow := []any{}
			for _, t := range s.Tunnels {
				allow = append(allow, map[string]int{"single": t.RemotePort})
			}
			config = map[string]any{"bindAddr": s.Listen, "bindPort": s.Port, "proxyBindAddr": s.Listen, "allowPorts": allow, "auth": auth, "transport": map[string]any{"tls": map[string]any{"force": true, "certFile": "tls.crt", "keyFile": "tls.key"}}}
		} else {
			engine = "frpc"
			proxies := []any{}
			for _, t := range s.Tunnels {
				proxies = append(proxies, map[string]any{"name": t.Name, "type": "tcp", "localIP": t.LocalHost, "localPort": t.LocalPort, "remotePort": t.RemotePort})
			}
			config = map[string]any{"serverAddr": s.Address, "serverPort": s.Port, "auth": auth, "transport": map[string]any{"tls": map[string]any{"enable": true, "trustedCaFile": "tls.crt", "serverName": s.Address}}, "proxies": proxies}
		}
	}
	b, e := json.MarshalIndent(config, "", "  ")
	files["config.json"] = b
	return engine, files, e
}
