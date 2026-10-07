// Package auth reproduces the Kotlin backend's authentication primitives:
// jjwt 0.11.5 compatible HMAC JWTs, the AES "loginToken" and the PBKDF2 IP hash.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"
	"strconv"
	"strings"
	"time"
)

// JWTValidity is JwtTokenUtils.JWT_TOKEN_VALIDITY (1 day).
const JWTValidity = 24 * time.Hour

// JWTCookieName is AuthenticationTokenFilter.JWT_COOKIE_KEY.
const JWTCookieName = "auth-jwt"

// ErrEmptyToken mirrors jjwt's IllegalArgumentException("JWT String argument
// cannot be null or empty."), which the Kotlin filter does NOT catch (→ 500).
var ErrEmptyToken = errors.New("IllegalArgumentException: JWT String argument cannot be null or empty")

// ErrInvalidJWT covers every io.jsonwebtoken.JwtException (caught → anonymous).
var ErrInvalidJWT = errors.New("JwtException")

// JWT signs and verifies tokens like io.jsonwebtoken 0.11.5 with
// Keys.hmacShaKeyFor(Base64.decode(JWT_SECRET)).
type JWT struct {
	key     []byte
	alg     string // algorithm selected by Keys.hmacShaKeyFor + signWith
	header  string // pre-encoded header segment
	now     func() time.Time
	invalid bool // key too short: jjwt throws WeakKeyException at use time
}

// NewJWT builds the signer from the base64 JWT_SECRET value.
func NewJWT(base64Secret string, now func() time.Time) *JWT {
	if now == nil {
		now = time.Now
	}
	key, err := decodeJJWTBase64(base64Secret)
	j := &JWT{key: key, now: now}
	if err != nil {
		j.invalid = true
		return j
	}
	bits := len(key) * 8
	switch {
	case bits >= 512:
		j.alg = "HS512"
	case bits >= 384:
		j.alg = "HS384"
	case bits >= 256:
		j.alg = "HS256"
	default:
		j.invalid = true
	}
	j.header = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"` + j.alg + `"}`))
	return j
}

func decodeJJWTBase64(s string) ([]byte, error) {
	// jjwt Decoders.BASE64 accepts standard alphabet with or without padding.
	s = strings.TrimRight(s, "=")
	return base64.RawStdEncoding.DecodeString(s)
}

func hasherFor(alg string) (func() hash.Hash, int) {
	switch alg {
	case "HS256":
		return sha256.New, 256
	case "HS384":
		return sha512.New384, 384
	case "HS512":
		return sha512.New, 512
	}
	return nil, 0
}

// Generate reproduces JwtTokenUtils.generateToken(userId): returns the compact
// token and the expiration in epoch milliseconds (exact, not truncated).
func (j *JWT) Generate(userID string) (string, int64, error) {
	if j.invalid {
		return "", 0, errors.New("WeakKeyException")
	}
	now := j.now()
	nowMs := now.UnixMilli()
	expMs := nowMs + JWTValidity.Milliseconds()
	// DefaultClaims is a LinkedHashMap: sub, iat, exp in insertion order.
	payload := `{"sub":` + jsonString(userID) +
		`,"iat":` + strconv.FormatInt(nowMs/1000, 10) +
		`,"exp":` + strconv.FormatInt(expMs/1000, 10) + `}`
	signingInput := j.header + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))
	h, _ := hasherFor(j.alg)
	mac := hmac.New(h, j.key)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig, expMs, nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Claims are the claims the backend uses.
type Claims struct {
	Subject string
	HasExp  bool
	Exp     time.Time
}

// Parse validates a compact token like jjwt parseClaimsJws + the Kotlin
// isCorrectSignatureAndTokenNotExpired / extractUserId pair.
// Returns (userId, nil) when the token authenticates the request,
// ErrInvalidJWT when jjwt throws a JwtException (request stays anonymous),
// ErrEmptyToken for an empty token (uncaught → HTTP 500).
func (j *JWT) Parse(token string) (string, error) {
	if token == "" || strings.TrimSpace(token) == "" {
		return "", ErrEmptyToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ErrInvalidJWT
	}
	if parts[2] == "" {
		// unsigned token passed to parseClaimsJws → UnsupportedJwtException
		return "", ErrInvalidJWT
	}
	headerJSON, err := decodeB64URL(parts[0])
	if err != nil {
		return "", ErrInvalidJWT
	}
	var header map[string]any
	if json.Unmarshal(headerJSON, &header) != nil {
		return "", ErrInvalidJWT
	}
	alg, _ := header["alg"].(string)
	if _, ok := header["zip"]; ok {
		// compression is never produced by this backend; treat as invalid
		return "", ErrInvalidJWT
	}
	h, minBits := hasherFor(alg)
	if h == nil {
		return "", ErrInvalidJWT
	}
	if j.invalid || len(j.key)*8 < minBits {
		return "", ErrInvalidJWT
	}
	payloadJSON, err := decodeB64URL(parts[1])
	if err != nil {
		return "", ErrInvalidJWT
	}
	sig, err := decodeB64URL(parts[2])
	if err != nil {
		return "", ErrInvalidJWT
	}
	mac := hmac.New(h, j.key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if subtle.ConstantTimeCompare(mac.Sum(nil), sig) != 1 {
		return "", ErrInvalidJWT
	}
	dec := json.NewDecoder(strings.NewReader(string(payloadJSON)))
	dec.UseNumber()
	var claims map[string]any
	if err := dec.Decode(&claims); err != nil || claims == nil {
		return "", ErrInvalidJWT
	}
	now := j.now()
	exp, hasExp, ok := numericDateClaim(claims, "exp")
	if !ok {
		return "", ErrInvalidJWT
	}
	if hasExp && now.After(exp) {
		return "", ErrInvalidJWT // ExpiredJwtException
	}
	nbf, hasNbf, ok := numericDateClaim(claims, "nbf")
	if !ok {
		return "", ErrInvalidJWT
	}
	if hasNbf && now.Before(nbf) {
		return "", ErrInvalidJWT // PrematureJwtException
	}
	if !hasExp {
		// Kotlin: claims.expiration is null → NPE → 500. Only reachable with a
		// forged-but-correctly-signed token; mirror as an empty-token style crash.
		return "", errors.New("NullPointerException: missing exp")
	}
	// isCorrectSignatureAndTokenNotExpired: expiration.after(Date())
	if !exp.After(now) {
		return "", ErrInvalidJWT
	}
	sub, _ := claims["sub"].(string)
	return sub, nil
}

func numericDateClaim(c map[string]any, name string) (time.Time, bool, bool) {
	v, present := c[name]
	if !present || v == nil {
		return time.Time{}, false, true
	}
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return time.UnixMilli(i * 1000), true, true
		}
		if f, err := n.Float64(); err == nil {
			return time.UnixMilli(int64(f * 1000)), true, true
		}
	case string:
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return time.UnixMilli(i * 1000), true, true
		}
		if t, err := time.Parse(time.RFC3339, n); err == nil {
			return t, true, true
		}
	}
	return time.Time{}, false, false
}

func decodeB64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// ExtractBearer mirrors JwtTokenUtils.extractJwtFromHeader: the header must
// start with exactly "Bearer "; the remainder is trimmed (Kotlin trim).
func ExtractBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	return kotlinTrim(header[len(prefix):]), true
}
