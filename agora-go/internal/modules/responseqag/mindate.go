package responseqag

import (
	"math"
	"strconv"
	"time"
	"unicode/utf16"

	"agora/internal/javacompat"
)

// parseMinDate reproduces
//
//	SimpleDateFormat("yyyy-MM-dd").apply { isLenient = false }.parse(minDateStr)
//
// of QagHomeController and ResponseQagPaginatedController: java.util.Date
// milliseconds of the local midnight, ok == false when Kotlin catches the exception
// (ParseException, IllegalArgumentException from the calendar: HTTP 400).
//
// SimpleDateFormat parses from the beginning of the text and ignores what follows the
// day. Each field is read by the locale's integer DecimalFormat: spaces and tabs are
// skipped before a field, an optional '-', then any Unicode decimal digits (Character.digit),
// an optional "E" exponent, and "NaN" is accepted; the '-' between the fields is a literal.
// The non lenient GregorianCalendar (Julian before 1582-10-15) then validates the fields.
func parseMinDate(s string) (ms int64, ok bool) {
	text := utf16.Encode([]rune(s))
	pos := 0
	var fields [3]int32
	for i := 0; i < 3; i++ {
		if i > 0 {
			// the literal '-'
			if pos >= len(text) || text[pos] != '-' {
				return 0, false
			}
			pos++
		}
		// spaces and tabs before the field; the end of the text fails
		for {
			if pos >= len(text) {
				return 0, false
			}
			if c := text[pos]; c != ' ' && c != '\t' {
				break
			}
			pos++
		}
		v, np, found := decimalParse(text, pos)
		if !found {
			return 0, false
		}
		fields[i] = v
		pos = np
	}
	year, month, day := fields[0], fields[1]-1, fields[2] // MONTH is zero based (Int arithmetic)
	if year < 1 || year > 292278994 || month < 0 || month > 11 || day < 1 || day > 31 {
		return 0, false
	}
	m := int(month) + 1
	if !validHybridDate(int(year), m, int(day)) || !fitsInMillis(int(year), m, int(day)) {
		return 0, false
	}
	return hybridMillis(int(year), m, int(day)), true
}

// decimalParse is NumberFormat.getIntegerInstance().parse(text, pos).intValue(): the value
// (Long.intValue or Double.intValue), the new position, or found == false when it returns null.
func decimalParse(text []uint16, pos int) (value int32, next int, found bool) {
	// special case NaN: Double.NaN, whose intValue is 0
	if hasPrefix16(text, pos, "NaN") {
		return 0, pos + 3, true
	}
	// positivePrefix "" and negativePrefix "-": the longer matching prefix wins
	negative := pos < len(text) && text[pos] == '-'
	p := pos
	if negative {
		p++
	}
	// "∞" is infinity: Int.MAX / Int.MIN, outside every calendar range
	if hasPrefix16(text, p, "∞") {
		return math.MaxInt32, p + 1, true
	}
	digits, decimalAt, p, okNum := subparseNumber(text, p, false)
	if !okNum {
		return 0, 0, false
	}
	return numberIntValue(digits, decimalAt, !negative), p, true
}

func hasPrefix16(text []uint16, pos int, prefix string) bool {
	u := utf16.Encode([]rune(prefix))
	if pos+len(u) > len(text) {
		return false
	}
	for i, c := range u {
		if text[pos+i] != c {
			return false
		}
	}
	return true
}

// subparseNumber is DecimalFormat.subparseNumber for a format that parses integers only and
// does not group: the significant digits (without leading zeros), decimalAt (the position
// of the decimal point, shifted by the exponent, Int arithmetic) and the end position.
func subparseNumber(text []uint16, pos int, isExponent bool) (digits []byte, decimalAt int32, next int, ok bool) {
	sawDigit := false
	digitCount := int32(0)
	exponent := int32(0)
loop:
	for ; pos < len(text); pos++ {
		ch := text[pos]
		d := int(ch) - '0'
		if d < 0 || d > 9 {
			d = javacompat.JavaCharacterDigit(rune(ch), 10)
		}
		switch {
		case d == 0:
			sawDigit = true
			if len(digits) == 0 {
				continue // leading zeros of the integer part
			}
			digitCount++
			digits = append(digits, '0')
		case d > 0 && d <= 9:
			sawDigit = true
			digitCount++
			digits = append(digits, byte('0'+d))
		case !isExponent && ch == '.':
			break loop // parseIntegerOnly
		case !isExponent && ch == 'E':
			// the exponent: parsed by a recursive call, whatever its outcome the loop ends
			ep := pos + 1
			negExp := ep < len(text) && text[ep] == '-'
			if negExp {
				ep++
			}
			edigits, edecimalAt, enext, eok := subparseNumber(text, ep, true)
			if eok {
				if v, fits := digitsToLong(edigits, edecimalAt, true); fits {
					pos = enext
					exponent = int32(v) // (int) exponentDigits.getLong()
					if negExp {
						exponent = -exponent
					}
				}
			}
			break loop
		default:
			break loop
		}
	}
	decimalAt = digitCount + exponent
	if !sawDigit && digitCount == 0 {
		return nil, 0, 0, false
	}
	return digits, decimalAt, pos, true
}

