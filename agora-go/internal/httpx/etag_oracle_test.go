package httpx

import (
	"math/rand"
	"testing"

	"agora/parity/oracle"
)

// ServletWebRequest.checkNotModified (the real Spring class) against ifNoneMatchMatches.
func TestOracleIfNoneMatch(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	const etag = `"0d41d8cd98f00b204e9800998ecf8427e"`
	parts := []string{etag, "W/" + etag, `"x"`, `W/"x"`, "*", ",", ", ", " ", "\t", "\x0b", "\x0c", "abc", `"`, `"x,`, `"0d41d8cd98f00b204e9800998ecf8427e`, `0d41d8cd98f00b204e9800998ecf8427e"`, "w/" + etag, "W/", `""`, " ", "é", ";", "\"a\\\"b\""}
	rnd := rand.New(rand.NewSource(17))
	var cases [][]string
	for _, p := range parts {
		cases = append(cases, []string{p})
	}
	for k := 0; k < 3000; k++ {
		var lines []string
		for l := rnd.Intn(3); l >= 0; l-- {
			v := ""
			for j := rnd.Intn(5); j >= 0; j-- {
				v += parts[rnd.Intn(len(parts))]
			}
			lines = append(lines, v)
		}
		cases = append(cases, lines)
	}
	for _, c := range cases {
		var want bool
		oracle.MustCall(t, "springCheckNotModified", map[string]any{"values": c, "etag": etag, "method": "GET"}, &want)
		if got := ifNoneMatchMatches(c, etag); got != want {
			t.Errorf("If-None-Match %q: Spring %v, Go %v", c, want, got)
		}
	}
}
