package javacompat

import (
	"encoding/base64"
	"math/rand"
	"testing"
	"unicode/utf16"

	"agora/parity/oracle"
)

func TestDecodeUTF8JavaOracle(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(7))
	interesting := []byte{0x00, 0x41, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0, 0xc1, 0xc2, 0xc3, 0xdf, 0xe0, 0xe1, 0xed, 0xee, 0xef, 0xf0, 0xf1, 0xf4, 0xf5, 0xf7, 0xf8, 0xfb, 0xfc, 0xfe, 0xff}
	var inputs [][]byte
	for _, s := range []string{"", "abc", "\xff", "\xe2\x82", "\xe2\x82A", "\xf0\x9f\x98", "\xf0\x9f\x98A", "\xed\xa0\x80", "\xc0\xaf", "\xe0\x80\xaf", "\xf4\x90\x80\x80", "é€😀"} {
		inputs = append(inputs, []byte(s))
	}
	for k := 0; k < 4000; k++ {
		n := rnd.Intn(8)
		b := make([]byte, n)
		for j := range b {
			if rnd.Intn(3) == 0 {
				b[j] = byte(rnd.Intn(256))
			} else {
				b[j] = interesting[rnd.Intn(len(interesting))]
			}
		}
		inputs = append(inputs, b)
	}
	bad := 0
	for _, in := range inputs {
		var units []uint16
		oracle.MustCall(t, "javaUtf8Decode", map[string]any{"b64": base64.StdEncoding.EncodeToString(in)}, &units)
		want := string(utf16.Decode(units))
		if got := DecodeUTF8Java(in); got != want {
			bad++
			if bad < 20 {
				t.Errorf("% x: got %+q want %+q", in, got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d/%d mismatches", bad, len(inputs))
	}
}

func TestLatin1(t *testing.T) {
	if got := Latin1("é"); got != "Ã©" { // the two UTF-8 bytes read as ISO-8859-1
		t.Errorf("got %q", got)
	}
	if got := Latin1("plain"); got != "plain" {
		t.Errorf("got %q", got)
	}
}
