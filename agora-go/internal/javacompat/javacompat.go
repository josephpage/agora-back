// Package javacompat reproduces, bit for bit, the Java/Kotlin standard library
// behaviours the Kotlin backend relied on (UUID parsing, UTF-16 string
// semantics, Kotlin whitespace rules, Math.round, URLEncoder, date formats...).
//
// Every function here is fuzzed against the JVM oracle (see parity/oracle).
package javacompat

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ---------------------------------------------------------------------------
// UUID
// ---------------------------------------------------------------------------

// UUID is a 128 bits identifier, formatted like java.util.UUID.toString().
type UUID struct{ Hi, Lo uint64 }

// NotFoundUUID is the "00000000-0000-0000-0000-000000000000" sentinel (UuidUtils.NOT_FOUND_UUID).
var NotFoundUUID = UUID{}

const (
	NotFoundUUIDString      = "00000000-0000-0000-0000-000000000000"
	SkipQuestionChoiceUUID  = NotFoundUUIDString
	NotApplicableChoiceUUID = "11111111-1111-1111-1111-111111111111"
	hexDigits               = "0123456789abcdef"
)

// String formats like java.util.UUID#toString (lowercase, 8-4-4-4-12).
func (u UUID) String() string {
	var b [36]byte
	put := func(off int, v uint64, n int) {
		for i := n - 1; i >= 0; i-- {
			b[off+i] = hexDigits[v&0xf]
			v >>= 4
		}
	}
	put(0, u.Hi>>32, 8)
	b[8] = '-'
	put(9, (u.Hi>>16)&0xffff, 4)
	b[13] = '-'
	put(14, u.Hi&0xffff, 4)
	b[18] = '-'
	put(19, u.Lo>>48, 4)
	b[23] = '-'
	put(24, u.Lo&0xffffffffffff, 12)
	return string(b[:])
}

// Bytes returns the 16 bytes big-endian representation (as stored by Postgres).
func (u UUID) Bytes() [16]byte {
	var b [16]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(u.Hi >> (56 - 8*i))
		b[8+i] = byte(u.Lo >> (56 - 8*i))
	}
	return b
}

// UUIDFromBytes is the inverse of Bytes.
func UUIDFromBytes(b [16]byte) UUID {
	var u UUID
	for i := 0; i < 8; i++ {
		u.Hi = u.Hi<<8 | uint64(b[i])
		u.Lo = u.Lo<<8 | uint64(b[8+i])
	}
	return u
}

// ParseUUID reproduces java.util.UUID.fromString (JDK 17), including its
// leniency ("1-1-1-1-1" is accepted, as are '+'/'-' signs and any Unicode
// decimal digit understood by Character.digit). ok=false means Java would
// have thrown IllegalArgumentException.
func ParseUUID(name string) (UUID, bool) {
	units := utf16.Encode([]rune(name))
	n := len(units)
	if n > 36 {
		return UUID{}, false
	}
	indexOf := func(from int) int {
		for i := from; i < n; i++ {
			if units[i] == '-' {
				return i
			}
		}
		return -1
	}
	dash1 := indexOf(0)
	dash2 := indexOf(dash1 + 1)
	dash3 := indexOf(dash2 + 1)
	dash4 := indexOf(dash3 + 1)
	dash5 := indexOf(dash4 + 1)
	if dash4 < 0 || dash5 >= 0 {
		return UUID{}, false
	}
	p1, ok1 := parseJavaLongHex(units, 0, dash1)
	p2, ok2 := parseJavaLongHex(units, dash1+1, dash2)
	p3, ok3 := parseJavaLongHex(units, dash2+1, dash3)
	p4, ok4 := parseJavaLongHex(units, dash3+1, dash4)
	p5, ok5 := parseJavaLongHex(units, dash4+1, n)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return UUID{}, false
	}
	hi := uint64(p1) & 0xffffffff
	hi <<= 16
	hi |= uint64(p2) & 0xffff
	hi <<= 16
	hi |= uint64(p3) & 0xffff
	lo := uint64(p4) & 0xffff
	lo <<= 48
	lo |= uint64(p5) & 0xffffffffffff
	return UUID{Hi: hi, Lo: lo}, true
}

