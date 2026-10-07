// Package xmljava serializes values like Jackson's XmlMapper (jackson-dataformat-xml
// 2.14 + Woodstox) as configured by Spring MVC (Jackson2ObjectMapperBuilder.xml()).
//
// Element names come from the `xml` tag, else the `json` tag, else the Go
// field name. Options in the `xml` tag: "attr" (isAttribute = true), "cdata"
// (@JacksonXmlCData), "unwrapped" (@JacksonXmlElementWrapper(useWrapping=false)).
// The root element is RootName() if implemented, else JavaName(), else the Go
// type name. Defaults (verified against the JVM oracle):
//   - null → empty element <name/>
//   - lists are wrapped: <prop><prop>v1</prop><prop>v2</prop></prop>; empty → <prop/>
//   - no XML declaration.
package xmljava

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"agora/internal/jsonjava"
)

// Rooted lets a type choose its root element (@JacksonXmlRootElement).
type Rooted interface{ RootName() string }

// JavaNamed exposes the Kotlin simple class name (default root element).
type JavaNamed interface{ JavaName() string }

type xfield struct {
	name      string
	index     int
	attr      bool
	cdata     bool
	unwrapped bool
	omitNull  bool
}

var cache sync.Map

func fieldsOf(t reflect.Type) []xfield {
	if f, ok := cache.Load(t); ok {
		return f.([]xfield)
	}
	var out []xfield
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		jt := sf.Tag.Get("json")
		if jt == "-" {
			continue
		}
		jname, jopts, _ := strings.Cut(jt, ",")
		f := xfield{name: sf.Name, index: i}
		if jname != "" {
			f.name = jname
		}
		if strings.Contains(jopts, "omitnull") || strings.Contains(jopts, "omitempty") {
			f.omitNull = true
		}
		if xt, ok := sf.Tag.Lookup("xml"); ok {
			xname, xopts, _ := strings.Cut(xt, ",")
			if xname != "" {
				f.name = xname
			}
			for _, o := range strings.Split(xopts, ",") {
				switch o {
				case "attr":
					f.attr = true
				case "cdata":
					f.cdata = true
				case "unwrapped":
					f.unwrapped = true
				}
			}
		}
		out = append(out, f)
	}
	cache.Store(t, out)
	return out
}

// RootName returns the root element name for v.
func RootName(v any) string {
	if r, ok := v.(Rooted); ok {
		return r.RootName()
	}
	if j, ok := v.(JavaNamed); ok {
		return j.JavaName()
	}
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "null"
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return "ArrayList"
	case reflect.Map:
		return "LinkedHashMap"
	case reflect.String:
		return "String"
	}
	return t.Name()
}

// Marshal serializes v with its root element.
func Marshal(v any) []byte {
	var b []byte
	root := RootName(v)
	rv := reflect.ValueOf(v)
	return appendElement(b, root, rv, false)
}

// MarshalChecked is Marshal but fails like Woodstox when the output would
// contain characters invalid in XML 1.0 (control characters other than
// TAB/LF/CR): Jackson throws, Spring answers 500.
func MarshalChecked(v any) ([]byte, error) {
	b := Marshal(v)
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return nil, errInvalidXMLChar
		}
	}
	return b, nil
}

var errInvalidXMLChar = errors.New("JsonMappingException: Invalid white space character in text to output")

func isNilValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func deref(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func appendElement(b []byte, name string, v reflect.Value, cdata bool) []byte {
	v = deref(v)
	if !v.IsValid() {
		return append(append(append(b, '<'), name...), "/>"...)
	}
	if _, ok := v.Interface().(jsonjava.Unit); ok {
		return append(append(append(b, '<'), name...), "/>"...)
	}
	switch v.Kind() {
	case reflect.Struct:
		fields := fieldsOf(v.Type())
		b = append(b, '<')
		b = append(b, name...)
		for _, f := range fields {
			if !f.attr {
				continue
			}
			fv := deref(v.Field(f.index))
			if !fv.IsValid() {
				continue // null attributes are omitted
			}
			b = append(b, ' ')
			b = append(b, f.name...)
			b = append(b, '=', '"')
			b = appendEscapedAttr(b, scalarText(fv))
			b = append(b, '"')
		}
		hasChildren := false
		for _, f := range fields {
			if !f.attr {
				hasChildren = true
				break
			}
		}
		if !hasChildren {
			return append(b, "/>"...)
		}
		b = append(b, '>')
		for _, f := range fields {
			if f.attr {
				continue
			}
			fv := v.Field(f.index)
			if isNilValue(fv) && f.omitNull {
				continue
			}
			fd := deref(fv)
			if fd.IsValid() && (fd.Kind() == reflect.Slice || fd.Kind() == reflect.Array) && !(fd.Kind() == reflect.Slice && fd.Type().Elem().Kind() == reflect.Uint8) {
				if f.unwrapped {
					for i := 0; i < fd.Len(); i++ {
						b = appendElement(b, f.name, fd.Index(i), f.cdata)
					}
					continue
				}
				if fd.Len() == 0 {
					b = append(append(append(b, '<'), f.name...), "/>"...)
					continue
				}
				b = append(append(append(b, '<'), f.name...), '>')
				for i := 0; i < fd.Len(); i++ {
					b = appendElement(b, f.name, fd.Index(i), f.cdata)
				}
				b = append(append(append(b, "</"...), f.name...), '>')
				continue
			}
			if fv.Kind() == reflect.Slice && fv.IsNil() && !f.omitNull {
				// Kotlin non-null empty list
				b = append(append(append(b, '<'), f.name...), "/>"...)
				continue
			}
			b = appendElement(b, f.name, fv, f.cdata)
		}
		return append(append(append(b, "</"...), name...), '>')
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return append(append(append(b, '<'), name...), "/>"...)
		}
		b = append(append(append(b, '<'), name...), '>')
		for i := 0; i < v.Len(); i++ {
			b = appendElement(b, "item", v.Index(i), false)
		}
		return append(append(append(b, "</"...), name...), '>')
	}
	text := scalarText(v)
	b = append(append(append(b, '<'), name...), '>')
	if cdata {
		// Jackson/Woodstox refuses "]]>" inside CDATA (JsonMappingException → 500):
		// the marker byte makes MarshalChecked fail.
		if strings.Contains(text, "]]>") {
			b = append(b, 0x00)
		}
		b = append(b, "<![CDATA["...)
		b = append(b, text...)
		b = append(b, "]]>"...)
	} else {
		b = appendEscapedText(b, text)
	}
	return append(append(append(b, "</"...), name...), '>')
}

func scalarText(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float64, reflect.Float32:
		return jsonjava.JavaDoubleToString(v.Float())
	}
	if m, ok := v.Interface().(jsonjava.EpochMillis); ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return string(jsonjava.Marshal(v.Interface()))
}

// appendEscapedText escapes element text exactly like Woodstox
// BufferingXmlWriter (verified with the JVM oracle):
//   - '&' → &amp;, '<' → &lt;, '\r' → &#xd;;
//   - '>' → &gt; only when it may be part of "]]>": short strings (< 12 UTF-16
//     units, String code path) escape it at index 0 or after ']'; longer strings
//     go through the char[] code path, processed in 512-unit chunks, where '>'
//     is escaped at the start of a segment (chunk start, or right after an
//     escaped character) or after ']'.
func appendEscapedText(b []byte, s string) []byte {
	units := utf16.Encode([]rune(s))
	n := len(units)
	short := n < 12
	segStart := 0
	for i, u := range units {
		if !short && i%512 == 0 {
			segStart = i
		}
		switch u {
		case '&':
			b = append(b, "&amp;"...)
			segStart = i + 1
			continue
		case '<':
			b = append(b, "&lt;"...)
			segStart = i + 1
			continue
		case '\r':
			b = append(b, "&#xd;"...)
			segStart = i + 1
			continue
		case '>':
			esc := false
			if short {
				esc = i == 0 || units[i-1] == ']'
			} else {
				esc = i == segStart || units[i-1] == ']'
			}
			if esc {
				b = append(b, "&gt;"...)
				segStart = i + 1
				continue
			}
		}
		// re-encode this unit (surrogate pairs are handled by decoding pairs)
		if u >= 0xD800 && u < 0xDC00 && i+1 < n && units[i+1] >= 0xDC00 && units[i+1] < 0xE000 {
			r := utf16.DecodeRune(rune(u), rune(units[i+1]))
			b = utf8.AppendRune(b, r)
			continue
		}
		if u >= 0xDC00 && u < 0xE000 && i > 0 && units[i-1] >= 0xD800 && units[i-1] < 0xDC00 {
			continue // second half already written
		}
		b = utf8.AppendRune(b, rune(u))
	}
	return b
}

func appendEscapedAttr(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '&':
			b = append(b, "&amp;"...)
		case '<':
			b = append(b, "&lt;"...)
		case '"':
			b = append(b, "&quot;"...)
		case '\r':
			b = append(b, "&#xd;"...)
		case '\n':
			b = append(b, "&#xa;"...)
		case '\t':
			b = append(b, "&#x9;"...)
		default:
			b = append(b, c)
		}
	}
	return b
}
