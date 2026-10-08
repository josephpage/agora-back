package acme

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"agora/internal/javacompat"
)

// CryptoHelper is infrastructure/acme/repository/AcmeCryptoHelper.kt:
// AES-256-GCM, 12 byte random IV, 128 bit tag; the stored value is the
// standard (padded) base64 of IV | ciphertext | tag. ACME_ENCRYPTION_KEY is
// the standard base64 of exactly 32 bytes. It is read on every call, like the
// Kotlin helper does.
type CryptoHelper struct{ cfg *Config }

const (
	gcmIVLength = 12
	aesKeySize  = 32
)

// IllegalStateError mirrors the IllegalStateException of the Kotlin code.
type IllegalStateError struct{ Msg string }

func (e *IllegalStateError) Error() string { return "IllegalStateException: " + e.Msg }

// Encrypt is AcmeCryptoHelper.encrypt.
func (h *CryptoHelper) Encrypt(plain string) (string, error) {
	key, err := h.resolveKey()
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcmIVLength)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	out := gcm.Seal(append([]byte(nil), iv...), iv, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt is AcmeCryptoHelper.decrypt (invalid UTF-8 in the plaintext is
// replaced like Java's String(bytes, UTF_8)).
func (h *CryptoHelper) Decrypt(cipherTextBase64 string) (string, error) {
	key, err := h.resolveKey()
	if err != nil {
		return "", err
	}
	combined, err := JavaBase64Decode(cipherTextBase64, false)
	if err != nil {
		return "", err
	}
	if len(combined) < gcmIVLength {
		// copyOfRange(12, size) with size < 12
		return "", errors.New("ArrayIndexOutOfBoundsException: ciphertext shorter than the IV")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, combined[:gcmIVLength], combined[gcmIVLength:], nil)
	if err != nil {
		return "", fmt.Errorf("AEADBadTagException: %w", err)
	}
	return javacompat.DecodeUTF8Java(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (h *CryptoHelper) resolveKey() ([]byte, error) {
	k := h.cfg.EncryptionKey
	if javacompat.KotlinIsBlank(k) {
		return nil, &IllegalStateError{"ACME_ENCRYPTION_KEY is not configured. Cannot encrypt/decrypt ACME private keys."}
	}
	b, err := JavaBase64Decode(k, false)
	if err != nil {
		return nil, &IllegalStateError{"ACME_ENCRYPTION_KEY is not a valid base64 string."}
	}
	if len(b) != aesKeySize {
		return nil, &IllegalStateError{fmt.Sprintf("ACME_ENCRYPTION_KEY must be exactly 32 bytes (256 bits) when decoded. Got %d bytes.", len(b))}
	}
	return b, nil
}

var (
	b64Std = func() (t [256]int8) {
		for i := range t {
			t[i] = -1
		}
		const a = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
		for i := 0; i < len(a); i++ {
			t[a[i]] = int8(i)
		}
		t['='] = -2
		return
	}()
	b64URL = func() (t [256]int8) {
		for i := range t {
			t[i] = -1
		}
		const a = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
		for i := 0; i < len(a); i++ {
			t[a[i]] = int8(i)
		}
		t['='] = -2
		return
	}()
)

// JavaBase64Decode is java.util.Base64.getDecoder() (url=false) or
// getUrlDecoder() (url=true) applied to a String: the characters are taken as
// ISO-8859-1 bytes, '=' padding is optional but must be well formed, any other
// character outside the alphabet is an error.
func JavaBase64Decode(s string, url bool) ([]byte, error) {
	src := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			r = '?' // unmappable in ISO-8859-1
		}
		src = append(src, byte(r))
	}
	table := &b64Std
	if url {
		table = &b64URL
	}
	sl := len(src)
	// decodedOutLength
	size := 0
	if sl != 0 {
		if sl < 2 {
			return nil, errors.New("IllegalArgumentException: Input byte[] should at least have 2 bytes for base64 bytes")
		}
		paddings := 0
		if src[sl-1] == '=' {
			paddings++
			if src[sl-2] == '=' {
				paddings++
			}
		}
		if paddings == 0 && sl&3 != 0 {
			paddings = 4 - sl&3
		}
		size = 3*((sl+3)/4) - paddings
	}
	dst := make([]byte, size)
	dp, sp, bits, shiftto := 0, 0, 0, 18
	put := func(b byte) error {
		if dp >= len(dst) {
			return errors.New("ArrayIndexOutOfBoundsException")
		}
		dst[dp] = b
		dp++
		return nil
	}
	for sp < sl {
		b := int(table[src[sp]])
		sp++
		if b < 0 {
			if b == -2 {
				if shiftto == 6 {
					if sp == sl {
						return nil, errors.New("IllegalArgumentException: Input byte array has wrong 4-byte ending unit")
					}
					next := src[sp]
					sp++
					if next != '=' {
						return nil, errors.New("IllegalArgumentException: Input byte array has wrong 4-byte ending unit")
					}
				} else if shiftto == 18 {
					return nil, errors.New("IllegalArgumentException: Input byte array has wrong 4-byte ending unit")
				}
				break
			}
			return nil, fmt.Errorf("IllegalArgumentException: Illegal base64 character %x", src[sp-1])
		}
		bits |= b << shiftto
		shiftto -= 6
		if shiftto < 0 {
			for _, v := range []byte{byte(bits >> 16), byte(bits >> 8), byte(bits)} {
				if err := put(v); err != nil {
					return nil, err
				}
			}
			shiftto, bits = 18, 0
		}
	}
	switch shiftto {
	case 6:
		if err := put(byte(bits >> 16)); err != nil {
			return nil, err
		}
	case 0:
		if err := put(byte(bits >> 16)); err != nil {
			return nil, err
		}
		if err := put(byte(bits >> 8)); err != nil {
			return nil, err
		}
	case 12:
		return nil, errors.New("IllegalArgumentException: Last unit does not have enough valid bits")
	}
	if sp < sl {
		return nil, fmt.Errorf("IllegalArgumentException: Input byte array has incorrect ending byte at %d", sp)
	}
	return dst[:dp], nil
}
