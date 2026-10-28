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
	return kind == "vless" || kind == "hysteria2" || kind == "tuic" || kind == "trojan" || kind == "vmess" || kind == "anytls" || kind == "hysteria" || kind == "naive" || kind == "http"
}
func Proxy(kind string) bool {
	return kind == "shadowsocks" || kind == "snell" || BasicProxy(kind) || TLSProxy(kind)
}
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
	if s.Engine != "" && s.Engine != "sing-box" && !EngineSupports(s.Engine, s.Kind) {
		return errors.New("engine does not support this protocol template")
	}
	if s.Peer != nil || len(s.Tunnels) > 0 {
		return errors.New("protocol template does not support FRP tunnels or route peers")
	}
	if e := s.validateCredential(); e != nil {
		return e
	}
	if s.Kind == "snell" {
		if s.Certificate != "" || s.PrivateKey != "" || s.ServerName != "" || s.UUID != "" {
			return errors.New("Snell does not use TLS or UUID fields")
		}
		return nil
	}
	if (s.Kind == "vless" || s.Kind == "tuic" || s.Kind == "vmess") && !uuidPattern.MatchString(s.UUID) {
		return errors.New("valid UUID required")
	}
	if s.IsReality() {
		return nil
	}
	if !s.UsesTLS() {
		if s.ServerName != "" || s.Certificate != "" || s.PrivateKey != "" {
			return errors.New("TLS fields require TLS mode")
		}
		return nil
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
	if extraProtocol(s.Kind) && s.Engine != "xray" {
		return s.renderExtra()
	}
	if s.Engine == "xray" {
		return s.renderXray()
	}
	inbound := map[string]any{"type": s.Kind, "tag": "entry", "listen": s.Listen, "listen_port": s.Port}
	files := map[string][]byte{}
	if s.Kind == "snell" {
		inbound["version"] = s.snellVersion()
		if s.snellVersion() == 6 {
			if s.SnellMode != "" {
				inbound["mode"] = s.SnellMode
			}
		} else if s.SnellObfs != "" {
			inbound["obfs_mode"] = s.SnellObfs
		}
		inbound["psk"] = s.Password
	} else {
		files["tls.crt"] = []byte(s.Certificate)
		files["tls.key"] = []byte(s.PrivateKey)
		tls := map[string]any{"enabled": true, "certificate_path": "tls.crt", "key_path": "tls.key"}
		if len(s.ALPN) > 0 {
			tls["alpn"] = s.ALPN
		}
		if s.UsesTLS() {
			inbound["tls"] = tls
		} else {
			delete(files, "tls.crt")
			delete(files, "tls.key")
		}
		if s.IsReality() {
			r := s.Reality
			inbound["tls"] = map[string]any{"enabled": true, "server_name": s.ServerName, "reality": map[string]any{"enabled": true, "handshake": map[string]any{"server": r.Target, "server_port": r.Port}, "private_key": r.PrivateKey, "short_id": []string{r.ShortID}}}
		}
		if tr := s.singTransport(); tr != nil {
			inbound["transport"] = tr
		}
		if s.Transport == "grpc" {
			inbound["transport"] = map[string]any{"type": "grpc", "service_name": s.ServiceName}
		}
		if s.Transport == "ws" {
			inbound["transport"] = map[string]any{"type": "ws", "path": s.Path}
		}
		user := map[string]any{"name": "harbor"}
		switch s.Kind {
		case "vless", "vmess":
			user["uuid"] = s.UUID
			if s.Flow != "" {
				user["flow"] = s.Flow
			}
		case "tuic":
			user["uuid"] = s.UUID
			user["password"] = s.Password
			inbound["congestion_control"] = s.congestion()
			inbound["zero_rtt_handshake"] = false
			tls["alpn"] = []string{"h3"}
		default:
			user["password"] = s.Password
		}
		if s.Kind == "hysteria2" {
			if s.UpMbps > 0 {
				inbound["up_mbps"] = s.UpMbps
			}
			if s.DownMbps > 0 {
				inbound["down_mbps"] = s.DownMbps
			}
			if s.HY2Obfs != "" {
				inbound["obfs"] = map[string]any{"type": "salamander", "password": s.HY2Obfs}
			}
		}
		inbound["users"] = []any{user}
		if s.ManagedUsers {
			users := []any{}
			for _, a := range s.Accounts {
				u := map[string]any{"name": a.Name}
				if s.Kind == "vless" || s.Kind == "vmess" || s.Kind == "tuic" {
					u["uuid"] = a.UUID
				}
				if s.Kind != "vless" && s.Kind != "vmess" {
					u["password"] = a.Password
				}
				if s.Flow != "" {
					u["flow"] = s.Flow
				}
				users = append(users, u)
			}
			inbound["users"] = users
		}
	}
	b, e := json.MarshalIndent(map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "egress"}}, "route": map[string]any{"final": "egress"}}, "", "  ")
	files["config.json"] = b
	if e == nil && s.Outbound != nil {
		e = s.patchOutbound("sing-box", files)
	}
	return "sing-box", files, e
}

