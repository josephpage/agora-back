package xmljava_test

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	"agora/internal/xmljava"
	"agora/parity/oracle"
)

// TestOracleTextEscapingRandom compares element-text escaping on random
// strings mixing the significant characters and lengths around 12 and 512.
func TestOracleTextEscapingRandom(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(5))
	alphabet := []rune("ab]>&<\r\né😀 \"'")
	for i := 0; i < 1500; i++ {
		n := []int{1, 5, 10, 11, 12, 13, 30, 511, 512, 513, 1030}[r.Intn(11)]
		var b strings.Builder
		for j := 0; j < n; j++ {
			if r.Intn(4) == 0 {
				b.WriteRune(alphabet[r.Intn(len(alphabet))])
			} else {
				b.WriteByte('x')
			}
		}
		s := b.String()
		in, _ := json.Marshal(map[string]string{"title": s})
		var want string
		oracle.MustCall(t, "xmlSerialize", map[string]any{"className": "fr.gouv.agora.infrastructure.common.ErrorResponse", "json": string(in)}, &want)
		got := string(xmljava.Marshal(errResp{Title: s}))
		if got != want {
			t.Fatalf("len %d %q:\n go   %s\n java %s", len(s), s, got, want)
		}
	}
}
