package live

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func TLSProxy(kind string) bool {
	return kind == "vless" || kind == "hysteria2" || kind == "tuic" || kind == "trojan"
}
func Proxy(kind string) bool { return kind == "shadowsocks" || kind == "snell" || TLSProxy(kind) }
func UUID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s Spec) validateProxy() error {
	if s.Engine != "" && s.Engine != "sing-box" && !(s.Engine == "xray" && (s.Kind == "vless" || s.Kind == "trojan")) {
		return errors.New("engine does not support this protocol template")
	}
	if s.Peer != nil || len(s.Tunnels) > 0 {
		return errors.New("protocol template does not support FRP tunnels or route peers")
	}
	key, e := base64.StdEncoding.DecodeString(s.Password)
	if e != nil || len(key) != 32 {
		return errors.New("random 32-byte credential required")
	}
	if s.Kind == "snell" {
		if s.Certificate != "" || s.PrivateKey != "" || s.ServerName != "" || s.UUID != "" {
			return errors.New("Snell does not use TLS or UUID fields")
		}
		return nil
	}
	if (s.Kind == "vless" || s.Kind == "tuic") && !uuidPattern.MatchString(s.UUID) {
		return errors.New("valid UUID required")
	}
	if !Address(s.ServerName) {
		return errors.New("TLS server name required")
	}
	pair, e := tlsPair(s.Certificate, s.PrivateKey)
	if e != nil {
		return errors.New("matching PEM certificate and private key required")
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || cert.VerifyHostname(s.ServerName) != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
		return errors.New("TLS certificate hostname or validity mismatch")
	}
	return nil
}
func (s Spec) renderProxy() (string, map[string][]byte, error) {
	if s.Engine == "xray" {
		return s.renderXray()
	}
	inbound := map[string]any{"type": s.Kind, "tag": "entry", "listen": s.Listen, "listen_port": s.Port}
	files := map[string][]byte{}
	if s.Kind == "snell" {
		inbound["version"] = 5
		inbound["psk"] = s.Password
	} else {
		files["tls.crt"] = []byte(s.Certificate)
		files["tls.key"] = []byte(s.PrivateKey)
		tls := map[string]any{"enabled": true, "certificate_path": "tls.crt", "key_path": "tls.key"}
		inbound["tls"] = tls
		user := map[string]any{"name": "harbor"}
		switch s.Kind {
		case "vless":
			user["uuid"] = s.UUID
		case "tuic":
			user["uuid"] = s.UUID
			user["password"] = s.Password
			inbound["congestion_control"] = "cubic"
			inbound["zero_rtt_handshake"] = false
			tls["alpn"] = []string{"h3"}
		default:
			user["password"] = s.Password
		}
		inbound["users"] = []any{user}
	}
	b, e := json.MarshalIndent(map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "egress"}}, "route": map[string]any{"final": "egress"}}, "", "  ")
	files["config.json"] = b
	return "sing-box", files, e
}

