package javacompat

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// screenFloatValue is Kotlin's ScreenFloatValueRegEx (the grammar of Double.valueOf:
// ASCII digits, decimal or hexadecimal significand, f/F/d/D suffix, chars <= U+0020
// around).
var screenFloatValue = func() *regexp.Regexp {
	digits := `([0-9]+)`
	hexDigits := `([0-9a-fA-F]+)`
	exp := `[eE][+-]?` + digits
	hexString := `(0[xX]` + hexDigits + `(\.)?)|(0[xX]` + hexDigits + `?(\.)` + hexDigits + `)`
	number := `(` + digits + `(\.)?(` + digits + `?)(` + exp + `)?)|(\.(` + digits + `)(` + exp + `)?)|((` + hexString + `)[pP][+-]?` + digits + `)`
	return regexp.MustCompile(`^[\x00-\x20]*[+-]?(NaN|Infinity|((` + number + `)[fFdD]?))[\x00-\x20]*$`)
}()

// KotlinToDoubleOrNull reproduces Kotlin String.toDoubleOrNull() (kotlin-stdlib 1.7):
// the ScreenFloatValueRegEx screening, then java.lang.Double.parseDouble (an overflow is
// an infinity, "NaN" and "Infinity" are case sensitive, "inf" or "nan" are not numbers).
func KotlinToDoubleOrNull(s string) (float64, bool) {
	if !screenFloatValue.MatchString(s) {
		return 0, false
	}
	t := JavaStringTrim(s)
	neg := false
	switch {
	case strings.HasPrefix(t, "-"):
		neg, t = true, t[1:]
	case strings.HasPrefix(t, "+"):
		t = t[1:]
	}
	var f float64
	switch t {
	case "NaN":
		return math.NaN(), true
	case "Infinity":
		f = math.Inf(1)
	default:
		// the grammar ends a hexadecimal literal with its p exponent, so a last f/F/d/D is
		// always the type suffix
		if n := len(t); n > 0 && strings.ContainsRune("fFdD", rune(t[n-1])) {
			t = t[:n-1]
		}
		var err error
		f, err = strconv.ParseFloat(t, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return 0, false
		}
	}
	if neg {
		f = -f
	}
	return f, true
}