// MustParseUUID panics on invalid input (test helper).
func MustParseUUID(s string) UUID {
	u, ok := ParseUUID(s)
	if !ok {
		panic("invalid uuid " + s)
	}
	return u
}

// ToUUIDOrNull mirrors UuidUtils.toUuidOrNull: canonical string or "" + false.
func ToUUIDOrNull(s string) (string, bool) {
	u, ok := ParseUUID(s)
	if !ok {
		return "", false
	}
	return u.String(), true
}

// parseJavaLongHex reproduces Long.parseLong(CharSequence, begin, end, 16).
func parseJavaLongHex(s []uint16, begin, end int) (int64, bool) {
	if begin >= end || begin < 0 {
		return 0, false
	}
	const radix = 16
	negative := false
	i := begin
	limit := -math.MaxInt64 // -Long.MAX_VALUE
	first := rune(s[i])
	if first < '0' {
		if first == '-' {
			negative = true
			limit = math.MinInt64
		} else if first != '+' {
			return 0, false
		}
		i++
		if i == end {
			return 0, false
		}
	}
	multmin := int64(limit) / radix
	var result int64
	for i < end {
		d := JavaCharacterDigit(rune(s[i]), radix)
		i++
		if d < 0 || result < multmin {
			return 0, false
		}
		result *= radix
		if result < int64(limit)+int64(d) {
			return 0, false
		}
		result -= int64(d)
	}
	if negative {
		return result, true
	}
	return -result, true
}

// JavaCharacterDigit reproduces Character.digit(char, radix) for BMP chars.
func JavaCharacterDigit(r rune, radix int) int {
	v := -1
	switch {
	case r >= '0' && r <= '9':
		v = int(r - '0')
	case r >= 'a' && r <= 'z':
		v = int(r-'a') + 10
	case r >= 'A' && r <= 'Z':
		v = int(r-'A') + 10
	case r >= 0xFF21 && r <= 0xFF3A: // fullwidth A-Z
		v = int(r-0xFF21) + 10
	case r >= 0xFF41 && r <= 0xFF5A: // fullwidth a-z
		v = int(r-0xFF41) + 10
	case r >= 0xD800 && r <= 0xDFFF:
		v = -1
	case unicode.Is(unicode.Nd, r):
		start := r
		for start > 0 && unicode.Is(unicode.Nd, start-1) {
			start--
		}
		v = int(r-start) % 10
	}
	if v >= radix {
		return -1
	}
	return v
}

// ---------------------------------------------------------------------------
// UTF-16 string semantics
// ---------------------------------------------------------------------------

// Len16 returns the Java String.length() (number of UTF-16 code units).
func Len16(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// Take16 reproduces Kotlin String.take(n) (UTF-16 code units). When the cut
// splits a surrogate pair, Java keeps a lone high surrogate; once encoded to
// UTF-8 (JDBC, HTTP) it becomes '?', which is what we return.
func Take16(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if count+w > n {
			if w == 2 && count+1 == n {
				return s[:i] + "?"
			}
			return s[:i]
		}
		count += w
	}
	return s
}

// ---------------------------------------------------------------------------
// Kotlin whitespace / blank / trim
// ---------------------------------------------------------------------------

// KotlinIsWhitespace reproduces Kotlin Char.isWhitespace() on the JVM:
// Character.isWhitespace(c) || Character.isSpaceChar(c).
func KotlinIsWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0x1C, 0x1D, 0x1E, 0x1F:
		return true
	}
	return unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

// KotlinTrim reproduces Kotlin CharSequence.trim().
func KotlinTrim(s string) string {
	return strings.TrimFunc(s, KotlinIsWhitespace)
}