// Client returns explicit client formats; private keys never enter subscriptions.
// Imported certificates must be publicly trusted for URI clients. JSON clients
// additionally receive the public certificate as an explicit trust anchor.
func (s Spec) Client(tag string) (map[string]any, map[string]any, string, error) {
	if !Proxy(s.Kind) {
		return nil, nil, "", errors.New("not a proxy endpoint")
	}
	sb := map[string]any{"type": s.Kind, "tag": tag, "server": s.Address, "server_port": s.Port}
	mh := map[string]any{"type": s.Kind, "name": tag, "server": s.Address, "port": s.Port, "udp": true}
	u := url.URL{Scheme: s.Kind, Host: net.JoinHostPort(s.Address, fmt.Sprint(s.Port)), Fragment: tag}
	q := url.Values{}
	if s.Kind == "shadowsocks" {
		sb["method"] = Method
		sb["password"] = s.Password
		mh["type"] = "ss"
		mh["cipher"] = Method
		mh["password"] = s.Password
		u.Scheme = "ss"
		u.User = url.UserPassword(Method, s.Password)
	} else if s.Kind == "snell" {
		sb["version"] = 4
		sb["psk"] = s.Password
		mh["version"] = 4
		mh["psk"] = s.Password
		u.User = url.User(s.Password)
		q.Set("version", "4")
	} else {
		sb["tls"] = map[string]any{"enabled": true, "server_name": s.ServerName, "certificate": s.Certificate}
		mh["sni"] = s.ServerName
		mh["tls"] = true
		mh["skip-cert-verify"] = false
		block, _ := pem.Decode([]byte(s.Certificate))
		if block == nil {
			return nil, nil, "", errors.New("missing certificate")
		}
		// Keep URI certificate validation enabled. No allowInsecure export.
		q.Set("sni", s.ServerName)
		q.Set("security", "tls")
		switch s.Kind {
		case "vless":
			delete(mh, "sni")
			mh["servername"] = s.ServerName
			sb["uuid"] = s.UUID
			mh["uuid"] = s.UUID
			mh["network"] = "tcp"
			u.User = url.User(s.UUID)
			q.Set("encryption", "none")
			q.Set("type", "tcp")
		case "tuic":
			sb["uuid"] = s.UUID
			sb["password"] = s.Password
			sb["congestion_control"] = "cubic"
			sb["tls"].(map[string]any)["alpn"] = []string{"h3"}
			mh["uuid"] = s.UUID
			mh["password"] = s.Password
			mh["congestion-controller"] = "cubic"
			mh["alpn"] = []string{"h3"}
			u.User = url.UserPassword(s.UUID, s.Password)
			q.Set("congestion_control", "cubic")
			q.Set("alpn", "h3")
		default:
			sb["password"] = s.Password
			mh["password"] = s.Password
			u.User = url.User(s.Password)
		}
	}
	u.RawQuery = q.Encode()
	if s.Kind == "snell" {
		return sb, mh, "", nil
	}
	return sb, mh, u.String(), nil
}

func (s Spec) VerifyPublicCertificate() error {
	roots, e := x509.SystemCertPool()
	if e != nil {
		return e
	}
	intermediates := x509.NewCertPool()
	rest := []byte(s.Certificate)
	var leaf *x509.Certificate
	for len(rest) > 0 {
		block, tail := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = tail
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		if leaf == nil {
			leaf = cert
		} else {
			intermediates.AddCert(cert)
		}
	}
	if leaf == nil {
		return errors.New("certificate required")
	}
	_, e = leaf.Verify(x509.VerifyOptions{DNSName: s.ServerName, Roots: roots, Intermediates: intermediates})
	if e != nil {
		return errors.New("publicly trusted fullchain required for portable subscriptions")
	}
	return nil
}

func (s Spec) renderXray() (string, map[string][]byte, error) {
	user := map[string]any{"email": "harbor"}
	settings := map[string]any{}
	if s.Kind == "vless" {
		user["id"] = s.UUID
		settings["decryption"] = "none"
	} else {
		user["password"] = s.Password
	}
	settings["users"] = []any{user}
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "entry", "listen": s.Listen, "port": s.Port, "protocol": s.Kind, "settings": settings, "streamSettings": map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificate": strings.Split(strings.TrimSpace(s.Certificate), "\n"), "key": strings.Split(strings.TrimSpace(s.PrivateKey), "\n")}}}}}}, "outbounds": []any{map[string]any{"protocol": "freedom", "tag": "egress"}}}
	b, e := json.MarshalIndent(cfg, "", "  ")
	return "xray", map[string][]byte{"config.json": b, "tls.crt": []byte(s.Certificate), "tls.key": []byte(s.PrivateKey)}, e
}

func ProtocolCapable(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	major, e := strconv.Atoi(parts[0])
	if e != nil || major < 0 {
		return false
	}
	minor, e := strconv.Atoi(parts[1])
	if e != nil || minor < 0 {
		return false
	}
	patch, e := strconv.Atoi(parts[2])
	return e == nil && patch >= 0 && (major > 0 || minor >= 6)
}
