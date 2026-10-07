package sanitize_test

import (
	"strings"
	"sync"
	"testing"

	"agora/internal/sanitize"
)

// Values produced by the real Kotlin ContentSanitizer (captured with the JVM oracle); they run without it.
func TestSanitizeKnownValues(t *testing.T) {
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"<script>x</script>a&amp;b <b>c</b>😀", 10, "a&b c\uf600"}, // (char) 0x1F600 == U+F600
		{"héllo wörld", 4, "héll"},
		{"{{x}}", 100, "{<!-- -->{x}}"},
		{"a{", 100, "a{<!-- -->"},
		{"{x}", 100, "{x}"},
		{"a < b && c > d", 100, "a < b && c > d"},
		{"l'article \"5\" + 2 = 3 @ x `y`", 100, "l'article \"5\" + 2 = 3 @ x `y`"},
		{"<noscript>a<b>c</b>d</noscript>e", 100, "cde"},
		{"<noscript>a</b>c", 100, ""},
		{"<title>t</title>x", 100, "x"},
		{"<iframe>a<b>c</b></iframe>d", 100, "d"},
		{"<table>text</table>", 100, "text"},
		{"<textarea>a &amp; <b>b</b></textarea>c", 100, "a & <b>b</b>c"},
		{"<textArea>x</textArea>y", 100, "xy"},
		{"<a b=\"x>y\">t</a>", 100, "t"},
		{"a<!-- c -->b<?pi?>c<!DOCTYPE x>d<![CDATA[e]]>f", 100, "abcdf"},
		{"a</é>b", 100, "ab"},
		{"x\x00y\x01z", 100, "xyz"},
		{"\U0001D800", 100, "?"},                    // &#x1d800; -> lone U+D800 -> '?' once stored
		{"\U0001D800\U0001DC00", 100, "\U00010000"}, // ... and side by side a valid pair
		{"\U0001D800\U0001DC00", 1, "?"},            // take(1) splits that pair
		{"&#128512; &#x1F600; &amp;#65;", 100, "\uf600 \uf600 &#65;"},
		{"क\u200cा", 100, "का"}, // the ZWNJ before an Indic vowel is dropped
		{"a&eacute;b&notit;c&NotEqualTilde;d", 100, "aéb¬it;c≂\u0338d"},
		{"abc", 0, ""},
		{"", 5, ""},
	} {
		if got := sanitize.Sanitize(tc.in, tc.max); got != tc.want {
			t.Errorf("Sanitize(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestSanitizeNegativeMaxLengthPanics(t *testing.T) {
	for _, in := range []string{"abc", "<b>abc"} { // fast path and general path
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Sanitize(%q, -1) did not panic", in)
				}
			}()
			sanitize.Sanitize(in, -1)
		}()
	}
}

func TestSanitizeUTF16KeepsLoneSurrogates(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []uint16
		want []uint16
	}{
		// A lone surrogate of the input is dropped by the encoder.
		{"lone input surrogate", []uint16{'a', 0xD800, 'b'}, []uint16{'a', 'b'}},
		// U+1D400 is written &#x1d400; and cast to (char) 0xD400.
		{"supplementary", []uint16{0xD835, 0xDC00}, []uint16{0xD400}},
		// U+1D800 becomes the lone surrogate 0xD800, which Java keeps.
		{"lone output surrogate", []uint16{0xD836, 0xDC00}, []uint16{0xD800}},
	} {
		if got := sanitize.SanitizeUTF16(tc.in, 10); !equal(got, tc.want) {
			t.Errorf("%s: got %x, want %x", tc.name, got, tc.want)
		}
	}
}

func TestSanitizeConcurrent(t *testing.T) {
	inputs := []string{
		"plain text", "<b>bold</b> &amp; <i>x</i>", "a&eacute;b&NotEqualTilde;c", "<script>x</script>y", "{{x}}", "😀 emoji", "<table><tr><td>a<td>b</table>",
	}
	want := make([]string, len(inputs))
	for i, in := range inputs {
		want[i] = sanitize.Sanitize(in, 50)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				for i, in := range inputs {
					if got := sanitize.Sanitize(in, 50); got != want[i] {
						t.Errorf("Sanitize(%q) = %q, want %q", in, got, want[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkSanitizePlain(b *testing.B) {
	in := "Pourquoi le gouvernement n'a-t-il pas encore mis en place une réforme de l'éducation nationale ?"
	for i := 0; i < b.N; i++ {
		sanitize.Sanitize(in, 200)
	}
}

func BenchmarkSanitizeSpecialChars(b *testing.B) {
	in := "Pourquoi le gouvernement {{n'a-t-il}} pas encore mis en place une réforme & de l'éducation <b>nationale</b> ? 😀"
	for i := 0; i < b.N; i++ {
		sanitize.Sanitize(in, 200)
	}
}

func BenchmarkSanitizeHTML(b *testing.B) {
	in := strings.Repeat("<p>Un <a href=\"http://example.org/?a=1&b=2\" title='x'>lien</a> &eacute;l&eacute;gant<br/>et <script>alert(1)</script>du texte</p>\n", 5)
	for i := 0; i < b.N; i++ {
		sanitize.Sanitize(in, 400)
	}
}

func equal(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