// KotlinIsBlank reproduces Kotlin CharSequence.isBlank().
func KotlinIsBlank(s string) bool {
	for _, r := range s {
		if !KotlinIsWhitespace(r) {
			return false
		}
	}
	return true
}

// JavaStringTrim reproduces java.lang.String.trim() (chars <= ' ').
func JavaStringTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

// IsJavaRegexSpace reproduces java.util.regex \s (ASCII only: [ \t\n\x0B\f\r]).
func IsJavaRegexSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Case-insensitive helpers
// ---------------------------------------------------------------------------

func charEqualsIgnoreCase(a, b uint16) bool {
	if a == b {
		return true
	}
	if a >= 0xD800 && a <= 0xDFFF || b >= 0xD800 && b <= 0xDFFF {
		return false
	}
	ua, ub := unicode.ToUpper(rune(a)), unicode.ToUpper(rune(b))
	if ua == ub {
		return true
	}
	return unicode.ToLower(ua) == unicode.ToLower(ub)
}

// ContainsIgnoreCase reproduces Kotlin CharSequence.contains(other, ignoreCase = true).
func ContainsIgnoreCase(s, other string) bool {
	a := utf16.Encode([]rune(s))
	b := utf16.Encode([]rune(other))
	if len(b) == 0 {
		return true
	}
outer:
	for i := 0; i+len(b) <= len(a); i++ {
		for j := range b {
			if !charEqualsIgnoreCase(a[i+j], b[j]) {
				continue outer
			}
		}
		return true
	}
	return false
}

// EqualsIgnoreCase reproduces String.equals(other, ignoreCase = true).
func EqualsIgnoreCase(s, other string) bool {
	a := utf16.Encode([]rune(s))
	b := utf16.Encode([]rune(other))
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !charEqualsIgnoreCase(a[i], b[i]) {
			return false
		}
	}
	return true
}

// KotlinToBoolean reproduces Kotlin String?.toBoolean() (equalsIgnoreCase "true").
func KotlinToBoolean(s string) bool { return strings.EqualFold(s, "true") && len(s) == 4 }

// ---------------------------------------------------------------------------
// Numbers
// ---------------------------------------------------------------------------

// JavaMathRound reproduces Math.round(double) (ties towards +infinity).
func JavaMathRound(x float64) int64 {
	if math.IsNaN(x) {
		return 0
	}
	r := math.Round(x) // ties away from zero
	if x < 0 && r-x == -0.5 {
		r++
	}
	if r >= math.MaxInt64 {
		return math.MaxInt64
	}
	if r <= math.MinInt64 {
		return math.MinInt64
	}
	return int64(r)
}

// KotlinRoundToInt reproduces Double.roundToInt() (NaN panics like Kotlin throws).
func KotlinRoundToInt(x float64) int {
	if math.IsNaN(x) {
		panic("Cannot round NaN value.")
	}
	if x > math.MaxInt32 {
		return math.MaxInt32
	}
	if x < math.MinInt32 {
		return math.MinInt32
	}
	return int(JavaMathRound(x))
}

// KotlinToIntOrNull reproduces String.toIntOrNull() (radix 10, optional sign,
// ASCII digits only — Kotlin uses Character.digit, i.e. Unicode digits too).
func KotlinToIntOrNull(s string) (int, bool) {
	units := utf16.Encode([]rune(s))
	n := len(units)
	if n == 0 {
		return 0, false
	}
	start := 0
	isNegative := false
	limit := int64(-math.MaxInt32)
	first := rune(units[0])
	if first < '0' {
		if n == 1 {
			return 0, false
		}
		start = 1
		if first == '-' {
			isNegative = true
			limit = math.MinInt32
		} else if first != '+' {
			return 0, false
		}
	}
	limitForMaxRadix := int64(-math.MaxInt32) / 36
	limitBeforeMul := limitForMaxRadix
	var result int64
	for i := start; i < n; i++ {
		d := JavaCharacterDigit(rune(units[i]), 10)
		if d < 0 {
			return 0, false
		}
		if result < limitBeforeMul {
			if limitBeforeMul == limitForMaxRadix {
				limitBeforeMul = limit / 10
				if result < limitBeforeMul {
					return 0, false
				}
			} else {
				return 0, false
			}
		}
		result *= 10
		if result < limit+int64(d) {
			return 0, false
		}
		result -= int64(d)
	}
	if isNegative {
		return int(result), true
	}
	return int(-result), true
}

