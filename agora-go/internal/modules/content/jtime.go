package content

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agora/internal/javacompat"
)

// This file ports what Jackson's JavaTimeModule (jackson-datatype-jsr310
// 2.14.3, default formatters, lenient) does when it reads a java.time.LocalDate
// or LocalDateTime out of a Strapi payload. Only success / failure and the
// resulting value matter: any failure makes the whole Strapi list undecodable
// (empty list / HTTP 500 for a single type), like in Kotlin.

var errBadJavaTime = errors.New("java.time: cannot deserialize")

// LocalDate is java.time.LocalDate (proleptic ISO calendar).
type LocalDate struct {
	Year  int64
	Month int
	Day   int
}

// LocalDateTime is java.time.LocalDateTime.
type LocalDateTime struct {
	Date   LocalDate
	Hour   int
	Minute int
	Second int
	Nano   int
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

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

const minYear, maxYear = -999_999_999, 999_999_999

// validDate is LocalDate.of(...) (also the STRICT resolver of ISO_LOCAL_DATE).
func validDate(y int64, m, d int) bool {
	return y >= minYear && y <= maxYear && m >= 1 && m <= 12 && d >= 1 && d <= monthLength(y, m)
}

// scanISODate parses ISO_LOCAL_DATE at the start of s (STRICT resolver):
// appendValue(YEAR, 4, 10, EXCEEDS_PAD) '-' MM '-' dd. It returns the index
// after the date.
func scanISODate(s string) (LocalDate, int, bool) {
	var zero LocalDate
	i := 0
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
		return zero, 0, false
	}
	switch {
	case negative:
		if year == 0 { // "-0000" is rejected
			return zero, 0, false
		}
		year = -year
	case positive:
		if digits <= 4 { // '+' is only parsed once the pad width is exceeded
			return zero, 0, false
		}
	default:
		if digits > 4 { // '+' is required once the pad width is exceeded
			return zero, 0, false
		}
	}
	if i >= len(s) || s[i] != '-' {
		return zero, 0, false
	}
	month, ok := twoDigits(s, i+1)
	if !ok || i+3 >= len(s) || s[i+3] != '-' {
		return zero, 0, false
	}
	day, ok := twoDigits(s, i+4)
	if !ok {
		return zero, 0, false
	}
	if !validDate(year, month, day) {
		return zero, 0, false
	}
	return LocalDate{year, month, day}, i + 6, true
}

// scanISOTime parses ISO_LOCAL_TIME at the start of s:
// HH ':' mm [':' ss ['.' fraction of 0 to 9 digits]] (STRICT resolver).
func scanISOTime(s string) (h, mi, sec, nano, next int, ok bool) {
	h, ok = twoDigits(s, 0)
	if !ok || len(s) < 3 || s[2] != ':' {
		return 0, 0, 0, 0, 0, false
	}
	mi, ok = twoDigits(s, 3)
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	i := 5
	// optionalStart: ':' ss — an optional section that fails to parse is skipped
	// (the leftover text is then reported as unparsed by the caller).
	if i < len(s) && s[i] == ':' {
		if v, ok2 := twoDigits(s, i+1); ok2 {
			sec = v
			i += 3
			// appendFraction(NANO_OF_SECOND, 0, 9, true): the '.' alone is accepted
			if i < len(s) && s[i] == '.' {
				i++
				n := 0
				for i < len(s) && n < 9 && isDigit(s[i]) {
					nano = nano*10 + int(s[i]-'0')
					i++
					n++
				}
				for ; n < 9; n++ {
					nano *= 10
				}
			}
		}
	}
	if h > 23 || mi > 59 || sec > 59 {
		return 0, 0, 0, 0, 0, false
	}
	return h, mi, sec, nano, i, true
}

// parseLocalDateISO is LocalDate.parse(text, ISO_LOCAL_DATE).
func parseLocalDateISO(s string) (LocalDate, bool) {
	d, n, ok := scanISODate(s)
	if !ok || n != len(s) {
		return LocalDate{}, false
	}
	return d, true
}

// parseLocalDateTimeISO is LocalDateTime.parse(text, ISO_LOCAL_DATE_TIME)
// ('T' is case insensitive).
func parseLocalDateTimeISO(s string) (LocalDateTime, bool) {
	d, n, ok := scanISODate(s)
	if !ok || n >= len(s) || (s[n] != 'T' && s[n] != 't') {
		return LocalDateTime{}, false
	}
	h, mi, sec, nano, m, ok := scanISOTime(s[n+1:])
	if !ok || n+1+m != len(s) {
		return LocalDateTime{}, false
	}
	return LocalDateTime{d, h, mi, sec, nano}, true
}

