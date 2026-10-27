package api

import (
	"testing"
	"time"
)

func TestMonthlySettingDay(t *testing.T) {
	for _, c := range []struct {
		anchor, after string
		day           int
		want          string
	}{
		{"2026-10-27T10:00:00+08:00", "2026-10-27T10:00:00+08:00", 5, "2026-11-05T10:00:00+08:00"},
		{"2026-02-18T10:00:00+08:00", "2026-02-18T10:00:00+08:00", 31, "2026-03-31T10:00:00+08:00"},
		{"2026-01-18T10:00:00+08:00", "2026-02-28T10:00:00+08:00", 31, "2026-03-31T10:00:00+08:00"},
	} {
		a, _ := time.Parse(time.RFC3339, c.anchor)
		n, _ := time.Parse(time.RFC3339, c.after)
		if got := nextMonthlyResetDay(a, n, c.day).Format(time.RFC3339); got != c.want {
			t.Fatalf("day %d: %s != %s", c.day, got, c.want)
		}
	}
}
