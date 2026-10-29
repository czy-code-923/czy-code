package authconfig

import "strings"

// Reject distributable example values even when their length passes the guard.
func ValidAdminToken(token string) bool {
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return false
	}
	lower := strings.ToLower(token)
	for _, marker := range []string{"generate-", "change-me", "changeme", "example", "replace-me", "your-admin"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return strings.Trim(token, string(token[0])) != ""
}
