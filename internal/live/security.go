package live

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

type Reality struct {
	Target      string `json:"target"`
	Port        int    `json:"port"`
	PrivateKey  string `json:"private_key,omitempty"`
	PublicKey   string `json:"public_key"`
	ShortID     string `json:"short_id"`
	Fingerprint string `json:"fingerprint"`
}
type Account struct {
	Name     string `json:"name"`
	UUID     string `json:"uuid"`
	Password string `json:"password"`
}

func (s Spec) ClientFormat(format string) error {
	if s.Kind == "snell" && s.UserKey != "" && format != "sing-box" {
		return errors.New("multi-user Snell requires sing-box 1.14+ with userkey")
	}
	if s.Transport == "xhttp" || s.Transport == "kcp" {
		if format != "xray" {
			return errors.New("XHTTP/mKCP export requires verified Xray JSON")
		}
	}
	if s.Transport == "httpupgrade" && format != "sing-box" && format != "xray" {
		return errors.New("HTTPUpgrade has no verified Mihomo adapter; use sing-box/Xray")
	}
	if s.Transport == "http" && format == "xray" {
		return errors.New("sing-box HTTP transport is not Xray XHTTP")
	}
	if s.Kind == "naive" {
		if format != "naive" {
			return errors.New("bundled sing-box has no Naive outbound; choose dedicated NaiveProxy JSON")
		}
		if s.Address != s.ServerName && net.ParseIP(s.Address) == nil {
			return errors.New("NaiveProxy export requires the connection address to equal its certificate domain")
		}
	} else if format == "naive" {
		return errors.New("NaiveProxy only supports Naive endpoints")
	}

	if s.Kind == "snell" && s.snellVersion() == 6 && format != "sing-box" {
		return errors.New("Snell v6 is verified only with sing-box 1.14+ JSON; use Snell v4-compatible mode for Mihomo")
	}
	if (s.Kind == "snell" || s.Kind == "anytls") && format == "shadowrocket" {
		return errors.New("this protocol has no verified Shadowrocket URI export; select sing-box or Mihomo")
	}
	if s.IsReality() && s.Engine == "xray" && format != "xray" {
		return errors.New("Xray 26.10.21 REALITY requires a compatible PQ handshake: select Xray JSON or use sing-box REALITY for sing-box/Mihomo clients")
	}
	if format == "xray" && !EngineSupports("xray", s.Kind) && s.Kind != "shadowsocks" {
		return errors.New("this protocol is unsupported in Xray client JSON")
	}
	return nil
}
func (s Spec) XrayClient(tag string) map[string]any { return s.xrayOutbound(tag) }

