package javacompat

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8Java decodes b like java.nio.charset.Charset.forName("UTF-8").decode(...)
// (CodingErrorAction.REPLACE), which Tomcat uses for query parameters: every
// malformed sequence becomes one U+FFFD, with the JDK 17 UTF_8.Decoder rules for
// the length of a malformed sequence, and a truncated sequence at the end of the
// input becomes a single U+FFFD.
//
// The JDK decoder is strict (no overlongs, no surrogates, nothing above
// U+10FFFF), exactly like Go's: valid input is returned unchanged.
func DecodeUTF8Java(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b) + 8)
	n := len(b)
	repl := func() { sb.WriteRune(utf8.RuneError) }
	for i := 0; i < n; {
		b1 := b[i]
		switch {
		case b1 < 0x80:
			sb.WriteByte(b1)
			i++
		case b1>>5 == 0x6 && b1&0x1e != 0: // C2..DF: 2 bytes
			if n-i < 2 {
				repl()
				i = n
				continue
			}
			b2 := b[i+1]
			if !isCont(b2) {
				repl()
				i++
				continue
			}
			sb.WriteRune(rune(b1&0x1f)<<6 | rune(b2&0x3f))
			i += 2
		case b1>>4 == 0xe: // E0..EF: 3 bytes
			rem := n - i
			if rem < 3 {
				if rem > 1 && isMalformed3_2(b1, b[i+1]) {
					repl()
					i++
					continue
				}
				repl() // underflow at the end of input: one replacement for the rest
				i = n
				continue
			}
			b2, b3 := b[i+1], b[i+2]
			if (b1 == 0xe0 && b2&0xe0 == 0x80) || !isCont(b2) || !isCont(b3) {
				l := 2
				if (b1 == 0xe0 && b2&0xe0 == 0x80) || !isCont(b2) {
					l = 1
				}
				repl()
				i += l
				continue
			}
			c := rune(b1&0x0f)<<12 | rune(b2&0x3f)<<6 | rune(b3&0x3f)
			if c >= 0xd800 && c <= 0xdfff {
				repl()
				i += 3
				continue
			}
			sb.WriteRune(c)
			i += 3
		case b1>>3 == 0x1e: // F0..F7: 4 bytes
			rem := n - i
			if rem < 4 {
				if b1 > 0xf4 || rem > 1 && isMalformed4_2(b1, b[i+1]) {
					repl()
					i++
					continue
				}
				if rem > 2 && !isCont(b[i+2]) {
					repl()
					i += 2
					continue
				}
				repl()
				i = n
				continue
			}
			b2, b3, b4 := b[i+1], b[i+2], b[i+3]
			uc := rune(b1&0x07)<<18 | rune(b2&0x3f)<<12 | rune(b3&0x3f)<<6 | rune(b4&0x3f)
			if !isCont(b2) || !isCont(b3) || !isCont(b4) || uc < 0x10000 || uc > 0x10ffff {
				l := 3
				if b1 > 0xf4 || (b1 == 0xf0 && (b2 < 0x90 || b2 > 0xbf)) || (b1 == 0xf4 && b2&0xf0 != 0x80) || !isCont(b2) {
					l = 1
				} else if !isCont(b3) {
					l = 2
				}
				repl()
				i += l
				continue
			}
			sb.WriteRune(uc)
			i += 4
		default: // 80..BF, C0, C1, F8..FF
			repl()
			i++
		}
	}
	return sb.String()
}

func isCont(b byte) bool { return b&0xc0 == 0x80 }

func isMalformed3_2(b1, b2 byte) bool {
	return (b1 == 0xe0 && b2&0xe0 == 0x80) || !isCont(b2)
}

func isMalformed4_2(b1, b2 byte) bool {
	return (b1 == 0xf0 && (b2 < 0x90 || b2 > 0xbf)) || (b1 == 0xf4 && b2&0xf0 != 0x80) || !isCont(b2)
}

// Latin1 converts raw bytes the way Tomcat decodes header values
// (ISO-8859-1: every byte is the code point of the same value).
func Latin1(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			var sb strings.Builder
			sb.Grow(len(s) + 8)
			sb.WriteString(s[:i])
			for ; i < len(s); i++ {
				if c := s[i]; c < 0x80 {
					sb.WriteByte(c)
				} else {
					sb.WriteRune(rune(c))
				}
			}
			return sb.String()
		}
	}
	return s
}
