package sanitize_test

// Differential tests of the Go port against the REAL ContentSanitizer (OWASP java-html-sanitizer 20220608.1 +
// Spring 6.0.14 HtmlUtils + Kotlin take) running in the JVM oracle (parity/oracle). Enable with PARITY_ORACLE=1:
//
//	PARITY_ORACLE=1 go test ./internal/sanitize/ -run Oracle -count=1
//	PARITY_ORACLE=1 SANITIZE_RANDOM_CASES=1000000 go test ./internal/sanitize/ -run OracleRandom -count=1
//	PARITY_ORACLE=1 go test ./internal/sanitize/ -run '^$' -fuzz FuzzSanitize -fuzztime 5m
//
// Every case compares the OWASP stage alone (sanitizeRaw), the Spring stage alone (htmlUnescape) and the full
// sanitize for several maxLength values, on the exact UTF-16 code units (lone surrogates included).

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"agora/internal/sanitize"
	"agora/parity/oracle"
)

type m = map[string]any

// maxLengths are the limits of the callers (50, 200, 400, ...) plus the edge values.
var maxLengths = []int{0, 1, 5, 50, 200, 400}

const huge = 1 << 28

// comparedCases counts the oracle comparisons (one per call pair) across the whole test binary.
var comparedCases atomic.Int64

func units(s string) []uint16 { return utf16.Encode([]rune(s)) }

