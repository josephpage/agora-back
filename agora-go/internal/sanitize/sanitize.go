// Package sanitize reproduces, byte for byte, the Kotlin ContentSanitizer
// (fr.gouv.agora.usecase.qag.ContentSanitizer):
//
//	HtmlUtils.htmlUnescape(HtmlPolicyBuilder().toFactory().sanitize(content)).take(maxLength)
//
// that is OWASP java-html-sanitizer 20220608.1 with an empty policy (every tag removed, the text kept and
// HTML-encoded, the content of script/style/title/... dropped), followed by Spring 6.0.14's HtmlUtils.htmlUnescape
// and Kotlin's String.take on UTF-16 code units. All the stages are ports of the Java classes (see
// parity/ledger/F7.md) and are verified against the real implementations through the JVM oracle.
package sanitize

import (
	"fmt"
	"unicode/utf8"

	"agora/internal/javacompat"
)

// Sanitize is ContentSanitizer.sanitize(content, maxLength).
//
// The result is what ends up in the database: a lone UTF-16 surrogate (a pair cut in two by take, or produced by a
// numeric entity such as &#xD800;) cannot be encoded to UTF-8 and becomes '?' as the PostgreSQL JDBC driver does when
// it stores the Java string. Use SanitizeUTF16 to get the exact Java string.
//
// It panics, like Kotlin's take, when maxLength is negative (IllegalArgumentException).
func Sanitize(content string, maxLength int) string {
	if isPlainText(content) {
		if maxLength < 0 {
			panic(negativeTake(maxLength))
		}
		return javacompat.Take16(content, maxLength)
	}
	return fromUTF16(SanitizeUTF16(toUTF16(content), maxLength))
}

// isPlainText reports whether the whole pipeline is the identity on content, so that the result is content.take(n):
// no '<' or '&' (a single text token, no entity), only characters that Encoding.encodePcdataOnto either writes as they
// are or writes as a numeric/named reference that HtmlUtils.htmlUnescape turns back into the very same char
// (" ' + = > @ `), no character that is elided (controls other than \t \n \r, DEL, U+FFFD and the other U+FE60..U+FFFF
// chars are sent to the general path), no '{' that the encoder splits ("{{" or a final "{"), no supplementary char
// (written as a reference that Spring casts to (char)) and none of the Indic vowels whose preceding ZWNJ is dropped
// (U+093A..U+0C4C is sent to the general path as a whole), nor U+1FEF. The equivalence is checked against the oracle
// (TestOracleFastPath).
func isPlainText(s string) bool {
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c >= 0x20 && c <= 0x7e:
				if c == '<' || c == '&' || (c == '{' && (i+1 == n || s[i+1] == '{')) {
					return false
				}
			case c == '\t' || c == '\n' || c == '\r':
			default:
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !(r >= 0x80 && r <= 0x939 || r >= 0xC4D && r <= 0xD7FF && r != 0x1FEF || r >= 0xE000 && r <= 0xFE5F) {
			return false
		}
		i += size
	}
	return true
}

// SanitizeUTF16 is ContentSanitizer.sanitize on a Java String given as its UTF-16 code units (which may hold lone
// surrogates, unlike a Go string), returning the exact UTF-16 code units of the Java result.
func SanitizeUTF16(content []uint16, maxLength int) []uint16 {
	out := htmlUnescape(owaspSanitize(content))
	return take(out, maxLength)
}

// Raw is the first stage alone, PolicyFactory.sanitize of the empty OWASP policy (HTML-encoded text), for debugging
// and tests.
func Raw(content []uint16) []uint16 { return owaspSanitize(content) }

// HTMLUnescape is HtmlUtils.htmlUnescape on a Java String, for tests.
func HTMLUnescape(s []uint16) []uint16 { return htmlUnescape(s) }

func negativeTake(n int) string {
	return fmt.Sprintf("java.lang.IllegalArgumentException: Requested character count %d is less than zero.", n)
}

// take is Kotlin String.take(n).
func take(s u16, n int) u16 {
	if n < 0 {
		panic(negativeTake(n))
	}
	if len(s) > n {
		return s[:n]
	}
	return s
}

// toUTF16 converts a Go string to UTF-16 code units; invalid UTF-8 becomes U+FFFD, as Jackson/Tomcat decoding does.
func toUTF16(s string) u16 {
	out := make(u16, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			out = append(out, uint16(c))
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// fromUTF16 converts UTF-16 code units to a Go string; a lone surrogate becomes '?'.
func fromUTF16(u u16) string {
	b := make([]byte, 0, len(u)+len(u)/2)
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c < 0x80:
			b = append(b, byte(c))
		case c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF:
			r := (rune(c)-0xD800)<<10 + (rune(u[i+1]) - 0xDC00) + 0x10000
			b = utf8.AppendRune(b, r)
			i++
		case c >= 0xD800 && c <= 0xDFFF:
			b = append(b, '?')
		default:
			b = utf8.AppendRune(b, rune(c))
		}
	}
	return string(b)
}
