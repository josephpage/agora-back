// Package auth reproduces the Kotlin backend's authentication primitives:
// jjwt 0.11.5 compatible HMAC JWTs, the AES "loginToken" and the PBKDF2 IP hash.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
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

// ExtractBearer mirrors JwtTokenUtils.extractJwtFromHeader: the header must
// start with exactly "Bearer "; the remainder is trimmed (Kotlin trim).
func ExtractBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	return kotlinTrim(header[len(prefix):]), true
}