// ---------------------------------------------------------------------------
// Encoding
// ---------------------------------------------------------------------------

// URLEncode reproduces java.net.URLEncoder.encode(s, UTF_8).
func URLEncode(s string) string {
	var b strings.Builder
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '*', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('+')
		default:
			if r == utf8.RuneError && size == 1 {
				// an invalid byte stands for an unpaired surrogate, which
				// String.getBytes(UTF_8) writes as '?'; a real U+FFFD is encoded
				r = '?'
			}
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			for i := 0; i < n; i++ {
				b.WriteByte('%')
				b.WriteByte(hex[buf[i]>>4])
				b.WriteByte(hex[buf[i]&0xf])
			}
		}
	}
	return b.String()
}

// ReplaceDiacritics reproduces StringUtils.replaceDiacritics:
// NFD then removal of \p{InCombiningDiacriticalMarks} (U+0300..U+036F).
func ReplaceDiacritics(s string) string {
	d := norm.NFD.String(s)
	return strings.Map(func(r rune) rune {
		if r >= 0x0300 && r <= 0x036F {
			return -1
		}
		return r
	}, d)
}

// KotlinLowercase reproduces String.lowercase() = toLowerCase(Locale.ROOT):
// per-char Character.toLowerCase plus the two special cases of the JDK
// (U+0130 → "i̇", final sigma → ς).
func KotlinLowercase(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch {
		case r == 0x130:
			b.WriteString("i̇")
		case r == 0x3A3:
			// final sigma: preceded by a cased letter, not followed by one
			prevLetter := i > 0 && unicode.IsLetter(rs[i-1])
			nextLetter := i+1 < len(rs) && unicode.IsLetter(rs[i+1])
			if prevLetter && !nextLetter {
				b.WriteRune(0x3C2)
			} else {
				b.WriteRune(0x3C3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// JavaStringHashCode reproduces java.lang.String.hashCode (UTF-16 based).
func JavaStringHashCode(s string) int32 {
	var h int32
	for _, u := range utf16.Encode([]rune(s)) {
		h = 31*h + int32(u)
	}
	return h
}

// JavaHashMapOrder returns the iteration order of a java.util.HashMap (or
// HashSet) after inserting keys in the given order (duplicates ignored),
// default capacity 16 and load factor 0.75. Use it only when the Kotlin code
// iterated a HashMap/HashSet whose order leaks into an output (Kotlin's
// mapOf/mutableMapOf/groupBy/toSet are LinkedHash* and keep insertion order).
func JavaHashMapOrder(keys []string) []string {
	type entry struct {
		key  string
		hash int32
	}
	capacity := 16
	buckets := make([][]entry, capacity)
	size := 0
	spread := func(h int32) int32 { return h ^ int32(uint32(h)>>16) }
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		h := spread(JavaStringHashCode(k))
		idx := int(h) & (capacity - 1)
		buckets[idx] = append(buckets[idx], entry{k, h})
		size++
		if size > capacity*3/4 {
			// resize: split each bucket preserving relative order (lo then hi)
			newCap := capacity * 2
			nb := make([][]entry, newCap)
			for i, b := range buckets {
				for _, e := range b {
					if int(e.hash)&capacity == 0 {
						nb[i] = append(nb[i], e)
					} else {
						nb[i+capacity] = append(nb[i+capacity], e)
					}
				}
			}
			buckets, capacity = nb, newCap
		}
	}
	out := make([]string, 0, size)
	for _, b := range buckets {
		for _, e := range b {
			out = append(out, e.key)
		}
	}
	return out
}
