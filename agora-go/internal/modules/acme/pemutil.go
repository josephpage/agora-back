package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// CreateKeyPair is KeyPairUtils.createKeyPair(2048): an RSA key pair.
func CreateKeyPair(bits int) (crypto.Signer, error) {
	return rsa.GenerateKey(rand.Reader, bits)
}

// WriteKeyPair is KeyPairUtils.writeKeyPair(keyPair, writer): BouncyCastle's
// JcaPEMWriter writes the private key of an RSA key pair as a PKCS#1
// "RSA PRIVATE KEY" block (64 column base64 lines, "\n" line endings), an EC
// key as an "EC PRIVATE KEY" block. This is the format stored (encrypted) in
// the database by the Kotlin application and the only one KeyPairUtils can read.
func WriteKeyPair(key crypto.Signer) (string, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})), nil
	case *ecdsa.PrivateKey:
		der, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return "", err
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
	}
	return "", errors.New("unsupported key type")
}

// ReadKeyPair is KeyPairUtils.readKeyPair(reader): PEMParser.readObject() cast
// to PEMKeyPair, which only exists for the traditional "RSA PRIVATE KEY" /
// "EC PRIVATE KEY" blocks (a PKCS#8 "PRIVATE KEY" is a ClassCastException on
// the Kotlin side, so it is an error here too).
func ReadKeyPair(pemText string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("Invalid PEM file")
	}
	if len(block.Headers) != 0 {
		return nil, fmt.Errorf("unsupported PEM object (%s with headers)", block.Type)
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("Invalid PEM file: %w", err)
		}
		return k, nil
	case "EC PRIVATE KEY":
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("Invalid PEM file: %w", err)
		}
		return k, nil
	}
	return nil, fmt.Errorf("ClassCastException: %s is not a PEMKeyPair", block.Type)
}

// CertificateChainPEM is downloadCertificate()'s buildString: every
// certificate as a CERTIFICATE block, base64 in 64 column lines
// (Base64.getMimeEncoder(64, "\n")), each block ending with a newline.
func CertificateChainPEM(chain [][]byte) string {
	var b strings.Builder
	for _, der := range chain {
		b.WriteString("-----BEGIN CERTIFICATE-----\n")
		b.WriteString(mimeBase64(der))
		b.WriteString("\n-----END CERTIFICATE-----\n")
	}
	return b.String()
}

func mimeBase64(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	var out strings.Builder
	for len(s) > 64 {
		out.WriteString(s[:64])
		out.WriteByte('\n')
		s = s[64:]
	}
	out.WriteString(s)
	return out.String()
}
