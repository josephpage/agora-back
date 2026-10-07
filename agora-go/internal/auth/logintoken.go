package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/pbkdf2"

	"agora/internal/javacompat"
)

func kotlinTrim(s string) string { return javacompat.KotlinTrim(s) }

// LoginTokenConfig holds the LOGIN_TOKEN_{ENCODE,DECODE}_* environment values.
type LoginTokenConfig struct {
	EncodeSecret, EncodeTransformation, EncodeAlgorithm string
	DecodeSecret, DecodeTransformation, DecodeAlgorithm string
}

// LoginTokens reproduces fr.gouv.agora.infrastructure.login.LoginTokenGenerator.
// Only "AES/ECB/PKCS5Padding" (and "AES", which the JCE maps to the same
// mode) with algorithm "AES" is supported — the only configuration in use.
type LoginTokens struct {
	enc, dec cipher.Block
	encErr   error
	decErr   error
}

// NewLoginTokens prepares the ciphers once (Kotlin rebuilt them per call).
func NewLoginTokens(c LoginTokenConfig) *LoginTokens {
	lt := &LoginTokens{}
	lt.enc, lt.encErr = buildBlock(c.EncodeSecret, c.EncodeTransformation, c.EncodeAlgorithm)
	lt.dec, lt.decErr = buildBlock(c.DecodeSecret, c.DecodeTransformation, c.DecodeAlgorithm)
	return lt
}

func buildBlock(secret, transformation, algorithm string) (cipher.Block, error) {
	t := strings.ToUpper(transformation)
	if t != "AES/ECB/PKCS5PADDING" && t != "AES" {
		return nil, fmt.Errorf("unsupported transformation %q", transformation)
	}
	if !strings.EqualFold(algorithm, "AES") {
		return nil, fmt.Errorf("unsupported algorithm %q", algorithm)
	}
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		// java.util.Base64 basic decoder accepts missing padding
		key, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(secret, "="))
		if err != nil {
			return nil, err
		}
	}
	return aes.NewCipher(key)
}

