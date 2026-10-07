package jsonjava

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"agora/internal/javacompat"
)

// fieldIndex caches the JSON properties of a struct type.
type fieldIndex struct {
	fields []dfield
	byName map[string]int
}

var decodeFieldCache sync.Map // reflect.Type → *fieldIndex

func fieldsOf(t reflect.Type) *fieldIndex {
	if fi, ok := decodeFieldCache.Load(t); ok {
		return fi.(*fieldIndex)
	}
	fs := decodeFields(t)
	fi := &fieldIndex{fields: fs, byName: make(map[string]int, len(fs))}
	for i, f := range fs {
		if _, dup := fi.byName[f.name]; !dup {
			fi.byName[f.name] = i
		}
	}
	actual, _ := decodeFieldCache.LoadOrStore(t, fi)
	return actual.(*fieldIndex)
}

// bind decodes the next value of the stream into v, with the semantics of
// bind (the tree binder) and of Jackson's BeanDeserializer for Kotlin data
// classes: properties are read in document order (a duplicate is bound again
// and the last one wins, but an invalid earlier one is still an error),
// unknown properties are skipped (and validated), and the missing / null
// checks of non-null properties run once the object is complete
// (KotlinValueInstantiator).
func (d *decoder) bind(v reflect.Value, path string) error {
	if v.CanAddr() && v.Addr().Type().Implements(unmarshalerType) {
		t, err := d.tree(0)
		if err != nil {
			return err
		}
		return v.Addr().Interface().(TreeUnmarshaler).UnmarshalJavaTree(t)
	}
	c, err := d.next()
	if err != nil {
		return err
	}
	switch v.Kind() {
	case reflect.Pointer:
		if c == 'n' {
			if err := d.literal("null"); err != nil {
				return err
			}
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		nv := reflect.New(v.Type().Elem())
		if err := d.bind(nv.Elem(), path); err != nil {
			return err
		}
		v.Set(nv)
		return nil
	case reflect.Interface:
		t, err := d.tree(0)
		if err != nil {
			return err
		}
		if t == nil {
			v.Set(reflect.Zero(v.Type()))
		} else {
			v.Set(reflect.ValueOf(t))
		}
		return nil
	case reflect.Struct:
		if c != '{' {
			return fail("%s: expected object", path)
		}
		return d.bindStruct(v, path)
	case reflect.Slice:
		if c != '[' {
			return fail("%s: expected array", path)
		}
		d.beginArray()
		s := reflect.MakeSlice(v.Type(), 0, 4)
		for first := true; ; first = false {
			ok, err := d.element(first)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			ev := reflect.New(v.Type().Elem()).Elem()
			c, err := d.next()
			if err != nil {
				return err
			}
			if c == 'n' {
				// a null element (Kotlin List<String> through type erasure): zero value
				if err := d.literal("null"); err != nil {
					return err
				}
			} else if err := d.bind(ev, fmt.Sprintf("%s[%d]", path, s.Len())); err != nil {
				return err
			}
			s = reflect.Append(s, ev)
		}
		v.Set(s)
		return nil
	case reflect.Map:
		if c != '{' {
			return fail("%s: expected object", path)
		}
		d.beginObject()
		m := reflect.MakeMap(v.Type())
		name, ok, err := d.firstMember()
		for ; err == nil && ok; name, ok, err = d.afterMember() {
			ev := reflect.New(v.Type().Elem()).Elem()
			c, err := d.next()
			if err != nil {
				return err
			}
			if c == 'n' {
				if err := d.literal("null"); err != nil {
					return err
				}
			} else if err := d.bind(ev, path+"."+name); err != nil {
				return err
			}
			m.SetMapIndex(reflect.ValueOf(name), ev)
		}
		if err != nil {
			return err
		}
		v.Set(m)
		return nil
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Float32, reflect.Float64:
		if c == '{' || c == '[' {
			return fail("%s: cannot coerce a container to %s", path, v.Kind())
		}
		t, err := d.scalar(c)
		if err != nil {
			return err
		}
		if t == nil {
			// null scalar outside a property (array element / map value / root)
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		return bindScalar(v, t, path)
	}
	return fail("%s: unsupported kind %s", path, v.Kind())
}

const (
	propMissing byte = iota
	propSet
	propNull
)

func (d *decoder) bindStruct(v reflect.Value, path string) error {
	fi := fieldsOf(v.Type())
	state := make([]byte, len(fi.fields))
	d.beginObject()
	name, ok, err := d.firstMember()
	for ; err == nil && ok; name, ok, err = d.afterMember() {
		idx, known := fi.byName[name]
		if !known {
			if err := d.skip(); err != nil {
				return err
			}
			continue
		}
		f := fi.fields[idx]
		c, err := d.next()
		if err != nil {
			return err
		}
		if c == 'n' {
			if err := d.literal("null"); err != nil {
				return err
			}
			state[idx] = propNull
			continue
		}
		if err := d.bind(v.Field(f.index), path+"."+f.name); err != nil {
			return err
		}
		state[idx] = propSet
	}
	if err != nil {
		return err
	}
	for idx, f := range fi.fields {
		fv := v.Field(f.index)
		switch state[idx] {
		case propMissing:
			if f.def || isNullableOrPrimitive(fv.Kind()) {
				continue
			}
			return fail("%s.%s: missing required property", path, f.name)
		case propNull:
			switch fv.Kind() {
			case reflect.Pointer, reflect.Interface,
				reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.Bool, reflect.Float32, reflect.Float64:
				fv.Set(reflect.Zero(fv.Type()))
				continue
			}
			return fail("%s.%s: null for non-null property", path, f.name)
		}
	}
	return nil
}

// ErrUnsupportedCharset reports a request charset the JVM knows but Go does
// not decode (multi-byte legacy charsets: Shift_JIS, GBK, ISO-2022-*...).
var ErrUnsupportedCharset = errors.New("jsonjava: request charset not supported")

// UnmarshalRequest decodes an HTTP request body like Spring MVC's
// MappingJackson2HttpMessageConverter. charset is the canonical Java name of
// the Content-Type charset ("UTF-8" when absent). For the Unicode charsets
// (UTF-8/16/32, US-ASCII) Spring hands the raw bytes to Jackson, which detects
// the actual encoding from a BOM or from zero bytes, whatever was declared;
// any other charset is decoded first (Java decoder, malformed → U+FFFD).
func UnmarshalRequest(body []byte, charset string, v any) error {
	text, chars, err := requestText(body, charset)
	if err != nil {
		return err
	}
	return unmarshalDecoder(&decoder{b: text, chars: chars}, v)
}

// requestText returns the bytes the parser reads, and whether they were
// decoded to characters first (Jackson's char-based parser) or are the raw
// UTF-8 request bytes (UTF8StreamJsonParser).
func requestText(body []byte, charset string) ([]byte, bool, error) {
	if !javacompat.JacksonUnicode(charset) {
		s, ok := javacompat.DecodeCharset(charset, body)
		if !ok {
			return nil, true, ErrUnsupportedCharset
		}
		return []byte(s), true, nil
	}
	switch enc, skip := detectEncoding(body); enc {
	case "UTF-16BE", "UTF-16LE":
		return decodeUTF16Java(body[skip:], enc == "UTF-16BE"), true, nil
	case "UTF-32BE", "UTF-32LE":
		b, err := decodeUTF32Jackson(body[skip:], enc == "UTF-32BE")
		return b, true, err
	case "UCS-4-odd":
		return nil, true, fail("unsupported UCS-4 endianness")
	default:
		return body[skip:], false, nil
	}
}

// detectEncoding reproduces Jackson's ByteSourceJsonBootstrapper.detectEncoding:
// BOM first, then the position of zero bytes in the first 4 (or 2) bytes.
func detectEncoding(b []byte) (string, int) {
	if len(b) >= 4 {
		quad := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		switch quad {
		case 0x0000feff:
			return "UTF-32BE", 4
		case 0xfffe0000:
			return "UTF-32LE", 4
		}
		switch quad >> 16 {
		case 0xfeff:
			return "UTF-16BE", 2
		case 0xfffe:
			return "UTF-16LE", 2
		}
		if quad>>8 == 0xefbbbf {
			return "UTF-8", 3
		}
		switch {
		case quad>>8 == 0:
			return "UTF-32BE", 0
		case quad&0x00ffffff == 0:
			return "UTF-32LE", 0
		case quad&^0x00ff0000 == 0, quad&^0x0000ff00 == 0:
			// UCS-4 "3412" / "2143": Jackson rejects them
			return "UCS-4-odd", 0
		}
		return utf16ByZeros(uint16(quad >> 16))
	}
	if len(b) >= 2 {
		return utf16ByZeros(uint16(b[0])<<8 | uint16(b[1]))
	}
	return "UTF-8", 0
}

func utf16ByZeros(i16 uint16) (string, int) {
	switch {
	case i16&0xff00 == 0:
		return "UTF-16BE", 0
	case i16&0x00ff == 0:
		return "UTF-16LE", 0
	}
	return "UTF-8", 0
}

// decodeUTF16Java reproduces the JDK's UTF-16BE/LE decoder with REPLACE
// (InputStreamReader): an unpaired high surrogate swallows the next unit
// (malformed length 4), an unpaired low surrogate and U+FFFE are malformed,
// and an odd trailing byte becomes U+FFFD.
func decodeUTF16Java(b []byte, be bool) []byte {
	unit := func(i int) uint16 {
		if be {
			return uint16(b[i])<<8 | uint16(b[i+1])
		}
		return uint16(b[i+1])<<8 | uint16(b[i])
	}
	var sb strings.Builder
	sb.Grow(len(b) / 2)
	i := 0
	for i+1 < len(b) {
		c := unit(i)
		switch {
		case c == 0xfffe:
			sb.WriteRune(utf8.RuneError)
			i += 2
		case c >= 0xd800 && c <= 0xdbff:
			if i+3 >= len(b) {
				// lone high surrogate at the end (with or without an odd byte)
				sb.WriteRune(utf8.RuneError)
				i = len(b)
				continue
			}
			c2 := unit(i + 2)
			if c2 < 0xdc00 || c2 > 0xdfff {
				sb.WriteRune(utf8.RuneError)
				i += 4
				continue
			}
			sb.WriteRune(rune(c-0xd800)<<10 + rune(c2-0xdc00) + 0x10000)
			i += 4
		case c >= 0xdc00 && c <= 0xdfff:
			sb.WriteRune(utf8.RuneError)
			i += 2
		default:
			sb.WriteRune(rune(c))
			i += 2
		}
	}
	if i < len(b) {
		sb.WriteRune(utf8.RuneError)
	}
	return []byte(sb.String())
}

// decodeUTF32Jackson reproduces Jackson's UTF32Reader: a unit above U+10FFFF
// is an error (raised as soon as the reader converts it: it converts up to
// 4000 units ahead of the parser), surrogate code points pass through (→ '?'
// for us), and a truncated last unit only fails if the parser reaches it.
func decodeUTF32Jackson(b []byte, be bool) ([]byte, error) {
	var w utf16Writer
	for k, i := 0, 0; i+3 < len(b); k, i = k+1, i+4 {
		var hi, lo uint32
		if be {
			hi, lo = uint32(b[i])<<8|uint32(b[i+1]), uint32(b[i+2])<<8|uint32(b[i+3])
		} else {
			lo, hi = uint32(b[i])|uint32(b[i+1])<<8, uint32(b[i+2])|uint32(b[i+3])<<8
		}
		if hi != 0 {
			if hi > 0x10 {
				if k < 4000 {
					return nil, fail("invalid UTF-32 character above 0x10FFFF")
				}
				// beyond the reader's first chunk: an error only if the parser gets there
				w.unit(0)
				break
			}
			ch := (hi-1)<<16 | lo
			w.unit(uint16(0xd800 + (ch >> 10)))
			w.unit(uint16(0xdc00 | (ch & 0x3ff)))
			continue
		}
		w.unit(uint16(lo))
	}
	return []byte(w.finish()), nil
}
