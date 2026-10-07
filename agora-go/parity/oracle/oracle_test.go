package oracle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Smoke tests of every oracle function. They are skipped unless
// PARITY_ORACLE=1:  PARITY_ORACLE=1 go test ./parity/oracle/ -v -count=1

func TestMain(m *testing.M) {
	code := m.Run()
	Shutdown()
	os.Exit(code)
}

type m = map[string]any

func call[T any](t *testing.T, fn string, args any) T {
	t.Helper()
	var out T
	MustCall(t, fn, args, &out)
	return out
}

func wantEq[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

// envVar reads a variable the way the JVM sees it: process env first, then common.env.
func envVar(t *testing.T, key string) string {
	t.Helper()
	skipOrFail(t)
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	vars, _, err := parseEnvFile(os.Getenv("PARITY_ORACLE_ENV_FILE"))
	if err != nil {
		vars, _, err = parseEnvFile(oracleDir + "/../env/common.env")
	}
	if err != nil {
		t.Fatalf("cannot read env file: %v", err)
	}
	v, ok := vars[key]
	if !ok {
		t.Fatalf("%s is neither in the environment nor in common.env", key)
	}
	return v
}

func TestAvailability(t *testing.T) {
	if !enabled() {
		if Available() {
			t.Fatal("Available() must be false without PARITY_ORACLE=1")
		}
		if !errors.Is(UnavailableReason(), ErrDisabled) {
			t.Fatalf("UnavailableReason = %v", UnavailableReason())
		}
		if err := Call("ping", nil, nil); !errors.Is(err, ErrDisabled) {
			t.Fatalf("Call without PARITY_ORACLE=1: %v", err)
		}
		t.Skip("JVM oracle disabled (set PARITY_ORACLE=1)")
	}
	if !Available() {
		t.Fatalf("Available() = false: %v", UnavailableReason())
	}
}

func TestPingAndFunctions(t *testing.T) {
	wantEq(t, "ping", call[string](t, "ping", nil), "pong")
	fns := call[[]string](t, "functions", nil)
	for _, want := range []string{
		"sanitize", "sanitizeRaw", "htmlUnescape", "richTextToHtml", "uuidFromString", "kotlinToIntOrNull",
		"kotlinTrim", "kotlinIsBlank", "containsIgnoreCase", "replaceDiacritics", "urlEncode", "isoDateTime",
		"doubleToString", "mathRound", "numberFormat", "regexReplace", "jsonRoundTrip", "jsonDecode",
		"xmlSerialize", "xmlSerializeValue", "loginTokenEncode", "loginTokenDecode", "ipHash", "jwtGenerate",
		"jwtParse", "aesGcmEncrypt", "aesGcmDecrypt", "javaStringLength", "kotlinTake",
	} {
		found := false
		for _, f := range fns {
			found = found || f == want
		}
		if !found {
			t.Errorf("function %q is not registered", want)
		}
	}
}

func TestErrors(t *testing.T) {
	oe := MustFail(t, "nope", nil)
	wantEq(t, "unknown fn class", oe.Class(), "IllegalArgumentException")
	if !strings.Contains(oe.Message(), "unknown function") {
		t.Errorf("message = %q", oe.Message())
	}
	oe = MustFail(t, "uuidFromString", m{}) // missing argument
	wantEq(t, "missing arg class", oe.Class(), "IllegalArgumentException")
	oe = MustFail(t, "kotlinTake", m{"s": "abc", "n": -1})
	wantEq(t, "take(-1) class", oe.Class(), "IllegalArgumentException")
	wantEq(t, "take(-1) fqcn", oe.Exception, "java.lang.IllegalArgumentException")
	// Call returns an *OracleError the caller can inspect.
	var out any
	err := Call("uuidFromString", m{"s": "zz"}, &out)
	var got *OracleError
	if !errors.As(err, &got) || got.Msg != "IllegalArgumentException: Invalid UUID string: zz" {
		t.Errorf("Call error = %v", err)
	}
}

func TestSanitize(t *testing.T) {
	// The OWASP policy encodes the emoji as &#x1f600;; Spring 6.0.14's HtmlUtils.htmlUnescape turns a
	// numeric reference above U+FFFF into (char) 0x1F600 == U+F600: a quirk the Go port must reproduce.
	r := call[Text](t, "sanitize", m{"content": "<script>x</script>a&amp;b <b>c</b>😀", "maxLength": 10})
	wantEq(t, "sanitize text", r.Text, "a&b c")
	wantEq(t, "sanitize utf16", r.Utf16, []uint16{'a', '&', 'b', ' ', 'c', 0xf600})
	wantEq(t, "units match text", r.Utf16, Units(r.Text))

	// take(maxLength) cuts after a UTF-16 unit.
	r = call[Text](t, "sanitize", m{"content": "héllo wörld", "maxLength": 4})
	wantEq(t, "sanitize take", r.Text, "héll")
	r = call[Text](t, "sanitize", m{"content": "a < b && c > d", "maxLength": 100})
	wantEq(t, "sanitize lt/gt", r.Text, "a < b && c > d")

	raw := call[Text](t, "sanitizeRaw", m{"content": "<script>x</script>a&amp;b <b>c</b>😀"})
	wantEq(t, "sanitizeRaw", raw.Text, "a&amp;b c&#x1f600;")

	wantEq(t, "negative maxLength", MustFail(t, "sanitize", m{"content": "a", "maxLength": -1}).Class(), "IllegalArgumentException")
}

func TestHtmlUnescape(t *testing.T) {
	r := call[Text](t, "htmlUnescape", m{"s": "&lt;a&gt; &amp;amp; &eacute; &#233; &#x1F600; &bogus; &#xZZ;"})
	wantEq(t, "htmlUnescape", r.Text, "<a> &amp; é é  &bogus; &#xZZ;")
	r = call[Text](t, "htmlUnescape", m{"s": "&#128512;"})
	wantEq(t, "numeric ref > U+FFFF", r.Utf16, []uint16{0xf600})
}

func TestLoneSurrogates(t *testing.T) {
	// Lone surrogate sent as input (Utf16) and returned with "utf16":true.
	r := call[Text](t, "kotlinTrim", m{"s": Utf16{0xd800, ' ', 'x', ' ', '\t'}, "utf16": true})
	wantEq(t, "trim keeps lone surrogate", r.Utf16, []uint16{0xd800, ' ', 'x'})
	wantEq(t, "length", call[int](t, "javaStringLength", m{"s": Utf16{0xdc00, 0xd800}}), 2)

	// kotlinTake splits a surrogate pair.
	r = call[Text](t, "kotlinTake", m{"s": "a😀b", "n": 2})
	wantEq(t, "take splits pair", r.Utf16, []uint16{'a', 0xd83d})
	r = call[Text](t, "kotlinTake", m{"s": "abc", "n": 10})
	wantEq(t, "take beyond", r.Text, "abc")
	wantEq(t, "take 0", call[Text](t, "kotlinTake", m{"s": "abc", "n": 0}).Text, "")
}

func TestJavaStringLength(t *testing.T) {
	wantEq(t, "ascii", call[int](t, "javaStringLength", m{"s": "abc"}), 3)
	wantEq(t, "emoji", call[int](t, "javaStringLength", m{"s": "😀"}), 2)
	wantEq(t, "combining", call[int](t, "javaStringLength", m{"s": "é"}), 2)
	wantEq(t, "empty", call[int](t, "javaStringLength", m{"s": ""}), 0)
}

func TestRichTextToHtml(t *testing.T) {
	blocks := `[{"type":"paragraph","children":[` +
		`{"type":"text","text":"Hello ","bold":true,"italic":true},` +
		`{"type":"link","url":"http://x","children":[{"type":"text","text":"l","underline":true}]}]},` +
		`{"type":"heading","level":2,"children":[{"type":"text","text":"T","strikethrough":true,"code":true}]},` +
		`{"type":"heading","level":9,"children":[{"type":"text","text":"H9"}]},` +
		`{"type":"list","format":"ordered","children":[{"type":"list-item","children":[{"type":"text","text":"i"}]}]},` +
		`{"type":"quote","children":[{"type":"text","text":"q"}],"unknownField":1},` +
		`{"type":"weird","children":[{"type":"text","text":"w"}]}]`
	got := call[string](t, "richTextToHtml", m{"json": blocks})
	wantEq(t, "richTextToHtml", got,
		`<p><i><b>Hello </b></i><a href="http://x"><u>l</u></a></p>`+
			`<h2><code><del>T</del></code></h2>H9<ol><li>i</li></ol><blockquote>q</blockquote>w`)
	wantEq(t, "body", call[string](t, "richTextToHtmlBody", m{"json": `[]`}), "<body></body>")

	wantEq(t, "not an array", MustFail(t, "richTextToHtml", m{"json": `{"a":1}`}).Class(), "MismatchedInputException")
	wantEq(t, "truncated json", MustFail(t, "richTextToHtml", m{"json": `[{`}).Class(), "JsonMappingException")
	wantEq(t, "bad json", MustFail(t, "richTextToHtml", m{"json": `[`}).Class(), "JsonEOFException")
	wantEq(t, "missing text", MustFail(t, "richTextToHtml", m{"json": `[{"type":"text"}]`}).Class(), "MissingKotlinParameterException")
}

func TestUUIDFromString(t *testing.T) {
	wantEq(t, "short form", call[string](t, "uuidFromString", m{"s": "1-1-1-1-1"}), "00000001-0001-0001-0001-000000000001")
	wantEq(t, "upper", call[string](t, "uuidFromString", m{"s": "ABCDEF01-2345-6789-ABCD-EF0123456789"}), "abcdef01-2345-6789-abcd-ef0123456789")
	// java.util.UUID.fromString is lenient with signs and 0x: "-1-1-1-1-1" is accepted in JDK 17.
	for bad, class := range map[string]string{
		"": "IllegalArgumentException", "xyz": "IllegalArgumentException", "1-1-1-1": "IllegalArgumentException",
		"1-1-1-1-1-1": "IllegalArgumentException", "g-1-1-1-1": "NumberFormatException",
	} {
		wantEq(t, "invalid "+bad, MustFail(t, "uuidFromString", m{"s": bad}).Class(), class)
	}
}

func TestKotlinToIntOrNull(t *testing.T) {
	for in, want := range map[string]*int{
		"12": ptr(12), "-5": ptr(-5), "+7": ptr(7), "007": ptr(7), "-0": ptr(0), "2147483647": ptr(2147483647),
		"2147483648": nil, "x": nil, "": nil, " 5": nil, "5 ": nil, "1.0": nil, "٣": ptr(3), "+": nil, "-": nil,
	} {
		wantEq(t, fmt.Sprintf("toIntOrNull(%q)", in), call[*int](t, "kotlinToIntOrNull", m{"s": in}), want)
	}
	wantEq(t, "toLongOrNull", call[*int64](t, "kotlinToLongOrNull", m{"s": "9223372036854775807"}), ptr(int64(9223372036854775807)))
	wantEq(t, "toLongOrNull overflow", call[*int64](t, "kotlinToLongOrNull", m{"s": "9223372036854775808"}), nil)
	d := call[*struct {
		Bits uint64 `json:"bits"`
		Text string `json:"text"`
	}](t, "kotlinToDoubleOrNull", m{"s": "1e3"})
	wantEq(t, "toDoubleOrNull", d.Text, "1000.0")
}

func ptr[T any](v T) *T { return &v }

func TestKotlinTrimAndIsBlank(t *testing.T) {
	for in, want := range map[string]string{
		"  a b  ": "a b", " x ": "x", "​x": "​x", "\x1f x \x1c": "x", "\u0085x": "\u0085x", "": "",
	} {
		wantEq(t, fmt.Sprintf("trim(%q)", in), call[string](t, "kotlinTrim", m{"s": in}), want)
	}
	for in, want := range map[string]bool{"": true, " \t\n": true, " ": true, " ": true, "​": false, " a ": false} {
		wantEq(t, fmt.Sprintf("isBlank(%q)", in), call[bool](t, "kotlinIsBlank", m{"s": in}), want)
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	for _, c := range []struct {
		s, other string
		want     bool
	}{
		{"Hello", "ELL", true}, {"Hello", "", true}, {"", "a", false}, {"STRASSE", "straße", false},
		{"Éléphant", "éLÉ", true}, {"İstanbul", "i", true}, {"ǅ", "ǆ", true}, {"abc", "abcd", false},
	} {
		wantEq(t, fmt.Sprintf("contains(%q,%q)", c.s, c.other),
			call[bool](t, "containsIgnoreCase", m{"s": c.s, "other": c.other}), c.want)
	}
}

func TestReplaceDiacritics(t *testing.T) {
	wantEq(t, "diacritics", call[string](t, "replaceDiacritics", m{"s": "Éléphant à l'été, ça va? ŒuvreÆ ß ł"}), "Elephant a l'ete, ca va? ŒuvreÆ ß ł")
	wantEq(t, "already NFD", call[string](t, "replaceDiacritics", m{"s": "éé"}), "ee")
}

func TestURLEncode(t *testing.T) {
	wantEq(t, "urlEncode", call[string](t, "urlEncode", m{"s": "a b&c/é~*-_.😀"}), "a+b%26c%2F%C3%A9%7E*-_.%F0%9F%98%80")
	wantEq(t, "lone surrogate", call[string](t, "urlEncode", m{"s": Utf16{'a', 0xd800}}), "a%3F")
}

func TestIsoDateTime(t *testing.T) {
	arg := func(y, mo, d, h, mi, s, n int) m {
		return m{"year": y, "month": mo, "day": d, "hour": h, "minute": mi, "second": s, "nano": n}
	}
	wantEq(t, "seconds always printed", call[string](t, "isoDateTime", arg(2020, 1, 2, 3, 4, 0, 0)), "2020-01-02T03:04:00")
	wantEq(t, "millis", call[string](t, "isoDateTime", arg(2020, 1, 2, 3, 4, 5, 120000000)), "2020-01-02T03:04:05.12")
	wantEq(t, "nanos", call[string](t, "isoDateTime", arg(2020, 12, 31, 23, 59, 59, 1)), "2020-12-31T23:59:59.000000001")
	wantEq(t, "leap day", call[string](t, "isoDateTime", arg(2024, 2, 29, 0, 0, 0, 0)), "2024-02-29T00:00:00")
	wantEq(t, "year > 9999", call[string](t, "isoDateTime", arg(12345, 1, 1, 0, 0, 0, 0)), "+12345-01-01T00:00:00")
	wantEq(t, "invalid month", MustFail(t, "isoDateTime", arg(2020, 13, 1, 0, 0, 0, 0)).Class(), "DateTimeException")
	wantEq(t, "invalid day", MustFail(t, "isoDateTime", arg(2023, 2, 29, 0, 0, 0, 0)).Class(), "DateTimeException")
}

func TestDoubleToString(t *testing.T) {
	for _, c := range []struct {
		arg  any
		want string
	}{
		{m{"d": 1.0}, "1.0"}, {m{"d": 1e7}, "1.0E7"}, {m{"d": 0.001}, "0.001"}, {m{"d": 0.0001}, "1.0E-4"},
		{m{"d": 123456789.125}, "1.23456789125E8"}, {m{"d": 100}, "100.0"}, {m{"d": "NaN"}, "NaN"},
		{F64(negZero()), "-0.0"}, {F64(inf(1)), "Infinity"}, {F64(inf(-1)), "-Infinity"},
		{m{"bits": 1}, "4.9E-324"}, {m{"d": 5e-324}, "4.9E-324"}, {m{"d": 1.7976931348623157e308}, "1.7976931348623157E308"},
		{m{"bits": "0x7ff8000000000000"}, "NaN"}, {m{"d": 2e23}, "1.9999999999999998E23"}, {m{"d": 9007199254740993.0}, "9.007199254740992E15"},
	} {
		wantEq(t, fmt.Sprintf("doubleToString(%v)", c.arg), call[string](t, "doubleToString", c.arg), c.want)
	}
}

func TestMathRound(t *testing.T) {
	for _, c := range []struct {
		arg  any
		want int64
	}{
		{m{"d": 2.5}, 3}, {m{"d": -2.5}, -2}, {m{"d": 0.49999999999999994}, 0}, {m{"d": -0.5}, 0}, {m{"d": 1.5}, 2},
		{F64(nan()), 0}, {F64(inf(1)), 9223372036854775807}, {F64(inf(-1)), -9223372036854775808},
		{m{"d": 1e300}, 9223372036854775807}, {m{"d": 4503599627370497.0}, 4503599627370497},
	} {
		wantEq(t, fmt.Sprintf("mathRound(%v)", c.arg), call[int64](t, "mathRound", c.arg), c.want)
	}
}

func TestNumberFormat(t *testing.T) {
	wantEq(t, "fr-FR", call[string](t, "numberFormat", m{"d": 1234567.891, "locale": "fr-FR"}), "1 234 567,891")
	wantEq(t, "en-US", call[string](t, "numberFormat", m{"d": 1234567.891, "locale": "en-US"}), "1,234,567.891")
	wantEq(t, "max 3 fraction digits", call[string](t, "numberFormat", m{"d": 0.12345, "locale": "en-US"}), "0.123")
	wantEq(t, "half even", call[string](t, "numberFormat", m{"d": 0.0625, "locale": "en-US"}), "0.062")
	wantEq(t, "negative zero", call[string](t, "numberFormat", f64Locale(negZero(), "en-US")), "-0")
	wantEq(t, "NaN", call[string](t, "numberFormat", f64Locale(nan(), "en-US")), "NaN")
	wantEq(t, "de-DE", call[string](t, "numberFormat", m{"d": -1234.5, "locale": "de-DE"}), "-1.234,5")
}

func TestRegexReplace(t *testing.T) {
	wantEq(t, "group", call[string](t, "regexReplace", m{"pattern": `(\d+)`, "input": "a1b22", "replacement": "<$1>"}), "a<1>b<22>")
	wantEq(t, "literal $ escaped", call[string](t, "regexReplace", m{"pattern": `a`, "input": "aa", "replacement": `\$`}), "$$")
	wantEq(t, "empty matches", call[string](t, "regexReplace", m{"pattern": `x*`, "input": "abc", "replacement": "-"}), "-a-b-c-")
	wantEq(t, "unicode classes", call[string](t, "regexReplace", m{"pattern": `\s+`, "input": "a  b", "replacement": "_"}), "a _b")
	wantEq(t, "bad pattern", MustFail(t, "regexReplace", m{"pattern": "(", "input": "", "replacement": ""}).Class(), "PatternSyntaxException")
	wantEq(t, "bad group", MustFail(t, "regexReplace", m{"pattern": "a", "input": "a", "replacement": "$2"}).Class(), "IndexOutOfBoundsException")
	wantEq(t, "dangling $", MustFail(t, "regexReplace", m{"pattern": "a", "input": "a", "replacement": "$"}).Class(), "IllegalArgumentException")
}

func TestJSONRoundTripAndDecode(t *testing.T) {
	const fb = "fr.gouv.agora.infrastructure.qag.FeedbackResultsJson"
	// unknown properties are ignored, defaults come from the Kotlin module
	wantEq(t, "FeedbackResultsJson", call[string](t, "jsonRoundTrip", m{"className": fb, "json": `{"count":3,"extra":[1,2],"positiveRatio":1,"negativeRatio":2}`}),
		`{"positiveRatio":1,"negativeRatio":2,"count":3}`)
	// numbers are coerced, so Go can compare which inputs are accepted
	wantEq(t, "coercion", call[string](t, "jsonDecode", m{"className": fb, "json": `{"positiveRatio":"7","negativeRatio":2.9}`}),
		`{"positiveRatio":7,"negativeRatio":2,"count":0}`)
	wantEq(t, "boolean for int", MustFail(t, "jsonDecode", m{"className": fb, "json": `{"count":true}`}).Class(), "MismatchedInputException")
	wantEq(t, "null for int", call[string](t, "jsonDecode", m{"className": fb, "json": `{"positiveRatio":null}`}),
		`{"positiveRatio":0,"negativeRatio":0,"count":0}`)
	wantEq(t, "trailing tokens ignored", call[string](t, "jsonDecode", m{"className": fb, "json": `{"count":1} garbage`}),
		`{"positiveRatio":0,"negativeRatio":0,"count":1}`)
	wantEq(t, "raw bytes", call[string](t, "jsonDecode", m{"className": fb, "jsonB64": base64.StdEncoding.EncodeToString([]byte("\xef\xbb\xbf{\"count\":5}"))}),
		`{"positiveRatio":0,"negativeRatio":0,"count":5}`)

	// A real response DTO (NON_NULL inclusion, nested objects)
	qag := `{"id":"i","thematique":{"label":"L","picto":"P"},"title":"t","description":"d","date":"2023-01-02","username":"u",` +
		`"canShare":true,"canSupport":false,"canDelete":false,"support":{"count":2,"isSupported":true},"isAuthor":false}`
	got := call[string](t, "jsonRoundTrip", m{"className": "fr.gouv.agora.infrastructure.qag.QagJson", "json": qag})
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("round trip is not JSON: %v: %s", err, got)
	}
	wantEq(t, "QagJson keys", back["id"], "i")
	wantEq(t, "QagJson null omitted", back["response"], nil)
	if _, has := back["response"]; has {
		t.Errorf("null response must be omitted (NON_NULL): %s", got)
	}

	// Spring writes JSON through the byte (UTF-8) generator, which escapes every surrogate: an emoji
	// is \uD83D\uDE00 (upper-case hex) on the wire, not the raw character; lone surrogates are escaped too.
	const info = "fr.gouv.agora.infrastructure.qag.AdditionalInfoJson"
	wantEq(t, "emoji escaped", call[string](t, "jsonRoundTrip", m{"className": info, "json": `{"title":"a😀<>&é\u2028","description":"\u0000\u001f/"}`}),
		`{"title":"a\uD83D\uDE00<>&é`+"\u2028"+`","description":"\u0000\u001F/"}`) // U+2028 stays raw
	wantEq(t, "lone surrogate escaped", call[string](t, "jsonRoundTrip", m{"className": info, "via": "string", "json": Utf16{'{', '"', 't', 'i', 't', 'l', 'e', '"', ':', '"', 0xd800, '"', ',', '"', 'd', 'e', 's', 'c', 'r', 'i', 'p', 't', 'i', 'o', 'n', '"', ':', '"', '"', '}'}}),
		`{"title":"\uD800","description":""}`)

	// generic types
	wantEq(t, "List<...>", call[string](t, "jsonRoundTrip", m{"className": "java.util.List<" + fb + ">", "json": `[{"count":1}]`}),
		`[{"positiveRatio":0,"negativeRatio":0,"count":1}]`)

	// failures
	wantEq(t, "missing creator param", MustFail(t, "jsonRoundTrip", m{"className": "fr.gouv.agora.infrastructure.qag.QagJson", "json": `{}`}).Class(), "MissingKotlinParameterException")
	wantEq(t, "bad type", MustFail(t, "jsonRoundTrip", m{"className": fb, "json": `{"count":"x"}`}).Class(), "InvalidFormatException")
	wantEq(t, "bad json", MustFail(t, "jsonRoundTrip", m{"className": fb, "json": `{"count":`}).Class(), "JsonEOFException")
	wantEq(t, "empty body", MustFail(t, "jsonRoundTrip", m{"className": fb, "json": ``}).Class(), "MismatchedInputException")
	wantEq(t, "unknown class", MustFail(t, "jsonDecode", m{"className": "fr.gouv.agora.Nope", "json": `{}`}).Class(), "ClassNotFoundException")
}

func TestXMLSerialize(t *testing.T) {
	got := call[string](t, "xmlSerialize", m{
		"className": "fr.gouv.agora.infrastructure.moderatus.ModeratusQagListXml",
		"json": `{"qagToModerateCount":1,"qagsToModerate":[{"qagId":"1","postDate":"d","userId":"u",` +
			`"username":"n<&","title":"t","description":"b"}]}`,
	})
	wantEq(t, "ModeratusQagListXml", got,
		`<contents><nb_content>1</nb_content><msg><content_id>1</content_id><date>d</date><user_id>u</user_id>`+
			`<pseudo><![CDATA[n<&]]></pseudo><title><![CDATA[t]]></title><body><![CDATA[b]]></body>`+
			`<message_type>question</message_type></msg></contents>`)

	// CDATA sections cannot hold "]]>": the real converter fails there
	wantEq(t, "CDATA end marker", MustFail(t, "xmlSerialize", m{
		"className": "fr.gouv.agora.infrastructure.moderatus.ModeratusQagListXml",
		"json":      `{"qagToModerateCount":0,"qagsToModerate":[{"qagId":"1","postDate":"d","userId":"u","username":"a]]>b","title":"t","description":"b"}]}`,
	}).Class(), "JsonMappingException")

	wantEq(t, "map", call[string](t, "xmlSerializeValue", m{"json": `{"a":[1,2],"b":"x&y","c":null}`}),
		`<LinkedHashMap><a>1</a><a>2</a><b>x&amp;y</b><c/></LinkedHashMap>`)
	wantEq(t, "list", call[string](t, "xmlSerializeValue", m{"json": `[1,"a"]`}), `<ArrayList><item>1</item><item>a</item></ArrayList>`)
	wantEq(t, "bad json", MustFail(t, "xmlSerializeValue", m{"json": `[`}).Class(), "JsonEOFException")
}

// ---------------------------------------------------------------------------
// Crypto / secrets (parameters come from parity/env/common.env)

func ecbDecrypt(t *testing.T, key, data []byte) []byte {
	t.Helper()
	blk, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		t.Fatalf("bad ciphertext length %d", len(data))
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		blk.Decrypt(out[i:], data[i:i+aes.BlockSize])
	}
	pad := int(out[len(out)-1])
	return out[:len(out)-pad]
}

