package jsonjava

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// decoder is a streaming JSON reader with the rules of Jackson 2.14's
// UTF8StreamJsonParser (the parser Spring MVC runs on request bodies):
//
//   - strict JSON grammar (no comments, no single quotes, no trailing commas,
//     no leading zeros, no NaN), whitespace = space, tab, LF, CR;
//   - only the first value is read: whatever follows it is never looked at;
//   - strings are decoded with Jackson's loose UTF-8 decoder: a lead byte
//     C0..DF / E0..EF / F0..F7 takes 1 / 2 / 3 continuation bytes, only the
//     "10xxxxxx" shape of continuation bytes is checked (overlong forms and
//     encoded surrogates are accepted), 80..BF and F8..FF as lead bytes and
//     raw control characters are errors. Skipped values are validated the
//     same way (Jackson's _skipString);
//   - the result is a Java string (UTF-16): an unpaired surrogate (escape
//     "\ud800", encoded surrogate, F4 90.. beyond U+10FFFF) becomes '?',
//     which is what the JDBC driver stores for it (class C, DIVERGENCES.md).
//
// Input that is not UTF-8 is converted to UTF-8 by the caller first (see
// UnmarshalRequest); valid UTF-8 decodes the same under the loose rules.
type decoder struct {
	b     []byte
	i     int
	depth int // open containers (a root-level number must be followed by whitespace)
	// chars: the input was decoded to characters first (UTF-16/32, other
	// charsets, String input): Jackson's ReaderBasedJsonParser rules apply
	// where they differ from the UTF-8 byte parser's.
	chars bool
}

func (d *decoder) errorf(format string, a ...any) error {
	return fail("JsonParseException at %d: "+format, append([]any{d.i}, a...)...)
}

// ws skips JSON whitespace and returns the next byte (0, false at EOF).
func (d *decoder) ws() (byte, bool) {
	for d.i < len(d.b) {
		switch c := d.b[d.i]; c {
		case ' ', '\t', '\n', '\r':
			d.i++
		default:
			return c, true
		}
	}
	return 0, false
}

// next returns the first byte of the next value (after whitespace), or an
// error at EOF or on a byte that cannot start a value.
func (d *decoder) next() (byte, error) {
	c, ok := d.ws()
	if !ok {
		return 0, d.errorf("unexpected end-of-input")
	}
	switch c {
	case '{', '[', '"', '-', 't', 'f', 'n':
		return c, nil
	}
	if c >= '0' && c <= '9' {
		return c, nil
	}
	return 0, d.errorf("unexpected character %q", c)
}

// --- containers -------------------------------------------------------------

// beginObject consumes '{'.
func (d *decoder) beginObject() { d.i++; d.depth++ }

// firstMember reads the first member name of an object, right after '{':
// ok=false at the closing '}' (consumed). The value starts after the ':'.
func (d *decoder) firstMember() (string, bool, error) {
	c, has := d.ws()
	if !has {
		return "", false, d.errorf("unexpected end-of-input in object")
	}
	if c == '}' {
		d.i++
		d.depth--
		return "", false, nil
	}
	return d.memberName()
}

func (d *decoder) memberName() (string, bool, error) {
	c, has := d.ws()
	if !has || c != '"' {
		return "", false, d.errorf("expected field name")
	}
	name, err := d.str(true)
	if err != nil {
		return "", false, err
	}
	c, has = d.ws()
	if !has || c != ':' {
		return "", false, d.errorf("expected ':'")
	}
	d.i++
	return name, true, nil
}

// afterMember reads ',' (then the next name) or '}' after a member value.
func (d *decoder) afterMember() (string, bool, error) {
	c, has := d.ws()
	if !has {
		return "", false, d.errorf("unexpected end-of-input in object")
	}
	switch c {
	case '}':
		d.i++
		d.depth--
		return "", false, nil
	case ',':
		d.i++
		return d.memberName()
	}
	return "", false, d.errorf("unexpected character %q in object", c)
}

// beginArray consumes '['.
func (d *decoder) beginArray() { d.i++; d.depth++ }

// element reports whether another array element follows ("first" right
// after '['), consuming ',' or the closing ']'.
func (d *decoder) element(first bool) (bool, error) {
	c, has := d.ws()
	if !has {
		return false, d.errorf("unexpected end-of-input in array")
	}
	if c == ']' {
		d.i++
		d.depth--
		return false, nil
	}
	if first {
		return true, nil
	}
	if c != ',' {
		return false, d.errorf("unexpected character %q in array", c)
	}
	d.i++
	c, has = d.ws()
	if has && c == ']' {
		return false, d.errorf("trailing comma")
	}
	return true, nil
}

// --- scalars ----------------------------------------------------------------

// scalar reads a string, number, true, false or null starting at c.
func (d *decoder) scalar(c byte) (any, error) {
	switch c {
	case '"':
		return d.str(false)
	case 't':
		return true, d.literal("true")
	case 'f':
		return false, d.literal("false")
	case 'n':
		return nil, d.literal("null")
	}
	return d.number()
}

