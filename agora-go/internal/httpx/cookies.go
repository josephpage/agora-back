package httpx

import (
	"net/http"

	"agora/internal/javacompat"
)

// tomcatMaxCookieCount is the Connector's maxCookieCount (default 200).
const tomcatMaxCookieCount = 200

// tomcatCookie is a ServerCookie: name and value bytes as found in the header.
type tomcatCookie struct{ name, value string }

// tomcatCookies parses every Cookie header in order the way Tomcat 10.1.16 does
// (Rfc6265CookieProcessor.parseCookieHeader → parser.Cookie.parseCookie). ok is
// false when a 201st cookie is found: ServerCookies.addCookie throws and
// CoyoteAdapter.postParseRequest answers sendError(400) before any filter.
// Every parsed name is an HTTP token, so Request.convertCookies keeps them all
// (the servlet Cookie name validation accepts tokens, and the values cannot
// hold the '\' that unescape() would reject).
func tomcatCookies(r *http.Request) (cookies []tomcatCookie, ok bool) {
	for _, h := range r.Header["Cookie"] {
		if cookies, ok = parseTomcatCookieHeader(h, cookies); !ok {
			return cookies, false
		}
	}
	return cookies, true
}

// tomcatCookieValue is the value of the first cookie with this name, as the
// String of Cookie.getValue() (bytes decoded as UTF-8, the processor's charset).
func tomcatCookieValue(cookies []tomcatCookie, name string) *string {
	for _, c := range cookies {
		if c.name == name {
			v := javacompat.DecodeUTF8Java([]byte(c.value))
			return &v
		}
	}
	return nil
}

// parseTomcatCookieHeader is parser.Cookie.parseCookie on one header value.
func parseTomcatCookieHeader(b string, out []tomcatCookie) ([]tomcatCookie, bool) {
	pos, limit := 0, len(b)
	skipLWS := func() {
		for pos < limit && (b[pos] == '\t' || b[pos] == ' ') {
			pos++
		}
	}
	skipUntilSemiColon := func() {
		for pos < limit {
			c := b[pos]
			pos++
			if c == ';' {
				break
			}
		}
	}
	// readCookieValueRfc6265: an optional opening quote, cookie octets up to ';', a
	// space, a tab or the closing quote; nil (false) on any other byte
	readValue := func() (string, bool) {
		quoted := false
		if pos < limit && b[pos] == '"' {
			quoted = true
			pos++
		}
		start, end := pos, limit
		for pos < limit {
			c := b[pos]
			pos++
			switch {
			case isCookieOctet(c):
			case c == ';' || c == ' ' || c == '\t':
				end = pos - 1
				pos = end
				return b[start:end], true
			case quoted && c == '"':
				end = pos - 1
				return b[start:end], true
			default:
				return "", false
			}
		}
		return b[start:end], true
	}
	for more := true; more; {
		skipLWS()
		start := pos
		for pos < limit && isHTTPTokenByte(b[pos]) {
			pos++
		}
		name := b[start:pos]
		value := ""
		skipLWS()
		if pos < limit && b[pos] == '=' {
			pos++
			skipLWS()
			v, ok := readValue()
			if !ok {
				// invalid cookie value: skip to the next semicolon
				skipUntilSemiColon()
				continue
			}
			value = v
			skipLWS()
		}
		switch {
		case pos >= limit:
			more = false
		case b[pos] == ';':
			pos++
		default:
			// invalid cookie: ignore it and skip to the next semicolon
			skipUntilSemiColon()
			continue
		}
		if name != "" {
			if len(out) >= tomcatMaxCookieCount {
				return out, false
			}
			out = append(out, tomcatCookie{name: name, value: value})
		}
	}
	return out, true
}

// isCookieOctet: %x21 / %x23-2B / %x2D-3A / %x3C-5B / %x5D-7E and %x80-FF.
func isCookieOctet(c byte) bool {
	return c >= 0x21 && c != '"' && c != ',' && c != ';' && c != '\\' && c != 0x7f
}

// isHTTPTokenByte is HttpParser.isToken on a (signed) byte: ASCII, not a control,
// not a separator.
func isHTTPTokenByte(c byte) bool {
	if c < 32 || c >= 127 {
		return false
	}
	switch c {
	case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
		return false
	}
	return true
}