func NewReality() *Reality {
	k, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		panic(e)
	}
	b := make([]byte, 8)
	if _, e = rand.Read(b); e != nil {
		panic(e)
	}
	return &Reality{Port: 443, PrivateKey: base64.RawURLEncoding.EncodeToString(k.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), ShortID: hex.EncodeToString(b), Fingerprint: "chrome"}
}
func (s Spec) IsReality() bool { return s.TLSMode == "reality" }
func (s Spec) validateSecurity(client bool) error {
	if e := s.validateOptions(); e != nil {
		return e
	}
	if s.Flow != "" && (s.Kind != "vless" || s.Flow != "xtls-rprx-vision" || s.Transport != "" && s.Transport != "tcp" || (!s.UsesTLS() && !s.IsReality())) {
		return errors.New("Vision requires VLESS over TCP with TLS or REALITY")
	}
	if s.IsReality() {
		if !(s.Kind == "vless" || s.Engine == "xray" && (s.Kind == "trojan" || s.Kind == "vmess")) || !s.validLayer() || s.Reality == nil || !Address(s.ServerName) || s.Certificate != "" || s.PrivateKey != "" {
			return errors.New("REALITY template requires VLESS TCP, SNI and keypair; no CA certificate")
		}
		r := s.Reality
		if s.Engine == "xray" && r.Fingerprint != "chrome" {
			return errors.New("pinned Xray REALITY template requires the verified Chrome fingerprint")
		}
		if r.Fingerprint != "chrome" && r.Fingerprint != "firefox" && r.Fingerprint != "safari" && r.Fingerprint != "edge" {
			return errors.New("unsupported REALITY fingerprint")
		}
		pub, e := base64.RawURLEncoding.DecodeString(r.PublicKey)
		if e != nil || len(pub) != 32 {
			return errors.New("REALITY public key must be 32-byte base64url")
		}
		short, e := hex.DecodeString(r.ShortID)
		if e != nil || len(short) < 1 || len(short) > 8 {
			return errors.New("short ID must be 2-16 hexadecimal characters, even length")
		}
		if client {
			if r.PrivateKey != "" {
				return errors.New("REALITY private key must not enter client configuration")
			}
		} else {
			if !Address(r.Target) || r.Port < 1 || r.Port > 65535 {
				return errors.New("REALITY handshake target and port required")
			}
			if ip := net.ParseIP(r.Target); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
				return errors.New("REALITY target must be public")
			}
			raw, e := base64.RawURLEncoding.DecodeString(r.PrivateKey)
			if e != nil {
				return errors.New("invalid REALITY private key")
			}
			key, e := ecdh.X25519().NewPrivateKey(raw)
			if e != nil || base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()) != r.PublicKey {
				return errors.New("REALITY keypair mismatch")
			}
		}
	} else if s.Reality != nil {
		return errors.New("REALITY settings require REALITY mode")
	}
	if s.Congestion != "" && (s.Kind != "tuic" || (s.Congestion != "cubic" && s.Congestion != "bbr" && s.Congestion != "new_reno")) {
		return errors.New("TUIC congestion must be cubic, bbr or new_reno")
	}
	if len(s.ALPN) > 8 {
		return errors.New("too many ALPN values")
	}
	for _, v := range s.ALPN {
		if len(v) == 0 || len(v) > 32 || strings.ContainsAny(v, "\r\n\t ") {
			return errors.New("invalid ALPN")
		}
	}
	if s.Kind == "tuic" && len(s.ALPN) > 0 && (len(s.ALPN) != 1 || s.ALPN[0] != "h3") {
		return errors.New("TUIC template uses h3 ALPN")
	}
	if s.WSHost != "" && (!slices.Contains([]string{"ws", "httpupgrade", "http", "xhttp"}, s.Transport) || !Address(s.WSHost)) {
		return errors.New("WebSocket Host requires WebSocket and a valid host")
	}
	if s.Transport == "grpc" {
		if len(s.ServiceName) < 1 || len(s.ServiceName) > 80 || strings.ContainsAny(s.ServiceName, "\r\n\t /\\?#") {
			return errors.New("gRPC service name required")
		}
	} else if s.ServiceName != "" {
		return errors.New("service name only valid for gRPC")
	}
	if len(s.Accounts) > 512 {
		return errors.New("at most 512 users per inbound")
	}
	if s.ManagedUsers && !Proxy(s.Kind) {
		return errors.New("managed users require a proxy protocol")
	}
	if s.ManagedUsers && s.SharedUserProtocol() && (s.Engine == "xray" || s.Kind == "shadowsocks" && s.SSMethod() != "2022-blake3-aes-128-gcm" && s.SSMethod() != Method) {
		return errors.New("independent shared-protocol identities require sing-box with SS 2022 AES or Snell")
	}
	seen := map[string]bool{}
	for _, a := range s.Accounts {
		if !s.ManagedUsers || !uuidPattern.MatchString(a.UUID) || len(a.Password) < 8 || a.Name == "" || seen[a.Name] {
			return errors.New("invalid managed account")
		}
		seen[a.Name] = true
	}
	return nil
}
func (s Spec) PublicReality() *Reality {
	if s.Reality == nil {
		return nil
	}
	r := *s.Reality
	r.PrivateKey = ""
	return &r
}
func (s *Spec) StripServerSecrets() {
	s.PrivateKey = ""
	s.Reality = s.PublicReality()
	s.Accounts = nil
	s.ManagedUsers = false
}
func (s Spec) congestion() string {
	if s.Congestion != "" {
		return s.Congestion
	}
	return "cubic"
}
func (s Spec) validateCredential() error {
	if s.Kind == "vless" || s.Kind == "vmess" {
		return nil
	}
	if s.Kind == "shadowsocks" && strings.HasPrefix(s.SSMethod(), "2022-") {
		n := 32
		if s.SSMethod() == "2022-blake3-aes-128-gcm" {
			n = 16
		}
		raw, e := base64.StdEncoding.DecodeString(s.Password)
		if e != nil || len(raw) != n {
			return fmt.Errorf("SS 2022 cipher requires a %d-byte base64 key", n)
		}
		return nil
	}
	if s.Kind == "shadowsocks" && s.SSMethod() != Method {
		if len(s.Password) < 8 || len(s.Password) > 256 || strings.ContainsAny(s.Password, "\r\n\x00") {
			return errors.New("Shadowsocks AEAD password must contain 8-256 bytes")
		}
		return nil
	}
	if s.Kind == "shadowsocks" || s.Kind == "frps" || s.Kind == "frpc" {
		key, e := base64.StdEncoding.DecodeString(s.Password)
		if e != nil || len(key) != 32 {
			return errors.New("SS 2022 / FRP credential requires 32-byte base64")
		}
		return nil
	}
	if len(s.Password) < 8 || len(s.Password) > 256 || strings.ContainsAny(s.Password, "\r\n\x00") {
		return errors.New("password must contain 8-256 bytes without line breaks")
	}
	return nil
}
func (s Spec) realityStream(server bool) map[string]any {
	r := s.Reality
	if server {
		return map[string]any{"target": net.JoinHostPort(r.Target, strconv.Itoa(r.Port)), "serverNames": []string{s.ServerName}, "privateKey": r.PrivateKey, "shortIds": []string{r.ShortID}, "show": false}
	}
	return map[string]any{"serverName": s.ServerName, "fingerprint": r.Fingerprint, "password": r.PublicKey, "shortId": r.ShortID}
}
