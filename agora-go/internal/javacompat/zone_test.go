package javacompat

import (
	"testing"
	"time"
)

func TestJavaAtZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip(err)
	}
	for _, tc := range []struct {
		wall string
		want string
	}{
		{"2026-10-12T10:00:00", "2026-10-12T08:00:00Z"},
		{"2026-03-29T02:30:00", "2026-03-29T01:30:00Z"}, // gap: 03:30 CEST
		{"2026-10-25T02:30:00", "2026-10-25T00:30:00Z"}, // overlap: the earlier offset (CEST)
		{"2026-10-25T01:59:59", "2026-10-24T23:59:59Z"},
		{"2026-10-25T03:00:00", "2026-10-25T02:00:00Z"},
		{"2026-01-15T00:00:00", "2026-01-14T23:00:00Z"},
	} {
		w, _ := time.Parse("2006-01-02T15:04:05", tc.wall)
		got := JavaAtZone(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, paris).UTC().Format(time.RFC3339)
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.wall, got, tc.want)
		}
	}
	// day overflow is normalized like time.Date
	if got := JavaAtZone(2026, 10, 25-14, 2, 30, 0, 0, paris).UTC().Format(time.RFC3339); got != "2026-10-11T00:30:00Z" {
		t.Errorf("normalized: %s", got)
	}
}
