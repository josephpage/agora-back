// Package common holds small helpers shared by modules (DateMapper, …).
package common

import (
	"strconv"
	"time"
)

// FormatDate is DateMapper.toFormattedDate(Date|LocalDateTime):
// "yyyy-MM-dd HH:mm:ss" in the process (JVM default) zone.
//
// For years outside 1..9999 the pattern letters `yyyy` (year of era,
// SignStyle.EXCEEDS_PAD) print the era year with a '+' beyond four digits.
func FormatDate(t time.Time) string {
	l := t.In(time.Local)
	y := l.Year()
	if y >= 1 && y <= 9999 {
		return l.Format("2006-01-02 15:04:05")
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

// FormatLocalDate is DateMapper.toFormattedDate(LocalDate) (atStartOfDay).
func FormatLocalDate(t time.Time) string {
	l := t.In(time.Local)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05")
}

// ParseLocalDate is DateMapper.toLocalDate(String): strict "yyyy-MM-dd"
// (DateTimeFormatter.ofPattern, ResolverStyle.SMART), nil when invalid.
func ParseLocalDate(s string) *time.Time {
	if len(s) != 10 {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		// SMART resolver: day-of-month 29-31 clamp for invalid month ends
		// (e.g. 2026-02-30 → 2026-02-28). Months/days out of 1..12/1..31 fail.
		var y, m, d int
		if n, _ := fmtSscanf(s, &y, &m, &d); n == 3 && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
			last := time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.Local).Day()
			if d > last {
				d = last
			}
			c := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
			return &c
		}
		return nil
	}
	return &t
}

func fmtSscanf(s string, y, m, d *int) (int, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return 0, nil
	}
	parse := func(p string) (int, bool) {
		v := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return 0, false
			}
			v = v*10 + int(c-'0')
		}
		return v, true
	}
	var ok bool
	if *y, ok = parse(s[0:4]); !ok {
		return 0, nil
	}
	if *m, ok = parse(s[5:7]); !ok {
		return 1, nil
	}
	if *d, ok = parse(s[8:10]); !ok {
		return 2, nil
	}
	return 3, nil
}

// StartOfDay returns the local midnight of t.
func StartOfDay(t time.Time) time.Time {
	l := t.In(time.Local)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.Local)
}
