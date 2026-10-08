package consultation

import (
	"os"
	"testing"
	"time"

	"agora/internal/jsonjava"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func TestLocalDateTimeDecoding(t *testing.T) {
	type holder struct {
		D LocalDateTime `json:"d"`
	}
	ok := map[string]string{
		`"2026-10-01T10:00:00"`:           "2026-10-01 10:00:00",
		`"2026-10-01T10:00"`:              "2026-10-01 10:00:00",
		`"2026-10-01T10:00:00.123Z"`:      "2026-10-01 10:00:00",
		`"  2026-10-01T10:00:05Z  "`:      "2026-10-01 10:00:05",
		`"2026-10-01t10:00:05"`:           "2026-10-01 10:00:05",
		`"2026-10-01T10:00:05."`:          "2026-10-01 10:00:05",
		`"2026-10-01T10:00:05.123456789"`: "2026-10-01 10:00:05",
		`"+12026-10-01T10:00:05"`:         "+12026-10-01 10:00:05",
		`"-0001-10-01T10:00:05"`:          "0002-10-01 10:00:05",
		`[2026,10,1,10,0]`:                "2026-10-01 10:00:00",
		`[2026,10,1,10,0,7]`:              "2026-10-01 10:00:07",
		`[2026,10,1,10,0,7,123456789]`:    "2026-10-01 10:00:07",
		`[2026.9,10,1,10,0]`:              "2026-10-01 10:00:00",
		`"2024-02-29T23:59:59"`:           "2024-02-29 23:59:59",
	}
	for in, want := range ok {
		var h holder
		if err := jsonjava.Unmarshal([]byte(`{"d":`+in+`}`), &h); err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got := h.D.Format(); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	bad := []string{
		`""`, `"   "`, `null`, `1790000000`, `1.5`, `true`, `{}`, `"2026-10-01"`, `"2026-10-01T10:00:00+02:00"`, `"2026-10-01T10:00:00Z[UTC]"`,
		`"2026-10-01T24:00:00"`, `"2026-10-01T10:60:00"`, `"2026-10-01T10:00:60"`, `"2026-02-30T10:00:00"`, `"2025-02-29T10:00:00"`, `"2026-13-01T10:00:00"`,
		`"2026-10-01 10:00:00"`, `"26-10-01T10:00:00"`, `"12026-10-01T10:00:00"`, `"+2026-10-01T10:00:00"`, `"2026-10-01T10:00:00.1234567890"`,
		`"2026-10-01T10:00:00ZZ"`, `"2026-10-01T1:00:00"`, `"2026-10-01T10:00:0"`, `[2026,10,1]`, `[2026,10,1,10]`, `[2026,10,1,10,0,0,0,0]`, `[]`,
		`[2026,"10",1,10,0]`, `[2026,13,1,10,0]`, `[2026,10,1,25,0]`, `[2026,10,1,10,0,0,1000000000]`, `["2026",10,1,10,0]`,
	}
	for _, in := range bad {
		var h holder
		if err := jsonjava.Unmarshal([]byte(`{"d":`+in+`}`), &h); err == nil {
			t.Errorf("%s must not be accepted (got %s)", in, h.D.Format())
		}
	}
}

func TestLocalDateTimeComparisonsAndDays(t *testing.T) {
	a := ldt(2026, 6, 1, 12, 0, 0)
	if !a.Before(ldt(2026, 6, 1, 12, 0, 1)) || a.Before(a) || !ldt(2026, 6, 1, 12, 0, 1).After(a) {
		t.Fatal("Before / After")
	}
	cases := []struct {
		from, to LocalDateTime
		days     int64
	}{
		{ldt(2026, 6, 1, 12, 0, 0), ldt(2026, 6, 1, 23, 59, 59), 0},
		{ldt(2026, 6, 1, 12, 0, 0), ldt(2026, 6, 2, 11, 59, 59), 0},
		{ldt(2026, 6, 1, 12, 0, 0), ldt(2026, 6, 2, 12, 0, 0), 1},
		{ldt(2026, 6, 1, 12, 0, 0), ldt(2026, 6, 8, 11, 0, 0), 6},
		{ldt(2026, 6, 1, 12, 0, 0), ldt(2026, 6, 8, 12, 0, 0), 7},
		{ldt(2026, 6, 2, 12, 0, 0), ldt(2026, 6, 1, 13, 0, 0), 0},
		{ldt(2026, 6, 2, 12, 0, 0), ldt(2026, 6, 1, 12, 0, 0), -1},
		{ldt(2026, 6, 2, 12, 0, 0), ldt(2026, 5, 31, 11, 0, 0), -2},
	}
	for _, c := range cases {
		if got := daysBetween(c.from, c.to); got != c.days {
			t.Errorf("daysBetween(%s, %s) = %d, want %d", c.from.Format(), c.to.Format(), got, c.days)
		}
	}
}

func TestLocalDateTimeToDateKeepsTheWallClockInAnyZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata")
	}
	saved := time.Local
	time.Local = paris
	defer func() { time.Local = saved }()

	// the same wall clock goes through Date and back (daylight saving time in October)
	d := ldt(2026, 10, 1, 10, 30, 0)
	if got := FromTime(d.ToDate()); got != d {
		t.Fatalf("round trip: %s", got.Format())
	}
	if got := d.ToDate().UTC().Format("15:04"); got != "08:30" {
		t.Fatalf("Paris is UTC+2 in October: %s", got)
	}
	// spring forward: 02:30 on 2026-03-29 does not exist and is shifted by the gap, like java.time does
	gap := ldt(2026, 3, 29, 2, 30, 0)
	if got := FromTime(gap.ToDate()).Format(); got != "2026-03-29 03:30:00" {
		t.Fatalf("gap: %s", got)
	}
	// FromTime reads the wall clock of the process zone
	if got := FromTime(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)).Format(); got != "2026-06-01 12:00:00" {
		t.Fatalf("FromTime: %s", got)
	}
}
