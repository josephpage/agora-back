package javacompat_test

import (
	"math"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"agora/internal/javacompat"
	"agora/parity/oracle"
)

// corpus of tricky strings shared by the differential tests.
var corpus = []string{
	"", " ", "a", " a ", "\t\n\v\f\r", " x ", "  ", "\u0085x\u0085", "　ab　",
	"\x1c\x1d\x1e\x1fz", "héllo", "ÉCOLOGIE à l'école", "😀", "a😀b", "😀😀😀", "İstanbul", "ß", "Straße",
	"ǅǆǄ", "ﬃ", "ſ", "K", "K", "00000000-0000-0000-0000-000000000000", "1-1-1-1-1", "+1-+2-3-4-5",
	"-1--1-1-1-1", "123e4567-E89B-12d3-A456-426614174000", "g-1-1-1-1", "١-٢-٣-٤-٥", "ＡＢ-1-1-1-1",
	"0x1-1-1-1-1", "1-1-1-1", "1-1-1-1-1-1", "ffffffff-ffff-ffff-ffff-ffffffffffff1", "7fffffffffffffff-1-1-1-1",
	"12", "-12", "+12", "+", "-", "2147483647", "2147483648", "-2147483648", "-2147483649", "١٢٣", "1_000",
	"a b+c&d=e/f?g*h~i.j-k_l", "café crème", "́e", "Ångström", "naïve façade",
}

func randomString(r *rand.Rand) string {
	alphabet := []rune("aZ09 -+_.*~&=/?#%éèàçÉ  ́😀İıſKk\t\n\r\x00\x1f١ＡＢ{}[]\"'\\<>")
	n := r.Intn(12)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

func allInputs() []string {
	r := rand.New(rand.NewSource(42))
	in := append([]string(nil), corpus...)
	for i := 0; i < 300; i++ {
		in = append(in, randomString(r))
	}
	return in
}

func TestOracleUUID(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, s := range allInputs() {
		var res string
		err := oracle.Call("uuidFromString", map[string]any{"s": s}, &res)
		got, ok := javacompat.ParseUUID(s)
		if (err == nil) != ok {
			t.Errorf("ParseUUID(%q): go ok=%v, java err=%v", s, ok, err)
			continue
		}
		if ok && got.String() != res {
			t.Errorf("ParseUUID(%q) = %s, java %s", s, got, res)
		}
	}
}

func TestOracleStrings(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	for _, s := range allInputs() {
		if !utf8.ValidString(s) {
			continue
		}
		var trim string
		oracle.MustCall(t, "kotlinTrim", map[string]any{"s": s}, &trim)
		if got := javacompat.KotlinTrim(s); got != trim {
			t.Errorf("KotlinTrim(%q) = %q, java %q", s, got, trim)
		}
		var blank bool
		oracle.MustCall(t, "kotlinIsBlank", map[string]any{"s": s}, &blank)
		if got := javacompat.KotlinIsBlank(s); got != blank {
			t.Errorf("KotlinIsBlank(%q) = %v, java %v", s, got, blank)
		}
		var enc string
		oracle.MustCall(t, "urlEncode", map[string]any{"s": s}, &enc)
		if got := javacompat.URLEncode(s); got != enc {
			t.Errorf("URLEncode(%q) = %q, java %q", s, got, enc)
		}
		var dia string
		oracle.MustCall(t, "replaceDiacritics", map[string]any{"s": s}, &dia)
		if got := javacompat.ReplaceDiacritics(s); got != dia {
			t.Errorf("ReplaceDiacritics(%q) = %q, java %q", s, got, dia)
		}
		var n *int
		oracle.MustCall(t, "kotlinToIntOrNull", map[string]any{"s": s}, &n)
		gn, gok := javacompat.KotlinToIntOrNull(s)
		if (n != nil) != gok || (gok && *n != gn) {
			t.Errorf("KotlinToIntOrNull(%q) = %v,%v java %v", s, gn, gok, n)
		}
		var l int
		oracle.MustCall(t, "javaStringLength", map[string]any{"s": s}, &l)
		if got := javacompat.Len16(s); got != l {
			t.Errorf("Len16(%q) = %d, java %d", s, got, l)
		}
		for _, k := range []int{0, 1, 2, 3, 5} {
			var tk oracle.Text
			oracle.MustCall(t, "kotlinTake", map[string]any{"s": s, "n": k}, &tk)
			// lone surrogates become '?' once encoded to UTF-8 by JDBC
			want := strings.Map(func(r rune) rune {
				if r == utf8.RuneError {
					return '?'
				}
				return r
			}, string(utf16Decode(tk.Utf16)))
			if got := javacompat.Take16(s, k); got != want {
				t.Errorf("Take16(%q,%d) = %q, java %q", s, k, got, want)
			}
		}
		for _, other := range []string{"i", "I", "é", "E", "ss", "k", "😀", ""} {
			var c bool
			oracle.MustCall(t, "containsIgnoreCase", map[string]any{"s": s, "other": other}, &c)
			if got := javacompat.ContainsIgnoreCase(s, other); got != c {
				t.Errorf("ContainsIgnoreCase(%q,%q) = %v, java %v", s, other, got, c)
			}
		}
	}
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			out = append(out, rune((int(c)-0xD800)<<10|(int(u[i+1])-0xDC00))+0x10000)
			i++
			continue
		}
		if c >= 0xD800 && c < 0xE000 {
			out = append(out, utf8.RuneError)
			continue
		}
		out = append(out, rune(c))
	}
	return out
}

func TestOracleMathRound(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	vals := []float64{0, 0.5, -0.5, 1.5, -1.5, 2.5, -2.5, 0.49999999999999994, -0.49999999999999994, 1e15 + 0.5, 28.499999999999996, 1e300, -1e300, math.MaxInt64, 0.285 * 100, 99.5, 66.66666666666667}
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 200; i++ {
		vals = append(vals, (r.Float64()-0.5)*1000)
		vals = append(vals, float64(r.Intn(2000)-1000)/2)
	}
	for _, v := range vals {
		var res int64
		oracle.MustCall(t, "mathRound", oracle.F64(v), &res)
		if got := javacompat.JavaMathRound(v); got != res {
			t.Errorf("JavaMathRound(%v) = %d, java %d", v, got, res)
		}
	}
}
