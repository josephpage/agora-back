package httpx

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"

	"agora/parity/oracle"
)

// HttpHeaders + CorsConfiguration.checkHeaders (the real Spring classes) against corsAllowHeaders.
func TestOracleCorsAllowHeaders(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	parts := []string{"X-Foo", "content-type", "Authorization", ",", ", ", " ", "\t", "\x0b", "\x1c", "\x1f", "\xa0", "\xc2\xa0", "\x85", `"`, `"a,b"`, `"x\"y"`, `\`, `" "`, `""`, "a b", "é", "\xe3\x80\x80", "*", ";"}
	rnd := rand.New(rand.NewSource(23))
	var cases [][]string
	for _, p := range parts {
		cases = append(cases, []string{p}, []string{"a," + p + ",b"})
	}
	for k := 0; k < 3000; k++ {
		var lines []string
		for l := rnd.Intn(2); l >= 0; l-- {
			var b strings.Builder
			for j := rnd.Intn(6); j >= 0; j-- {
				b.WriteString(parts[rnd.Intn(len(parts))])
			}
			lines = append(lines, b.String())
		}
		cases = append(cases, lines)
	}
	for _, c := range cases {
		enc := make([]string, len(c))
		for i, v := range c {
			enc[i] = base64.StdEncoding.EncodeToString([]byte(v))
		}
		var want struct {
			Rejected bool   `json:"rejected"`
			Value    string `json:"value"`
		}
		oracle.MustCall(t, "springCorsAllowHeaders", map[string]any{"values": enc}, &want)
		got, ok := corsAllowHeaders(c)
		if want.Rejected {
			if ok {
				t.Errorf("%q: Spring rejects, Go allows %q", c, got)
			}
			continue
		}
		wantValue, _ := base64.StdEncoding.DecodeString(want.Value)
		if !ok || strings.Join(got, ", ") != string(wantValue) {
			t.Errorf("%q: Spring %q, Go %q (ok=%v)", c, wantValue, strings.Join(got, ", "), ok)
		}
	}
}
