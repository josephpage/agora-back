package httpx

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

// Spring's binding of @RequestParam String? / List<String>? (cases from the S9 slice).
func TestParamBinding(t *testing.T) {
	ctx := func(q string) *Ctx { return &Ctx{R: httptest.NewRequest("GET", "/x?"+q, nil)} }
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return "'" + *p + "'"
	}
	for _, tc := range []struct{ query, wantStr string }{
		{"", "<nil>"},
		{"titre=", "''"},
		{"titre=a", "'a'"},
		{"titre=a&titre=b", "'a,b'"},
		{"titre=a,b", "'a,b'"},
		{"titre=&titre=", "','"},
		{"titre=%FF", "'�'"},
	} {
		if got := str(ctx(tc.query).OptionalParam("titre")); got != tc.wantStr {
			t.Errorf("OptionalParam(%q): %s, want %s", tc.query, got, tc.wantStr)
		}
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", nil},
		{"etape=", []string{}},
		{"etape=a", []string{"a"}},
		{"etape=a,b", []string{"a", "b"}},
		{"etape=%20a%20,b%20", []string{"a", "b"}},
		{"etape=a,,b", []string{"a", "", "b"}},
		{"etape=,", []string{"", ""}},
		{"etape=a,b&etape=c,d", []string{"a,b", "c,d"}},
		{"etape=%20a&etape=b%20", []string{" a", "b "}},
		{"etape=&etape=", []string{"", ""}},
		{"etape=%C2%85a", []string{"\u0085a"}},
	} {
		if got := ctx(tc.query).ParamList("etape"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParamList(%q) = %#v, want %#v", tc.query, got, tc.want)
		}
	}
	if got := ctx("a=1&a=2").ParamValues("a"); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Errorf("ParamValues = %#v", got)
	}
	if got := ctx("a=1&a=2").ParamDefault("a", "d"); got != "1,2" {
		t.Errorf("ParamDefault = %q", got)
	}
}
