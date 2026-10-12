package javacompat

import (
	"math/rand"
	"testing"
	"time"

	"agora/parity/oracle"
)

// Wall clocks around the daylight saving transitions of a few zones (30 min DST of
// Lord Howe, southern hemisphere, Paris, New York) against LocalDateTime.atZone.
func TestJavaAtZoneOracle(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(5))
	n := 0
	for _, zone := range []string{"Europe/Paris", "America/New_York", "Australia/Lord_Howe", "America/Santiago", "UTC"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		// every transition of 2024..2027: probe each 10 minutes from -3 h to +3 h (both
		// wall-clock sides of the change), plus random times of the year
		var walls []time.Time
		for u := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC); u.Year() < 2028; u = u.Add(time.Hour) {
			_, o1 := u.In(loc).Zone()
			_, o2 := u.Add(time.Hour).In(loc).Zone()
			if o1 == o2 {
				continue
			}
			l := u.In(loc)
			base := time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), 0, 0, 0, time.UTC)
			for m := -180; m <= 180; m += 10 {
				walls = append(walls, base.Add(time.Duration(m)*time.Minute))
			}
		}
		for k := 0; k < 100; k++ {
			walls = append(walls, time.Date(2024+rnd.Intn(4), 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rnd.Int63n(int64(365*24*time.Hour)))).Truncate(time.Millisecond))
		}
		for _, w := range walls {
			var want int64
			oracle.MustCall(t, "javaAtZone", map[string]any{"wall": w.Format("2006-01-02T15:04:05.000"), "zone": zone}, &want)
			got := JavaAtZone(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), loc).UnixMilli()
			if got != want {
				t.Errorf("%s %s: Go %s, Java %s", zone, w.Format("2006-01-02T15:04:05"), time.UnixMilli(got).UTC(), time.UnixMilli(want).UTC())
			}
			var wantCal int64
			oracle.MustCall(t, "javaCalendarTimestamp", map[string]any{"wall": w.Format("2006-01-02T15:04:05.000"), "zone": zone}, &wantCal)
			if got := JavaCalendarTime(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), loc).UnixMilli(); got != wantCal {
				t.Errorf("calendar %s %s: Go %s, Java %s", zone, w.Format("2006-01-02T15:04:05"), time.UnixMilli(got).UTC(), time.UnixMilli(wantCal).UTC())
			}
			n++
		}
	}
	t.Logf("%d wall clocks compared", n)
}
