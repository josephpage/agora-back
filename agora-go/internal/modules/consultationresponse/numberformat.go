package consultationresponse

import (
	"math"
	"math/big"
	"os"
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

// format is DecimalFormat.format(double). The digits are the exact value of the double rounded
// HALF_EVEN to 4 fraction digits (what the JDK obtains from the shortest decimal digits and the
// "rounded up" / "exact" flags of FloatingDecimal), the integer digits are grouped by 3, a negative
// number (-0.0 and the negative numbers that round to 0 included) is prefixed by the minus sign.
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
		return sign + "∞"
	}
	r := new(big.Rat).SetFloat64(math.Abs(d))
	r.Mul(r, big.NewRat(10000, 1))
	// r = n + f, 0 <= f < 1
	n := new(big.Int).Quo(r.Num(), r.Denom())
	f := new(big.Rat).Sub(r, new(big.Rat).SetInt(n))
	switch f.Cmp(big.NewRat(1, 2)) {
	case 1:
		n.Add(n, big.NewInt(1))
	case 0:
		if n.Bit(0) == 1 { // HALF_EVEN
			n.Add(n, big.NewInt(1))
		}
	}
	q, rem := new(big.Int).QuoRem(n, big.NewInt(10000), new(big.Int))
	integer := q.String()
	var sb strings.Builder
	sb.WriteString(sign)
	// grouping of the integer digits
	for i, c := range integer {
		if i > 0 && (len(integer)-i)%3 == 0 {
			sb.WriteString(nf.group)
		}
		sb.WriteRune(c)
	}
	if rem.Sign() != 0 {
		frac := rem.String()
		frac = strings.Repeat("0", 4-len(frac)) + frac
		sb.WriteString(nf.decimal)
		sb.WriteString(strings.TrimRight(frac, "0"))
	}
	return sb.String()
}
