// Package controlorigin validates control endpoints and proves that a new
// endpoint knows the existing Agent identity before any bearer token is sent.
package controlorigin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var dnsName = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)

func Normalize(raw string, allowHTTP bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("control address must be an HTTPS origin without credentials, path or query")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return "", errors.New("HTTPS control address required")
	}
	if net.ParseIP(u.Hostname()) == nil && (!dnsName.MatchString(u.Hostname()) || strings.Contains(u.Hostname(), "..")) {
		return "", errors.New("invalid control hostname")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid control port")
		}
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func Proof(tokenHash, nonce, origin string) string {
	m := hmac.New(sha256.New, []byte(tokenHash))
	m.Write([]byte("starhub-control-origin-v1:" + nonce + ":" + origin))
	return hex.EncodeToString(m.Sum(nil))
}

func TokenHash(token string) string {
	b := sha256.Sum256([]byte(token))
	return hex.EncodeToString(b[:])
}