func eq16(a, b []uint16) bool {
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

func show(u []uint16) string {
	if len(u) > 160 {
		return fmt.Sprintf("(%d units) %s ... %s", len(u), show(u[:80]), show(u[len(u)-80:]))
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, c := range u {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%04x", c)
	}
	sb.WriteString("] ")
	sb.WriteString(strconv.QuoteToASCII(string(utf16.Decode(u))))
	return sb.String()
}

// dbString is what PostgreSQL stores for a Java string: lone surrogates are encoded as '?' by the JDBC driver.
func dbString(u []uint16) string {
	var sb strings.Builder
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF:
			sb.WriteRune(utf16.DecodeRune(rune(c), rune(u[i+1])))
			i++
		case c >= 0xD800 && c <= 0xDFFF:
			sb.WriteByte('?')
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

func hasLoneSurrogate(u []uint16) bool {
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			i++
		} else if c >= 0xD800 && c <= 0xDFFF {
			return true
		}
	}
	return false
}

// compare checks one input against the oracle; it returns a description of the first mismatch ("" if none).
// lengths are the maxLength values to compare the full sanitize with.
func compare(content []uint16, lengths []int) string {
	arg := oracle.Utf16(content)

	var raw oracle.Text
	if err := oracle.Call("sanitizeRaw", m{"content": arg}, &raw); err != nil {
		return fmt.Sprintf("oracle sanitizeRaw failed: %v", err)
	}
	if got := sanitize.Raw(content); !eq16(got, raw.Utf16) {
		return fmt.Sprintf("sanitizeRaw mismatch\n  go:     %s\n  oracle: %s", show(got), show(raw.Utf16))
	}

	// The Spring stage on the OWASP output (the production composition).
	var un oracle.Text
	if err := oracle.Call("htmlUnescape", m{"s": oracle.Utf16(raw.Utf16)}, &un); err != nil {
		return fmt.Sprintf("oracle htmlUnescape failed: %v", err)
	}
	if got := sanitize.HTMLUnescape(raw.Utf16); !eq16(got, un.Utf16) {
		return fmt.Sprintf("htmlUnescape(raw) mismatch\n  raw:    %s\n  go:     %s\n  oracle: %s", show(raw.Utf16), show(got), show(un.Utf16))
	}

	for _, n := range lengths {
		var full oracle.Text
		if err := oracle.Call("sanitize", m{"content": arg, "maxLength": n}, &full); err != nil {
			return fmt.Sprintf("oracle sanitize(%d) failed: %v", n, err)
		}
		if got := sanitize.SanitizeUTF16(content, n); !eq16(got, full.Utf16) {
			return fmt.Sprintf("sanitize(%d) mismatch\n  go:     %s\n  oracle: %s", n, show(got), show(full.Utf16))
		}
		if !hasLoneSurrogate(content) {
			s := string(utf16.Decode(content))
			if got, want := sanitize.Sanitize(s, n), dbString(full.Utf16); got != want {
				return fmt.Sprintf("Sanitize(string, %d) mismatch\n  go:     %q\n  oracle: %q", n, got, want)
			}
		}
	}
	comparedCases.Add(1)
	return ""
}

func requireOracle(t testing.TB) {
	t.Helper()
	if !oracle.Available() {
		if err := oracle.UnavailableReason(); err != nil && err != oracle.ErrDisabled {
			t.Fatalf("oracle unavailable: %v", err)
		}
		t.Skip("PARITY_ORACLE!=1")
	}
}

// runCases compares every input in parallel (the oracle pipelines concurrent calls) and reports the mismatches.
func runCases(t *testing.T, name string, inputs [][]uint16, lengths func(i int) []int) {
	t.Helper()
	workers := runtime.GOMAXPROCS(0) * 2
	var (
		mu       sync.Mutex
		failures []string
		next     atomic.Int64
		wg       sync.WaitGroup
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(inputs) {
					return
				}
				if msg := compare(inputs[i], lengths(i)); msg != "" {
					mu.Lock()
					if len(failures) < 20 {
						failures = append(failures, fmt.Sprintf("input %d: %s\n%s", i, show(inputs[i]), msg))
					}
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	for _, f := range failures {
		t.Error(f)
	}
	t.Logf("%s: %d inputs compared", name, len(inputs))
}

// ---------------------------------------------------------------------------------------------------------------
// (a) hand-written corpus

var corpus = []string{
	// plain text
	"", " ", "a", "hello world", "  leading and trailing  ", "line1\nline2\r\nline3\ttab",
	// plain text with characters the encoder rewrites
	"a & b", "1 < 2 > 0", "a=b+c@d`e", `say "hi" and 'bye'`, "{", "{{", "a{", "{a", "{{a}}", "a{{b}}c{", "x{ {y",
	"{{{{", "{<", "{&", "2 + 2 = 4 @ home",
	// well formed markup
	"<b>bold</b>", "<p>para</p><p>another", "<div><span>x</span></div>", "<a href=\"http://x\">link</a>", "text <br> more <br/> end",
	"<img src=x onerror=alert(1)>", "<IMG SRC=x>", "<B>CAPS</B>", "<Div>Mixed</dIV>",
	// unclosed / misnested
	"<b>unclosed", "<b><i>misnested</b></i>", "</b>stray close", "</p>", "<p>a<p>b<p>c", "<li>a<li>b", "<ul>x</ul>",
	"<ul><li>a</li></ul>", "<ol>text</ol>", "<table>text</table>", "<table><tr><td>a<td>b<tr><td>c</table>",
	"<table> <tr> <td> x </td> </tr> </table>", "<select>text<option>a<option>b</select>", "<select><optgroup><option>x",
	"<a>1<a>2</a>3</a>", "<a href=x><div>block in a</div></a>", "<b><p>block in inline</p></b>", "<p><b>x<p>y</b>z",
	"<h1>a<h2>b</h1>c", "<h3>x</h5>y", "<dl><dt>a<dd>b<dt>c</dl>", "<table><caption>c<col><colgroup><col></table>",
	"<tbody><tr><td>x", "<td>a</td>", "<tr>b", "<th>h", "<caption>cap", "<col>", "<option>o", "<optgroup>g",
	// attributes
	`<a title="x>y" href='z'>t</a>`, `<a title='he said "hi"'>t</a>`, `<a b=c d= e f="g" h>t</a>`, `<a b=c=d>t</a>`,
	`<a href=x/>t`, `<a href="x"/>t`, "<a\nhref\n=\nx>t", `<a b=">t`, `<a b='>t`, `<input checked disabled value=x>t`,
	`<a onclick=this.clicked=true title=foo bar>t`, `<a x=1 y=2 x=3>t`, `<a =>t`, `<a ="x">t`, `<a """>t`, `<a b="&amp;&lt;&#65;">t`,
	`<img alt="a&quot;b">t`, `<a b=c d>t`, `<a b=c checked>t`, `<a b=c  d= e>t`, `<a b= c d=>t`, "<a b=\"unterminated",
	"<a b='x\ny'>t", `<a/b>t`, `<a / b>t`, `<br/>`, `<br />x`, `<a b=/>x`, `<p/>x`,
	// raw text elements
	"<script>alert(1)</script>after", "before<script>alert(1)", "<script>a</script>b<script>c</script>d",
	"<SCRIPT>x</SCRIPT>y", "<script >x</script >y", "<script/>x</script>y", "<script>a<b>c</b>d</script>e",
	"<script>a</scr</script>b", "<script>x</script", "<script>x</script ", "<script>x</script y>z", "<script>1 < 2</script>3",
	"<script><!--x--></script>y", "<script>&lt;&amp;</script>y", "<script src=x></script>text",
	"<style>p{color:red}</style>text", "<style>a</style>b<style>", "<title>t &amp; u</title>after", "<title>x<b>y</b></title>z",
	"<title>unclosed", "<title></title>", "<textarea>a &amp; <b>b</b></textarea>c", "<textarea>unclosed <b>x",
	"<TEXTAREA>x</TEXTAREA>y", "<textArea>x</textArea>y", "<textarea>x</textarea >y", "<xmp><b>x</b></xmp>y", "<xmp>unclosed",
	"<plaintext>a</plaintext>b<b>c", "<plaintext>", "<PLAINTEXT>x", "<listing><b>x</b></listing>y", "<comment><b>x</b></comment>y",
	"<noscript>a</noscript>b", "<noscript>a<b>c</b>d</noscript>e", "<noscript>a</b>c", "<noscript>unclosed",
	"<iframe>a<b>c</b></iframe>d", "<iframe src=x>fallback</iframe>e", "<iframe>unclosed", "<noembed>a</noembed>b",
	"<noframes>a</noframes>b", "<nostyle>a</nostyle>b", "<object>a<param>b</object>c", "<object>x", "<frameset>a<frame>b</frameset>c",
	"<frame>x", "<embed>x", "<svg><title>x</title>y</svg>z", "<svg:script>x</svg:script>y", "<foreignObject>a</foreignObject>",
	"<math><mi>x</mi></math>", "<template>a</template>b", "<xcustom>a</xcustom>", "<my-element>a</my-element>b", "<x:y>a</x:y>b",
	"<script><script>a</script>b</script>c", "<title><script>a</script></title>b", "<style><title>a</title></style>b",
	"<script>a</SCRIPT>b", "<script>a</Script >b", "<script a=b>x</script>y", "<script\n>x</script>y", "<script/ >x</script>y",
	"<title>a</title>b</title>c", "</script>a", "</title>a", "<noscript></noscript>x", "<noscript><noscript>a</noscript>b</noscript>c",
	// comments, directives, processing instructions, CDATA
	"a<!-- c -->b", "a<!---->b", "a<!--->b", "a<!-->b", "a<!--x", "a<!--x-", "a<!--x--", "a<!-- x -- y -->b", "a<!-- x --!>b",
	"a<!DOCTYPE html>b", "a<!doctype html>b", "<!DOCTYPE", "<!>", "<!", "a<!x>b", "a<![CDATA[x<b>y]]>b", "a<![CDATA[x]]", "a<?xml version=\"1.0\"?>b",
	"a<?php echo 1; ?>b", "a<?x", "a<?>b", "<?", "a<%= x %>b", "a<% x %", "a<%", "a<%%>b", "a<% > %>b", "a<%x%>%>b",
	"<!-- a --><!-- b -->c", "<!--<b>-->d", "<!-- <script> -->d<script>x</script>", "<script><!-- x --></script>y",
	"<script><!x></script>y", "<script><?x></script>y", "<script><%x%></script>y", "<title><!--x--></title>y", "<title><?x></title>y",
	// '<' edge cases
	"<", "<<", "<<<", "a<", "a <", "< a", "<1>", "<-->", "<=>", "< b >", "a < b", "a<b", "a<b c", "a<b>c", "<3", "<>", "</>", "</", "</ ", "</ a>",
	"</3>", "</-->", "</é>x", "</a", "</a b>", "</a/>x", "<a<b>c", "<a<b", "<a\"b>c", "<a'b>c", "<a=b>c", "<é>x", "<日本>x", "<!<>", "<?<>", "<%<>",
	"<//>x", "<a//>x", "<a/ />x", "< / a>", "<\tp>x", "<\np>x", "<p\n>x", "<\u00a0p>x", "<p\u00a0q>x", "<p\u2003q>x", "<p\u2028q>x",
	"</é>x", "</न>x", "</ก>x",
	// entities
	"&amp;", "&lt;b&gt;", "&AMP;", "&Amp;", "&amp", "&ampx", "&amp;amp;", "&nbsp;", "&copy;", "&copy", "&COPY", "&Copy;", "&eacute;", "&Eacute;",
	"&eacute", "&para;", "&para", "&notin;", "&notit;", "&not", "&nota", "&lt", "&gt", "&quot;", "&apos;", "&#39;", "&#x27;", "&#X27;",
	"&#65;", "&#x41;", "&#0;", "&#x0;", "&#1;", "&#9;", "&#10;", "&#13;", "&#31;", "&#127;", "&#128;", "&#159;", "&#xD800;", "&#xDFFF;",
	"&#55357;&#56832;", "&#xFFFE;", "&#xFFFF;", "&#x10FFFF;", "&#x110000;", "&#1114112;", "&#99999999999;", "&#xFFFFFFFFFF;", "&#x1F600;",
	"&#128512;", "&#128512", "&#;", "&#x;", "&#", "&#x", "&", "&&", "&&&&amp;", "& amp;", "&;", "&#a;", "&#xg;", "&#12a;", "&#x1g;", "&#-1;", "&#+65;",
	"&#٣;", "&#x٣;", "&#xFF21;", "&#65;&#66;", "&#65&#66;", "&#65 ", "&#65<b>", "&amp<b>", "a&b", "a&b;c", "&abc;", "&aMP;", "&LT;", "&Lt;",
	"&CounterClockwiseContourIntegral;", "&CounterClockwiseContourIntegral", "&NotEqualTilde;", "&fjlig;", "&bne;", "&Aopf;", "&hearts;",
	"&hearts", "&ThickSpace;", "&nvlt;", "&acE;", "&amp&amp&amp", "&amp;&amp;&amp;", "&lt;script&gt;alert(1)&lt;/script&gt;",
	"&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a&a",
	"&#0000000000000000000000000000000000065;", "&#x000000000000000000000000000000000041;", "&#xFFFFFFFF;", "&#xFFFFFFFFF;", "&#x80000000;", "&#4294967296;",
	"&#2147483648;", "&#2147483647;", "&#xFFFF0041;", "&#x100000041;", "&#99999999999999999999;",
	"<a title=\"&amp;&para;&param=1\">t", "<a href=\"?a=1&para=2&notit;\">t", "<a title=&amp>t", "<a title=\"&#x41\">t",
	"<title>&amp;</title>x", "<textarea>&lt;b&gt;&amp;</textarea>x", "<script>&amp;</script>x", "<xmp>&amp;</xmp>x", "<iframe>&amp;</iframe>x",
	// control characters, NUL, banned code units
	"\x00", "a\x00b", "\x01\x02\x03", "a\x08b", "a\x0bb", "a\x0cb", "a\x1fb", "\x7f", "a\u0080b", "a\u009fb", "\ufffe", "\uffff", "a\ufffeb", "a\uffffb",
	"\ufdd0", "\ufdef", "\ufeff", "a\ufeffb", "﹟", "﹠", "﹡", "＜", "＞", "＂", "＇", "＆", "＜ script＞",
	"<script>\x00x\x01</script>", "<script>a\ufffe</script>b", "<xmp>a\x00b</xmp>", "<xmp>a\x00b\ufffeX</xmp>", "<plaintext>a\x00b", "<title>a\x00b</title>c",
	"<a\x00b>c", "<a b=\"\x00\">c", "<\x00a>b", "a\x00<b>c", "&#xFFFE;a", "&#x1;a", "\r", "\r\n", "\n\n\n", "\t\t", "x\ty", "\u000b", "\u000c",
	// whitespace
	"\u00a0", "a\u00a0b", "\u2003", "\u3000", "\u1680", "\u2028", "\u2029", "\u205f", "\u0085", "\u200b", "\u200c", "\u200d", "\u202f", "\u2007",
	"<b> </b>", "<table>  </table>", "<table> x </table>", "<ul> <li>a</li> </ul>", "<select> <option>a</option> </select>", "<tr> </tr>", "<p> </p>",
	"<table>\n<tr>\n<td>\na\n</td>\n</tr>\n</table>\n", "<ul>\n<li>a\n<li>b\n</ul>\n", "<table><tbody> </tbody></table>",
	// non BMP, emoji, RTL, combining, Indic
	"😀", "a😀b", "😀😀😀", "👨\u200d👩\u200d👧\u200d👦", "🇫🇷", "\U0001F600<b>\U0001F601</b>", "\U0010FFFF", "\U00010000", "\U0001d504", "é", "e\u0301", "ÉCOLOGIE à l'école",
	"שלום עולם", "مرحبا بالعالم", "\u202eabc\u202c", "\u200fabc", "<b>שלום</b>", "日本語のテキスト", "한국어", "ไทย", "Ελληνικά", "Привет",
	"क\u094dष", "क\u094d\u200cष", "क\u094d\u200cषा", "क\u200cा", "\u200cा", "a\u200cा", "\u200cक",
	"ক\u200cা", "ক\u200cৌ", "క\u200c\u0c3e", "క\u200c\u0c48", "క\u200c\u0c4c", "క\u200c\u0c4d", "क\u200c\u093a",
	"क\u200cह", "क\u200cॏ", "क\u200cॐ", "অ\u200c", "\u200cঅ", "ক\u200cৠ", "ক\u200c\u09e3", "ক\u200c\u09e4",
	"\u200c\x00ा", "\u200c\x01का", "\u200c&ा", "\u200c<b>ा", "\u200c<!--x-->ा</b>", "x\u200c<b></b>ा",
	"`", "a`b", "΅", "\u1ff0", "`", "``", "a`b",
	// combining with entities of the above
	"&#x200c;&#x93e;", "&#xfe60;", "&#x1fef;", "&#8175;", "&#xd83d;&#xde00;", "&#xd83d;", "&#xde00;", "&#xde00;&#xd83d;", "&#x1f600;&#x1f600;",
	// nesting
	strings.Repeat("<div>", 10), strings.Repeat("<div>", 255) + "x", strings.Repeat("<div>", 256) + "x", strings.Repeat("<div>", 257) + "x",
	strings.Repeat("<div>", 300) + "x" + strings.Repeat("</div>", 300) + "y", strings.Repeat("<b>", 300) + "x</b>y", strings.Repeat("<i><b>", 130) + "x",
	strings.Repeat("<span>", 255) + "<script>x</script>y", strings.Repeat("<table>", 70) + "x", strings.Repeat("<ul>", 100) + "x", strings.Repeat("<a>", 300) + "x",
	strings.Repeat("<p>", 300) + "x", strings.Repeat("<li>", 300) + "x", strings.Repeat("<td>", 300) + "x", strings.Repeat("<noscript>", 300) + "x",
	strings.Repeat("<noscript>x", 300), strings.Repeat("<div>x</div>", 300), strings.Repeat("<div>", 256) + "</div>x",
	// long inputs
	strings.Repeat("a", 10000), strings.Repeat("<b>x</b>", 5000), strings.Repeat("&amp;", 5000), strings.Repeat("{{", 5000), strings.Repeat("😀", 3000),
	strings.Repeat("<a b='", 1000), strings.Repeat("<!--", 2000), strings.Repeat("<", 5000), strings.Repeat("&", 5000), strings.Repeat("&#", 3000),
	strings.Repeat("<script>", 1000), strings.Repeat("a b c ", 10000), strings.Repeat("<p>é&eacute;क\u200cा😀{{", 500),
	// misc real-world looking
	"Bonjour, je m'appelle Jean & j'ai une <question> sur l'article 5 > 3.", "Pourquoi pas ? <script>alert('xss')</script> Parce que.",
	"<p>Pourquoi les <strong>impôts</strong> augmentent-ils&nbsp;?</p>", "Titre avec \"guillemets\" et 'apostrophes' + plus = égal @ arobase",
	"jean.dupont@example.org", "{{7*7}}", "${7*7}", "<img src=\"x\" onerror=\"alert(1)\">", "javascript:alert(1)", "<a href=\"javascript:alert(1)\">x</a>",
	"<svg onload=alert(1)>", "<body onload=alert(1)>", "<iframe src=javascript:alert(1)>", "<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>",
	"<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\">", "<form><math><mtext></form><form><mglyph><style></math><img src onerror=alert(1)>",
	"<a href=\"x\" onmouseover=\"alert(1)\">hover</a>", "\"><script>alert(1)</script>", "'><script>alert(1)</script>", "</textarea><script>alert(1)</script>",
	"</title><script>alert(1)</script>", "</style><script>alert(1)</script>", "<scr<script>ipt>alert(1)</scr</script>ipt>", "<<script>alert(1)//<</script>",
}

func TestOracleSanitizeCorpus(t *testing.T) {
	requireOracle(t)
	inputs := make([][]uint16, 0, len(corpus))
	for _, s := range corpus {
		inputs = append(inputs, units(s))
	}
	// lone surrogates and split pairs cannot be written as a Go string
	for _, u := range [][]uint16{
		{0xD800}, {0xDC00}, {0xDBFF, 0xDFFF}, {'a', 0xD800, 'b'}, {'a', 0xDC00, 'b'}, {0xDC00, 0xD800}, {0xD800, 0xD800, 0xDC00}, {0xD800, '<', 'b', '>', 0xDC00},
		{'<', 'a', ' ', 'b', '=', '"', 0xD800, '"', '>', 'x'}, {'<', 's', 'c', 'r', 'i', 'p', 't', '>', 0xD800, '<', '/', 's', 'c', 'r', 'i', 'p', 't', '>', 'y'},
		{'<', 'x', 'm', 'p', '>', 0xDC00, 0xD800, '<', '/', 'x', 'm', 'p', '>'}, {'&', 0xD800, ';'}, {'&', '#', 0xD800}, {'<', '/', 0xD800, '>'}, {'<', 0xD800, '>'},
		{0xD83D, 0xDE00, 0xDE00}, {0xD83D, 0xD83D, 0xDE00}, {'{', 0xD800, '{'}, {'{', 0xD800}, {0x200c, 0xD800, 0x93e}, {0xFFFE, 0xD800, 0xFFFF},
	} {
		inputs = append(inputs, u)
	}
	runCases(t, "corpus", inputs, func(int) []int { return append(append([]int(nil), maxLengths...), huge) })
}

// ---------------------------------------------------------------------------------------------------------------
// (b) seeded random generator

var (
	tagNames = []string{
		"a", "b", "i", "u", "p", "div", "span", "br", "hr", "img", "input", "ul", "ol", "li", "dl", "dt", "dd", "table", "thead", "tbody", "tfoot", "tr", "td", "th",
		"caption", "col", "colgroup", "select", "option", "optgroup", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "code", "em", "strong", "font", "center", "form",
		"button", "label", "script", "style", "title", "textarea", "xmp", "plaintext", "listing", "comment", "noscript", "noembed", "noframes", "nostyle",
		"iframe", "object", "frame", "frameset", "embed", "param", "svg", "math", "template", "html", "head", "body", "details", "summary", "marquee",
		"SCRIPT", "Script", "TITLE", "TextArea", "textArea", "NOSCRIPT", "IFRAME", "foreignObject", "clipPath", "feBlend", "svg:rect", "x:y", "my-el", "xcustom",
		"video", "audio", "canvas", "map", "ins", "del", "nobr", "blink", "bgsound", "wbr", "source", "track", "keygen", "command", "basefont", "isindex",
		"é", "日", "a-b", "a_b", "a.b", "a1", "1a", "", "scrİpt", "ſcript", "a\x00", "script\x00",
	}
	attrNames = []string{"href", "src", "title", "class", "id", "onclick", "checked", "disabled", "selected", "value", "style", "data-x", "x:y", "viewBox", "", "é"}
	entities  = []string{
		"&amp;", "&lt;", "&gt;", "&quot;", "&apos;", "&nbsp;", "&copy;", "&eacute;", "&Eacute;", "&#39;", "&#x27;", "&#65;", "&#x41;", "&#0;", "&#1;", "&#x7f;", "&#x80;",
		"&#xD800;", "&#xDFFF;", "&#55357;", "&#56832;", "&#xFFFE;", "&#xFFFF;", "&#x1F600;", "&#128512;", "&#x10FFFF;", "&#x110000;", "&#99999999999;", "&#xFFFFFFFFFF;",
		"&#;", "&#x;", "&#", "&", "&&", "&;", "&#a;", "&#12a;", "&amp", "&lt", "&gt", "&copy", "&para", "&para;", "&notin;", "&not", "&AMP", "&Amp;", "&LT;", "&aMP;",
		"&CounterClockwiseContourIntegral;", "&NotEqualTilde;", "&fjlig;", "&bne;", "&hearts;", "&ThickSpace;", "&#-1;", "&#+65;", "&#٣;", "&#x٣;", "&#xFF21;",
		"&#x200c;", "&#x93e;", "&#xfe60;", "&#x1fef;", "&#8175;", "&#xd83d;&#xde00;", "&zwnj;", "&shy;", "&#173;",
	}
	chunks = []string{
		"hello", " ", "  ", "\n", "\t", "\r\n", "world", "é", "à", "ç", "😀", "👍🏽", "日本", "שלום", "مرحبا", "क\u094d\u200cषा", "క\u200c\u0c3e",
		"{", "{{", "}}", "}", "+", "=", "@", "`", "\"", "'", "\x00", "\x01", "\x1f", "\x7f", "\u0080", "\u009f", "\ufffe", "\uffff", "\ufeff", "\u200c", "\u200d",
		"\u00a0", "\u2003", "\u2028", "\u3000", "`", "＜", "\ue000", "\ufdd0", "﹠", "\U0001F600", "\U0010FFFF",
		"a", "b", "1", "2", "Z", ".", ",", ";", ":", "?", "!", "-", "--", "_", "*", "/", "\\", "(", ")", "[", "]", "%", "#", "$", "~", "|", "^",
	}
	rawContents = []string{
		"alert(1)", "a<b>c</b>d", "1 < 2 && 3 > 2", "&amp; &lt; &#65;", "<!-- x -->", "</scr", "</", "<", "x\x00y", "{{x}}", "😀", "", " ", "\n",
		"</SCRIPT", "</script", "</script ", "</script/", "</script\t", "</scriptx>", "<%x%>", "<?x>", "<!x>",
	}
	structural = []string{"<", ">", "/", "=", "\"", "'", "&", ";", "!", "-", "--", "?", "%", "#", " ", "\n", "a", "b", "x", "0", "[", "]", "CDATA", "script", "style", "title", "{", "\x00", "é"}
)

func pick[T any](r *rand.Rand, s []T) T { return s[r.Intn(len(s))] }

func genAttrs(r *rand.Rand, sb *[]uint16) {
	n := r.Intn(4)
	for i := 0; i < n; i++ {
		ws := pick(r, []string{" ", " ", "  ", "\n", "\t", "\u00a0", "\u2003"})
		*sb = append(*sb, units(ws)...)
		*sb = append(*sb, units(pick(r, attrNames))...)
		switch r.Intn(7) {
		case 0:
		case 1:
			*sb = append(*sb, units("=")...)
		case 2:
			*sb = append(*sb, units("="+pick(r, chunks))...)
		case 3:
			*sb = append(*sb, units(`="`+pick(r, chunks)+pick(r, entities)+`"`)...)
		case 4:
			*sb = append(*sb, units(`='`+pick(r, chunks)+`'`)...)
		case 5:
			*sb = append(*sb, units(" = "+pick(r, structural))...)
		case 6:
			*sb = append(*sb, units(`="unterminated`)...)
		}
	}
}

func genTag(r *rand.Rand, sb *[]uint16) {
	name := pick(r, tagNames)
	closing := r.Intn(4) == 0
	*sb = append(*sb, '<')
	if closing {
		*sb = append(*sb, '/')
	}
	*sb = append(*sb, units(name)...)
	if !closing {
		genAttrs(r, sb)
	}
	switch r.Intn(10) {
	case 0:
		*sb = append(*sb, units(" /")...)
	case 1:
		*sb = append(*sb, '/')
	case 2:
	// unterminated tag
	default:
	}
	if r.Intn(12) != 0 {
		*sb = append(*sb, '>')
	}
}

func genFragment(r *rand.Rand, sb *[]uint16, depth int) {
	switch k := r.Intn(30); {
	case k < 8:
		*sb = append(*sb, units(pick(r, chunks))...)
	case k < 14:
		genTag(r, sb)
	case k < 17:
		*sb = append(*sb, units(pick(r, entities))...)
	case k < 19:
		*sb = append(*sb, units(pick(r, []string{"<!-- ", "<!--", "<!---->", "-->", "<!DOCTYPE html>", "<!doctype", "<?xml version=\"1.0\"?>", "<?php ", "?>", "<![CDATA[", "]]>", "<%", "%>", "<% x %>", "<!", "<?"}))...)
		*sb = append(*sb, units(pick(r, rawContents))...)
	case k < 22:
		// raw text element with content
		name := pick(r, []string{"script", "style", "title", "textarea", "xmp", "plaintext", "listing", "comment", "noscript", "iframe", "noembed", "noframes", "SCRIPT", "textArea"})
		*sb = append(*sb, units("<"+name+">")...)
		*sb = append(*sb, units(pick(r, rawContents))...)
		if r.Intn(4) != 0 {
			*sb = append(*sb, units(pick(r, []string{"</" + name + ">", "</" + name + " >", "</" + strings.ToUpper(name) + ">", "</" + name, "</" + name + "/>"}))...)
		}
	case k < 24:
		*sb = append(*sb, units(pick(r, structural))...)
		*sb = append(*sb, units(pick(r, structural))...)
	case k < 26:
		// surrogates: valid pairs, lone high, lone low, reversed
		switch r.Intn(7) {
		case 5, 6:
			// A supplementary code point whose low 16 bits are a surrogate: Spring's (char) cast of the numeric
			// reference the encoder writes turns it into a lone surrogate (or, side by side, into a valid pair).
			cp := rune(1+r.Intn(16))<<16 | rune(0xD800+r.Intn(0x800))
			hi, lo := utf16.EncodeRune(cp)
			*sb = append(*sb, uint16(hi), uint16(lo))
		case 0:
			*sb = append(*sb, 0xD83D, 0xDE00)
		case 1:
			*sb = append(*sb, 0xD800+uint16(r.Intn(0x400)))
		case 2:
			*sb = append(*sb, 0xDC00+uint16(r.Intn(0x400)))
		case 3:
			*sb = append(*sb, 0xDC00+uint16(r.Intn(0x400)), 0xD800+uint16(r.Intn(0x400)))
		case 4:
			*sb = append(*sb, 0xD800+uint16(r.Intn(0x400)), 0xDC00+uint16(r.Intn(0x400)))
		}
	case k < 28:
		// random code unit anywhere in the BMP (including the specials the encoder looks at)
		var c uint16
		switch r.Intn(4) {
		case 0:
			c = uint16(r.Intn(0x10000))
		case 1:
			c = uint16(0x0900 + r.Intn(0x400)) // Indic block
		case 2:
			c = uint16(0xFE00 + r.Intn(0x200))
		case 3:
			c = uint16(r.Intn(0x100))
		}
		*sb = append(*sb, c)
	default:
		if depth < 3 {
			// nesting: <name> fragments </name>
			name := pick(r, tagNames[:60])
			*sb = append(*sb, units("<"+name+">")...)
			for i, n := 0, r.Intn(4); i < n; i++ {
				genFragment(r, sb, depth+1)
			}
			if r.Intn(3) != 0 {
				*sb = append(*sb, units("</"+name+">")...)
			}
		}
	}
}

// genCase builds one HTML-ish input.
func genCase(r *rand.Rand) []uint16 {
	var sb []uint16
	switch mode := r.Intn(10); {
	case mode < 6: // structured fragments
		for i, n := 0, 1+r.Intn(14); i < n; i++ {
			genFragment(r, &sb, 0)
		}
	case mode < 8: // dense syntax characters
		for i, n := 0, 1+r.Intn(24); i < n; i++ {
			sb = append(sb, units(pick(r, structural))...)
		}
	case mode < 9: // mutated corpus entry
		src := units(pick(r, corpus))
		if len(src) > 400 {
			src = src[:400]
		}
		sb = append(sb, src...)
		for i, n := 0, 1+r.Intn(4); i < n && len(sb) > 0; i++ {
			p := r.Intn(len(sb))
			switch r.Intn(4) {
			case 0:
				sb = append(sb[:p], sb[p+1:]...)
			case 1:
				ins := units(pick(r, structural))
				sb = append(sb[:p], append(ins, sb[p:]...)...)
			case 2:
				sb[p] = uint16(r.Intn(0x10000))
			case 3:
				q := r.Intn(len(sb))
				sb[p], sb[q] = sb[q], sb[p]
			}
		}
	default: // deep nesting and long runs
		name := pick(r, []string{"div", "b", "i", "span", "a", "p", "li", "td", "table", "ul", "noscript", "script", "font", "h1"})
		depth := 240 + r.Intn(40)
		for i := 0; i < depth; i++ {
			sb = append(sb, units("<"+name+">")...)
			if r.Intn(40) == 0 {
				sb = append(sb, units(pick(r, chunks))...)
			}
		}
		for i, n := 0, 1+r.Intn(6); i < n; i++ {
			genFragment(r, &sb, 0)
		}
		if r.Intn(2) == 0 {
			sb = append(sb, units(strings.Repeat("</"+name+">", r.Intn(depth+1)))...)
			genFragment(r, &sb, 0)
		}
	}
	return sb
}

func randomCases() int {
	if v := os.Getenv("SANITIZE_RANDOM_CASES"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	if testing.Short() {
		return 5000
	}
	return 120000
}

func TestOracleSanitizeRandom(t *testing.T) {
	requireOracle(t)
	seed := int64(20220608)
	if v := os.Getenv("SANITIZE_RANDOM_SEED"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			seed = n
		}
	}
	n := randomCases()
	r := rand.New(rand.NewSource(seed))
	inputs := make([][]uint16, n)
	lens := make([][]int, n)
	for i := range inputs {
		inputs[i] = genCase(r)
		// Every input is compared with a few limits, the whole range of them over the run.
		lens[i] = []int{maxLengths[r.Intn(len(maxLengths))], huge}
		if i%7 == 0 {
			lens[i] = append(lens[i], maxLengths...)
		}
	}
	runCases(t, fmt.Sprintf("random(seed %d)", seed), inputs, func(i int) []int { return lens[i] })
}

// TestOracleHtmlUnescapeRandom compares the Spring stage alone on arbitrary strings (the OWASP output only contains
// the references the encoder writes, so the decoder's other paths need direct input).
func TestOracleHtmlUnescapeRandom(t *testing.T) {
	requireOracle(t)
	n := randomCases()
	r := rand.New(rand.NewSource(7))
	springPieces := []string{
		"&", "#", "x", "X", ";", "amp", "lt", "gt", "quot", "nbsp", "eacute", "euro", "hearts", "#39", "0", "1", "65", "FF", "ff", "g", "-", "+", " ", "a", "é", "٣", "Ａ",
		"2147483647", "2147483648", "4294967297", "-2147483648", "-2147483649", "7FFFFFFF", "80000000", "FFFFFFFF", "100000000", "65535", "65536", "1114112", "55357", "56832", "😀",
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	var next atomic.Int64
	workers := runtime.GOMAXPROCS(0) * 2
	inputs := make([][]uint16, n)
	for i := range inputs {
		var u []uint16
		for j, k := 0, 1+r.Intn(30); j < k; j++ {
			switch r.Intn(6) {
			case 0:
				u = append(u, uint16(r.Intn(0x10000)))
			case 1:
				u = append(u, units(pick(r, entities))...)
			default:
				u = append(u, units(pick(r, springPieces))...)
			}
		}
		inputs[i] = u
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				var res oracle.Text
				if err := oracle.Call("htmlUnescape", m{"s": oracle.Utf16(inputs[i])}, &res); err != nil {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("oracle error on %s: %v", show(inputs[i]), err))
					mu.Unlock()
					continue
				}
				if got := sanitize.HTMLUnescape(inputs[i]); !eq16(got, res.Utf16) {
					mu.Lock()
					if len(failures) < 20 {
						failures = append(failures, fmt.Sprintf("htmlUnescape mismatch on %s\n  go:     %s\n  oracle: %s", show(inputs[i]), show(got), show(res.Utf16)))
					}
					mu.Unlock()
				}
				comparedCases.Add(1)
			}
		}()
	}
	wg.Wait()
	for _, f := range failures {
		t.Error(f)
	}
	t.Logf("htmlUnescape: %d inputs compared", n)
}

// htmlUnescapeCorpus are tricky inputs of the Spring decoder.
var htmlUnescapeCorpus = []string{
	"", "&", "&;", "&#;", "&#x;", "&#X;", "&amp;", "&amp", "&amp;&amp;", "&&amp;", "&amp;&", "a&amp;b", "&lt;&gt;&quot;&#39;&nbsp;", "&#39;", "&#x27;", "&#X27;",
	"&#65;", "&#065;", "&#+65;", "&#-65;", "&#+;", "&#-;", "&#--65;", "&#++65;", "&#x+41;", "&#x-41;", "&#x41;", "&#xdeadbeef;", "&#xDEADBEEF;", "&#x0000041;",
	"&#2147483647;", "&#2147483648;", "&#-2147483648;", "&#-2147483649;", "&#4294967295;", "&#4294967296;", "&#4294967361;", "&#-4294967231;", "&#99999999999;",
	"&#x7fffffff;", "&#x80000000;", "&#xffffffff;", "&#x100000000;", "&#x-80000000;", "&#x-80000001;", "&#x1F600;", "&#128512;", "&#65536;", "&#65535;", "&#0;", "&#x0;",
	"&#55357;&#56832;", "&#xD83D;&#xDE00;", "&#xD83D;", "&#56832;", "&#xFFFF;", "&#xFFFE;", "&#-1;", "&#-65535;", "&#-65536;", "&#-65537;",
	"&#٣٥;", "&#x٣٥;", "&#１２;", "&#xＡＢ;", "&#xａ;", "&#๑;", "&# 65;", "&#65 ;", "&#6 5;", "&#\u00a065;", "&#x4g;", "&#12a;",
	"&nbsp;&iexcl;&euro;&hearts;&Alpha;&alpha;&OElig;&ndash;&hellip;", "&NBSP;", "&Nbsp;", "&nbsp ;", "& nbsp;", "&nbsp;;", "&nbsp&nbsp;", "&#39", "&#39 ;",
	"&bull;&bullet;", "&image;&real;&weierp;", "&lang;&rang;&lt;&gt;", "&sup2;&sup3;&sup1;", "&quot", "&QUOT;", "&amp;amp;", "&amp;#65;", "&#38;amp;", "&#38;#65;",
	"a&b;c", "a&bcdefghij;c", "a&bcdefghijk;c", "a&bcdefghijkl;c", "&aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa;", "&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&&;",
	"&amp; &amp; &amp; &amp; &amp; &amp; &amp; &amp; &amp; &amp; &amp; &amp;", "x&y&z&amp;", "&a&b&c&d&e&f&g&h&i&j&k&l&m&n&o&p&q&r&s&t&u&v&w&x&y&z;", "&aaaaaaaaaa;&amp;",
	"&aaaaaaaaa;&amp;", "&aaaaaaaa;&amp;", "&1234567890;&amp;", "&123456789;&amp;", "&#123456789;&amp;", "&#1234567890;&amp;", "&#12345678901;&amp;",
	"text without any reference but a ; semicolon", "a;b&c;d", ";&;", ";;;&amp;;;;", "&;;;;;;;;;;;;;;;;;;;;;;;;;;;;;;;;;&amp;", "&" + strings.Repeat("a", 100) + ";&lt;",
	strings.Repeat("&amp;", 1000), strings.Repeat("&", 1000) + ";", strings.Repeat("&#", 1000) + ";", strings.Repeat("x", 1000) + "&amp;" + strings.Repeat("y", 1000),
	"&" + strings.Repeat("x", 9) + ";", "&" + strings.Repeat("x", 10) + ";", "&" + strings.Repeat("x", 8) + ";", "&#" + strings.Repeat("1", 8) + ";", "&#" + strings.Repeat("1", 9) + ";",
	"&#" + strings.Repeat("1", 10) + ";", "&#x" + strings.Repeat("1", 7) + ";", "&#x" + strings.Repeat("1", 8) + ";", "&#x" + strings.Repeat("1", 9) + ";",
	"\\u0000 \\x00 &#92;", "\x00&amp;\x00", "\ud7ff&#xd7ff;", "\U0001F600&#128512;&amp;", "&\U0001F600;", "&#\U0001F600;",
}

func TestOracleHtmlUnescapeCorpus(t *testing.T) {
	requireOracle(t)
	for _, s := range htmlUnescapeCorpus {
		in := units(s)
		var res oracle.Text
		oracle.MustCall(t, "htmlUnescape", m{"s": oracle.Utf16(in)}, &res)
		if got := sanitize.HTMLUnescape(in); !eq16(got, res.Utf16) {
			t.Errorf("htmlUnescape mismatch on %s\n  go:     %s\n  oracle: %s", show(in), show(got), show(res.Utf16))
		}
		comparedCases.Add(1)
	}
}

// Every supplementary code point whose low 16 bits are a surrogate: Spring's (char) cast turns the numeric reference
// the encoder writes for it (&#x1d800;) into a lone surrogate, and two of them side by side into a valid pair that
// take(n) can then split.
func TestOracleSupplementaryWithSurrogateLowBits(t *testing.T) {
	requireOracle(t)
	var inputs [][]uint16
	enc := func(cp rune) []uint16 {
		hi, lo := utf16.EncodeRune(cp)
		return []uint16{uint16(hi), uint16(lo)}
	}
	r := rand.New(rand.NewSource(3))
	for plane := rune(1); plane <= 16; plane++ {
		for u := rune(0xD800); u <= 0xDFFF; u++ {
			cp := plane<<16 | u
			if cp > 0x10FFFF {
				continue
			}
			inputs = append(inputs, enc(cp))
			// a high-surrogate lookalike followed by a low-surrogate lookalike forms a valid pair in the output
			if u < 0xDC00 {
				low := enc(rune(1+r.Intn(16))<<16 | rune(0xDC00+r.Intn(0x400)))
				inputs = append(inputs, append(enc(cp), low...))
				inputs = append(inputs, append(append([]uint16{'a', '<', 'b', '>'}, enc(cp)...), append(low, 'z')...))
			}
		}
	}
	runCases(t, "supplementary with surrogate low bits", inputs, func(int) []int { return []int{0, 1, 2, 3, huge} })
}

// ---------------------------------------------------------------------------------------------------------------
// Every UTF-16 code unit in the contexts where a class test of the lexer/encoder decides (Character.isLetter,
// isWhitespace, the Indic and full-width special cases, ...).

func TestOracleEveryCodeUnit(t *testing.T) {
	requireOracle(t)
	contexts := []func(c uint16) []uint16{
		func(c uint16) []uint16 { return []uint16{'a', c, 'b'} },
		func(c uint16) []uint16 { return append(units("a</"), c, 'x', '>', 'b') },          // isLetter after "</"
		func(c uint16) []uint16 { return append(units("<a"), c, 'b', '>', 'c') },           // tag name terminators
		func(c uint16) []uint16 { return append(units("<a"), c, 'b', '=', 'c', '>', 'd') }, // whitespace in a tag
		func(c uint16) []uint16 { return append(units("<a b="), c, 'c', ' ', 'd', '>', 'e') },
		func(c uint16) []uint16 {
			return append(units("<script>"), c, '<', '/', 's', 'c', 'r', 'i', 'p', 't', '>', 'x')
		},
		func(c uint16) []uint16 {
			return append(units("<title>"), c, '<', '/', 't', 'i', 't', 'l', 'e', '>', 'x')
		},
		func(c uint16) []uint16 { return append(units("&#"), c, '1', ';') }, // digits accepted by parseInt after &amp;
		func(c uint16) []uint16 { return append(units("&amp;#"), c, '1', ';', 'z') },
		func(c uint16) []uint16 { return append(units("&amp;#x"), c, '1', ';', 'z') },
		func(c uint16) []uint16 { return []uint16{0x0915, 0x200c, c, 0x0915} },
		func(c uint16) []uint16 { return []uint16{0x200c, c} },
		func(c uint16) []uint16 { return []uint16{'<', 'b', '>', 0x200c, '<', '/', 'b', '>', c} },
		func(c uint16) []uint16 { return []uint16{0xD800, c} },
		func(c uint16) []uint16 { return []uint16{c, 0xDC00} },
	}
	var inputs [][]uint16
	for c := 0; c < 0x10000; c++ {
		for _, ctx := range contexts {
			inputs = append(inputs, ctx(uint16(c)))
		}
	}
	runCases(t, "every code unit", inputs, func(int) []int { return []int{huge} })
}

// ---------------------------------------------------------------------------------------------------------------
// (c) native fuzzing against the oracle

func FuzzSanitize(f *testing.F) {
	for _, s := range corpus {
		if len(s) <= 2000 {
			f.Add(s, uint8(5))
		}
	}
	f.Add("a</é>b", uint8(1))
	f.Fuzz(func(t *testing.T, s string, lenSel uint8) {
		if os.Getenv("PARITY_ORACLE") != "1" {
			t.Skip("PARITY_ORACLE!=1")
		}
		requireOracle(t)
		if !utf8.ValidString(s) {
			// invalid UTF-8 becomes U+FFFD on both sides the same way a JSON/HTTP decoder would: keep the input valid
			s = strings.ToValidUTF8(s, "�")
		}
		content := units(s)
		lengths := []int{maxLengths[int(lenSel)%len(maxLengths)], huge}
		if msg := compare(content, lengths); msg != "" {
			t.Fatalf("%s\ninput: %s", msg, show(content))
		}
	})
}

// TestOracleCompared logs the total number of comparisons of the run (run last: tests run in source order).
func TestOracleZZCompared(t *testing.T) {
	requireOracle(t)
	t.Logf("oracle comparisons in this run: %d", comparedCases.Load())
}
