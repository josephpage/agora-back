# F7 - `internal/sanitize`: divergences

## Residual mismatches of the package against the JVM oracle

**None.** Every comparison of `sanitizeRaw`, `htmlUnescape` and `sanitize` (limits 0, 1, 5, 50, 200, 400 and unbounded) with the real
OWASP 20220608.1 / Spring 6.0.14 / Kotlin code agreed on the exact UTF-16 code units (see `parity/ledger/F7.md` for the inputs
and the counts).

## Integration notes (outside the package; class C if left as is, for the lead)

1. **Lone surrogates in a request body.** Jackson turns the JSON escape `"\ud800"` into a lone UTF-16 surrogate (checked with the
   reference jar: `{"x":"a\ud800b"}` -> `61 d800 62`), which the OWASP encoder elides: Kotlin `sanitize("a\ud800b")` is `ab`.
   `encoding/json` (used by `internal/jsonjava`) replaces it by U+FFFD before the sanitizer sees it, and `sanitize.Sanitize` of the string a, U+FFFD, b
   is `a` + U+FFFD + `b`, as Kotlin would give for a real U+FFFD. `sanitize.SanitizeUTF16` is exact for such input if the JSON layer
   can ever provide UTF-16 units. A raw invalid UTF-8 byte in the body is a 400 for Jackson (`JsonParseException`) but U+FFFD for Go.
2. **Result type.** `Sanitize` returns a Go string, so the lone surrogates that the Java result can contain become `?` in it. This is
   the stored value: the PostgreSQL JDBC driver encodes a Java string with `String.getBytes(UTF_8)`, which writes `?` for an unpaired
   surrogate. Such surrogates arise from a supplementary character whose code point has a surrogate as its low 16 bits (U+1D800,
   U+1DC00, U+2D800, ... 32 768 code points: OWASP writes `&#x1d800;`, Spring casts to `(char)`), and from `take(n)` cutting a
   valid pair (the pair itself only exists in that same quirk, e.g. U+1D800 U+1DC00 -> U+10000). Anything that uses the sanitized
   value other than storing it (length checks on the result, echoing it in the response) must keep that in mind;
   `SanitizeUTF16` returns the exact Java string.
3. **Negative `maxLength`** panics (Kotlin `IllegalArgumentException`); all callers pass positive constants.
