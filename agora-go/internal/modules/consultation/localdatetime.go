package consultation

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"agora/internal/javacompat"
)

// LocalDateTime is java.time.LocalDateTime: a wall clock without zone. It is
// held as a time.Time whose fields are the wall clock fields, in UTC, so that
// comparisons are plain field comparisons (never affected by the daylight
// saving time of the process zone, like in Java).
type LocalDateTime struct{ T time.Time }

var errBadDateTime = errors.New("java.time.format.DateTimeParseException")

// FromTime is LocalDateTime.now(clock) / LocalDateTime.ofInstant: the wall
// clock of an instant in the process zone.
func FromTime(t time.Time) LocalDateTime {
	l := t.In(time.Local)
	return LocalDateTime{T: time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), time.UTC)}
}

// Before is LocalDateTime.isBefore.
func (d LocalDateTime) Before(o LocalDateTime) bool { return d.T.Before(o.T) }

// After is LocalDateTime.isAfter.
func (d LocalDateTime) After(o LocalDateTime) bool { return d.T.After(o.T) }

// ToDate is DateUtils.LocalDateTime.toDate(): the instant of the wall clock in
// the process zone (a time in a daylight saving gap is shifted like Java does).
func (d LocalDateTime) ToDate() time.Time {
	return time.Date(d.T.Year(), d.T.Month(), d.T.Day(), d.T.Hour(), d.T.Minute(), d.T.Second(), d.T.Nanosecond(), time.Local)
}

// PlusDays is LocalDateTime.plusDays.
func (d LocalDateTime) PlusDays(n int) LocalDateTime { return LocalDateTime{T: d.T.AddDate(0, 0, n)} }

