package auth

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"agora/internal/javacompat"
	"agora/internal/jsonjava"
)

// This file follows io.jsonwebtoken 0.11.5 DefaultJwtParser.parse + parseClaimsJws,
// then JwtTokenUtils (isCorrectSignatureAndTokenNotExpired, extractUserId), checked by
// the JVM oracle (TestOracleJWTFilter). A token is a string of chars: the Authorization
// header as Tomcat decodes it (ISO-8859-1), or the cookie value.
//
// Outcomes: (subject, nil) authenticates; ErrInvalidJWT is a JwtException (the filter
// catches it: anonymous request); any other error escapes the filter (HTTP 500).

// errJavaRuntime marks a non-JwtException (ClassCastException, IllegalStateException,
// IllegalArgumentException, NullPointerException): HTTP 500.
type errJavaRuntime struct{ msg string }

func (e errJavaRuntime) Error() string { return e.msg }

func javaRuntime(format string, args ...any) error {
	return errJavaRuntime{fmt.Sprintf(format, args...)}
}

// Parse validates a compact token and returns its subject (see the file comment).
func (j *JWT) Parse(token string) (string, error) {
	// Assert.hasText(jwt): IllegalArgumentException (not a JwtException)
	if !javaHasText(token) {
		return "", ErrEmptyToken
	}
	segs := strings.Split(token, ".")
	if len(segs) != 3 {
		return "", ErrInvalidJWT // "JWT strings must contain exactly 2 period characters"
	}
	// Strings.clean: Character.isWhitespace trimmed, empty → null; the digest is kept as is
	h64, hasHeader := jjwtClean(segs[0])
	p64, hasPayload := jjwtClean(segs[1])
	digest := segs[2]
	signed := digest != ""

	// =============== Header ===============
	var header map[string]any
	var codec string
	if hasHeader {
		b, err := jjwtBase64URLDecode(h64)
		if err != nil {
			return "", ErrInvalidJWT // DecodingException
		}
		v, err := jacksonReadValue(b)
		if err != nil {
			return "", ErrInvalidJWT // DeserializationException
		}
		m, isMap := v.(map[string]any)
		switch {
		case v == nil:
			return "", javaRuntime("IllegalArgumentException: Map argument cannot be null")
		case !isMap:
			return "", javaRuntime("ClassCastException: %T cannot be cast to java.util.Map", v)
		}
		header = m
		if zip := javaStringValueOf(header["zip"]); zip != nil && javaHasText(*zip) {
			switch {
			case strings.EqualFold(*zip, "DEF"), strings.EqualFold(*zip, "GZIP"):
				codec = strings.ToUpper(*zip)
			default:
				return "", ErrInvalidJWT // CompressionException: unsupported algorithm
			}
		}
	}

	// =============== Body ===============
	payload := ""
	if hasPayload {
		b, err := jjwtBase64URLDecode(p64)
		if err != nil {
			return "", ErrInvalidJWT
		}
		if codec != "" {
			if b, err = decompress(codec, b); err != nil {
				return "", ErrInvalidJWT // CompressionException
			}
		}
		payload = javaUTF8String(b)
	}
	var claims map[string]any
	if payload != "" && payload[0] == '{' && payload[len(payload)-1] == '}' {
		v, err := jacksonReadValue([]byte(payload))
		if err != nil {
			return "", ErrInvalidJWT
		}
		claims, _ = v.(map[string]any)
	}

	// =============== Signature ===============
	if signed {
		var algName string
		if header != nil {
			if a := javaStringValueOf(header["alg"]); a != nil && javaHasText(*a) {
				algName = *a
			}
		}
		if algName == "" {
			return "", ErrInvalidJWT // MalformedJwtException: no valid signature algorithm
		}
		alg, known := signatureAlgorithmForName(algName)
		if !known || alg == "none" {
			return "", ErrInvalidJWT // SignatureException / MalformedJwtException
		}
		h, minBits := hasherFor(alg)
		if h == nil {
			return "", ErrInvalidJWT // the HMAC key cannot verify RSA/EC signatures
		}
		if j.invalid || len(j.key)*8 < minBits {
			return "", ErrInvalidJWT // WeakKeyException
		}
		input := h64 + "."
		if hasPayload {
			input += p64
		}
		sig, err := jjwtBase64URLDecode(digest)
		if err != nil {
			return "", ErrInvalidJWT
		}
		mac := hmac.New(h, j.key)
		mac.Write(usASCII(input))
		if !hmac.Equal(mac.Sum(nil), sig) {
			return "", ErrInvalidJWT // SignatureException
		}
	}

	// =============== Claims ===============
	now := j.now()
	if claims != nil {
		exp, err := jjwtSpecDate(claims, "exp")
		if err != nil {
			return "", err
		}
		if exp != nil && now.After(*exp) {
			return "", ErrInvalidJWT // ExpiredJwtException
		}
		nbf, err := jjwtSpecDate(claims, "nbf")
		if err != nil {
			return "", err
		}
		if nbf != nil && now.Before(*nbf) {
			return "", ErrInvalidJWT // PrematureJwtException
		}
	}
	// parseClaimsJws: an unsigned token or a plaintext body is an UnsupportedJwtException
	if !signed || claims == nil {
		return "", ErrInvalidJWT
	}

	// =============== JwtTokenUtils ===============
	exp, _ := jjwtSpecDate(claims, "exp")
	if exp == nil {
		return "", javaRuntime("NullPointerException: claims.expiration must not be null")
	}
	if !exp.After(now) {
		return "", ErrInvalidJWT // isCorrectSignatureAndTokenNotExpired false: anonymous
	}
	sub := javaStringValueOf(claims["sub"])
	if sub == nil {
		return "", javaRuntime("NullPointerException: claims.subject must not be null")
	}
	return *sub, nil
}

