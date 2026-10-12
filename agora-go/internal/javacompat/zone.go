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
// The calendar uses java.util.TimeZone's offsets (JavaLegacyOffset), not the full tzdb.
func JavaCalendarTime(y int, m time.Month, d, h, mi, s, ns int, loc *time.Location) time.Time {
	return resolveWall(y, m, d, h, mi, s, ns, loc, true)
}

// javaLegacyStart is ZoneInfoFile's UTC1900: sun.util.calendar.ZoneInfo (java.util.TimeZone)
// keeps no transition before 1900-01-01T00:00:00Z.
var javaLegacyStart = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// javaLegacyLastYear is the year of Long.MAX_VALUE milliseconds (292278994-08-17T07:12:55.807Z).
const javaLegacyLastYear = 292278994

// JavaLegacyOffset is java.util.TimeZone.getOffset (in seconds) of the zone at t: the tzdb
// offset from 1900 on (daylight saving rules included, beyond 2037 too), the zone's raw
// offset before (Europe/Paris: +01:00 where java.time and Go give the local mean time
// +00:09:21). GregorianCalendar, SimpleDateFormat and pgjdbc use these offsets.
func JavaLegacyOffset(t time.Time, loc *time.Location) int {
	if t.Before(javaLegacyStart) {
		return javaRawOffset(loc)
	}
	if raw := javaRawOffset(loc); t.Add(time.Duration(raw)*time.Second).UTC().Year() >= javaLegacyLastYear {
		// In the last year a long can hold, SimpleTimeZone never reaches the start of daylight
		// saving time (its computation overflows) but still ends it: only a daylight saving
		// period carried over from the previous year applies, until its end.
		return javaLastYearOffset(t, loc, raw)
	}
	_, off := t.In(loc).Zone()
	return off
}

// javaLastYearOffset is JavaLegacyOffset in javaLegacyLastYear: the offset of t when it is in
// the daylight saving period that began the previous year (no standard time since the start of
// the year), the raw offset otherwise.
func javaLastYearOffset(t time.Time, loc *time.Location, raw int) int {
	start := time.Date(javaLegacyLastYear, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(raw) * time.Second)
	if !t.In(loc).IsDST() || t.Before(start) {
		return raw
	}
	for u := start; u.Before(t); u = u.Add(time.Hour) {
		if !u.In(loc).IsDST() {
			return raw
		}
	}
	_, off := t.In(loc).Zone()
	return off
}

// javaRawOffset is TimeZone.getRawOffset: the standard (not daylight saving) offset of the
// zone's current rules. (Zones whose tzdb main data has a negative daylight saving time,
// such as Europe/Dublin, are compiled by the JDK from the rearguard data and may differ.)
func javaRawOffset(loc *time.Location) int {
	for _, m := range []time.Month{time.January, time.July} {
		if t := time.Date(2100, m, 1, 0, 0, 0, 0, loc); !t.IsDST() {
			_, off := t.Zone()
			return off
		}
	}
	_, off := time.Date(2100, 1, 1, 0, 0, 0, 0, loc).Zone()
	return off
}

// resolveWall finds the instant of a wall clock; legacy selects the java.util.Calendar
// behaviour (the later offset in an overlap, java.util.TimeZone offsets) instead of
// java.time's (the earlier offset, tzdb offsets).
func resolveWall(y int, m time.Month, d, h, mi, s, ns int, loc *time.Location, legacy bool) time.Time {
	offAt := func(t time.Time) int {
		if legacy {
			return JavaLegacyOffset(t, loc)
		}
		_, off := t.In(loc).Zone()
		return off
	}
	t := time.Date(y, m, d, h, mi, s, ns, loc)
	wall := time.Date(y, m, d, h, mi, s, ns, time.UTC)
	// the valid instants of this wall clock are wall - o for the offsets o around it
	best := time.Time{}
	for _, probe := range []time.Duration{-12 * time.Hour, 0, 12 * time.Hour} {
		off := offAt(t.Add(probe))
		u := wall.Add(-time.Duration(off) * time.Second).In(loc)
		if offAt(u) == off && (best.IsZero() || (legacy && u.After(best)) || (!legacy && u.Before(best))) {
			best = u
		}
	}
	if best.IsZero() {
		// gap: the wall clock read with the offset in force before the transition (time.Date
		// moves backward instead when the offsets are negative, e.g. America/Santiago)
		before := offAt(t.Add(-12 * time.Hour))
		return wall.Add(-time.Duration(before) * time.Second).In(loc)
	}
	return best
}
