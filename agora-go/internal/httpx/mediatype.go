package httpx

import (
	"math"
	"strconv"
	"strings"

	"agora/internal/javacompat"
)

// mediaType is a Content-Type parsed like Spring's MediaType.parseMediaType.
type mediaType struct {
	typ, sub string // lower-cased
	// charset is the canonical JVM name of the "charset" parameter (exact,
	// lower-case parameter name, like MimeType.checkParameters), "" if none.
	charset string
}

func (m mediaType) wildcardType() bool { return m.typ == "*" }
func (m mediaType) wildcardSubtype() bool {
	return m.sub == "*" || strings.HasPrefix(m.sub, "*+")
}

// includedBy reproduces supported.includes(m) for a concrete-type supported
// media type such as application/json or application/*+json.
func (m mediaType) includedBy(typ, sub string) bool {
	if typ != m.typ {
		return false
	}
	if sub == m.sub {
		return true
	}
	if sub == "*" || strings.HasPrefix(sub, "*+") {
		plus := strings.LastIndexByte(sub, '+')
		if plus == -1 {
			return true
		}
		if op := strings.LastIndexByte(m.sub, '+'); op != -1 {
			return sub[plus+1:] == m.sub[op+1:] && sub[:plus] == "*"
		}
	}
	return false
}

// readableJSON / readableXML: MappingJackson2HttpMessageConverter /
// MappingJackson2XmlHttpMessageConverter canRead(mediaType).
func (m mediaType) readableJSON() bool {
	return m.includedBy("application", "json") || m.includedBy("application", "*+json")
}

func (m mediaType) readableXML() bool {
	return m.includedBy("application", "xml") || m.includedBy("text", "xml") || m.includedBy("application", "*+xml")
}

// parseSpringMediaType reproduces MimeTypeUtils.parseMimeType + the MimeType
// and MediaType constructors' checks; ok=false where Spring throws
// InvalidMediaTypeException. s must not be empty.
func parseSpringMediaType(s string) (mediaType, bool) {
	var m mediaType
	semi := strings.IndexByte(s, ';')
	full := s
	if semi >= 0 {
		full = s[:semi]
	}
	full = javaTrim(full)
	if full == "" {
		return m, false
	}
	if full == "*" {
		full = "*/*"
	}
	slash := strings.IndexByte(full, '/')
	if slash == -1 || slash == len(full)-1 {
		return m, false
	}
	typ, sub := full[:slash], full[slash+1:]
	if typ == "*" && sub != "*" {
		return m, false
	}
	// parameters: split on ';' outside double quotes; "name=value" pairs,
	// a parameter without '=' is ignored. The parse map is a LinkedHashMap:
	// a repeated name keeps its first position and its last value.
	type kv struct{ k, v string }
	var params []kv
	index := semi
	for {
		next := index + 1
		quoted := false
		for next < len(s) {
			ch := s[next]
			if ch == ';' {
				if !quoted {
					break
				}
			} else if ch == '"' {
				quoted = !quoted
			}
			next++
		}
		if next > len(s) {
			next = len(s)
		}
		p := javaTrim(s[min(index+1, len(s)):next])
		if p != "" {
			if eq := strings.IndexByte(p, '='); eq >= 0 {
				k, v := javaTrim(p[:eq]), javaTrim(p[eq+1:])
				replaced := false
				for i := range params {
					if params[i].k == k {
						params[i].v = v
						replaced = true
						break
					}
				}
				if !replaced {
					params = append(params, kv{k, v})
				}
			}
		}
		index = next
		if index >= len(s) {
			break
		}
	}
	if typ == "" || sub == "" || !isToken(typ) || !isToken(sub) {
		return m, false
	}
	m.typ, m.sub = strings.ToLower(typ), strings.ToLower(sub)
	for _, p := range params {
		if p.k == "" || p.v == "" || !isToken(p.k) {
			return m, false
		}
		if p.k == "charset" {
			if m.charset == "" {
				c, ok := javacompat.LookupCharset(unquote(p.v))
				if !ok {
					return m, false
				}
				m.charset = c
			}
		} else if !isQuotedString(p.v) && !isToken(p.v) {
			return m, false
		}
		if p.k == "q" {
			q, ok := javaParseDouble(unquote(p.v))
			if !ok || !(q >= 0 && q <= 1) {
				return m, false
			}
		}
	}
	return m, true
}

// javaTrim is String.trim(): strips every char <= ' ' at both ends.
func javaTrim(s string) string {
	i, j := 0, len(s)
	for i < j && s[i] <= ' ' {
		i++
	}
	for j > i && s[j-1] <= ' ' {
		j--
	}
	return s[i:j]
}

// isToken reproduces MimeType.checkToken (RFC 2616 token, ASCII only).
func isToken(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 31 || c >= 127 {
			return false
		}
		switch c {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
			return false
		}
	}
	return true
}

func isQuotedString(s string) bool {
	if len(s) < 2 {
		return false
	}
	return s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\''
}

func unquote(s string) string {
	if isQuotedString(s) {
		return s[1 : len(s)-1]
	}
	return s
}

// javaParseDouble reproduces Double.parseDouble (decimal and hexadecimal
// forms, "NaN", "Infinity", f/F/d/D suffix, surrounding whitespace).
func javaParseDouble(s string) (float64, bool) {
	s = javaTrim(s)
	if s == "" {
		return 0, false
	}
	body := s
	sign := 1.0
	if body[0] == '+' || body[0] == '-' {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	switch body {
	case "NaN":
		return math.NaN(), true
	case "Infinity":
		return math.Inf(int(sign)), true
	}
	if n := len(body); n > 0 && strings.IndexByte("fFdD", body[n-1]) >= 0 {
		body = body[:n-1]
	}
	if body == "" || strings.ContainsAny(body, "_+-") && !strings.ContainsAny(body, "eEpP") {
		return 0, false
	}
	lower := strings.ToLower(body)
	if strings.HasPrefix(lower, "0x") {
		if !strings.Contains(lower, "p") {
			return 0, false // Java requires the binary exponent
		}
	} else if strings.ContainsAny(lower, "abcdfghijklmnopqrstuvwxyz") && !onlyExponent(lower) {
		return 0, false
	}
	if strings.Contains(body, "_") {
		return 0, false
	}
	f, err := strconv.ParseFloat(body, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return sign * f, true
		}
		return 0, false
	}
	return sign * f, true
}

// onlyExponent reports whether the only letter of a decimal literal is one 'e'.
func onlyExponent(s string) bool {
	n := 0
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'a' && c <= 'z' {
			if c != 'e' {
				return false
			}
			n++
		}
	}
	return n == 1
}
