package qaglist

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"agora/parity/oracle"
)

// TestGeneratePowExceptions writes javapow_table.go: the whole-number bases below 1<<17 for which the
// JVM's Math.pow(x, 1.5) is not the correctly rounded value, with the JVM's result.
//
//	S3_GEN_POW=1 PARITY_ORACLE=1 go test ./internal/modules/qaglist -run GeneratePowExceptions
func TestGeneratePowExceptions(t *testing.T) {
	if os.Getenv("S3_GEN_POW") != "1" || !oracle.Available() {
		t.Skip("generator")
	}
	const limit = 1 << 17
	var xs []float64
	for x := 2; x < limit; x++ {
		xs = append(xs, float64(x))
	}
	var java []string
	for i := 0; i < len(xs); i += 20000 {
		end := min(i+20000, len(xs))
		var part []string
		oracle.MustCall(t, "mathPow", map[string]any{"xs": xs[i:end], "y": 1.5}, &part)
		java = append(java, part...)
	}
	var b strings.Builder
	b.WriteString("// Code generated from the JVM oracle (TestGeneratePowExceptions); DO NOT EDIT.\n\npackage qaglist\n\n")
	b.WriteString("// jvmPow15Exceptions lists the whole numbers x < 1<<17 for which HotSpot's Math.pow(x, 1.5) is not the\n")
	b.WriteString("// correctly rounded value of x^1.5 (the default TRENDING_SCORE_EXPONENT): the bits the JVM returns.\n")
	b.WriteString("var jvmPow15Exceptions = map[float64]uint64{\n")
	n := 0
	for i, x := range xs {
		want, _ := strconv.ParseUint(java[i], 16, 64)
		if math.Float64bits(bigPow(x, 1.5)) != want {
			fmt.Fprintf(&b, "\t%v: 0x%x,\n", x, want)
			n++
		}
	}
	b.WriteString("}\n")
	if err := os.WriteFile("javapow_table.go", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d exceptions", n)
}
