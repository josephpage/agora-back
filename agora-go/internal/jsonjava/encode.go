// Package jsonjava encodes and decodes JSON exactly like the Jackson
// ObjectMapper used by Spring MVC in the Kotlin backend.
//
// Encoding rules (Jackson 2.14 defaults, UTF8JsonGenerator):
//   - struct fields are written in declaration order, named by the `json` tag;
//   - nil pointers / interfaces are written as null, unless the field carries
//     the `omitnull` option (@JsonInclude(NON_NULL) on the Kotlin DTO);
//   - nil slices are written as [] (Kotlin non-null List) unless the field
//     carries `nullable` (Kotlin List?) in which case they are written as null;
//   - strings: only '"', '\\' and control characters are escaped; \b \t \n \f \r
//     use short escapes, other control chars use \u00XX with UPPERCASE hex.
//     No HTML escaping, U+2028/U+2029 are written raw;
//   - floats follow java.lang.Double.toString.
package jsonjava

import (
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

// Marshaler lets a type write its own Jackson-compatible JSON.
type Marshaler interface {
	AppendJavaJSON(dst []byte) []byte
}

// Unit is Kotlin's Unit, serialized by Jackson as an empty object.
type Unit struct{}

// AppendJavaJSON implements Marshaler.
func (Unit) AppendJavaJSON(dst []byte) []byte { return append(dst, '{', '}') }

// Raw is pre-encoded JSON written verbatim.
type Raw []byte

// AppendJavaJSON implements Marshaler.
func (r Raw) AppendJavaJSON(dst []byte) []byte { return append(dst, r...) }

// Entry is one key/value of an OrderedMap.
type Entry struct {
	Key   string
	Value any
}

// OrderedMap is a Java LinkedHashMap: keys are written in slice order.
type OrderedMap []Entry

// AppendJavaJSON implements Marshaler.
func (m OrderedMap) AppendJavaJSON(dst []byte) []byte {
	dst = append(dst, '{')
	for i, e := range m {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = AppendString(dst, e.Key)
		dst = append(dst, ':')
		dst = appendValue(dst, reflect.ValueOf(e.Value))
	}
	return append(dst, '}')
}

// EpochMillis is a java.util.Date written as a timestamp (WRITE_DATES_AS_TIMESTAMPS).
type EpochMillis int64

// AppendJavaJSON implements Marshaler.
func (e EpochMillis) AppendJavaJSON(dst []byte) []byte { return strconv.AppendInt(dst, int64(e), 10) }

// Marshal returns the Jackson-compatible encoding of v.
func Marshal(v any) []byte {
	return Append(make([]byte, 0, 256), v)
}

// Append appends the Jackson-compatible encoding of v to dst.
func Append(dst []byte, v any) []byte {
	return appendValue(dst, reflect.ValueOf(v))
}

// MarshalString is a convenience returning a string.
func MarshalString(v any) string { return string(Marshal(v)) }

var marshalerType = reflect.TypeOf((*Marshaler)(nil)).Elem()

type fieldInfo struct {
	name     []byte // pre-encoded `"name":`
	index    int
	omitNull bool
	nullable bool
}

var fieldCache sync.Map // reflect.Type -> []fieldInfo

func structFields(t reflect.Type) []fieldInfo {
	if f, ok := fieldCache.Load(t); ok {
		return f.([]fieldInfo)
	}
	var fields []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := sf.Name
		opts := ""
		if tag != "" {
			if idx := strings.IndexByte(tag, ','); idx >= 0 {
				name, opts = tag[:idx], tag[idx+1:]
				if name == "" {
					name = sf.Name
				}
			} else {
				name = tag
			}
		}
		fi := fieldInfo{index: i}
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitnull", "omitempty":
				fi.omitNull = true
			case "nullable":
				fi.nullable = true
			}
		}
		enc := AppendString(nil, name)
		fi.name = append(enc, ':')
		fields = append(fields, fi)
	}
	fieldCache.Store(t, fields)
	return fields
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func appendValue(dst []byte, v reflect.Value) []byte {
	if !v.IsValid() {
		return append(dst, "null"...)
	}
	if v.Type().Implements(marshalerType) {
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
			return append(dst, "null"...)
		}
		return v.Interface().(Marshaler).AppendJavaJSON(dst)
	}
	if v.Kind() != reflect.Pointer && v.CanAddr() && reflect.PointerTo(v.Type()).Implements(marshalerType) {
		return v.Addr().Interface().(Marshaler).AppendJavaJSON(dst)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return append(dst, "null"...)
		}
		return appendValue(dst, v.Elem())
	case reflect.String:
		return AppendString(dst, v.String())
	case reflect.Bool:
		if v.Bool() {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(dst, v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.AppendUint(dst, v.Uint(), 10)
	case reflect.Float64:
		return AppendJavaDouble(dst, v.Float())
	case reflect.Float32:
		return AppendJavaFloat(dst, float32(v.Float()))
	case reflect.Struct:
		return appendStruct(dst, v)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// Jackson writes byte[] as base64.
			return AppendString(dst, base64.StdEncoding.EncodeToString(v.Bytes()))
		}
		fallthrough
	case reflect.Array:
		dst = append(dst, '[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendValue(dst, v.Index(i))
		}
		return append(dst, ']')
	case reflect.Map:
		// Only used for tests / ad-hoc data: keys sorted for determinism.
		// DTOs that need Java map ordering must use OrderedMap.
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendString(dst, fmt.Sprint(k.Interface()))
			dst = append(dst, ':')
			dst = appendValue(dst, v.MapIndex(k))
		}
		return append(dst, '}')
	}
	panic("jsonjava: unsupported kind " + v.Kind().String())
}

