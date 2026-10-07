package qag

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"agora/internal/common"
	"agora/internal/javacompat"
)

// formatDate is DateMapper.toFormattedDate(Date): "yyyy-MM-dd HH:mm:ss". For
// years outside 1..9999 the pattern letters `yyyy` (year of era, EXCEEDS_PAD)
// print the era year with a '+' beyond four digits.
func formatDate(t time.Time) string {
	l := t.In(time.Local)
	y := l.Year()
	if y >= 1 && y <= 9999 {
		return common.FormatDate(t)
	}
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
	return s + l.Format("-01-02 15:04:05")
}

// localDate is a java.time.LocalDate.
type localDate struct{ Year, Month, Day int }

var errBadDate = errors.New("java.time.format.DateTimeParseException")

func digitsAt(s string, i, n int) (int, bool) {
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

func daysIn(y, m int) int {
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

// parseISOLocalDate parses the whole of s with DateTimeFormatter.ISO_LOCAL_DATE
// (STRICT resolver): [+-]yyyy-MM-dd with the EXCEEDS_PAD year (4 digits; '+' and
// 5..10 digits; '-' and 4..10 digits, never -0000).
func parseISOLocalDate(s string) (localDate, int, error) {
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
		return localDate{}, 0, errBadDate
	}
	switch {
	case sign == '+' && n <= 4, sign == 0 && n > 4:
		return localDate{}, 0, errBadDate
	}
	y, err := strconv.ParseInt(s[start:i], 10, 64)
	if err != nil {
		return localDate{}, 0, errBadDate
	}
	if sign == '-' {
		if y == 0 {
			return localDate{}, 0, errBadDate
		}
		y = -y
	}
	if i >= len(s) || s[i] != '-' {
		return localDate{}, 0, errBadDate
	}
	m, ok := digitsAt(s, i+1, 2)
	if !ok || i+3 >= len(s) || s[i+3] != '-' {
		return localDate{}, 0, errBadDate
	}
	d, ok := digitsAt(s, i+4, 2)
	if !ok {
		return localDate{}, 0, errBadDate
	}
	if y < -999999999 || y > 999999999 || m < 1 || m > 12 || d < 1 || d > daysIn(int(y), m) {
		return localDate{}, 0, errBadDate
	}
	return localDate{int(y), m, d}, i + 6, nil
}

// parseISOLocalDateTime parses the whole of s with ISO_LOCAL_DATE_TIME and
// returns its date (HH:mm[:ss[.fraction]] with the strict ranges).
func parseISOLocalDateTime(s string) (localDate, error) {
	d, end, err := parseISOLocalDate(s)
	if err != nil || end >= len(s) || s[end] != 'T' {
		return localDate{}, errBadDate
	}
	i := end + 1
	h, ok1 := digitsAt(s, i, 2)
	if !ok1 || h > 23 || i+2 >= len(s) || s[i+2] != ':' {
		return localDate{}, errBadDate
	}
	m, ok2 := digitsAt(s, i+3, 2)
	if !ok2 || m > 59 {
		return localDate{}, errBadDate
	}
	i += 5
	if i < len(s) && s[i] == ':' {
		sec, ok := digitsAt(s, i+1, 2)
		if !ok || sec > 59 {
			return localDate{}, errBadDate
		}
		i += 3
		if i < len(s) && s[i] == '.' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' && j-(i+1) < 9 {
				j++
			}
			i = j // ISO_LOCAL_TIME: fraction of 0 to 9 digits (a bare '.' is accepted)
		}
	}
	if i != len(s) {
		return localDate{}, errBadDate
	}
	return d, nil
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler like Jackson's
// LocalDateDeserializer (default formatter, lenient): "yyyy-MM-dd"; a string with
// a 'T' at index 10 is a date-time (ISO_LOCAL_DATE_TIME, optionally followed by
// "Z"); an array [year, month, day].
func (d *localDate) UnmarshalJavaTree(tree any) error {
	switch t := tree.(type) {
	case string:
		s := javacompat.JavaStringTrim(t)
		if s == "" {
			return errBadDate // empty string: null for a non-null Kotlin property
		}
		if len(s) > 10 && s[10] == 'T' {
			if strings.HasSuffix(s, "Z") {
				s = s[:len(s)-1] // read as a LocalDateTime, the zone is only checked to be "Z"
			}
			v, err := parseISOLocalDateTime(s)
			if err != nil {
				return err
			}
			*d = v
			return nil
		}
		v, end, err := parseISOLocalDate(s)
		if err != nil || end != len(s) {
			return errBadDate
		}
		*d = v
		return nil
	case []any:
		if len(t) != 3 {
			return errBadDate // empty array: null; wrong length: "Expected array to end" / invalid month or day
		}
		var v [3]int
		for i, e := range t {
			n, ok := e.(interface{ Int64() (int64, error) })
			if !ok {
				return errBadDate
			}
			x, err := n.Int64()
			if err != nil || x != int64(int32(x)) {
				return errBadDate
			}
			v[i] = int(x)
		}
		if v[1] < 1 || v[1] > 12 || v[2] < 1 || v[2] > daysIn(v[0], v[1]) || v[0] < -999999999 || v[0] > 999999999 {
			return errBadDate
		}
		*d = localDate{v[0], v[1], v[2]}
		return nil
	}
	return errBadDate
}

// toDate is DateUtils.LocalDate.toDate(): the start of the day with the CURRENT
// time of day (LocalTime.now) in the process zone.
func (d localDate) toDate(now time.Time) time.Time {
	n := now.In(time.Local)
	return time.Date(d.Year, time.Month(d.Month), d.Day, n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), time.Local)
}
