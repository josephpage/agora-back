package runner

import (
	"testing"
	"time"
)

func TestCompareTextNowDates(t *testing.T) {
	now := time.Date(2026, 10, 12, 14, 0, 59, 0, time.Local)
	b := NewBinder(nil)
	b.now = func() time.Time { return now }
	body := func(d string) []byte {
		return []byte("<N><notifications><date>" + d + "</date><title>x</title></notifications></N>")
	}
	cases := []struct {
		ref, gov string
		same     bool
	}{
		{"2026-10-12 14:00:59", "2026-10-12 14:00:59", true},
		{"2026-10-12 14:00:59", "2026-10-12 14:01:02", true}, // created a few seconds apart
		{"2026-10-12 14:00:59", "2026-10-12 14:01:09", false},
		{"2026-10-12 14:00:59", "2026-10-12T14:01:00", false}, // another format
		{"2025-10-12 14:00:59", "2025-10-12 14:01:00", false}, // not generated now
	}
	for _, c := range cases {
		if got := len(b.compareText(body(c.ref), body(c.gov))) == 0; got != c.same {
			t.Errorf("%s vs %s: same=%v, want %v", c.ref, c.gov, got, c.same)
		}
	}
	if len(b.compareText([]byte("<a>2026-10-12 14:00:59</a><b>1</b>"), []byte("<a>2026-10-12 14:01:00</a><b>2</b>"))) == 0 {
		t.Error("a difference outside the dates must be reported")
	}
}
