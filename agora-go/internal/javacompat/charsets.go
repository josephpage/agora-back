package javacompat

import "strings"

// LookupCharset reproduces Charset.forName: the canonical JVM name of a
// charset name or alias (case-insensitive). ok is false when Java throws
// (illegal or unsupported name).
func LookupCharset(name string) (string, bool) {
	c, ok := javaCharsetNames[strings.ToLower(name)]
	return c, ok
}

// JacksonUnicode reports whether Spring's AbstractJackson2HttpMessageConverter
// hands the raw bytes to Jackson (encoding auto-detection) for this canonical
// charset, instead of decoding them with an InputStreamReader first.
func JacksonUnicode(canonical string) bool {
	switch canonical {
	case "UTF-8", "UTF-16BE", "UTF-16LE", "UTF-32BE", "UTF-32LE", "US-ASCII", "UTF-16", "UTF-32":
		return true
	}
	return false
}

// DecodeCharset decodes b with a single-byte JVM charset exactly like
// Charset.decode (unmappable bytes → U+FFFD). ok is false for the charsets Go
// does not reproduce (multi-byte legacy charsets).
func DecodeCharset(canonical string, b []byte) (string, bool) {
	tab := javaSingleByte[canonical]
	if tab == nil {
		return "", false
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		u := tab[c]
		if u < 0x80 {
			sb.WriteByte(byte(u))
		} else {
			sb.WriteRune(rune(u))
		}
	}
	return sb.String(), true
}
