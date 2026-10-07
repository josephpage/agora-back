package jsonjava

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Decoding rules (Jackson 2.14 + jackson-module-kotlin, FAIL_ON_UNKNOWN_PROPERTIES=false):
//   - only the first JSON value is read; trailing content is ignored;
//   - unknown properties are ignored; duplicate properties: last wins;
//   - non-pointer string / slice / struct / map fields are Kotlin non-null
//     types: missing or null → error, unless the `def` tag option is present
//     (Kotlin default value: missing → keep the pre-initialized value, null → error);
//   - pointer fields are nullable: missing → untouched, null → nil;
//   - non-pointer int / bool / float fields are JVM primitives: missing →
//     untouched (zero or default), null → zero;
//   - scalar coercions: number/bool → String (original text), numeric String →
//     int/float, float → int (truncated), int → bool (0=false), "true"/"false" → bool.

// DecodeError describes why Jackson would fail (→ HttpMessageNotReadableException).
type DecodeError struct{ Msg string }

func (e *DecodeError) Error() string { return e.Msg }

func fail(format string, a ...any) error { return &DecodeError{fmt.Sprintf(format, a...)} }

// Unmarshal decodes data into v (pointer to struct or slice) with Jackson rules.
func Unmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return fail("unreadable JSON: %v", err)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("jsonjava: Unmarshal needs a non-nil pointer")
	}
	if root == nil {
		// readValue returns null → "Required request body is missing"
		return fail("null body")
	}
	return bind(rv.Elem(), root, "$")
}

// UnmarshalTree binds an already parsed tree (from encoding/json with UseNumber).
func UnmarshalTree(tree any, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("jsonjava: UnmarshalTree needs a non-nil pointer")
	}
	return bind(rv.Elem(), tree, "$")
}

type dfield struct {
	name  string
	index int
	def   bool
}

func decodeFields(t reflect.Type) []dfield {
	var out []dfield
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = sf.Name
		}
		f := dfield{name: name, index: i}
		for _, o := range strings.Split(opts, ",") {
			if o == "def" {
				f.def = true
			}
		}
		out = append(out, f)
	}
	return out
}

var unmarshalerType = reflect.TypeOf((*TreeUnmarshaler)(nil)).Elem()

// TreeUnmarshaler lets a type implement custom (polymorphic) decoding.
type TreeUnmarshaler interface {
	UnmarshalJavaTree(tree any) error
}

func bind(v reflect.Value, tree any, path string) error {
	if v.CanAddr() && v.Addr().Type().Implements(unmarshalerType) {
		return v.Addr().Interface().(TreeUnmarshaler).UnmarshalJavaTree(tree)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if tree == nil {
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		nv := reflect.New(v.Type().Elem())
		if err := bind(nv.Elem(), tree, path); err != nil {
			return err
		}
		v.Set(nv)
		return nil
	case reflect.Interface:
		v.Set(reflect.ValueOf(tree))
		return nil
	case reflect.Struct:
		obj, ok := tree.(map[string]any)
		if !ok {
			return fail("%s: expected object", path)
		}
		for _, f := range decodeFields(v.Type()) {
			fv := v.Field(f.index)
			raw, present := obj[f.name]
			if !present {
				if f.def || isNullableOrPrimitive(fv.Kind()) {
					continue
				}
				return fail("%s.%s: missing required property", path, f.name)
			}
			if raw == nil {
				switch fv.Kind() {
				case reflect.Pointer, reflect.Interface:
					fv.Set(reflect.Zero(fv.Type()))
					continue
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
					reflect.Bool, reflect.Float32, reflect.Float64:
					fv.Set(reflect.Zero(fv.Type()))
					continue
				}
				return fail("%s.%s: null for non-null property", path, f.name)
			}
			if err := bind(fv, raw, path+"."+f.name); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice:
		arr, ok := tree.([]any)
		if !ok {
			return fail("%s: expected array", path)
		}
		s := reflect.MakeSlice(v.Type(), len(arr), len(arr))
		for i, e := range arr {
			ev := s.Index(i)
			if e == nil {
				if ev.Kind() == reflect.Pointer || ev.Kind() == reflect.Interface {
					continue
				}
				// Kotlin List<String> containing null: Jackson accepts the null
				// element (type erasure) — represent as zero value.
				continue
			}
			if err := bind(ev, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		v.Set(s)
		return nil
	case reflect.Map:
		obj, ok := tree.(map[string]any)
		if !ok {
			return fail("%s: expected object", path)
		}
		m := reflect.MakeMapWithSize(v.Type(), len(obj))
		for k, e := range obj {
			ev := reflect.New(v.Type().Elem()).Elem()
			if e != nil {
				if err := bind(ev, e, path+"."+k); err != nil {
					return err
				}
			}
			m.SetMapIndex(reflect.ValueOf(k), ev)
		}
		v.Set(m)
		return nil
	case reflect.String:
		switch t := tree.(type) {
		case string:
			v.SetString(t)
		case json.Number:
			v.SetString(t.String())
		case bool:
			v.SetString(strconv.FormatBool(t))
		default:
			return fail("%s: cannot coerce %T to String", path, tree)
		}
		return nil
	case reflect.Bool:
		switch t := tree.(type) {
		case bool:
			v.SetBool(t)
		case json.Number:
			i, err := t.Int64()
			if err != nil {
				return fail("%s: cannot coerce float to Boolean", path)
			}
			v.SetBool(i != 0)
		case string:
			switch strings.TrimSpace(t) {
			case "true", "True", "TRUE":
				v.SetBool(true)
			case "false", "False", "FALSE", "":
				v.SetBool(false)
			default:
				return fail("%s: cannot coerce %q to Boolean", path, t)
			}
		default:
			return fail("%s: cannot coerce %T to Boolean", path, tree)
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := coerceInt(tree, v.Kind() != reflect.Int64)
		if err != nil {
			return fail("%s: %v", path, err)
		}
		v.SetInt(n)
		return nil
	case reflect.Float32, reflect.Float64:
		switch t := tree.(type) {
		case json.Number:
			f, err := strconv.ParseFloat(t.String(), 64)
			if err != nil {
				return fail("%s: %v", path, err)
			}
			v.SetFloat(f)
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if err != nil {
				return fail("%s: cannot coerce %q to double", path, t)
			}
			v.SetFloat(f)
		default:
			return fail("%s: cannot coerce %T to double", path, tree)
		}
		return nil
	}
	return fail("%s: unsupported kind %s", path, v.Kind())
}

func isNullableOrPrimitive(k reflect.Kind) bool {
	switch k {
	case reflect.Pointer, reflect.Interface,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Bool, reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func coerceInt(tree any, kotlinInt bool) (int64, error) {
	var n int64
	switch t := tree.(type) {
	case json.Number:
		s := t.String()
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			n = i
		} else {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, err
			}
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return 0, errors.New("non finite number")
			}
			n = int64(f) // ACCEPT_FLOAT_AS_INT: truncation
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce %q to int", t)
		}
		n = i
	default:
		return 0, fmt.Errorf("cannot coerce %T to int", tree)
	}
	// Go int/int32 represent Kotlin Int, int64 represents Kotlin Long.
	if kotlinInt && (n > math.MaxInt32 || n < math.MinInt32) {
		return 0, errors.New("int overflow")
	}
	return n, nil
}

// ErrPanic marks a rendering panic in tests (Kotlin NPE equivalent).
var ErrPanic = errors.New("panic during rendering")
