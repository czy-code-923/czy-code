package live

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
)

func (s Spec) username() string {
	if s.Username != "" {
		return s.Username
	}
	return "harbor"
}
func extraProtocol(kind string) bool {
	return BasicProxy(kind) || kind == "naive" || kind == "hysteria"
}

func (s Spec) renderExtra() (string, map[string][]byte, error) {
	in := map[string]any{"type": s.Kind, "tag": "entry", "listen": s.Listen, "listen_port": s.Port}
	files := map[string][]byte{}
	users := []any{}
	add := func(name, username, password string) {
		u := map[string]any{"username": username, "password": password}
		if s.Kind == "hysteria" {
			u = map[string]any{"name": name, "auth_str": password}
		}
		users = append(users, u)
	}
	if s.ManagedUsers {
		for _, a := range s.Accounts {
			username := a.Name
			if a.Name == "harbor-chain" {
				username = s.username()
			}
			add(a.Name, username, a.Password)
		}
		if len(users) == 0 {
			sum := sha256.Sum256([]byte(s.Password + "/starhub-unassigned"))
			add("unassigned", "_starhub_unassigned", hex.EncodeToString(sum[:]))
		}
	} else {
		add("harbor", s.username(), s.Password)
	}
	in["users"] = users

	if s.Kind == "naive" {
		in["network"] = "tcp"
	}
	if s.Kind == "hysteria" {
		if s.HysteriaObfs != "" {
			in["obfs"] = s.HysteriaObfs
		}
		in["up_mbps"] = s.UpMbps
		in["down_mbps"] = s.DownMbps
	}
	if s.UsesTLS() {
		files["tls.crt"] = []byte(s.Certificate)
		files["tls.key"] = []byte(s.PrivateKey)
		tls := map[string]any{"enabled": true, "certificate_path": "tls.crt", "key_path": "tls.key"}
		if len(s.ALPN) > 0 {
			tls["alpn"] = s.ALPN
		}
		in["tls"] = tls
	}
	cfg := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{in}, "outbounds": []any{map[string]any{"type": "direct", "tag": "egress"}}, "route": map[string]any{"final": "egress"}}
	raw, e := json.MarshalIndent(cfg, "", "  ")
	files["config.json"] = raw
	if e == nil && s.Outbound != nil {
		e = s.patchOutbound("sing-box", files)
	}
	return "sing-box", files, e
}
func (s Spec) extraClient(tag string) (map[string]any, map[string]any, string, error) {
	kind := s.Kind
	if kind == "mixed" {
		kind = "socks"
	}
	sb := map[string]any{"type": kind, "tag": tag, "server": s.Address, "server_port": s.Port, "username": s.username(), "password": s.Password}
	mh := map[string]any{"type": kind, "name": tag, "server": s.Address, "port": s.Port, "username": s.username(), "password": s.Password}
	scheme := kind
	if kind == "socks" {
		sb["version"] = "5"
		mh["type"] = "socks5"
		mh["udp"] = false
		scheme = "socks5"
	}
	q := url.Values{}
	if s.UsesTLS() {
		sb["tls"] = map[string]any{"enabled": true, "server_name": s.ServerName, "certificate": s.Certificate}
		mh["tls"] = true
		mh["sni"] = s.ServerName
		q.Set("sni", s.ServerName)
		if kind == "http" {
			scheme = "https"
		}
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(s.Address, strconv.Itoa(s.Port)), User: url.UserPassword(s.username(), s.Password), Fragment: tag}
	if kind == "hysteria" {
		if s.HysteriaObfs != "" {
			sb["obfs"] = s.HysteriaObfs
			mh["obfs"] = s.HysteriaObfs
			q.Set("obfsParam", s.HysteriaObfs)
		}
		delete(sb, "username")
		delete(sb, "password")
		sb["auth_str"] = s.Password
		sb["up_mbps"] = s.UpMbps
		sb["down_mbps"] = s.DownMbps
		delete(mh, "username")
		delete(mh, "password")
		mh["auth-str"] = s.Password
		mh["up"] = s.UpMbps
		mh["down"] = s.DownMbps
		q.Set("auth", s.Password)
		q.Set("upmbps", strconv.Itoa(s.UpMbps))
		q.Set("downmbps", strconv.Itoa(s.DownMbps))
		u.User = nil
	}
	if kind == "naive" {
		u.Scheme = "https"
		u.Host = net.JoinHostPort(s.ServerName, strconv.Itoa(s.Port))
		u.Fragment = ""
		u.RawQuery = ""
		return sb, mh, u.String(), nil
	}
	u.RawQuery = q.Encode()
	return sb, mh, u.String(), nil
}

func (s Spec) validateExtra() error {
	if s.Username != "" && (len(s.Username) > 128 || !safeToken(s.Username)) {
		return errors.New("username must be 1-128 printable characters without separators")
	}
	if !BasicProxy(s.Kind) && s.Kind != "naive" && s.Username != "" {
		return errors.New("username requires HTTP/SOCKS/Mixed/Naive")
	}
	if s.Kind == "hysteria" && (s.UpMbps <= 0 || s.DownMbps <= 0) {
		return errors.New("Hysteria v1 requires positive up_mbps and down_mbps")
	}
	return nil
}
func safeToken(s string) bool {
	for _, r := range s {
		if r <= 32 || r == 127 || r == ':' {
			return false
		}
	}
	return true
}
