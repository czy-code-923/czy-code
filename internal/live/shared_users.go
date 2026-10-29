package live

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

func (s Spec) SharedUserProtocol() bool { return s.Kind == "snell" || s.Kind == "shadowsocks" }
func (s Spec) accountKey(password string) string {
	if s.Kind == "snell" {
		return password
	}
	mac := hmac.New(sha256.New, []byte(password))
	mac.Write([]byte("starhub-ss-user-v1/" + s.Password + "/" + s.SSMethod()))
	size := 32
	if s.SSMethod() == "2022-blake3-aes-128-gcm" {
		size = 16
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)[:size])
}
func (s *Spec) ApplyAccount(a Account) {
	s.UUID = a.UUID
	s.Username = a.Name
	if s.SharedUserProtocol() {
		s.UserKey = s.accountKey(a.Password)
	} else {
		s.Password = a.Password
	}
}
func (s Spec) sharedUsers() []any {
	users := []any{}
	field := "password"
	if s.Kind == "snell" {
		field = "userkey"
	}
	for _, a := range s.Accounts {
		users = append(users, map[string]any{"name": a.Name, field: s.accountKey(a.Password)})
	}
	if len(users) == 0 {
		// The control plane normally supplies a stable, master-key-derived decoy.
		// Standalone previews still fail closed; the published server PSK cannot derive this key.
		users = append(users, map[string]any{"name": "_starhub_unassigned", field: s.accountKey(Secret())})
	}
	return users
}
func ManagedSharedCapable(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return false
	}
	major, e := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	return e == nil && e2 == nil && (major > 0 || minor >= 23)
}