func ecbEncrypt(t *testing.T, key, data []byte) []byte {
	t.Helper()
	blk, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append([]byte(nil), data...)
	for i := 0; i < pad; i++ {
		data = append(data, byte(pad))
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		blk.Encrypt(out[i:], data[i:i+aes.BlockSize])
	}
	return out
}

func TestLoginToken(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString(envVar(t, "LOGIN_TOKEN_ENCODE_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	enc := call[struct{ Token string }](t, "loginTokenEncode", m{"userId": "abc"})
	raw, err := base64.StdEncoding.DecodeString(enc.Token)
	if err != nil {
		t.Fatalf("token is not base64: %v", err)
	}
	wantEq(t, "plaintext", string(ecbDecrypt(t, key, raw)), `{"userId":"abc"}`)
	wantEq(t, "deterministic (ECB)", call[struct{ Token string }](t, "loginTokenEncode", m{"userId": "abc"}).Token, enc.Token)

	wantEq(t, "decode own token", call[struct{ UserID string }](t, "loginTokenDecode", m{"token": enc.Token}).UserID, "abc")
	goTok := base64.StdEncoding.EncodeToString(ecbEncrypt(t, key, []byte(`{"userId":"héllo \"q\"","other":1}`)))
	wantEq(t, "decode Go token", call[struct{ UserID string }](t, "loginTokenDecode", m{"token": goTok}).UserID, `héllo "q"`)

	oe := MustFail(t, "loginTokenDecode", m{"token": "garbage!"})
	wantEq(t, "decode garbage class", oe.Class(), "Failure")
	if !strings.Contains(oe.Message(), "decodeLoginToken") {
		t.Errorf("decode failure should carry the swallowed exception: %q", oe.Message())
	}
	MustFail(t, "loginTokenDecode", m{"token": base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))})

	// per-call environment override, restored afterwards
	other := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	var over struct{ Token string }
	if err := CallEnv(map[string]any{"LOGIN_TOKEN_ENCODE_SECRET": other}, "loginTokenEncode", m{"userId": "abc"}, &over); err != nil {
		t.Fatal(err)
	}
	if over.Token == enc.Token {
		t.Error("env override had no effect")
	}
	wantEq(t, "env restored", call[struct{ Token string }](t, "loginTokenEncode", m{"userId": "abc"}).Token, enc.Token)
	var oe2 *OracleError
	err = Call("loginTokenEncode", m{"userId": "abc"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = CallEnv(map[string]any{"LOGIN_TOKEN_ENCODE_TRANSFORMATION": nil}, "loginTokenEncode", m{"userId": "abc"}, nil)
	if !errors.As(err, &oe2) || oe2.Class() != "Failure" {
		t.Errorf("unset env must make the encode fail, got %v", err)
	}
}

func TestIPHash(t *testing.T) {
	salt := envVar(t, "REMOTE_ADDRESS_HASH_SALT")
	for _, ip := range []string{"1.2.3.4", "::1", "", "é😀"} {
		want, err := pbkdf2.Key(sha512.New, ip, []byte(salt), 1000, 32)
		if err != nil {
			t.Fatal(err)
		}
		wantEq(t, "ipHash("+ip+")", call[string](t, "ipHash", m{"ip": ip}), hex.EncodeToString(want))
	}
	// the request-level logic (X-Forwarded-For, X-Remote-Address, remoteAddr)
	wantEq(t, "xff", call[string](t, "ipRetrieve", m{"xForwardedFor": " , 5.6.7.8 ,9", "remoteAddr": "1.1.1.1"}), "5.6.7.8")
	wantEq(t, "xra", call[string](t, "ipRetrieve", m{"xForwardedFor": "  ", "xRemoteAddress": " 2.2.2.2 ", "remoteAddr": "1.1.1.1"}), "2.2.2.2")
	wantEq(t, "remote", call[string](t, "ipRetrieve", m{"remoteAddr": " 1.1.1.1 "}), "1.1.1.1")
	wantEq(t, "retrieveHash", call[string](t, "ipRetrieveHash", m{"remoteAddr": "1.2.3.4"}), call[string](t, "ipHash", m{"ip": "1.2.3.4"}))

	var out string
	err := CallEnv(map[string]any{"REMOTE_ADDRESS_HASH_ITERATIONS": "abc"}, "ipHash", m{"ip": "x"}, &out)
	var oe *OracleError
	if !errors.As(err, &oe) || oe.Msg != "Exception: Invalid remoteAddress hash iterations number" {
		t.Errorf("invalid iterations: %v", err)
	}
	wantEq(t, "restored", call[string](t, "ipHash", m{"ip": "1.2.3.4"}), call[string](t, "ipHash", m{"ip": "1.2.3.4"}))
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func goJWT(t *testing.T, key []byte, claims string) string {
	t.Helper()
	signing := b64url([]byte(`{"alg":"HS512"}`)) + "." + b64url([]byte(claims))
	mac := hmac.New(sha512.New, key)
	mac.Write([]byte(signing))
	return signing + "." + b64url(mac.Sum(nil))
}

func TestJWT(t *testing.T) {
	key, err := base64.StdEncoding.DecodeString(envVar(t, "JWT_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	type generated struct {
		Token                string `json:"token"`
		ExpirationEpochMilli int64  `json:"expirationEpochMilli"`
	}
	before := time.Now().UnixMilli()
	gen := call[generated](t, "jwtGenerate", m{"userId": "user-1"})
	after := time.Now().UnixMilli()
	day := (24 * time.Hour).Milliseconds()
	if gen.ExpirationEpochMilli < before+day || gen.ExpirationEpochMilli > after+day {
		t.Errorf("expiration %d is not now+24h (%d..%d)", gen.ExpirationEpochMilli, before+day, after+day)
	}

	// layout and signature of the Java token, checked in Go
	parts := strings.Split(gen.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", gen.Token)
	}
	wantEq(t, "header", parts[0], b64url([]byte(`{"alg":"HS512"}`)))
	mac := hmac.New(sha512.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	wantEq(t, "signature", parts[2], b64url(mac.Sum(nil)))
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Sub string `json:"sub"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	wantEq(t, "sub", claims.Sub, "user-1")
	wantEq(t, "exp seconds", claims.Exp, gen.ExpirationEpochMilli/1000)

	type parsed struct {
		Valid  bool   `json:"valid"`
		UserID string `json:"userId"`
	}
	wantEq(t, "parse own", call[parsed](t, "jwtParse", m{"token": gen.Token}), parsed{true, "user-1"})
	future := fmt.Sprintf(`{"sub":"go-user","iat":1,"exp":%d}`, time.Now().Add(time.Hour).Unix())
	wantEq(t, "parse Go token", call[parsed](t, "jwtParse", m{"token": goJWT(t, key, future)}), parsed{true, "go-user"})

	custom := call[generated](t, "jwtGenerate", m{"userId": "u", "claims": m{"role": "admin"}})
	body, _ := base64.RawURLEncoding.DecodeString(strings.Split(custom.Token, ".")[1])
	if !strings.Contains(string(body), `"role":"admin"`) {
		t.Errorf("claims missing from %s", body)
	}

	past := fmt.Sprintf(`{"sub":"u","iat":1,"exp":%d}`, time.Now().Add(-time.Hour).Unix())
	for _, c := range []struct{ name, token, class string }{
		{"expired", goJWT(t, key, past), "ExpiredJwtException"},
		{"bad signature", gen.Token[:len(gen.Token)-4] + "AAAA", "SignatureException"},
		{"wrong key", goJWT(t, []byte(strings.Repeat("k", 64)), future), "SignatureException"},
		{"malformed", "abc", "MalformedJwtException"},
		{"empty", "", "IllegalArgumentException"},
		{"no exp", goJWT(t, key, `{"sub":"u"}`), "NullPointerException"},
		{"unsigned", b64url([]byte(`{"alg":"none"}`)) + "." + b64url([]byte(future)) + ".", "UnsupportedJwtException"},
	} {
		wantEq(t, c.name, MustFail(t, "jwtParse", m{"token": c.token}).Class(), c.class)
	}

	var out any
	err = CallEnv(map[string]any{"JWT_SECRET": nil}, "jwtGenerate", m{"userId": "u"}, &out)
	var oe *OracleError
	if !errors.As(err, &oe) {
		t.Errorf("an empty JWT secret must be rejected, got %v", err)
	}
}

func TestAesGcm(t *testing.T) {
	key32 := make([]byte, 32)
	for i := range key32 {
		key32[i] = byte(i)
	}
	key := base64.StdEncoding.EncodeToString(key32)
	gcm := func() cipher.AEAD {
		blk, _ := aes.NewCipher(key32)
		g, err := cipher.NewGCM(blk)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}()

	// Java -> Go: base64(iv(12) | ciphertext | tag(16))
	enc := call[string](t, "aesGcmEncrypt", m{"key": key, "plaintext": "héllo 😀"})
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil {
		t.Fatalf("Go cannot open the Java ciphertext: %v", err)
	}
	wantEq(t, "plaintext", string(plain), "héllo 😀")
	if enc2 := call[string](t, "aesGcmEncrypt", m{"key": key, "plaintext": "héllo 😀"}); enc2 == enc {
		t.Error("IV must be random")
	}
	wantEq(t, "decrypt own", call[string](t, "aesGcmDecrypt", m{"key": key, "ciphertext": enc}), "héllo 😀")

	// Go -> Java
	iv := []byte("0123456789ab")
	goEnc := base64.StdEncoding.EncodeToString(append(append([]byte(nil), iv...), gcm.Seal(nil, iv, []byte(""), nil)...))
	wantEq(t, "empty plaintext", call[string](t, "aesGcmDecrypt", m{"key": key, "ciphertext": goEnc}), "")

	// failures
	wantEq(t, "short key", MustFail(t, "aesGcmEncrypt", m{"key": "AAEC", "plaintext": "x"}).Class(), "IllegalStateException")
	wantEq(t, "blank key", MustFail(t, "aesGcmEncrypt", m{"key": " ", "plaintext": "x"}).Class(), "IllegalStateException")
	wantEq(t, "not base64 key", MustFail(t, "aesGcmEncrypt", m{"key": "***", "plaintext": "x"}).Class(), "IllegalStateException")
	tampered := []byte(enc)
	if tampered[20] == 'A' {
		tampered[20] = 'B'
	} else {
		tampered[20] = 'A'
	}
	wantEq(t, "tampered", MustFail(t, "aesGcmDecrypt", m{"key": key, "ciphertext": string(tampered)}).Class(), "AEADBadTagException")
	wantEq(t, "not base64", MustFail(t, "aesGcmDecrypt", m{"key": key, "ciphertext": "***"}).Class(), "IllegalArgumentException")
	wantEq(t, "shorter than the IV", MustFail(t, "aesGcmDecrypt", m{"key": key, "ciphertext": "AAEC"}).Class(), "IndexOutOfBoundsException")
	wantEq(t, "shorter than the tag", MustFail(t, "aesGcmDecrypt", m{"key": key, "ciphertext": base64.StdEncoding.EncodeToString(make([]byte, 20))}).Class(), "ProviderException")
}

func TestConcurrentCalls(t *testing.T) {
	if !Available() {
		MustCall(t, "ping", nil, nil) // skips (or fails) with the right message
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8*100)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s := strings.Repeat("é", g*10+i) + "😀"
				var n int
				if err := Call("javaStringLength", m{"s": s}, &n); err != nil {
					errs <- err
					return
				}
				if n != g*10+i+2 {
					errs <- fmt.Errorf("goroutine %d call %d: length %d", g, i, n)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTimeoutRestartsJVM(t *testing.T) {
	if !Available() {
		MustCall(t, "ping", nil, nil)
	}
	old := Timeout
	Timeout = 1500 * time.Millisecond
	t.Setenv("PARITY_ORACLE_TIMEOUT", "")
	defer func() { Timeout = old }()

	// catastrophic backtracking: the JVM is stuck until the client kills it
	start := time.Now()
	err := Call("regexReplace", m{"pattern": "(.*a){30}", "input": strings.Repeat("a", 28) + strings.Repeat("b", 10), "replacement": "x"}, nil)
	var oe *OracleError
	if err == nil || errors.As(err, &oe) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("timeout took %v", d)
	}
	Timeout = old
	wantEq(t, "ping after restart", call[string](t, "ping", nil), "pong")
	wantEq(t, "still works", call[int](t, "javaStringLength", m{"s": "abc"}), 3)
}

// ---------------------------------------------------------------------------
// Helpers without the JVM

func TestUtf16MarshalAndEnvFile(t *testing.T) {
	b, _ := json.Marshal(m{"s": Utf16{0xd800, 'a', 0x00e9}})
	wantEq(t, "Utf16 json", string(b), `{"s":"\ud800\u0061\u00e9"}`)

	path := t.TempDir() + "/e.env"
	content := "# comment\n\nA=1\nexport B=two words\nC=\"q\"\nD=x=y==\nE='s'\r\nbad line\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	vars, order, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "env file", vars, map[string]string{"A": "1", "B": "two words", "C": "q", "D": "x=y==", "E": "s"})
	wantEq(t, "env order", order, []string{"A", "B", "C", "D", "E"})
}

func negZero() float64     { return math.Copysign(0, -1) }
func nan() float64         { return math.NaN() }
func inf(sign int) float64 { return math.Inf(sign) }

// f64Locale is the numberFormat argument for a double given as raw bits.
func f64Locale(d float64, locale string) map[string]any {
	a := F64(d)
	a["locale"] = locale
	return a
}

func BenchmarkCall(b *testing.B) {
	MustCall(b, "ping", nil, nil)
	b.Run("sequential", func(b *testing.B) {
		var n int
		for i := 0; i < b.N; i++ {
			if err := Call("javaStringLength", m{"s": "héllo wörld"}, &n); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var n int
			for pb.Next() {
				if err := Call("javaStringLength", m{"s": "héllo wörld"}, &n); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
	b.Run("sanitize", func(b *testing.B) {
		var r Text
		for i := 0; i < b.N; i++ {
			if err := Call("sanitize", m{"content": "<b>x</b> a &amp; b", "maxLength": 100}, &r); err != nil {
				b.Fatal(err)
			}
		}
	})
}