// literal matches true/false/null; like Jackson's _matchToken, a following
// character that is a Java identifier part makes the token invalid.
func (d *decoder) literal(lit string) error {
	if !strings.HasPrefix(string(d.b[d.i:min(len(d.b), d.i+len(lit))]), lit) {
		return d.errorf("unrecognized token")
	}
	d.i += len(lit)
	if d.i < len(d.b) {
		if c := d.b[d.i]; c >= '0' && c != ']' && c != '}' {
			if d.chars {
				r, _ := utf8.DecodeRune(d.b[d.i:])
				if isJavaIdentifierPart(r) {
					return d.errorf("unrecognized token")
				}
			} else if c >= 0x80 || isJavaIdentifierPart(rune(c)) {
				// UTF8StreamJsonParser: any non-ASCII byte there is an error
				return d.errorf("unrecognized token")
			}
		}
	}
	return nil
}

// isJavaIdentifierPart approximates Character.isJavaIdentifierPart(char).
func isJavaIdentifierPart(r rune) bool {
	switch {
	case r == '$' || r == '_':
		return true
	case r < 0x80:
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r <= 8 || r >= 0x0e && r <= 0x1b || r == 0x7f
	case r >= 0x7f && r <= 0x9f:
		return true // identifier-ignorable controls
	}
	return unicode.In(r, unicode.L, unicode.Nd, unicode.Nl, unicode.Mn, unicode.Mc, unicode.Pc, unicode.Sc, unicode.Cf)
}

// number validates Jackson's number grammar and returns its text.
func (d *decoder) number() (json.Number, error) {
	start := d.i
	if d.b[d.i] == '-' {
		d.i++
	}
	digits := func() int {
		n := 0
		for d.i < len(d.b) && d.b[d.i] >= '0' && d.b[d.i] <= '9' {
			d.i++
			n++
		}
		return n
	}
	intStart := d.i
	if digits() == 0 {
		return "", d.errorf("expected digit")
	}
	if d.b[intStart] == '0' && d.i-intStart > 1 {
		return "", d.errorf("leading zeroes not allowed")
	}
	if d.i < len(d.b) && d.b[d.i] == '.' {
		d.i++
		if digits() == 0 {
			return "", d.errorf("decimal point not followed by a digit")
		}
	}
	if d.i < len(d.b) && (d.b[d.i] == 'e' || d.b[d.i] == 'E') {
		d.i++
		if d.i < len(d.b) && (d.b[d.i] == '+' || d.b[d.i] == '-') {
			d.i++
		}
		if digits() == 0 {
			return "", d.errorf("exponent not followed by a digit")
		}
	}
	if d.depth == 0 && d.i < len(d.b) {
		// Jackson's _verifyRootSpace
		switch d.b[d.i] {
		case ' ', '\t', '\r', '\n':
		default:
			return "", d.errorf("expected space separating root-level values")
		}
	}
	return json.Number(d.b[start:d.i]), nil
}

// str reads a string (d.b[d.i] == '"'). Names (name=true) decode 4-byte
// sequences like Jackson's addName ("0xD800 + (c >> 10)"), values like
// _finishString ("0xD800 | (c >> 10)"); they differ only for the invalid
// forms Jackson accepts.
func (d *decoder) str(name bool) (string, error) {
	d.i++
	start := d.i
	// fast path: printable ASCII without escapes
	for d.i < len(d.b) {
		c := d.b[d.i]
		if c == '"' {
			s := string(d.b[start:d.i])
			d.i++
			return s, nil
		}
		if c < 0x20 || c == '\\' || c >= 0x80 {
			break
		}
		d.i++
	}
	var w utf16Writer
	w.sb.Grow(d.i - start + 16)
	w.sb.Write(d.b[start:d.i])
	for {
		if d.i >= len(d.b) {
			return "", d.errorf("unexpected end-of-input in string")
		}
		c := d.b[d.i]
		switch {
		case c == '"':
			d.i++
			return w.finish(), nil
		case c == '\\':
			d.i++
			u, err := d.escape()
			if err != nil {
				return "", err
			}
			w.unit(u)
		case c < 0x20:
			return "", d.errorf("illegal unquoted character (CTRL-CHAR, code %d)", c)
		case c < 0x80:
			w.unit(uint16(c))
			d.i++
		case c&0xe0 == 0xc0:
			d2, err := d.cont(1)
			if err != nil {
				return "", err
			}
			w.unit(uint16(c&0x1f)<<6 | uint16(d2&0x3f))
			d.i += 2
		case c&0xf0 == 0xe0:
			if _, err := d.cont(1); err != nil {
				return "", err
			}
			if _, err := d.cont(2); err != nil {
				return "", err
			}
			w.unit(uint16(c&0x0f)<<12 | uint16(d.b[d.i+1]&0x3f)<<6 | uint16(d.b[d.i+2]&0x3f))
			d.i += 3
		case c&0xf8 == 0xf0:
			for k := 1; k <= 3; k++ {
				if _, err := d.cont(k); err != nil {
					return "", err
				}
			}
			v := (int32(c&0x07)<<18 | int32(d.b[d.i+1]&0x3f)<<12 | int32(d.b[d.i+2]&0x3f)<<6 | int32(d.b[d.i+3]&0x3f)) - 0x10000
			if name {
				w.unit(uint16(0xd800 + (v >> 10)))
			} else {
				w.unit(uint16(0xd800 | (v >> 10)))
			}
			w.unit(uint16(0xdc00 | (v & 0x3ff)))
			d.i += 4
		default:
			return "", d.errorf("invalid UTF-8 start byte 0x%02x", c)
		}
	}
}