// Format is DateMapper.toFormattedDate(LocalDateTime): "yyyy-MM-dd HH:mm:ss"
// of the wall clock fields (no zone conversion).
func (d LocalDateTime) Format() string {
	y := d.T.Year()
	if y >= 1 && y <= 9999 {
		return d.T.Format("2006-01-02 15:04:05")
	}
	// years outside 1..9999: `yyyy` is the year of era, '+' beyond four digits
	yoe := y
	if y <= 0 {
		yoe = 1 - y
	}
	s := strconv.Itoa(yoe)
	for len(s) < 4 {
		s = "0" + s
	}
	if yoe > 9999 {
		s = "+" + s
	}
	return s + d.T.Format("-01-02 15:04:05")
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler like Jackson's
// LocalDateTimeDeserializer (default formatter, lenient): a string is trimmed
// then read with ISO_LOCAL_DATE_TIME (a trailing "Z" is dropped when the string
// has a 'T' at index 10); an array [y, M, d, H, m(, s(, nano))] of integers is
// accepted; an empty string is null (an error for a non-null Kotlin property);
// numbers are refused.
func (d *LocalDateTime) UnmarshalJavaTree(tree any) error {
	switch t := tree.(type) {
	case string:
		s := javacompat.JavaStringTrim(t)
		if s == "" {
			return errBadDateTime
		}
		if len(s) > 10 && s[10] == 'T' && strings.HasSuffix(s, "Z") {
			s = s[:len(s)-1]
		}
		v, err := parseISOLocalDateTime(s)
		if err != nil {
			return err
		}
		*d = v
		return nil
	case []any:
		return d.fromArray(t)
	}
	return errBadDateTime
}

func jsonInt(e any) (int64, bool) {
	n, ok := e.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

// fromArray reads [year, month, day, hour, minute], optionally followed by the
// second and the nano of second (READ_DATE_TIMESTAMPS_AS_NANOSECONDS is on).
func (d *LocalDateTime) fromArray(a []any) error {
	if len(a) < 5 || len(a) > 7 {
		return errBadDateTime
	}
	var v [7]int64
	for i, e := range a {
		if i == 0 {
			// getIntValue() also accepts a floating point number (truncated)
			if n, ok := e.(json.Number); ok {
				if f, err := n.Float64(); err == nil && f > -2147483649 && f < 2147483648 {
					v[0] = int64(f)
					continue
				}
			}
			return errBadDateTime
		}
		x, ok := jsonInt(e)
		if !ok || x != int64(int32(x)) {
			return errBadDateTime
		}
		v[i] = x
	}
	year, month, day, hour, minute, second, nano := v[0], v[1], v[2], v[3], v[4], v[5], v[6]
	if year < -999999999 || year > 999999999 || month < 1 || month > 12 || day < 1 || day > int64(daysInMonth(int(year), int(month))) ||
		hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 || nano < 0 || nano > 999999999 {
		return errBadDateTime
	}
	*d = LocalDateTime{T: time.Date(int(year), time.Month(month), int(day), int(hour), int(minute), int(second), int(nano), time.UTC)}
	return nil
}

func daysInMonth(y, m int) int {
	switch m {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
			return 29
		}
		return 28
	}
	return 31
}

func digits(s string, i, n int) (int, bool) {
	if i+n > len(s) {
		return 0, false
	}
	v := 0
	for k := 0; k < n; k++ {
		c := s[i+k]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

// parseISOLocalDateTime parses the whole of s with
// DateTimeFormatter.ISO_LOCAL_DATE_TIME (STRICT resolver, case insensitive):
// [+-]yyyy-MM-dd'T'HH:mm[:ss[.fraction of 0 to 9 digits]].
func parseISOLocalDateTime(s string) (LocalDateTime, error) {
	i := 0
	sign := byte(0)
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		sign = s[i]
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' && i-start < 10 {
		i++
	}
	n := i - start
	if n < 4 {
		return LocalDateTime{}, errBadDateTime
	}
	// SignStyle.EXCEEDS_PAD: a sign is mandatory above 4 digits and refused otherwise for '+'
	switch {
	case sign == '+' && n <= 4, sign == 0 && n > 4:
		return LocalDateTime{}, errBadDateTime
	}
	y, err := strconv.ParseInt(s[start:i], 10, 64)
	if err != nil {
		return LocalDateTime{}, errBadDateTime
	}
	if sign == '-' {
		if y == 0 {
			return LocalDateTime{}, errBadDateTime
		}
		y = -y
	}
	if i >= len(s) || s[i] != '-' {
		return LocalDateTime{}, errBadDateTime
	}
	m, ok := digits(s, i+1, 2)
	if !ok || i+3 >= len(s) || s[i+3] != '-' {
		return LocalDateTime{}, errBadDateTime
	}
	day, ok := digits(s, i+4, 2)
	if !ok {
		return LocalDateTime{}, errBadDateTime
	}
	if y < -999999999 || y > 999999999 || m < 1 || m > 12 || day < 1 || day > daysInMonth(int(y), m) {
		return LocalDateTime{}, errBadDateTime
	}
	i += 6
	if i >= len(s) || (s[i] != 'T' && s[i] != 't') {
		return LocalDateTime{}, errBadDateTime
	}
	i++
	h, ok1 := digits(s, i, 2)
	if !ok1 || h > 23 || i+2 >= len(s) || s[i+2] != ':' {
		return LocalDateTime{}, errBadDateTime
	}
	mi, ok2 := digits(s, i+3, 2)
	if !ok2 || mi > 59 {
		return LocalDateTime{}, errBadDateTime
	}
	i += 5
	sec, nano := 0, 0
	if i < len(s) && s[i] == ':' {
		x, ok := digits(s, i+1, 2)
		if !ok || x > 59 {
			return LocalDateTime{}, errBadDateTime
		}
		sec = x
		i += 3
		if i < len(s) && s[i] == '.' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' && j-(i+1) < 9 {
				j++
			}
			if frac := s[i+1 : j]; frac != "" {
				nano, _ = strconv.Atoi(frac + strings.Repeat("0", 9-len(frac)))
			}
			i = j
		}
	}
	if i != len(s) {
		return LocalDateTime{}, errBadDateTime
	}
	return LocalDateTime{T: time.Date(int(y), time.Month(m), day, h, mi, sec, nano, time.UTC)}, nil
}
