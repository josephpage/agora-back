package httpx

import (
	"math/rand"
	"strings"
	"testing"

	"agora/parity/oracle"
)

func TestOracleSpringMediaType(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	rnd := rand.New(rand.NewSource(3))
	types := []string{"application/json", "APPLICATION/JSON", "application/*+json", "application/vnd.a+json", "application/json-patch+json", "text/json", "*/*", "*", "application/*",
		"application/xml", "text/xml", "application/*+xml", "application/soap+xml", "json", "application/", "/json", "application/json/x", "*/json", "a/b", "application/js on",
		"application/x-json", "application/problem+json", "text/plain", "multipart/form-data", "application/+json", "application/*+", " application/json ", "application/jsoné"}
	params := []string{"", ";", ";;", "; charset=utf-8", ";charset=UTF-8", ";charset=utf8", ";charset=UTF_8", ";charset=foo", ";charset=", `;charset=""`, `;charset="utf-8"`, ";charset='latin1'",
		";Charset=foo", ";CHARSET=ISO-8859-1", ";charset=ISO-8859-1", ";charset=@@", ";charset=x-user-defined", ";charset=cp1252", ";charset=UTF-16", ";charset=utf-32le",
		";q=0.5", ";q=1", ";q=2", ";q=abc", ";q=", `;q="0.5"`, ";q=1e-1", ";q=0x1p-1", ";q=NaN", ";q=1f", ";q=.5", ";q=5.", ";q=-0", ";q=+0.5",
		"; foo", ";foo=", ";=x", ";a=b c", `;a="b c"`, `;a="b;c"`, `;a="b`, ";a=b;a=c", ";charset=foo;charset=utf-8", ";a=é", `;a="é"`, ";a=b,c", ";x=(y)", "; boundary=x"}
	var inputs []string
	for _, ty := range types {
		for _, p := range params {
			inputs = append(inputs, ty+p)
		}
	}
	for k := 0; k < 1500; k++ {
		s := types[rnd.Intn(len(types))]
		for j := rnd.Intn(3); j >= 0; j-- {
			s += params[rnd.Intn(len(params))]
		}
		inputs = append(inputs, s)
	}
	bad := 0
	for _, in := range inputs {
		var res struct {
			Error           string `json:"error"`
			Type            string `json:"type"`
			Subtype         string `json:"subtype"`
			Charset         string `json:"charset"`
			WildcardType    bool   `json:"wildcardType"`
			WildcardSubtype bool   `json:"wildcardSubtype"`
			JSON            bool   `json:"json"`
			XML             bool   `json:"xml"`
		}
		oracle.MustCall(t, "springMediaType", map[string]any{"s": in}, &res)
		m, ok := parseSpringMediaType(in)
		var diff []string
		if ok != (res.Error == "") {
			diff = append(diff, "validity")
		} else if ok {
			if m.typ != res.Type || m.sub != res.Subtype {
				diff = append(diff, "type "+m.typ+"/"+m.sub)
			}
			if m.charset != res.Charset {
				diff = append(diff, "charset "+m.charset)
			}
			if m.wildcardType() != res.WildcardType || m.wildcardSubtype() != res.WildcardSubtype {
				diff = append(diff, "wildcard")
			}
			if m.readableJSON() != res.JSON || m.readableXML() != res.XML {
				diff = append(diff, "readable")
			}
		}
		if len(diff) > 0 {
			bad++
			if bad <= 25 {
				t.Errorf("%q: %s (java %+v)", in, strings.Join(diff, ", "), res)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d/%d mismatches", bad, len(inputs))
	}
}