// javaHasText is StringUtils/Strings.hasText: a char that is not Character.isWhitespace.
func javaHasText(s string) bool {
	for _, r := range s {
		if !javacompat.JavaIsWhitespace(r) {
			return true
		}
	}
	return false
}

// jjwtClean is io.jsonwebtoken.lang.Strings.clean: trimmed of Character.isWhitespace,
// absent when empty.
func jjwtClean(s string) (string, bool) {
	t := strings.TrimFunc(s, javacompat.JavaIsWhitespace)
	return t, t != ""
}

// usASCII is String.getBytes(US_ASCII): a char above U+007F becomes '?'.
func usASCII(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0x7F {
			r = '?'
		}
		out = append(out, byte(r))
	}
	return out
}

// javaUTF8String is new String(bytes, UTF_8) (malformed input replaced by U+FFFD), as Go
// UTF-8: the same chars when encoded again (jjwt re-encodes before Jackson reads it).
func javaUTF8String(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// jjwtAlphabetURL / jjwtIAlphabet are io.jsonwebtoken.io.Base64.URL_SAFE: the URL-safe
// alphabet, '=' read as 0, anything else invalid.
var jjwtIAlphabet = func() [256]int {
	var t [256]int
	for i := range t {
		t[i] = -1
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for i := 0; i < len(alphabet); i++ {
		t[alphabet[i]] = i
	}
	t['='] = 0
	return t
}()

// jjwtBase64URLDecode is io.jsonwebtoken.io.Base64.decodeFast (MiGBase64) with the
// URL-safe alphabet, on the token's UTF-16 chars: characters outside the alphabet are
// skipped at both ends, trailing '=' are padding, a CR at index 76 announces 76-char
// lines (MIME), and any other illegal char is a DecodingException. The trimming loops
// index IALPHABET (256 entries) without a bound check: a char above U+00FF they look
// at throws ArrayIndexOutOfBoundsException, which ExceptionPropagatingDecoder turns
// into a DecodingException too.
func jjwtBase64URLDecode(s string) ([]byte, error) {
	chars := utf16.Encode([]rune(s))
	sLen := len(chars)
	if sLen == 0 {
		return []byte{}, nil
	}
	errBounds := errors.New("DecodingException: index out of bounds")
	sIx, eIx := 0, sLen-1
	for sIx < eIx {
		if chars[sIx] > 255 {
			return nil, errBounds
		}
		if jjwtIAlphabet[chars[sIx]] >= 0 {
			break
		}
		sIx++
	}
	for eIx > 0 {
		if chars[eIx] > 255 {
			return nil, errBounds
		}
		if jjwtIAlphabet[chars[eIx]] >= 0 {
			break
		}
		eIx--
	}
	pad := 0
	if chars[eIx] == '=' {
		if eIx == 0 {
			return nil, errBounds // sArr[eIx - 1]
		}
		pad = 1
		if chars[eIx-1] == '=' {
			pad = 2
		}
	}
	cCnt := eIx - sIx + 1
	sepCnt := 0
	if sLen > 76 {
		if chars[76] == '\r' {
			sepCnt = cCnt / 78
		}
		sepCnt <<= 1
	}
	length := ((cCnt-sepCnt)*6)>>3 - pad
	if length < 0 {
		return nil, errors.New("DecodingException: negative length")
	}
	ctoi := func(c uint16) (int, error) {
		if c <= 255 && jjwtIAlphabet[c] >= 0 {
			return jjwtIAlphabet[c], nil
		}
		return 0, fmt.Errorf("DecodingException: illegal base64url character %q", rune(c))
	}
	out := make([]byte, length)
	d, cc := 0, 0
	for eLen := (length / 3) * 3; d < eLen; {
		var v [4]int
		for k := 0; k < 4; k++ {
			if sIx >= len(chars) {
				return nil, errors.New("DecodingException: index out of bounds")
			}
			x, err := ctoi(chars[sIx])
			if err != nil {
				return nil, err
			}
			v[k] = x
			sIx++
		}
		i := v[0]<<18 | v[1]<<12 | v[2]<<6 | v[3]
		out[d] = byte(i >> 16)
		out[d+1] = byte(i >> 8)
		out[d+2] = byte(i)
		d += 3
		if sepCnt > 0 {
			cc++
			if cc == 19 {
				sIx += 2
				cc = 0
			}
		}
	}
	if d < length {
		i := 0
		for j := 0; sIx <= eIx-pad; j++ {
			if sIx >= len(chars) {
				return nil, errors.New("DecodingException: index out of bounds")
			}
			x, err := ctoi(chars[sIx])
			if err != nil {
				return nil, err
			}
			i |= x << (18 - j*6)
			sIx++
		}
		for r := 16; d < length; r -= 8 {
			out[d] = byte(i >> r)
			d++
		}
	}
	return out, nil
}

// jacksonReadValue is ObjectMapper.readValue(bytes, Object.class) for jjwt's
// JacksonDeserializer: the first JSON value (a UTF-8 BOM is skipped, the rest of the input
// is not read, but a root-level scalar must be followed by a separator), with Jackson's
// number types (json.Number here).
func jacksonReadValue(b []byte) (any, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	rest := b[dec.InputOffset():]
	switch v.(type) {
	case json.Number:
		// _verifyRootSpace: a root-level number must be followed by white space or the end
		if len(rest) > 0 && !strings.ContainsRune(" \t\r\n", rune(rest[0])) {
			return nil, errors.New("expected space separating root-level values")
		}
	case bool, nil:
		// _matchToken / _checkMatchEnd: a char >= '0' (but ']' and '}') that could go on
		// an identifier makes the literal an unrecognized token. A non-ASCII char always
		// fails: _decodeCharForError reads the lead byte again as its continuation byte
		// ("Invalid UTF-8 middle byte").
		if len(rest) > 0 && rest[0] >= '0' && rest[0] != ']' && rest[0] != '}' {
			if rest[0] >= 0x80 || javaIdentifierPart(rune(rest[0])) {
				return nil, errors.New("unrecognized token")
			}
		}
	}
	return v, nil
}

// javaIdentifierPart is Character.isJavaIdentifierPart.
func javaIdentifierPart(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r), unicode.Is(unicode.Nl, r), unicode.Is(unicode.Mn, r),
		unicode.Is(unicode.Mc, r), unicode.Is(unicode.Pc, r), unicode.Is(unicode.Sc, r), unicode.Is(unicode.Cf, r):
		return true
	case r <= 0x08, r >= 0x0E && r <= 0x1B, r >= 0x7F && r <= 0x9F: // identifier-ignorable controls
		return true
	}
	return false
}