// Client returns explicit client formats; private keys never enter subscriptions.
// Imported certificates must be publicly trusted for URI clients. JSON clients
// additionally receive the public certificate as an explicit trust anchor.
func (s Spec) Client(tag string) (map[string]any, map[string]any, string, error) {
	if !Proxy(s.Kind) {
		return nil, nil, "", errors.New("not a proxy endpoint")
	}
	if extraProtocol(s.Kind) {
		return s.extraClient(tag)
	}
	sb := map[string]any{"type": s.Kind, "tag": tag, "server": s.Address, "server_port": s.Port}
	mh := map[string]any{"type": s.Kind, "name": tag, "server": s.Address, "port": s.Port, "udp": true}
	u := url.URL{Scheme: s.Kind, Host: net.JoinHostPort(s.Address, fmt.Sprint(s.Port)), Fragment: tag}
	q := url.Values{}
	if s.Kind == "shadowsocks" {
		mh["udp"] = s.ListenNetwork != "tcp"
		if s.ListenNetwork == "tcp" || s.ListenNetwork == "udp" {
			sb["network"] = s.ListenNetwork
		}
		sb["method"] = s.SSMethod()
		sb["password"] = s.Password
		mh["type"] = "ss"
		mh["cipher"] = s.SSMethod()
		mh["password"] = s.Password
		u.Scheme = "ss"
		u.User = url.UserPassword(s.SSMethod(), s.Password)
	} else if s.Kind == "snell" {
		sb["version"] = 4
		if s.snellVersion() == 6 {
			sb["version"] = 6
			if s.SnellMode != "" {
				sb["mode"] = s.SnellMode
			}
		} else if s.SnellObfs != "" {
			sb["obfs_mode"] = s.SnellObfs
			if s.SnellHost != "" {
				sb["obfs_host"] = s.SnellHost
			}
		}
		sb["psk"] = s.Password
		mh["version"] = 4
		mh["psk"] = s.Password
		if s.SnellObfs == "http" {
			opts := map[string]any{"mode": "http"}
			if s.SnellHost != "" {
				opts["host"] = s.SnellHost
			}
			mh["obfs-opts"] = opts
		}
		u.User = url.User(s.Password)
		q.Set("version", "4")
	} else {
		if s.UsesTLS() {
			sb["tls"] = map[string]any{"enabled": true, "server_name": s.ServerName, "certificate": s.Certificate}
		}
		mh["sni"] = s.ServerName
		mh["tls"] = s.UsesTLS()
		mh["skip-cert-verify"] = false
		block, _ := pem.Decode([]byte(s.Certificate))
		if block == nil && s.UsesTLS() && s.Certificate != "" {
			return nil, nil, "", errors.New("missing certificate")
		}
		// Keep URI certificate validation enabled. No allowInsecure export.
		q.Set("sni", s.ServerName)
		q.Set("security", "tls")
		switch s.Kind {
		case "vless", "vmess":
			delete(mh, "sni")
			mh["servername"] = s.ServerName
			sb["uuid"] = s.UUID
			if s.Kind == "vmess" {
				sb["security"] = "aes-128-gcm"
				mh["cipher"] = "aes-128-gcm"
				mh["alterId"] = 0
			}
			mh["uuid"] = s.UUID
			mh["network"] = "tcp"
			u.User = url.User(s.UUID)
			q.Set("encryption", "none")
			q.Set("type", "tcp")
		case "tuic":
			sb["uuid"] = s.UUID
			sb["password"] = s.Password
			sb["congestion_control"] = s.congestion()
			sb["tls"].(map[string]any)["alpn"] = []string{"h3"}
			mh["uuid"] = s.UUID
			mh["password"] = s.Password
			mh["congestion-controller"] = s.congestion()
			mh["alpn"] = []string{"h3"}
			u.User = url.UserPassword(s.UUID, s.Password)
			q.Set("congestion_control", s.congestion())
			q.Set("alpn", "h3")
		default:
			sb["password"] = s.Password
			mh["password"] = s.Password
			u.User = url.User(s.Password)
		}
	}
	if s.Kind == "hysteria2" {
		if s.UpMbps > 0 {
			sb["up_mbps"] = s.UpMbps
			mh["up"] = s.UpMbps
		}
		if s.DownMbps > 0 {
			sb["down_mbps"] = s.DownMbps
			mh["down"] = s.DownMbps
		}
		if s.HY2Obfs != "" {
			sb["obfs"] = map[string]any{"type": "salamander", "password": s.HY2Obfs}
			mh["obfs"] = "salamander"
			mh["obfs-password"] = s.HY2Obfs
			q.Set("obfs", "salamander")
			q.Set("obfs-password", s.HY2Obfs)
		}
	}
	if len(s.ALPN) > 0 && s.UsesTLS() {
		sb["tls"].(map[string]any)["alpn"] = s.ALPN
		mh["alpn"] = s.ALPN
		q.Set("alpn", strings.Join(s.ALPN, ","))
	}
	if s.Flow != "" {
		sb["flow"] = s.Flow
		mh["flow"] = s.Flow
		q.Set("flow", s.Flow)
	}
	if s.IsReality() {
		r := s.Reality
		if r == nil {
			return nil, nil, "", errors.New("REALITY missing")
		}
		sb["tls"] = map[string]any{"enabled": true, "server_name": s.ServerName, "utls": map[string]any{"enabled": true, "fingerprint": r.Fingerprint}, "reality": map[string]any{"enabled": true, "public_key": r.PublicKey, "short_id": r.ShortID}}
		mh["tls"] = true
		mh["servername"] = s.ServerName
		mh["client-fingerprint"] = r.Fingerprint
		mh["reality-opts"] = map[string]any{"public-key": r.PublicKey, "short-id": r.ShortID}
		q.Set("security", "reality")
		q.Set("sni", s.ServerName)
		q.Set("fp", r.Fingerprint)
		q.Set("pbk", r.PublicKey)
		q.Set("sid", r.ShortID)
	}
	if s.Transport == "grpc" {
		sb["transport"] = map[string]any{"type": "grpc", "service_name": s.ServiceName}
		mh["network"] = "grpc"
		mh["grpc-opts"] = map[string]any{"grpc-service-name": s.ServiceName}
		q.Set("type", "grpc")
		q.Set("serviceName", s.ServiceName)
	}
	if s.WSHost != "" {
		q.Set("host", s.WSHost)
	}
	if !s.UsesTLS() && !s.IsReality() {
		delete(mh, "sni")
		delete(mh, "servername")
		q.Del("sni")
		q.Del("security")
	}
	if s.Transport == "ws" {
		sb["transport"] = map[string]any{"type": "ws", "path": s.Path}
		mh["network"] = "ws"
		mh["ws-opts"] = map[string]any{"path": s.Path}
		q.Set("type", "ws")
		q.Set("path", s.Path)
		if s.WSHost != "" {
			headers := map[string]string{"Host": s.WSHost}
			sb["transport"].(map[string]any)["headers"] = headers
			mh["ws-opts"].(map[string]any)["headers"] = headers
		}
	}
	if s.Transport == "httpupgrade" || s.Transport == "http" || s.Transport == "xhttp" || s.Transport == "kcp" {
		q.Set("type", s.Transport)
		if s.Path != "" {
			q.Set("path", s.Path)
		}
		if t := s.singTransport(); t != nil {
			sb["transport"] = t
		}
		if s.Transport == "http" {
			mh["network"] = "h2"
			mh["h2-opts"] = map[string]any{"path": s.Path}
			if s.WSHost != "" {
				mh["h2-opts"].(map[string]any)["host"] = []string{s.WSHost}
			}
		}
	}
	if s.Kind == "vmess" {
		payload := map[string]any{"v": "2", "ps": tag, "add": s.Address, "port": s.Port, "id": s.UUID, "aid": "0", "scy": "aes-128-gcm", "net": func() string {
			if s.Transport != "" {
				return s.Transport
			}
			return "tcp"
		}(), "type": "none", "tls": "", "sni": s.ServerName, "path": s.Path}
		if s.UsesTLS() {
			payload["tls"] = "tls"
		}
		if s.Transport == "ws" {
			payload["net"] = "ws"
			payload["host"] = s.WSHost
		}
		if s.Transport == "grpc" {
			payload["net"] = "grpc"
			payload["path"] = s.ServiceName
			payload["type"] = "gun"
		}
		raw, _ := json.Marshal(payload)
		return sb, mh, "vmess://" + base64.StdEncoding.EncodeToString(raw), nil
	}
	u.RawQuery = q.Encode()
	if s.Kind == "snell" || s.Kind == "anytls" {
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
