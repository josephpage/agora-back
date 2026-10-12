package javacompat

import (
	"math/rand"
	"slices"
	"testing"

	"agora/parity/oracle"
)

func TestKotlinLinesOracle(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(3))
	alphabet := []string{"a", "\r", "\n", "\r\n", " ", "-----", "é"}
	for k := 0; k < 1000; k++ {
		s := ""
		for j := rnd.Intn(8); j > 0; j-- {
			s += alphabet[rnd.Intn(len(alphabet))]
		}
		var want []string
		oracle.MustCall(t, "kotlinLines", map[string]any{"s": s}, &want)
		if got := KotlinLines(s); !slices.Equal(got, want) {
			t.Errorf("%q: Go %q, Kotlin %q", s, got, want)
		}
	}
}
