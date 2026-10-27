package live

import (
	"errors"
	"strconv"
	"strings"
)

// ProtocolOptions are a bounded, versioned subset of native engine options.
// Embedding keeps API, encrypted storage and Agent wire fields identical.
type ProtocolOptions struct {
	Cipher       string `json:"cipher,omitempty"`
	SnellVersion int    `json:"snell_version,omitempty"`
	SnellObfs    string `json:"snell_obfs,omitempty"`
	SnellHost    string `json:"snell_host,omitempty"`
	SnellMode    string `json:"snell_mode,omitempty"`
	HY2Obfs      string `json:"hy2_obfs_password,omitempty"`
	UpMbps       int    `json:"up_mbps,omitempty"`
	DownMbps     int    `json:"down_mbps,omitempty"`
}

func (s Spec) snellVersion() int {
	if s.SnellVersion == 6 {
		return 6
	}
	return 5
}
func (s Spec) RequiresOptions() bool {
	return (s.Engine == "xray" && s.Kind == "shadowsocks") || s.ProtocolOptions != (ProtocolOptions{}) || (s.Outbound != nil && s.Outbound.ProtocolOptions != (ProtocolOptions{}))
}
func MeterCapable(version string) bool {
	if !ProtocolCapable(version) {
		return false
	}
	p := strings.Split(version, ".")
	major, _ := strconv.Atoi(p[0])
	minor, _ := strconv.Atoi(p[1])
	return major > 0 || minor >= 10
}
func OptionsCapable(version string) bool {
	if !ProtocolCapable(version) {
		return false
	}
	p := strings.Split(version, ".")
	major, _ := strconv.Atoi(p[0])
	minor, _ := strconv.Atoi(p[1])
	return major > 0 || minor >= 9
}
func (s Spec) validateOptions() error {
	if s.Cipher != "" && s.Kind != "shadowsocks" {
		return errors.New("cipher selection requires Shadowsocks")
	}
	if s.Kind == "shadowsocks" && s.SSMethod() != Method && s.SSMethod() != "aes-128-gcm" && s.SSMethod() != "aes-256-gcm" && s.SSMethod() != "chacha20-ietf-poly1305" {
		return errors.New("unsupported Shadowsocks cipher")
	}
	if s.Kind != "snell" && (s.SnellVersion != 0 || s.SnellObfs != "" || s.SnellHost != "" || s.SnellMode != "") {
		return errors.New("Snell options require Snell protocol")
	}
	if s.Kind == "snell" {
		if s.SnellVersion != 0 && s.SnellVersion != 5 && s.SnellVersion != 6 {
			return errors.New("Snell server version must be 5 (v4 compatible) or 6")
		}
		if s.SnellObfs != "" && s.SnellObfs != "none" && s.SnellObfs != "http" {
			return errors.New("Snell v5 supports none or http obfuscation")
		}
		if s.SnellHost != "" && (s.SnellObfs != "http" || !Address(s.SnellHost)) {
			return errors.New("Snell HTTP obfuscation host must be a valid host")
		}
		if s.snellVersion() == 6 {
			if s.SnellObfs != "" || s.SnellHost != "" || len(s.Password) < 12 || len(s.Password) > 255 {
				return errors.New("Snell v6 requires 12-255 byte PSK and no HTTP obfuscation")
			}
			if s.SnellMode != "" && s.SnellMode != "default" && s.SnellMode != "unshaped" {
				return errors.New("Snell v6 mode must be default or unshaped")
			}
		} else if s.SnellMode != "" {
			return errors.New("traffic shaping mode requires Snell v6")
		}
	}
	if s.Kind != "hysteria2" && (s.HY2Obfs != "" || s.UpMbps != 0 || s.DownMbps != 0) {
		return errors.New("bandwidth and salamander options require Hysteria2")
	}
	if s.UpMbps < 0 || s.DownMbps < 0 || s.UpMbps > 100000 || s.DownMbps > 100000 {
		return errors.New("bandwidth must be 0-100000 Mbps")
	}
	if s.HY2Obfs != "" && (len(s.HY2Obfs) < 8 || len(s.HY2Obfs) > 256) {
		return errors.New("Hysteria2 salamander password must contain 8-256 bytes")
	}
	return nil
}
func (s Spec) SSMethod() string {
	if s.Cipher != "" {
		return s.Cipher
	}
	return Method
}
