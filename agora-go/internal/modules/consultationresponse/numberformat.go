package consultationresponse

import (
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
)

// javaNumberFormat is `NumberFormat.getInstance().also { it.maximumFractionDigits = 4 }` of the TSV
// mapper: a DecimalFormat of the default FORMAT locale of the JVM, grouping used, no minimum
// fraction digit, 4 maximum fraction digits, RoundingMode.HALF_EVEN.
type javaNumberFormat struct {
	group, decimal, minus string
}

// The symbols of the locales of JDK 17 (CLDR). A locale that is not listed here is formatted
// like English (see parity/ledger/S6.md, open questions).
var numberSymbols = map[string]javaNumberFormat{
	"en": {group: ",", decimal: ".", minus: "-"},
	"fr": {group: " ", decimal: ",", minus: "-"},
	"de": {group: ".", decimal: ",", minus: "-"},
	"es": {group: ".", decimal: ",", minus: "-"},
	"it": {group: ".", decimal: ",", minus: "-"},
	"pt": {group: ".", decimal: ",", minus: "-"},
	"nl": {group: ".", decimal: ",", minus: "-"},
}

// processNumberFormat reads the locale of the process the way the JVM does: -Duser.language.format,
// then -Duser.language of JAVA_OPTS, else the POSIX locale (LC_ALL, then LC_CTYPE, then LANG); "C" and
// "POSIX" are English.
func processNumberFormat() javaNumberFormat {
	return numberFormatFor(processLanguage(os.Getenv))
}

func processLanguage(getenv func(string) string) string {
	opts := getenv("JAVA_OPTS")
	var lang, langFormat string
	for _, f := range strings.Fields(opts) {
		if v, ok := strings.CutPrefix(f, "-Duser.language.format="); ok {
			langFormat = v
		} else if v, ok := strings.CutPrefix(f, "-Duser.language="); ok {
			lang = v
		}
	}
	if langFormat != "" {
		return langFormat
	}
	if lang != "" {
		return lang
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			return posixLanguage(v)
		}
	}
	return "en"
}

// posixLanguage is the language part of a POSIX locale name ("fr_FR.UTF-8" -> "fr").
func posixLanguage(name string) string {
	if i := strings.IndexAny(name, "_.@"); i >= 0 {
		name = name[:i]
	}
	if name == "" || name == "C" || name == "POSIX" {
		return "en"
	}
	return strings.ToLower(name)
}

func numberFormatFor(language string) javaNumberFormat {
	if s, ok := numberSymbols[language]; ok {
		return s
	}
	return numberSymbols["en"]
}

// format is DecimalFormat.format(double), with the JDK 17 algorithm of DigitList.set: the digits are those of Double.toString
// (the shortest decimal that reads back as the double), rounded to 4 fraction digits HALF_EVEN with the two flags FloatingDecimal gives
// ("the digits were rounded up", "the digits are the exact value"), the integer digits are grouped by 3, a negative number (-0.0 and the
// negative numbers that round to 0 included) is prefixed by the minus sign.
func (nf javaNumberFormat) format(d float64) string {
	if math.IsNaN(d) {
		return "NaN"
	}
	negative := d < 0 || (d == 0 && math.Signbit(d))
	sign := ""
	if negative {
		sign = nf.minus
	}
	if math.IsInf(d, 0) {
		return sign + "\u221e"
	}
	if d == 0 {
		return sign + "0"
	}
	ax := math.Abs(d)
	// shortest digits and the position of the decimal point: value = 0.<digits> * 10^decimalAt
	e := strconv.FormatFloat(ax, 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	digits := []byte(strings.Replace(mant, ".", "", 1))
	exp10, _ := strconv.Atoi(exp)
	decimalAt := exp10 + 1

	const maximumDigits = 4
	switch {
	case -decimalAt > maximumDigits:
		return sign + "0" // too small
	case -decimalAt == maximumDigits:
		// the value is in [1e-5, 1e-4): E-form, so Double.toString has at least two digits ("5.0E-5") and the digit string keeps a trailing
		// zero here, which makes "5" an exact tie (rounded down) whatever the flags say
		first := digits[0]
		if first > '5' || (first == '5' && len(digits) > 1) {
			return sign + "0" + nf.decimal + "0001"
		}
		return sign + "0"
	}
	// flags of FloatingDecimal: is the shortest decimal above the exact value (rounded up) or equal to it?
	exactRat := new(big.Rat).SetFloat64(ax)
	shortRat, _ := new(big.Rat).SetString(e)
	cmp := shortRat.Cmp(exactRat)
	alreadyRounded, valueExact := cmp > 0, cmp == 0

	keep := maximumDigits + decimalAt
	if keep >= 0 && keep < len(digits) {
		if shouldRoundUpHalfEven(digits, keep, alreadyRounded, valueExact) {
			k := keep
			for {
				k--
				if k < 0 {
					digits[0] = '1'
					decimalAt++
					k = 0
					break
				}
				digits[k]++
				if digits[k] <= '9' {
					break
				}
			}
			keep = k + 1
		}
		digits = digits[:keep]
		for len(digits) > 1 && digits[len(digits)-1] == '0' {
			digits = digits[:len(digits)-1]
		}
	}
	// integer part and fraction
	var integer, fraction string
	switch {
	case decimalAt <= 0:
		integer = "0"
		fraction = strings.Repeat("0", -decimalAt) + string(digits)
	case decimalAt >= len(digits):
		integer = string(digits) + strings.Repeat("0", decimalAt-len(digits))
	default:
		integer, fraction = string(digits[:decimalAt]), string(digits[decimalAt:])
	}
	fraction = strings.TrimRight(fraction, "0")
	var sb strings.Builder
	sb.WriteString(sign)
	for i, c := range integer {
		if i > 0 && (len(integer)-i)%3 == 0 {
			sb.WriteString(nf.group)
		}
		sb.WriteRune(c)
	}
	if fraction != "" {
		sb.WriteString(nf.decimal)
		sb.WriteString(fraction)
	}
	return sb.String()
}

// shouldRoundUpHalfEven is DigitList.shouldRoundUp for RoundingMode.HALF_EVEN.
func shouldRoundUpHalfEven(digits []byte, maximumDigits int, alreadyRounded, valueExactAsDecimal bool) bool {
	count := len(digits)
	if maximumDigits >= count {
		return false
	}
	switch {
	case digits[maximumDigits] == '5':
		if maximumDigits == count-1 {
			if alreadyRounded {
				return false
			}
			if !valueExactAsDecimal {
				return true
			}
			return maximumDigits > 0 && digits[maximumDigits-1]%2 != 0
		}
		for _, c := range digits[maximumDigits+1:] {
			if c != '0' {
				return true
			}
		}
	case digits[maximumDigits] > '5':
		return true
	}
	return false
}
