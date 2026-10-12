package javacompat

import "time"

// JavaAtZone is LocalDateTime.atZone(zone).toInstant() (ZonedDateTime.ofLocal with no
// preferred offset) for the wall clock y-m-d h:mi:s.ns (out-of-range fields are normalized
// like time.Date): in a gap (spring forward) the wall clock moves forward by the length
// of the gap, in an overlap (fall back) the EARLIER offset wins. time.Date keeps the later
// offset in an overlap (the legacy java.util.Calendar choice, which is what JDBC uses for
// timestamp columns: see store.LocalTimestamp) and is not always forward in a gap.
func JavaAtZone(y int, m time.Month, d, h, mi, s, ns int, loc *time.Location) time.Time {
	return resolveWall(y, m, d, h, mi, s, ns, loc, false)
}

// JavaCalendarTime is the wall clock set on a lenient java.util.GregorianCalendar of the zone
// (pgjdbc reading a timestamp without time zone column): like JavaAtZone in a gap, but the
// LATER offset (standard time) wins in an overlap. time.Date matches it only for zones
// east of Greenwich.
func JavaCalendarTime(y int, m time.Month, d, h, mi, s, ns int, loc *time.Location) time.Time {
	return resolveWall(y, m, d, h, mi, s, ns, loc, true)
}

func resolveWall(y int, m time.Month, d, h, mi, s, ns int, loc *time.Location, later bool) time.Time {
	t := time.Date(y, m, d, h, mi, s, ns, loc)
	wall := time.Date(y, m, d, h, mi, s, ns, time.UTC)
	// the valid instants of this wall clock are wall - o for the offsets o around it
	best := time.Time{}
	for _, probe := range []time.Duration{-12 * time.Hour, 0, 12 * time.Hour} {
		_, off := t.Add(probe).Zone()
		u := wall.Add(-time.Duration(off) * time.Second).In(loc)
		if _, o := u.Zone(); o == off && (best.IsZero() || (later && u.After(best)) || (!later && u.Before(best))) {
			best = u
		}
	}
	if best.IsZero() {
		// gap: the wall clock read with the offset in force before the transition (time.Date
		// moves backward instead when the offsets are negative, e.g. America/Santiago)
		_, before := t.Add(-12 * time.Hour).Zone()
		return wall.Add(-time.Duration(before) * time.Second).In(loc)
	}
	return best
}