func appendStruct(dst []byte, v reflect.Value) []byte {
	fields := structFields(v.Type())
	dst = append(dst, '{')
	first := true
	for _, f := range fields {
		fv := v.Field(f.index)
		if isNil(fv) {
			if f.omitNull {
				continue
			}
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst = append(dst, f.name...)
			if fv.Kind() == reflect.Slice && !f.nullable {
				dst = append(dst, '[', ']')
			} else {
				dst = append(dst, "null"...)
			}
			continue
		}
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = append(dst, f.name...)
		dst = appendValue(dst, fv)
	}
	return append(dst, '}')
}

const hexUpper = "0123456789ABCDEF"

// AppendString appends s as a JSON string literal with Jackson escaping.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			if c < utf8.RuneSelf {
				i++
				continue
			}
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8 cannot come from a Java String; write U+FFFD.
				dst = append(dst, s[start:i]...)
				dst = append(dst, "�"...)
				i++
				start = i
				continue
			}
			if r >= 0x10000 {
				// Jackson 2.14 UTF8JsonGenerator escapes each UTF-16 surrogate
				// of a supplementary character: U+1F600 → 😀 (uppercase).
				dst = append(dst, s[start:i]...)
				hi, lo := utf16.EncodeRune(r)
				dst = appendUnicodeEscape(dst, uint16(hi))
				dst = appendUnicodeEscape(dst, uint16(lo))
				i += size
				start = i
				continue
			}
			i += size
			continue
		}
		dst = append(dst, s[start:i]...)
		switch c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\r':
			dst = append(dst, '\\', 'r')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexUpper[c>>4], hexUpper[c&0xf])
		}
		i++
		start = i
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

func appendUnicodeEscape(dst []byte, u uint16) []byte {
	return append(dst, '\\', 'u', hexUpper[u>>12&0xf], hexUpper[u>>8&0xf], hexUpper[u>>4&0xf], hexUpper[u&0xf])
}

// AppendJavaDouble appends d formatted like java.lang.Double.toString.
func AppendJavaDouble(dst []byte, d float64) []byte {
	return append(dst, JavaDoubleToString(d)...)
}

// AppendJavaFloat appends f formatted like java.lang.Float.toString.
func AppendJavaFloat(dst []byte, f float32) []byte {
	return append(dst, javaFloatingToString(float64(f), 32)...)
}

// JavaDoubleToString formats like java.lang.Double.toString (shortest
// round-trip digits, scientific notation outside [1e-3, 1e7)).
func JavaDoubleToString(d float64) string { return javaFloatingToString(d, 64) }

func javaFloatingToString(d float64, bits int) string {
	switch {
	case math.IsNaN(d):
		return "NaN"
	case math.IsInf(d, 1):
		return "Infinity"
	case math.IsInf(d, -1):
		return "-Infinity"
	case d == 0:
		if math.Signbit(d) {
			return "-0.0"
		}
		return "0.0"
	}
	neg := d < 0
	a := math.Abs(d)
	// shortest decimal digits and exponent
	s := strconv.FormatFloat(a, 'e', -1, bits) // d.ddddde±XX
	mant, expS, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expS)
	digits := strings.Replace(mant, ".", "", 1)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if a >= 1e-3 && a < 1e7 {
		// plain notation: at least one digit after the point
		pointPos := exp + 1 // number of digits before the point
		if pointPos <= 0 {
			b.WriteString("0.")
			b.WriteString(strings.Repeat("0", -pointPos))
			b.WriteString(digits)
		} else if pointPos >= len(digits) {
			b.WriteString(digits)
			b.WriteString(strings.Repeat("0", pointPos-len(digits)))
			b.WriteString(".0")
		} else {
			b.WriteString(digits[:pointPos])
			b.WriteByte('.')
			b.WriteString(digits[pointPos:])
		}
		return b.String()
	}
	b.WriteByte(digits[0])
	b.WriteByte('.')
	if len(digits) > 1 {
		b.WriteString(digits[1:])
	} else {
		b.WriteByte('0')
	}
	b.WriteByte('E')
	b.WriteString(strconv.Itoa(exp))
	return b.String()
}