// digitsToLong is DigitList.fitsIntoLong + getLong (a positive or negative zero fits since the
// format only parses integers).
func digitsToLong(digits []byte, decimalAt int32, positive bool) (int64, bool) {
	count := len(digits)
	for count > 0 && digits[count-1] == '0' {
		count--
	}
	if count == 0 {
		return 0, true
	}
	const maxCount = 19
	if int(decimalAt) < count || decimalAt > maxCount {
		return 0, false
	}
	d := digits[:count]
	if decimalAt == maxCount {
		const longMinRep = "9223372036854775808"
		cmp := 0
		for i := 0; i < count && cmp == 0; i++ {
			switch {
			case d[i] > longMinRep[i]:
				cmp = 1
			case d[i] < longMinRep[i]:
				cmp = -1
			}
		}
		switch {
		case cmp > 0:
			return 0, false
		case cmp == 0 && count == int(decimalAt) && positive:
			return 0, false // Long.MAX_VALUE + 1
		}
		if cmp == 0 && count == int(decimalAt) {
			return math.MinInt64, true
		}
	}
	s := string(d)
	for i := count; i < int(decimalAt); i++ {
		s += "0"
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// numberIntValue is DecimalFormat.parse(..) followed by Number.intValue(): a Long keeps its low
// 32 bits, a Double saturates (NaN is 0).
func numberIntValue(digits []byte, decimalAt int32, positive bool) int32 {
	if v, fits := digitsToLong(digits, decimalAt, positive); fits {
		if !positive && v != math.MinInt64 {
			v = -v
		}
		return int32(v)
	}
	// DigitList.getDouble: Double.parseDouble(".<digits>E<decimalAt>")
	count := len(digits)
	f, err := strconv.ParseFloat("."+string(digits[:count])+"E"+strconv.Itoa(int(decimalAt)), 64)
	if err != nil && !math.IsInf(f, 0) {
		return 0
	}
	if !positive {
		f = -f
	}
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return int32(f)
}

// lastDayHybrid returns the number of days of the month in the GregorianCalendar
// with the 1582-10-15 cutover (Julian leap years before it).
func monthLengthHybrid(year, month int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		leap := year%4 == 0
		if year > 1582 {
			leap = year%4 == 0 && (year%100 != 0 || year%400 == 0)
		}
		if leap {
			return 29
		}
		return 28
	}
	return 31
}

// validHybridDate reports whether the non lenient calendar keeps the fields as they are.
func validHybridDate(year, month, day int) bool {
	if day > monthLengthHybrid(year, month) {
		return false
	}
	// the ten days skipped by the cutover do not exist
	if year == 1582 && month == 10 && day > 4 && day < 15 {
		return false
	}
	return true
}

// hybridMillis is the instant of the local midnight of a valid date (java.util.Date).
func hybridMillis(year, month, day int) int64 {
	days := epochDay(year, month, day)
	local := days * 86_400_000 // wraps like Java's long arithmetic
	return local - zoneOffsetMillis(local)
}

// fitsInMillis reports whether the local midnight is representable: the calendar of a date beyond
// Long.MAX_VALUE milliseconds (292278994-08-17) does not keep its fields, so the non lenient parse fails.
func fitsInMillis(year, month, day int) bool {
	days := epochDay(year, month, day)
	return days <= math.MaxInt64/86_400_000
}

// epochDay is the number of days since 1970-01-01 in the Julian calendar before the 1582-10-15
// cutover, in the Gregorian calendar after it.
func epochDay(year, month, day int) int64 {
	a := int64((14 - month) / 12)
	y := int64(year) + 4800 - a
	m := int64(month) + 12*a - 3
	jdn := int64(day) + (153*m+2)/5 + 365*y + y/4
	if year > 1582 || year == 1582 && (month > 10 || month == 10 && day >= 15) {
		jdn += -y/100 + y/400 - 32045
	} else {
		jdn += -32083
	}
	return jdn - 2440588
}

// zoneOffsetMillis is the UTC offset of the process zone at the given local wall time.
func zoneOffsetMillis(localMillis int64) int64 {
	if time.Local == time.UTC {
		return 0
	}
	// two passes: the offset at the guessed instant, then at the corrected one
	if localMillis < -62135596800000 || localMillis > 253402300799000 {
		return 0
	}
	_, off := time.UnixMilli(localMillis).In(time.Local).Zone()
	_, off2 := time.UnixMilli(localMillis - int64(off)*1000).In(time.Local).Zone()
	return int64(off2) * 1000
}
