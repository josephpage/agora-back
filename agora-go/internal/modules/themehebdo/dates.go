package themehebdo

import (
	"errors"
	"math"
	"strconv"
	"sync"
	"time"
	_ "time/tzdata" // Europe/Paris must be resolvable even on images without zoneinfo
)

// errBadDate is a java.time exception (DateTimeParseException, DateTimeException,
// IllegalArgumentException from Date.from...): uncaught in Kotlin, hence HTTP 500.
var errBadDate = errors.New("invalid ISO_OFFSET_DATE_TIME")

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// twoDigits reads exactly two ASCII digits at s[i:].
func twoDigits(s string, i int) (int, bool) {
	if i+2 > len(s) || !isDigit(s[i]) || !isDigit(s[i+1]) {
		return 0, false
	}
	return int(s[i]-'0')*10 + int(s[i+1]-'0'), true
}

func isLeap(y int64) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func monthLength(y int64, m int) int {
	switch m {
	case 2:
		if isLeap(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// daysFromCivil is the proleptic Gregorian day number since 1970-01-01.
func daysFromCivil(y int64, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era = (y - 399) / 400
	}
	yoe := y - era*400
	mp := int64((m + 9) % 12)
	doy := (153*mp+2)/5 + int64(d) - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// parseOffsetDateTimeMillis is Date.from(OffsetDateTime.parse(text).toInstant()).time:
// DateTimeFormatter.ISO_OFFSET_DATE_TIME (STRICT resolver, case-insensitive,
// lenient offset) then Instant.toEpochMilli (floor to the millisecond,
// overflow → IllegalArgumentException).
//
//	[+|-]yyyy-MM-dd'T'HH:mm[:ss[.f{0,9}]](Z|±HH[:mm[:ss]])
func parseOffsetDateTimeMillis(s string) (int64, error) {
	i := 0
	// year: appendValue(YEAR, 4, 10, EXCEEDS_PAD), strict
	negative, positive := false, false
	if i < len(s) && s[i] == '+' {
		positive = true
		i++
	} else if i < len(s) && s[i] == '-' {
		negative = true
		i++
	}
	start := i
	var year int64
	for i < len(s) && i-start < 10 && isDigit(s[i]) {
		year = year*10 + int64(s[i]-'0')
		i++
	}
	digits := i - start
	if digits < 4 {
		return 0, errBadDate
	}
	switch {
	case negative:
		if year == 0 { // minus zero is not allowed
			return 0, errBadDate
		}
		year = -year
	case positive:
		if digits <= 4 { // '+' only parsed when the pad width is exceeded
			return 0, errBadDate
		}
	default:
		if digits > 4 { // '+' must be given when the pad width is exceeded
			return 0, errBadDate
		}
	}
	if year < -999_999_999 || year > 999_999_999 {
		return 0, errBadDate
	}
	if i >= len(s) || s[i] != '-' {
		return 0, errBadDate
	}
	month, ok := twoDigits(s, i+1)
	if !ok || i+3 >= len(s) || s[i+3] != '-' {
		return 0, errBadDate
	}
	day, ok := twoDigits(s, i+4)
	if !ok {
		return 0, errBadDate
	}
	i += 6
	if i >= len(s) || (s[i] != 'T' && s[i] != 't') {
		return 0, errBadDate
	}
	hour, ok := twoDigits(s, i+1)
	if !ok || i+3 >= len(s) || s[i+3] != ':' {
		return 0, errBadDate
	}
	minute, ok := twoDigits(s, i+4)
	if !ok {
		return 0, errBadDate
	}
	i += 6
	second, nano := 0, int64(0)
	if i < len(s) && s[i] == ':' {
		if second, ok = twoDigits(s, i+1); !ok {
			return 0, errBadDate
		}
		i += 3
		if i < len(s) && s[i] == '.' {
			i++
			n := 0
			for i < len(s) && n < 9 && isDigit(s[i]) {
				nano = nano*10 + int64(s[i]-'0')
				i++
				n++
			}
			for ; n < 9; n++ {
				nano *= 10
			}
		}
	}
	// offset: appendOffsetId() = "+HH:MM:ss" / "Z", parsed leniently
	if i >= len(s) {
		return 0, errBadDate
	}
	var offset int64
	switch s[i] {
	case 'Z', 'z':
		i++
	case '+', '-':
		sign := int64(1)
		if s[i] == '-' {
			sign = -1
		}
		oh, ok := twoDigits(s, i+1)
		if !ok || oh > 59 {
			return 0, errBadDate
		}
		i += 3
		om, os := 0, 0
		if i < len(s) && s[i] == ':' {
			if v, ok := twoDigits(s, i+1); ok && v <= 59 {
				om = v
				i += 3
				if i < len(s) && s[i] == ':' {
					if v, ok := twoDigits(s, i+1); ok && v <= 59 {
						os = v
						i += 3
					}
				}
			}
		}
		if oh > 23 {
			return 0, errBadDate
		}
		offset = sign * (int64(oh)*3600 + int64(om)*60 + int64(os))
	default:
		return 0, errBadDate
	}
	if i != len(s) { // unparsed text
		return 0, errBadDate
	}
	// resolve (STRICT)
	if offset < -18*3600 || offset > 18*3600 {
		return 0, errBadDate
	}
	if month < 1 || month > 12 || day < 1 || day > monthLength(year, month) {
		return 0, errBadDate
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, errBadDate
	}
	secs := daysFromCivil(year, month, day)*86400 + int64(hour)*3600 + int64(minute)*60 + int64(second) - offset
	// Instant.toEpochMilli(): Math.multiplyExact / addExact (ArithmeticException
	// → Date.from throws IllegalArgumentException)
	mulSecs := secs
	add := nano / 1_000_000
	if secs < 0 && nano > 0 {
		mulSecs = secs + 1
		add -= 1000
	}
	if mulSecs > math.MaxInt64/1000 || mulSecs < math.MinInt64/1000 {
		return 0, errBadDate
	}
	ms := mulSecs * 1000
	if (add > 0 && ms > math.MaxInt64-add) || (add < 0 && ms < math.MinInt64-add) {
		return 0, errBadDate
	}
	return ms + add, nil
}

var (
	parisOnce sync.Once
	parisLoc  *time.Location
	parisErr  error
)

// paris is ZoneId.of("Europe/Paris").
func paris() *time.Location {
	parisOnce.Do(func() { parisLoc, parisErr = time.LoadLocation("Europe/Paris") })
	if parisErr != nil {
		panic(parisErr)
	}
	return parisLoc
}

func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// formatISOOffsetDateTime is DateTimeFormatter.ISO_OFFSET_DATE_TIME
// .withZone(ZoneId.of("Europe/Paris")).format(Instant.ofEpochMilli(ms)):
// year (4 digits, "+" beyond 9999, "-" when negative), seconds always, the
// fraction only when not zero without trailing zeros, offset "Z" or
// "+HH:MM[:SS]".
func formatISOOffsetDateTime(ms int64) string {
	t := time.UnixMilli(ms).In(paris())
	y := int64(t.Year())
	var b []byte
	switch {
	case y > 9999:
		b = append(b, '+')
		b = append(b, strconv.FormatInt(y, 10)...)
	case y < 0:
		b = append(b, '-')
		b = append(b, pad(-y, 4)...)
	default:
		b = append(b, pad(y, 4)...)
	}
	b = append(b, '-')
	b = append(b, pad(int64(t.Month()), 2)...)
	b = append(b, '-')
	b = append(b, pad(int64(t.Day()), 2)...)
	b = append(b, 'T')
	b = append(b, pad(int64(t.Hour()), 2)...)
	b = append(b, ':')
	b = append(b, pad(int64(t.Minute()), 2)...)
	b = append(b, ':')
	b = append(b, pad(int64(t.Second()), 2)...)
	if nano := t.Nanosecond(); nano != 0 {
		frac := pad(int64(nano), 9)
		end := len(frac)
		for end > 0 && frac[end-1] == '0' {
			end--
		}
		b = append(b, '.')
		b = append(b, frac[:end]...)
	}
	_, off := t.Zone()
	if off == 0 {
		b = append(b, 'Z')
		return string(b)
	}
	if off < 0 {
		b = append(b, '-')
		off = -off
	} else {
		b = append(b, '+')
	}
	b = append(b, pad(int64(off/3600), 2)...)
	b = append(b, ':')
	b = append(b, pad(int64(off%3600/60), 2)...)
	if s := off % 60; s != 0 {
		b = append(b, ':')
		b = append(b, pad(int64(s), 2)...)
	}
	return string(b)
}
