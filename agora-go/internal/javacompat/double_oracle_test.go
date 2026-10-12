package javacompat

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"agora/parity/oracle"
)

func TestKotlinToDoubleOrNullOracle(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	inputs := []string{"1.5", "2", "2f", "1.5F", "1.5d", "1.5D", " 1.5 ", "\t2\n", " 2", "inf", "Inf", "infinity", "Infinity", "-Infinity", "+Infinity", "nan", "NaN", "-NaN", "1e400", "-1e400", "1e-400",
		"0x1p3", "0x1.8p1", "0X.8p1", "0x1p3f", "0x1", ".5", "5.", ".", "", " ", "1_0", "١٢", "1e", "1e+", "+", "-", "1.5e3d", "0x1P-1074", "4.9e-324", "2.4703282292062328e-324", "1.7976931348623157e308", "1.7976931348623159e308", "00012", "-0", "-0.0f", "1ef", "1.5dd", "0xfp1d"}
	rnd := rand.New(rand.NewSource(8))
	alphabet := []string{"0", "1", "9", ".", "e", "E", "-", "+", "x", "p", "f", "d", "a", " ", "N", "I"}
	for k := 0; k < 3000; k++ {
		s := ""
		for j := rnd.Intn(7) + 1; j > 0; j-- {
			s += alphabet[rnd.Intn(len(alphabet))]
		}
		inputs = append(inputs, s)
	}
	for _, s := range inputs {
		var want struct {
			Null bool   `json:"null"`
			Bits string `json:"bits"`
		}
		oracle.MustCall(t, "kotlinToDoubleOrNullBits", map[string]any{"s": s}, &want)
		got, ok := KotlinToDoubleOrNull(s)
		if want.Null {
			if ok {
				t.Errorf("%q: Kotlin null, Go %v", s, got)
			}
			continue
		}
		bits, _ := strconv.ParseInt(want.Bits, 10, 64)
		w := math.Float64frombits(uint64(bits))
		if !ok || (math.Float64bits(got) != uint64(bits) && !(math.IsNaN(got) && math.IsNaN(w))) {
			t.Errorf("%q: Kotlin %v, Go %v (ok=%v)", s, w, got, ok)
		}
	}
}
