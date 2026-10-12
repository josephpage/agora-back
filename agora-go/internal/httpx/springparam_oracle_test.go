package httpx

import (
	"math/rand"
	"strconv"
	"testing"

	"agora/parity/oracle"
)

// springConvertGo runs the Go conversion and reports its value or "error".
func springConvertGo(typ, s string) (v string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			v, failed = "", true
		}
	}()
	if typ == "Boolean" {
		return strconv.FormatBool(SpringBoolParam(s)), false
	}
	return strconv.Itoa(SpringIntPathVar(s)), false
}

// Spring's StringToBooleanConverter / NumberUtils.parseNumber (the real classes, through
// DefaultConversionService) against SpringBoolParam / SpringIntPathVar.
func TestOracleSpringParamConversion(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	spaces := []string{"", " ", "\t", "\n", "\v", "\f", "\r", "\x1c", "\x1f", "\x01", "\x00", "\u0085", " ", " ", " ", " ", " ", "​", " ", " ", " ", " ", "　", "᠎", "\ufeff"}
	words := []string{"true", "TRUE", "on", "On", "yes", "1", "0", "false", "off", "no", "NO", "y", "t", "", "2", "12", "-5", "+7", "007", "0x1F", "0X1f", "#ff", "-0x10", "+0x10", "0x", "#",
		"2147483647", "2147483648", "-2147483648", "-2147483649", "0x7fffffff", "0x80000000", "-0x80000000", "#-1", "1_000", "١٢", "１２", "1.0", "1e3", "abc", "tr ue", "1 2", "- 3"}
	rnd := rand.New(rand.NewSource(21))
	var inputs []string
	for _, w := range words {
		for _, sp := range spaces {
			inputs = append(inputs, sp+w, w+sp)
		}
	}
	for k := 0; k < 1500; k++ {
		s := ""
		for j := rnd.Intn(4); j >= 0; j-- {
			if rnd.Intn(2) == 0 {
				s += spaces[rnd.Intn(len(spaces))]
			} else {
				w := words[rnd.Intn(len(words))]
				if len(w) > 1 && rnd.Intn(3) == 0 {
					i := 1 + rnd.Intn(len(w)-1)
					w = w[:i] + spaces[rnd.Intn(len(spaces))] + w[i:]
				}
				s += w
			}
		}
		inputs = append(inputs, s)
	}
	n := 0
	for _, typ := range []string{"Boolean", "Integer"} {
		for _, s := range inputs {
			var want struct {
				Value *string `json:"value"`
				Error string  `json:"error"`
			}
			oracle.MustCall(t, "springConvert", map[string]any{"s": s, "type": typ}, &want)
			if want.Error == "" && want.Value == nil {
				continue // an empty text converts to null: the callers handle it before converting
			}
			got, failed := springConvertGo(typ, s)
			if want.Error != "" {
				if !failed {
					t.Errorf("%s %q: Spring %s, Go %s", typ, s, want.Error, got)
				}
			} else if failed || got != *want.Value {
				t.Errorf("%s %q: Spring %s, Go %s (failed=%v)", typ, s, *want.Value, got, failed)
			}
			n++
		}
	}
	t.Logf("%d conversions compared", n)
}
