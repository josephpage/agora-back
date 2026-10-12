package jsonjava

import "testing"

// Jackson's String → boolean/int coercions trim with String.trim(): chars <= U+0020
// only (U+001F is trimmed, U+00A0 and U+3000 are not).
func TestCoercionTrimsLikeJava(t *testing.T) {
	type dto struct {
		B bool `json:"b"`
		I int  `json:"i"`
	}
	for _, tc := range []struct {
		in   string
		ok   bool
		want dto
	}{
		{`{"b":" true\u001f","i":"\u001f 2 "}`, true, dto{true, 2}},
		{`{"b":"true "}`, false, dto{}},
		{`{"b":"　true"}`, false, dto{}},
		{`{"i":" 2"}`, false, dto{}},
		{`{"i":" "}`, true, dto{}},
	} {
		var d dto
		err := Unmarshal([]byte(tc.in), &d)
		if (err == nil) != tc.ok || (tc.ok && d != tc.want) {
			t.Errorf("%s: %+v, %v", tc.in, d, err)
		}
	}
}