// javaStringValueOf is JwtMap.getString: String.valueOf(value), nil for a missing or
// null claim. Collections print like Java's (they only matter as non-UUID user ids).
func javaStringValueOf(v any) *string {
	if v == nil {
		return nil
	}
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case bool:
		s = strconv.FormatBool(t)
	case json.Number:
		s = javaNumberString(t)
	default:
		s = fmt.Sprint(t)
	}
	return &s
}

// javaNumberString is Integer/Long/BigInteger/Double.toString of a Jackson number.
func javaNumberString(n json.Number) string {
	txt := n.String()
	if !strings.ContainsAny(txt, ".eE") {
		return txt
	}
	f, _ := strconv.ParseFloat(txt, 64)
	return jsonjava.JavaDoubleToString(f)
}

// signatureAlgorithmForName is SignatureAlgorithm.forName (equalsIgnoreCase).
func signatureAlgorithmForName(name string) (string, bool) {
	for _, a := range []string{"none", "HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512"} {
		if strings.EqualFold(a, name) {
			return a, true
		}
	}
	return "", false
}

// decompress is GzipCompressionCodec / DeflateCompressionCodec.doDecompress. A DEF payload
// cut short is not an error: after InflaterInputStream's EOFException, jjwt's
// back-compat path (InflaterOutputStream.write + flush) returns what could be inflated.
// Corrupt data and a bad checksum fail both paths.
func decompress(codec string, b []byte) ([]byte, error) {
	if codec == "GZIP" {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return out, nil
	}
	return out, err
}

