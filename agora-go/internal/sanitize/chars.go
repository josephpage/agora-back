package sanitize

import "sort"

// u16 is a Java String: a sequence of UTF-16 code units (possibly with lone surrogates).
type u16 = []uint16

// inRanges reports whether c is inside one of the inclusive [lo, hi] pairs of table (sorted).
func inRanges(table []uint16, c uint16) bool {
	n := len(table) / 2
	i := sort.Search(n, func(i int) bool { return table[2*i+1] >= c })
	return i < n && table[2*i] <= c
}

// javaIsLetter is Character.isLetter(char) of the reference JDK.
func javaIsLetter(c uint16) bool {
	if c < 0x80 {
		return c|0x20 >= 'a' && c|0x20 <= 'z'
	}
	return inRanges(javaLetterRanges[:], c)
}

// javaIsWhitespace is Character.isWhitespace(char) of the reference JDK.
func javaIsWhitespace(c uint16) bool {
	if c < 0x80 {
		return c == ' ' || (c >= 0x09 && c <= 0x0d) || (c >= 0x1c && c <= 0x1f)
	}
	return inRanges(javaWhitespaceRanges[:], c)
}

// javaDigit is Character.digit(char, radix) of the reference JDK (-1 when c is not a digit of that radix).
func javaDigit(c uint16, radix int) int {
	i := sort.Search(len(javaDigitRuns), func(i int) bool {
		r := javaDigitRuns[i]
		return int(r[0])+int(r[1]) > int(c)
	})
	if i == len(javaDigitRuns) || javaDigitRuns[i][0] > c {
		return -1
	}
	v := int(javaDigitRuns[i][2]) + int(c-javaDigitRuns[i][0])
	if v >= radix {
		return -1
	}
	return v
}

// isSurrogatePair is Character.isSurrogatePair(high, low).
func isSurrogatePair(hi, lo uint16) bool {
	return hi >= 0xD800 && hi <= 0xDBFF && lo >= 0xDC00 && lo <= 0xDFFF
}

// asciiLower is OWASP Strings.toLowerCase: it only folds A-Z.
func asciiLower(s u16) u16 {
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			out := make(u16, len(s))
			copy(out, s)
			for j := i; j < len(out); j++ {
				if out[j] >= 'A' && out[j] <= 'Z' {
					out[j] |= 0x20
				}
			}
			return out
		}
	}
	return s
}

func str16(s string) u16 {
	out := make(u16, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = uint16(s[i]) // ASCII only
	}
	return out
}
