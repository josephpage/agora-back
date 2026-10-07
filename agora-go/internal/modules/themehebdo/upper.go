package themehebdo

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// kotlinUppercase is String.uppercase() = toUpperCase(Locale.ROOT): full Unicode
// case mapping ("ß" → "SS", "ŉ" → "ʼN", ligatures...), no locale rules.
func kotlinUppercase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToUpper(s)
	}
	// Java 17 implements Unicode 13: the lower case letters added by Unicode 14+
	// (which Go's tables know) have no upper case there and stay unchanged.
	var b strings.Builder
	caser := cases.Upper(language.Und)
	start := 0
	for i, r := range s {
		if isAfterUnicode13Lowercase(r) {
			b.WriteString(caser.String(s[start:i]))
			b.WriteRune(r)
			start = i + utf8.RuneLen(r)
		}
	}
	b.WriteString(caser.String(s[start:]))
	return b.String()
}

// isAfterUnicode13Lowercase reports the lower case letters that gained an upper
// case mapping after Unicode 13 (Glagolitic ⱟ, Latin Extended-D ꟁ ꟑ ꟗ ꟙ, Vithkuqi).
func isAfterUnicode13Lowercase(r rune) bool {
	switch r {
	case 0x2C5F, 0xA7C1, 0xA7D1, 0xA7D7, 0xA7D9:
		return true
	}
	return r >= 0x10597 && r <= 0x105BC
}
