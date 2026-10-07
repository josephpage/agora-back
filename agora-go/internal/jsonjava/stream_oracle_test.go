package jsonjava

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"agora/parity/oracle"
)

// goTokens walks the first value like the oracle's jacksonTokens does.
func goTokens(body []byte) ([]any, error) {
	text, chars, err := requestText(body, "UTF-8")
	if err != nil {
		return nil, err
	}
	d := &decoder{b: text, chars: chars}
	if _, ok := d.ws(); !ok {
		return nil, fail("no content")
	}
	var toks []any
	var walk func() error
	walk = func() error {
		c, err := d.next()
		if err != nil {
			return err
		}
		switch c {
		case '{':
			d.beginObject()
			toks = append(toks, "{")
			name, ok, err := d.firstMember()
			for ; err == nil && ok; name, ok, err = d.afterMember() {
				toks = append(toks, map[string]any{"name": name})
				if err := walk(); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			toks = append(toks, "}")
			return nil
		case '[':
			d.beginArray()
			toks = append(toks, "[")
			for first := true; ; first = false {
				ok, err := d.element(first)
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				if err := walk(); err != nil {
					return err
				}
			}
			toks = append(toks, "]")
			return nil
		}
		v, err := d.scalar(c)
		if err != nil {
			return err
		}
		switch t := v.(type) {
		case nil:
			toks = append(toks, "null")
		case bool:
			toks = append(toks, fmt.Sprint(t))
		case string:
			toks = append(toks, map[string]any{"string": t})
		case json.Number:
			toks = append(toks, map[string]any{"number": string(t)})
		}
		return nil
	}
	return toks, walk()
}

// javaString converts oracle UTF-16 units the way the Go decoder represents
// them (unpaired surrogates → '?').
func javaString(units []any) string {
	var w utf16Writer
	for _, u := range units {
		w.unit(uint16(u.(float64)))
	}
	return w.finish()
}

func normalizeOracleTokens(toks []any) []any {
	out := make([]any, 0, len(toks))
	for _, t := range toks {
		if m, ok := t.(map[string]any); ok {
			for k, v := range m {
				if k == "number" {
					out = append(out, map[string]any{k: v})
				} else {
					out = append(out, map[string]any{k: javaString(v.([]any))})
				}
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

func fuzzInputs(rnd *rand.Rand, n int) [][]byte {
	pieces := []string{`{`, `}`, `[`, `]`, `,`, `:`, `"a"`, `"yearOfBirth"`, `"x"`, `1`, `-0`, `01`, `1.`, `1.5e+3`, `-`, `true`, `false`, `null`, `tru`,
		`"\u00e9"`, `"\ud83d\ude00"`, `"\ud800"`, `"\udc00x"`, `"\q"`, `"\u12"`, `" "`, `"\t"`, ` `, "\t", "\n", "\x00", `"é"`, `"😀"`,
		"\xc0\xaf", "\xe0\x80\xaf", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xf0\x80\x80\x80", "\xf7\xbf\xbf\xbf", "\xf8", "\x80", "\xc3", "\xc3\x28", "\xe2\x82", "\xff",
		`/*`, `#`, `'a'`, `NaN`, `+1`, `.5`, "\xef\xbb\xbf"}
	var out [][]byte
	for k := 0; k < n; k++ {
		var b strings.Builder
		if rnd.Intn(3) > 0 {
			// mostly-valid objects with a corrupted string
			b.WriteString(`{"a":"`)
			for j := rnd.Intn(4); j >= 0; j-- {
				b.WriteString(strings.Trim(pieces[rnd.Intn(len(pieces))], `"`))
			}
			b.WriteString(`",`)
			b.WriteString(pieces[rnd.Intn(len(pieces))])
			b.WriteString(`:[1,"`)
			b.WriteString(strings.Trim(pieces[rnd.Intn(len(pieces))], `"`))
			b.WriteString(`"]}`)
			if rnd.Intn(4) == 0 {
				b.WriteString(pieces[rnd.Intn(len(pieces))])
			}
		} else {
			for j := rnd.Intn(10); j >= 0; j-- {
				b.WriteString(pieces[rnd.Intn(len(pieces))])
			}
		}
		s := b.String()
		switch rnd.Intn(8) {
		case 0:
			out = append(out, encodeUTF16(s, rnd.Intn(2) == 0, rnd.Intn(2) == 0))
		case 1:
			out = append(out, encodeUTF32(s, rnd.Intn(2) == 0))
		default:
			out = append(out, []byte(s))
		}
	}
	return out
}

func encodeUTF16(s string, be, bom bool) []byte {
	var out []byte
	units := utf16.Encode([]rune(s))
	if bom {
		units = append([]uint16{0xfeff}, units...)
	}
	for _, u := range units {
		if be {
			out = append(out, byte(u>>8), byte(u))
		} else {
			out = append(out, byte(u), byte(u>>8))
		}
	}
	return out
}

func encodeUTF32(s string, be bool) []byte {
	var out []byte
	for _, r := range s {
		if be {
			out = append(out, byte(r>>24), byte(r>>16), byte(r>>8), byte(r))
		} else {
			out = append(out, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		}
	}
	return out
}

func TestOracleJacksonTokens(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(11))
	inputs := fuzzInputs(rnd, 6000)
	for _, s := range []string{`{"yearOfBirth":"1990"}`, "\xef\xbb\xbf{}", "  \xef\xbb\xbf{}", `{"a":1}garbage`, `{"a":1`, ``, `   `, `{"a":"\ud83d\ude00"}`, `{"\u00e9":1}`, "{\"\xc1\xb9\":1}"} {
		inputs = append(inputs, []byte(s))
	}
	bad := 0
	for _, in := range inputs {
		var res struct {
			Tokens []any  `json:"tokens"`
			Error  string `json:"error"`
		}
		oracle.MustCall(t, "jacksonTokens", map[string]any{"b64": base64.StdEncoding.EncodeToString(in)}, &res)
		got, err := goTokens(in)
		switch {
		case res.Error != "" && err != nil:
			continue
		case res.Error != "" || err != nil:
			bad++
			if bad <= 25 {
				t.Errorf("%q: jackson error %q, go error %v (go tokens %v)", in, res.Error, err, got)
			}
			continue
		}
		want := normalizeOracleTokens(res.Tokens)
		if !reflect.DeepEqual(fmt.Sprint(got), fmt.Sprint(want)) {
			bad++
			if bad <= 25 {
				t.Errorf("%q:\n got  %v\n want %v", in, got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d/%d mismatches", bad, len(inputs))
	}
}