// Build reproduces buildLoginToken: Base64(AES-ECB-PKCS5({"userId":"…"})).
func (lt *LoginTokens) Build(userID string) (string, error) {
	if lt.encErr != nil {
		return "", lt.encErr
	}
	plain := []byte(`{"userId":` + jsonJacksonString(userID) + `}`)
	bs := lt.enc.BlockSize()
	pad := bs - len(plain)%bs
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(plain))
	for i := 0; i < len(plain); i += bs {
		lt.enc.Encrypt(out[i:i+bs], plain[i:i+bs])
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// ErrLoginTokenDecode is DecodeResult.Failure.
var ErrLoginTokenDecode = errors.New("login token decode failure")

// Decode reproduces decodeLoginToken. Any failure → ErrLoginTokenDecode.
func (lt *LoginTokens) Decode(token string) (string, error) {
	if lt.decErr != nil {
		return "", ErrLoginTokenDecode
	}
	raw, err := decodeJavaBasicBase64(token)
	if err != nil {
		return "", ErrLoginTokenDecode
	}
	bs := lt.dec.BlockSize()
	if len(raw) == 0 || len(raw)%bs != 0 {
		return "", ErrLoginTokenDecode
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += bs {
		lt.dec.Decrypt(out[i:i+bs], raw[i:i+bs])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > bs || pad > len(out) {
		return "", ErrLoginTokenDecode
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return "", ErrLoginTokenDecode
		}
	}
	out = out[:len(out)-pad]
	// jacksonObjectMapper().readValue(bytes, LoginTokenDataJson) with
	// @JsonIgnoreProperties(ignoreUnknown = true); userId non-null String.
	var m map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(&m); err != nil || m == nil {
		return "", ErrLoginTokenDecode
	}
	rawID, ok := m["userId"]
	if !ok {
		return "", ErrLoginTokenDecode
	}
	var id any
	if err := json.Unmarshal(rawID, &id); err != nil {
		return "", ErrLoginTokenDecode
	}
	switch v := id.(type) {
	case string:
		return v, nil
	case float64, bool:
		// Jackson coerces scalars to String
		return strings.TrimSpace(string(rawID)), nil
	}
	return "", ErrLoginTokenDecode
}

// decodeJavaBasicBase64 mirrors java.util.Base64.getDecoder().decode(String):
// standard alphabet only, padding optional, no whitespace.
func decodeJavaBasicBase64(s string) ([]byte, error) {
	trimmed := s
	if i := strings.IndexByte(s, '='); i >= 0 {
		trimmed = s[:i]
		rest := s[i:]
		// padding must be "=" or "==" and complete the final quantum
		if rest != "=" && rest != "==" {
			return nil, errors.New("illegal padding")
		}
		if (len(trimmed)+len(rest))%4 != 0 {
			return nil, errors.New("illegal padding length")
		}
	}
	if len(trimmed)%4 == 1 {
		return nil, errors.New("last unit does not have enough valid bits")
	}
	return base64.RawStdEncoding.DecodeString(trimmed)
}

func jsonJacksonString(s string) string {
	// Jackson escaping (subset sufficient for UUID-like ids, kept generic).
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20:
			switch r {
			case '\n':
				b.WriteString(`\n`)
			case '\t':
				b.WriteString(`\t`)
			case '\r':
				b.WriteString(`\r`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			default:
				fmt.Fprintf(&b, `\u%04X`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// IPHasher reproduces IpAddressUtils.hash (PBKDF2WithHmacSHA512) with a
// bounded memo: the hash is deterministic so caching is invisible.
type IPHasher struct {
	salt       []byte
	iterations int
	keyBytes   int
	err        error

	mu   sync.Mutex
	memo map[string]string
	ring []string
	next int
}

// NewIPHasher validates the REMOTE_ADDRESS_* settings like the Kotlin code
// does lazily (invalid settings make every hashed request fail with 500).
func NewIPHasher(algorithm, iterations, keyLength, salt string) *IPHasher {
	h := &IPHasher{salt: []byte(salt), memo: make(map[string]string), ring: make([]string, 100_000)}
	it, ok := javacompat.KotlinToIntOrNull(iterations)
	if !ok {
		h.err = errors.New("Invalid remoteAddress hash iterations number")
		return h
	}
	kl, ok := javacompat.KotlinToIntOrNull(keyLength)
	if !ok {
		h.err = errors.New("Invalid remoteAddress hash keyLength number")
		return h
	}
	if algorithm != "PBKDF2WithHmacSHA512" {
		h.err = fmt.Errorf("NoSuchAlgorithmException: %s", algorithm)
		return h
	}
	if it <= 0 || kl <= 0 || kl%8 != 0 {
		h.err = errors.New("InvalidKeySpecException")
		return h
	}
	h.iterations, h.keyBytes = it, kl/8
	return h
}

// Hash returns the lowercase hex PBKDF2 of ip, or an error (→ HTTP 500).
func (h *IPHasher) Hash(ip string) (string, error) {
	if h.err != nil {
		return "", h.err
	}
	h.mu.Lock()
	if v, ok := h.memo[ip]; ok {
		h.mu.Unlock()
		return v, nil
	}
	h.mu.Unlock()
	v := hex.EncodeToString(pbkdf2.Key([]byte(ip), h.salt, h.iterations, h.keyBytes, sha512.New))
	h.mu.Lock()
	if old := h.ring[h.next]; old != "" {
		delete(h.memo, old)
	}
	h.ring[h.next] = ip
	h.next = (h.next + 1) % len(h.ring)
	h.memo[ip] = v
	h.mu.Unlock()
	return v, nil
}

// ClientIP mirrors IpAddressUtils.retrieveIpAddress.
func ClientIP(xForwardedFor, xRemoteAddress, remoteAddr string) string {
	if xForwardedFor != "" && !javacompat.KotlinIsBlank(xForwardedFor) {
		for _, a := range strings.Split(xForwardedFor, ",") {
			if !javacompat.KotlinIsBlank(a) {
				return javacompat.KotlinTrim(a)
			}
		}
	}
	if xRemoteAddress != "" && !javacompat.KotlinIsBlank(xRemoteAddress) {
		return javacompat.KotlinTrim(xRemoteAddress)
	}
	return javacompat.KotlinTrim(remoteAddr)
}
