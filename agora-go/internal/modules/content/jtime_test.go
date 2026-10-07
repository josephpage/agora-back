package content

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeTree(t testing.TB, raw string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return tree
}

func TestLocalDateDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string // formatStartOfDay or "ERR"
	}{
		{`"2024-03-01"`, "2024-03-01 00:00:00"},
		{`" 2024-03-01\n"`, "2024-03-01 00:00:00"},
		{`"2024-03-01T10:00:00.000Z"`, "2024-03-01 00:00:00"},
		{`"2024-03-01T10:00:00"`, "2024-03-01 00:00:00"},
		{`"2024-03-01T10:00"`, "2024-03-01 00:00:00"},
		{`"2024-03-01t10:00:00"`, "ERR"}, // only an upper case T starts the date-time form of LocalDate
		{`"2024-03-01T10:00:00+02:00"`, "ERR"},
		{`"2024-03-01TZ"`, "ERR"},
		{`"2024-03-01T10:00:00ZZ"`, "ERR"},
		{`"2024-03-01Z"`, "ERR"},
		{`"2024-3-1"`, "ERR"},
		{`"20240301"`, "ERR"},
		{`"2024-02-29"`, "2024-02-29 00:00:00"},
		{`"2023-02-29"`, "ERR"},
		{`"2024-13-01"`, "ERR"},
		{`"+10000-01-01"`, "+10000-01-01 00:00:00"},
		{`"10000-01-01"`, "ERR"},
		{`"-0001-01-01"`, "0002-01-01 00:00:00"}, // year-of-era 2 (BCE)
		{`"0000-01-01"`, "0001-01-01 00:00:00"},
		{`"-0000-01-01"`, "ERR"},
		{`""`, "ERR"}, {`"  "`, "ERR"}, {`"x"`, "ERR"}, {`null`, "ERR"}, {`true`, "ERR"}, {`{}`, "ERR"}, {`1.5`, "ERR"}, {`1e3`, "ERR"},
		{`19800`, "2024-03-18 00:00:00"}, {`0`, "1970-01-01 00:00:00"}, {`-1`, "1969-12-31 00:00:00"}, {`-0`, "1970-01-01 00:00:00"},
		{`[2024,3,1]`, "2024-03-01 00:00:00"}, {`[2024,3]`, "ERR"}, {`[2024,13,1]`, "ERR"}, {`[]`, "ERR"}, {`["2024",3,1]`, "ERR"},
	} {
		var d LocalDate
		err := d.UnmarshalJavaTree(decodeTree(t, tc.in))
		got := "ERR"
		if err == nil {
			got = d.formatStartOfDay()
		}
		if got != tc.want {
			t.Errorf("%s: %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestLocalDateOfEpochDay(t *testing.T) {
	for day, want := range map[int64]LocalDate{
		0: {1970, 1, 1}, 1: {1970, 1, 2}, -1: {1969, 12, 31}, 59: {1970, 3, 1}, 11016: {2000, 2, 29}, 19800: {2024, 3, 18},
		-719528: {0, 1, 1}, 2932896: {9999, 12, 31}, 2932897: {10000, 1, 1}, -719529: {-1, 12, 31},
	} {
		if got := localDateOfEpochDay(day); got != want {
			t.Errorf("%d: %+v, want %+v", day, got, want)
		}
	}
	// round trip over a wide range
	for day := int64(-800000); day < 3_000_000; day += 997 {
		d := localDateOfEpochDay(day)
		if !validDate(d.Year, d.Month, d.Day) {
			t.Fatalf("%d: invalid %+v", day, d)
		}
		if back := daysFromCivil(d.Year, d.Month, d.Day); back != day {
			t.Fatalf("%d: %+v -> %d", day, d, back)
		}
	}
}

// daysFromCivil is the proleptic Gregorian day number since 1970-01-01 (test helper).
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

func TestLocalDateTimeDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want LocalDateTime
		ok   bool
	}{
		{`"2020-01-01T10:00:00.000Z"`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`"2020-01-01T10:00:00Z"`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`"2020-01-01T10:00Z"`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`"2020-01-01T10:00:00"`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`"2020-01-01T10:00:00.123456789"`, ldt(2020, 1, 1, 10, 0, 0, 123456789), true},
		{`"2020-01-01T10:00:00.5Z"`, ldt(2020, 1, 1, 10, 0, 0, 500000000), true},
		{`"2020-01-01T10:00:00."`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`"2020-01-01t10:00:00z"`, LocalDateTime{}, false},
		{`"2020-01-01T10:00:00.1234567891"`, LocalDateTime{}, false},
		{`"2020-01-01T10:00:00+02:00"`, LocalDateTime{}, false},
		{`"2020-01-01"`, LocalDateTime{}, false},
		{`"2020-01-01T24:00:00"`, LocalDateTime{}, false},
		{`"2020-01-01T23:59:60"`, LocalDateTime{}, false},
		{`"2020-01-01T10:0"`, LocalDateTime{}, false},
		{`"2020-02-30T10:00:00"`, LocalDateTime{}, false},
		{`[2020,1,1,10,0]`, ldt(2020, 1, 1, 10, 0, 0, 0), true},
		{`[2020,1,1,10,0,5,7]`, ldt(2020, 1, 1, 10, 0, 5, 7), true},
		{`[2020,1,1]`, LocalDateTime{}, false},
		{`1577872800000`, LocalDateTime{}, false},
		{`null`, LocalDateTime{}, false},
	} {
		var d LocalDateTime
		err := d.UnmarshalJavaTree(decodeTree(t, tc.in))
		if (err == nil) != tc.ok || (tc.ok && d != tc.want) {
			t.Errorf("%s: %+v %v, want %+v ok=%v", tc.in, d, err, tc.want, tc.ok)
		}
	}
}

func TestLocalDateTimeCompare(t *testing.T) {
	a := ldt(2024, 12, 25, 12, 0, 0, 0)
	for _, tc := range []struct {
		b    LocalDateTime
		want int
	}{
		{a, 0}, {ldt(2024, 12, 25, 12, 0, 0, 1), -1}, {ldt(2024, 12, 25, 11, 59, 59, 999999999), 1},
		{ldt(2025, 1, 1, 0, 0, 0, 0), -1}, {ldt(2023, 12, 31, 23, 59, 59, 0), 1}, {ldt(2024, 11, 30, 23, 0, 0, 0), 1},
	} {
		if got := a.Compare(tc.b); got != tc.want {
			t.Errorf("%+v: %d, want %d", tc.b, got, tc.want)
		}
	}
}