// javaTrim is String.trim(): every char <= U+0020.
func javaTrim(s string) string { return javacompat.JavaStringTrim(s) }

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler: LocalDateDeserializer.
func (d *LocalDate) UnmarshalJavaTree(tree any) error {
	switch t := tree.(type) {
	case string:
		s := javaTrim(t)
		if s == "" { // _fromEmptyString: null, which Kotlin refuses for a non-null property
			return errBadJavaTime
		}
		if len(s) > 10 && s[10] == 'T' {
			// the 'T' form is read with ISO_LOCAL_DATE_TIME; a trailing 'Z' (lenient)
			// is dropped first. Only the date part is kept.
			if strings.HasSuffix(s, "Z") {
				s = s[:len(s)-1]
			}
			v, ok := parseLocalDateTimeISO(s)
			if !ok {
				return errBadJavaTime
			}
			*d = v.Date
			return nil
		}
		v, ok := parseLocalDateISO(s)
		if !ok {
			return errBadJavaTime
		}
		*d = v
		return nil
	case json.Number:
		// a JSON integer is an epoch day (LocalDate.ofEpochDay); a float is refused
		n, err := strconv.ParseInt(t.String(), 10, 64)
		if err != nil || n < minEpochDay || n > maxEpochDay {
			return errBadJavaTime
		}
		*d = localDateOfEpochDay(n)
		return nil
	case []any:
		// [year, month, day]
		ints, ok := intArray(t, 3, 3)
		if !ok || !validDate(int64(ints[0]), ints[1], ints[2]) {
			return errBadJavaTime
		}
		*d = LocalDate{int64(ints[0]), ints[1], ints[2]}
		return nil
	}
	return errBadJavaTime
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler: LocalDateTimeDeserializer.
func (d *LocalDateTime) UnmarshalJavaTree(tree any) error {
	switch t := tree.(type) {
	case string:
		s := javaTrim(t)
		if s == "" {
			return errBadJavaTime
		}
		if len(s) > 10 && s[10] == 'T' && strings.HasSuffix(s, "Z") {
			s = s[:len(s)-1] // lenient: a trailing 'Z' is dropped
		}
		v, ok := parseLocalDateTimeISO(s)
		if !ok {
			return errBadJavaTime
		}
		*d = v
		return nil
	case []any:
		// [year, month, day, hour, minute [, second [, nano]]]
		ints, ok := intArray(t, 5, 7)
		if !ok {
			return errBadJavaTime
		}
		v := LocalDateTime{Date: LocalDate{int64(ints[0]), ints[1], ints[2]}, Hour: ints[3], Minute: ints[4]}
		if len(ints) > 5 {
			v.Second = ints[5]
		}
		if len(ints) > 6 {
			v.Nano = ints[6]
		}
		if !validDate(v.Date.Year, v.Date.Month, v.Date.Day) ||
			v.Hour < 0 || v.Hour > 23 || v.Minute < 0 || v.Minute > 59 ||
			v.Second < 0 || v.Second > 59 || v.Nano < 0 || v.Nano > 999_999_999 {
			return errBadJavaTime
		}
		*d = v
		return nil
	}
	return errBadJavaTime
}

const (
	minEpochDay = -365243219162
	maxEpochDay = 365241780471
)

// localDateOfEpochDay is LocalDate.ofEpochDay (proleptic Gregorian, days since 1970-01-01).
func localDateOfEpochDay(z int64) LocalDate {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era = (z - 146096) / 146097
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := int(doy - (153*mp+2)/5 + 1)
	m := int(mp + 3)
	if mp >= 10 {
		m = int(mp - 9)
	}
	if m <= 2 {
		y++
	}
	return LocalDate{y, m, d}
}

// intArray reads a JSON array of between lo and hi integers (the numeric
// array form of the java.time deserializers).
func intArray(a []any, lo, hi int) ([]int, bool) {
	if len(a) < lo || len(a) > hi {
		return nil, false
	}
	out := make([]int, len(a))
	for i, e := range a {
		n, ok := e.(json.Number)
		if !ok {
			return nil, false
		}
		v, err := strconv.ParseInt(n.String(), 10, 32)
		if err != nil {
			return nil, false
		}
		out[i] = int(v)
	}
	return out, true
}

// Compare orders two LocalDateTime values (LocalDateTime.compareTo).
func (d LocalDateTime) Compare(o LocalDateTime) int {
	for _, p := range [][2]int64{
		{d.Date.Year, o.Date.Year}, {int64(d.Date.Month), int64(o.Date.Month)}, {int64(d.Date.Day), int64(o.Date.Day)},
		{int64(d.Hour), int64(o.Hour)}, {int64(d.Minute), int64(o.Minute)}, {int64(d.Second), int64(o.Second)},
		{int64(d.Nano), int64(o.Nano)},
	} {
		if p[0] < p[1] {
			return -1
		}
		if p[0] > p[1] {
			return 1
		}
	}
	return 0
}

// localDateTimeOf is LocalDateTime.now(clock): the wall clock of the process
// zone, with the microsecond precision of Clock.systemDefaultZone() on JDK 17.
func localDateTimeOf(t time.Time) LocalDateTime {
	t = t.In(time.Local).Truncate(time.Microsecond)
	return LocalDateTime{
		Date: LocalDate{int64(t.Year()), int(t.Month()), t.Day()},
		Hour: t.Hour(), Minute: t.Minute(), Second: t.Second(), Nano: t.Nanosecond(),
	}
}

// formatStartOfDay is DateMapper.toFormattedDate(LocalDate):
// DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss") applied to atStartOfDay().
// "yyyy" is the year-of-era printed with SignStyle.EXCEEDS_PAD (4 to 19 digits).
func (d LocalDate) formatStartOfDay() string {
	yoe := d.Year
	if yoe <= 0 {
		yoe = 1 - yoe
	}
	year := strconv.FormatInt(yoe, 10)
	for len(year) < 4 {
		year = "0" + year
	}
	if yoe > 9999 {
		year = "+" + year
	}
	return fmt.Sprintf("%s-%02d-%02d 00:00:00", year, d.Month, d.Day)
}
