package fakestrapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Object is an insertion-ordered JSON object. Fixtures keep their authoring
// order when served, which makes responses stable and diff-friendly (and lets
// the "missingfield" fault target the first declared string field).
//
// JSON values are represented as: nil, bool, json.Number, string, []any,
// *Object.
type Object struct {
	keys []string
	m    map[string]any
}

// NewObject returns an empty Object.
func NewObject() *Object {
	return &Object{m: map[string]any{}}
}

// Get returns the value stored under key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.m[key]
	return v, ok
}

// Set stores a value, appending the key if it is new.
func (o *Object) Set(key string, v any) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Delete removes key if present.
func (o *Object) Delete(key string) {
	if _, ok := o.m[key]; !ok {
		return
	}
	delete(o.m, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string {
	return append([]string(nil), o.keys...)
}

// MarshalJSON writes the object preserving key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseJSON decodes a JSON document into the ordered value model.
func ParseJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after top-level JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool, nil
	}
	switch delim {
	case '{':
		obj := NewObject()
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("non-string object key %v", keyTok)
			}
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			obj.Set(key, v)
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected delimiter %v", delim)
}

// Marshal serialises a value of the ordered model (plus plain map/slice
// values used for envelopes) as compact JSON.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(t.String())
	case string:
		return writeString(buf, t)
	case *Object:
		buf.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeValue(buf, t.m[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []*Object:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeValue(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeValue(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		// Envelope helpers (maps, structs, numbers): defer to encoding/json.
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return err
		}
		buf.Write(bytes.TrimRight(b.Bytes(), "\n"))
	}
	return nil
}

func writeString(buf *bytes.Buffer, s string) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Write(bytes.TrimRight(b.Bytes(), "\n"))
	return nil
}

// clone deep-copies a value of the ordered model.
func clone(v any) any {
	switch t := v.(type) {
	case *Object:
		c := NewObject()
		for _, k := range t.keys {
			c.Set(k, clone(t.m[k]))
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, e := range t {
			c[i] = clone(e)
		}
		return c
	default:
		return v
	}
}

// scalarString renders a leaf value the way filters compare it.
func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

// isTemplateLike reports whether s looks like a {{...}} template.
func isTemplateLike(s string) bool {
	return strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}")
}