// jjwtSpecDate is JwtMap.toSpecDate of a claim: a number or a numeric string is in
// seconds (Number.longValue, Long.parseLong, × 1000 wrapping like long), another string
// must be ISO-8601 (IllegalArgumentException), any other type is an
// IllegalStateException. Both escape the filter (HTTP 500).
func jjwtSpecDate(claims map[string]any, name string) (*time.Time, error) {
	v, present := claims[name]
	if !present || v == nil {
		return nil, nil
	}
	var ms int64
	switch t := v.(type) {
	case json.Number:
		ms = javaNumberLongValue(t) * 1000
	case string:
		if sec, ok := javaParseLong(t); ok {
			ms = sec * 1000
		} else {
			d, ok := jjwtParseISO8601(t)
			if !ok {
				return nil, javaRuntime("IllegalArgumentException: '%s' value does not appear to be ISO-8601-formatted: %s", name, t)
			}
			return &d, nil
		}
	default:
		return nil, javaRuntime("IllegalStateException: Cannot create Date from '%s' value", name)
	}
	d := time.UnixMilli(ms)
	return &d, nil
}

// javaNumberLongValue is Number.longValue of a Jackson number: an integer literal is an
// Integer, Long or BigInteger (low 64 bits), a decimal one a Double ((long) cast).
func javaNumberLongValue(n json.Number) int64 {
	txt := n.String()
	if !strings.ContainsAny(txt, ".eE") {
		bi, ok := new(big.Int).SetString(txt, 10)
		if !ok {
			return 0
		}
		mask := new(big.Int).Lsh(big.NewInt(1), 64)
		u := new(big.Int).Mod(bi, mask) // two's complement low 64 bits
		return int64(u.Uint64())
	}
	f, _ := strconv.ParseFloat(txt, 64)
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f <= math.MinInt64:
		return math.MinInt64
	}
	return int64(f)
}

// javaParseLong is Long.parseLong (sign, Unicode decimal digits, no spaces, overflow fails).
func javaParseLong(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	neg := false
	i := 0
	switch s[0] {
	case '-':
		neg, i = true, 1
	case '+':
		i = 1
	}
	if i == len(s) {
		return 0, false
	}
	var acc uint64
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	for _, r := range s[i:] {
		d := javacompat.JavaCharacterDigit(r, 10)
		if d < 0 {
			return 0, false
		}
		if acc > (limit-uint64(d))/10 {
			return 0, false
		}
		acc = acc*10 + uint64(d)
	}
	if neg {
		return -int64(acc-1) - 1, true
	}
	return int64(acc), true
}

// SimpleDateFormat.subParse skips spaces and tabs before each numeric field.
var isoPrefix = regexp.MustCompile(`^[ \t]*(\d+)-[ \t]*(\d+)-[ \t]*(\d+)T[ \t]*(\d+):[ \t]*(\d+):[ \t]*(\d+)Z`)
var isoMillisPrefix = regexp.MustCompile(`^[ \t]*(\d+)-[ \t]*(\d+)-[ \t]*(\d+)T[ \t]*(\d+):[ \t]*(\d+):[ \t]*(\d+)\.[ \t]*(\d+)Z`)

// jjwtParseISO8601 is DateFormats.parseIso8601Date: SimpleDateFormat
// "yyyy-MM-dd'T'HH:mm:ss'Z'" (".SSS" when the text has a '.'), lenient, UTC, text after
// the pattern ignored. Field overflows roll over like the lenient calendar.
func jjwtParseISO8601(s string) (time.Time, bool) {
	re := isoPrefix
	if strings.Contains(s, ".") {
		re = isoMillisPrefix
	}
	m := re.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n := make([]int, len(m))
	for i := 1; i < len(m); i++ {
		v, err := strconv.Atoi(m[i])
		if err != nil {
			return time.Time{}, false
		}
		n[i] = v
	}
	ms := 0
	if len(m) == 8 {
		ms = n[7]
	}
	return time.Date(n[1], time.Month(n[2]), n[3], n[4], n[5], n[6], 0, time.UTC).Add(time.Duration(ms) * time.Millisecond), true
}
