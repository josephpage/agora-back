package acme

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// AcmeCryptoHelperTest

func keyBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestEncryptThenDecryptReturnsThePlaintext(t *testing.T) {
	h := &CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(32)}}
	for _, plain := range []string{"my-super-secret-private-key-pem", "", "é😀\n-----BEGIN-----\n"} {
		enc, err := h.Encrypt(plain)
		mustNoErr(t, err)
		dec, err := h.Decrypt(enc)
		mustNoErr(t, err)
		if dec != plain {
			t.Errorf("decrypted %q, want %q", dec, plain)
		}
	}
}

func TestEncryptUsesARandomIV(t *testing.T) {
	h := &CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(32)}}
	a, _ := h.Encrypt("same-input-text")
	b, _ := h.Encrypt("same-input-text")
	if a == b {
		t.Error("two encryptions of the same text are identical")
	}
	// IV (12) | ciphertext (len) | tag (16), standard padded Base64
	raw, err := base64.StdEncoding.DecodeString(a)
	mustNoErr(t, err)
	if len(raw) != 12+len("same-input-text")+16 {
		t.Errorf("stored length %d", len(raw))
	}
}

func TestEncryptFailsWithAnInvalidKey(t *testing.T) {
	for _, c := range []struct{ name, key, msg string }{
		{"blank", "", "ACME_ENCRYPTION_KEY is not configured"},
		{"only spaces", "  \t", "ACME_ENCRYPTION_KEY is not configured"},
		{"not base64", "this-is-not-valid-base64!!!", "not a valid base64"},
		{"16 bytes", keyBytes(16), "must be exactly 32 bytes"},
		{"33 bytes", keyBytes(33), "must be exactly 32 bytes"},
	} {
		h := &CryptoHelper{cfg: &Config{EncryptionKey: c.key}}
		_, err := h.Encrypt("anything")
		var ise *IllegalStateError
		if !errors.As(err, &ise) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: %v", c.name, err)
		}
		if _, err := h.Decrypt("AAAA"); !errors.As(err, &ise) {
			t.Errorf("%s: decrypt: %v", c.name, err)
		}
	}
	// the message of the wrong size carries the decoded size
	_, err := (&CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(16)}}).Encrypt("x")
	if !strings.Contains(err.Error(), "Got 16 bytes.") {
		t.Errorf("message %v", err)
	}
}

func TestDecryptRejectsBadValues(t *testing.T) {
	h := &CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(32)}}
	enc, _ := h.Encrypt("secret")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	tampered := append([]byte(nil), raw...)
	tampered[13] ^= 1
	other := &CryptoHelper{cfg: &Config{EncryptionKey: strings.Repeat("A", 43) + "="}}
	for name, v := range map[string]string{
		"tampered":     base64.StdEncoding.EncodeToString(tampered),
		"truncated":    base64.StdEncoding.EncodeToString(raw[:len(raw)-1]),
		"only the IV":  base64.StdEncoding.EncodeToString(raw[:12]),
		"shorter":      base64.StdEncoding.EncodeToString(raw[:5]),
		"not base64":   "***",
		"empty":        "",
		"unpadded":     strings.TrimRight(enc, "="),
		"wrong key":    enc,
		"URL alphabet": strings.NewReplacer("+", "-", "/", "_").Replace(enc),
	} {
		hh := h
		if name == "wrong key" {
			hh = other
		}
		got, err := hh.Decrypt(v)
		if name == "unpadded" || name == "URL alphabet" {
			// java.util.Base64.getDecoder() accepts a missing padding, never the URL alphabet
			if name == "unpadded" && (err != nil || got != "secret") {
				t.Errorf("%s: %q %v", name, got, err)
			}
			if name == "URL alphabet" && strings.ContainsAny(enc, "+/") && err == nil {
				t.Errorf("%s: accepted", name)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted (%q)", name, got)
		}
	}
}

func TestDecryptReplacesInvalidUTF8(t *testing.T) {
	// the plaintext is built from the bytes of an encrypted invalid sequence: String(bytes, UTF_8)
	h := &CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(32)}}
	enc, err := h.Encrypt("a\xffb")
	mustNoErr(t, err)
	got, err := h.Decrypt(enc)
	mustNoErr(t, err)
	if got != "a�b" {
		t.Errorf("%q", got)
	}
}
