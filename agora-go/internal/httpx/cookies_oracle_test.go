package httpx

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"testing"

	"agora/internal/javacompat"
	"agora/parity/oracle"
)

// Tomcat's Rfc6265CookieProcessor + Request.convertCookies (the real classes) against tomcatCookies.
func TestOracleTomcatCookies(t *testing.T) {
	if !oracle.Available() {
		t.Skip("oracle disabled (PARITY_ORACLE=1)")
	}
	parts := []string{"a", "auth-jwt", "x.y_z", "=", ";", " ", "\t", `"`, ",", `\`, "\x7f", "\x00", "\x0b", "\x1f", "!", "#", "$Version=1", "$Path=/",
		"é", "\xe9", "\xc3", "\xff", "\x80", "\xef\xbf\xbd", "😀", "(", ")", "<", ">", "@", ":", "/", "[", "]", "?", "{", "}", "+", "%20", "a=b", "a=\"b\"", "a=b c", "; ", " ;"}
	rnd := rand.New(rand.NewSource(29))
	var cases [][]string
	for _, p := range parts {
		cases = append(cases, []string{p}, []string{"a=" + p}, []string{p + "=1"}, []string{"a=1; " + p + "; b=2"})
	}
	many := func(n int, sep string) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteString(sep)
			}
			fmt.Fprintf(&b, "c%d=%d", i, i)
		}
		return b.String()
	}
	cases = append(cases, []string{many(200, "; ")}, []string{many(201, "; ")}, []string{many(100, "; "), many(100, ";")}, []string{many(100, "; "), many(101, ";")},
		[]string{many(200, "; ") + "; =x; bad=a,b; ;"}, []string{many(200, "; ") + "; x"}, []string{many(199, "; ") + "; auth-jwt=t"})
	for k := 0; k < 5000; k++ {
		var lines []string
		for l := rnd.Intn(3); l >= 0; l-- {
			var b strings.Builder
			for j := rnd.Intn(10); j >= 0; j-- {
				b.WriteString(parts[rnd.Intn(len(parts))])
			}
			lines = append(lines, b.String())
		}
		cases = append(cases, lines)
	}
	// structured lines: name=value pairs whose values hold random parts
	names := []string{"a", "auth-jwt", "JSESSIONID", "x.y", "", " a", "a ", "$Version", "b\tc"}
	seps := []string{";", "; ", " ;", ";;", " ", ";\t", ","}
	for k := 0; k < 5000; k++ {
		var b strings.Builder
		for j := rnd.Intn(4); j >= 0; j-- {
			b.WriteString(names[rnd.Intn(len(names))])
			if rnd.Intn(6) != 0 {
				b.WriteString("=")
				for v := rnd.Intn(3); v >= 0; v-- {
					if rnd.Intn(2) == 0 {
						b.WriteString("eyJhbGciOiJIUzUxMiJ9")
					} else {
						b.WriteString(parts[rnd.Intn(len(parts))])
					}
				}
			}
			if j > 0 {
				b.WriteString(seps[rnd.Intn(len(seps))])
			}
		}
		cases = append(cases, []string{b.String()})
	}
	nonEmpty, tooMany := 0, 0
	for _, c := range cases {
		enc := make([]string, len(c))
		for i, v := range c {
			enc[i] = base64.StdEncoding.EncodeToString([]byte(v))
		}
		var want struct {
			TooMany bool       `json:"tooMany"`
			Cookies [][]string `json:"cookies"`
		}
		oracle.MustCall(t, "tomcatCookies", map[string]any{"values": enc}, &want)
		if want.TooMany {
			tooMany++
		} else if len(want.Cookies) > 0 {
			nonEmpty++
		}
		r := &http.Request{Header: http.Header{"Cookie": c}}
		got, ok := tomcatCookies(r)
		if want.TooMany || !ok {
			if want.TooMany != !ok {
				t.Errorf("%q: Tomcat tooMany=%v, Go %v", c, want.TooMany, !ok)
			}
			continue
		}
		var gotPairs [][]string
		for _, gc := range got {
			gotPairs = append(gotPairs, []string{gc.name, javacompat.DecodeUTF8Java([]byte(gc.value))})
		}
		if fmt.Sprintf("%q", gotPairs) != fmt.Sprintf("%q", want.Cookies) && !(len(gotPairs) == 0 && len(want.Cookies) == 0) {
			t.Errorf("%q: Tomcat %q, Go %q", c, want.Cookies, gotPairs)
		}
	}
	t.Logf("%d cases, %d with cookies, %d over the limit", len(cases), nonEmpty, tooMany)
}