// cont checks the k-th continuation byte after d.i.
func (d *decoder) cont(k int) (byte, error) {
	if d.i+k >= len(d.b) {
		return 0, d.errorf("unexpected end-of-input in string")
	}
	b := d.b[d.i+k]
	if b&0xc0 != 0x80 {
		return 0, d.errorf("invalid UTF-8 middle byte 0x%02x", b)
	}
	return b, nil
}

// escape decodes the escape after a backslash (d.i is after the '\').
func (d *decoder) escape() (uint16, error) {
	if d.i >= len(d.b) {
		return 0, d.errorf("unexpected end-of-input in escape")
	}
	c := d.b[d.i]
	d.i++
	switch c {
	case '"', '\\', '/':
		return uint16(c), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		if d.i+4 > len(d.b) {
			return 0, d.errorf("unexpected end-of-input in \\u escape")
		}
		var v uint16
		for k := 0; k < 4; k++ {
			h := d.b[d.i+k]
			var x byte
			switch {
			case h >= '0' && h <= '9':
				x = h - '0'
			case h >= 'a' && h <= 'f':
				x = h - 'a' + 10
			case h >= 'A' && h <= 'F':
				x = h - 'A' + 10
			default:
				return 0, d.errorf("invalid hex digit in \\u escape")
			}
			v = v<<4 | uint16(x)
		}
		d.i += 4
		return v, nil
	}
	return 0, d.errorf("unrecognized character escape %q", c)
}

// skip validates and skips one value (iteratively: no recursion on nesting).
func (d *decoder) skip() error {
	var stack []byte // '{' or '[' of the open containers
	for {
		c, err := d.next()
		if err != nil {
			return err
		}
		switch c {
		case '{':
			d.beginObject()
			_, ok, err := d.firstMember()
			if err != nil {
				return err
			}
			if ok {
				stack = append(stack, '{')
				continue
			}
		case '[':
			d.beginArray()
			ok, err := d.element(true)
			if err != nil {
				return err
			}
			if ok {
				stack = append(stack, '[')
				continue
			}
		default:
			if _, err := d.scalar(c); err != nil {
				return err
			}
		}
		// a value ended: move to the next one, closing the containers it completes
		for {
			if len(stack) == 0 {
				return nil
			}
			var more bool
			var err error
			if stack[len(stack)-1] == '{' {
				_, more, err = d.afterMember()
			} else {
				more, err = d.element(false)
			}
			if err != nil {
				return err
			}
			if more {
				break
			}
			stack = stack[:len(stack)-1]
		}
	}
}

// tree builds a generic value (map[string]any with the last duplicate
// winning, []any, string, json.Number, bool, nil) for TreeUnmarshaler and
// interface{} targets.
func (d *decoder) tree(depth int) (any, error) {
	if depth > 10000 {
		return nil, d.errorf("document too deeply nested")
	}
	c, err := d.next()
	if err != nil {
		return nil, err
	}
	switch c {
	case '{':
		d.beginObject()
		m := map[string]any{}
		name, ok, err := d.firstMember()
		for ; err == nil && ok; name, ok, err = d.afterMember() {
			v, err := d.tree(depth + 1)
			if err != nil {
				return nil, err
			}
			m[name] = v
		}
		if err != nil {
			return nil, err
		}
		return m, nil
	case '[':
		d.beginArray()
		arr := []any{}
		first := true
		for {
			ok, err := d.element(first)
			if err != nil {
				return nil, err
			}
			if !ok {
				return arr, nil
			}
			first = false
			v, err := d.tree(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
	}
	return d.scalar(c)
}

// utf16Writer accumulates UTF-16 units into a Go string; an unpaired
// surrogate becomes '?'.
type utf16Writer struct {
	sb   strings.Builder
	high uint16 // pending high surrogate, 0 if none
}

func (w *utf16Writer) unit(u uint16) {
	if w.high != 0 {
		if u >= 0xdc00 && u <= 0xdfff {
			w.sb.WriteRune(rune(w.high-0xd800)<<10 + rune(u-0xdc00) + 0x10000)
			w.high = 0
			return
		}
		w.sb.WriteByte('?')
		w.high = 0
	}
	switch {
	case u < 0x80:
		w.sb.WriteByte(byte(u))
	case u >= 0xd800 && u <= 0xdbff:
		w.high = u
	case u >= 0xdc00 && u <= 0xdfff:
		w.sb.WriteByte('?')
	default:
		var buf [3]byte
		n := utf8.EncodeRune(buf[:], rune(u))
		w.sb.Write(buf[:n])
	}
}

func (w *utf16Writer) finish() string {
	if w.high != 0 {
		w.sb.WriteByte('?')
		w.high = 0
	}
	return w.sb.String()
}
