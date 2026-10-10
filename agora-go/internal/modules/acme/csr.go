package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"agora/internal/javacompat"
)

// BuildCSR is the CSRBuilder usage of the use case: every domain is added with
// addDomain (AcmeUtils.toAce), the first one becomes the CN of the subject,
// all of them are dNSName entries of the subjectAltName extension request, and
// the request is signed with SHA256withRSA (SHA256withECDSA for an EC key).
// The result is the DER encoding (CSRBuilder.getEncoded).
func BuildCSR(domains []string, key crypto.Signer) ([]byte, error) {
	if len(domains) == 0 {
		return nil, &IllegalStateError{"No domain or IP address was set"}
	}
	names := make([]string, 0, len(domains))
	for _, d := range domains {
		ace, err := ToACE(d)
		if err != nil {
			return nil, err
		}
		names = append(names, ace)
	}
	tpl := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: names[0]},
		DNSNames: names,
	}
	switch key.(type) {
	case *rsa.PrivateKey:
		tpl.SignatureAlgorithm = x509.SHA256WithRSA
	case *ecdsa.PrivateKey:
		tpl.SignatureAlgorithm = x509.ECDSAWithSHA256
	}
	return x509.CreateCertificateRequest(rand.Reader, tpl, key)
}

// ToACE is AcmeUtils.toAce: IDN.toASCII(domain.trim()).toLowerCase().
// Labels made of ASCII only are kept as they are (only their length is
// checked); other labels are NFKC normalized, lower-cased and Punycode encoded
// (nameprep is approximated by NFKC + lower casing).
func ToACE(domain string) (string, error) {
	s := javacompat.JavaStringTrim(domain)
	if s == "." || s == "。" || s == "．" || s == "｡" {
		return ".", nil
	}
	isDot := func(r rune) bool { return r == '.' || r == '。' || r == '．' || r == '｡' }
	var out strings.Builder
	runes := []rune(s)
	for p := 0; p < len(runes); {
		q := p
		for q < len(runes) && !isDot(runes[q]) {
			q++
		}
		label, err := toASCIILabel(string(runes[p:q]))
		if err != nil {
			return "", err
		}
		out.WriteString(label)
		if q != len(runes) {
			out.WriteByte('.')
		}
		p = q + 1
	}
	return javacompat.KotlinLowercase(out.String()), nil
}

func toASCIILabel(label string) (string, error) {
	ascii := true
	for i := 0; i < len(label); i++ {
		if label[i] >= 0x80 {
			ascii = false
			break
		}
	}
	dest := label
	if !ascii {
		preppedStr := javacompat.KotlinLowercase(norm.NFKC.String(label))
		dest = preppedStr
		for i := 0; i < len(preppedStr); i++ {
			if preppedStr[i] >= 0x80 { // still not ASCII after nameprep: Punycode
				enc, err := punycodeEncode([]rune(preppedStr))
				if err != nil {
					return "", err
				}
				dest = "xn--" + enc
				break
			}
		}
	}
	if len(dest) == 0 {
		return "", errors.New("IllegalArgumentException: Empty label is not a legal name")
	}
	if len(dest) > 63 {
		return "", errors.New("IllegalArgumentException: The label in the input is too long")
	}
	return dest, nil
}

// punycodeEncode is RFC 3492 section 6.3.
func punycodeEncode(input []rune) (string, error) {
	const (
		base, tMin, tMax, skew, damp = 36, 1, 26, 38, 700
		initialBias, initialN        = 72, 128
	)
	adapt := func(delta, numPoints int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / numPoints
		k := 0
		for delta > ((base-tMin)*tMax)/2 {
			delta /= base - tMin
			k += base
		}
		return k + (base-tMin+1)*delta/(delta+skew)
	}
	digit := func(d int) byte {
		if d < 26 {
			return byte('a' + d)
		}
		return byte('0' + d - 26)
	}
	var out []byte
	for _, r := range input {
		if r < 0x80 {
			out = append(out, byte(r))
		}
	}
	b := len(out)
	h := b
	if b > 0 {
		out = append(out, '-')
	}
	n, delta, bias := initialN, 0, initialBias
	for h < len(input) {
		m := int(^uint32(0) >> 1)
		for _, r := range input {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range input {
			if int(r) < n {
				delta++
			}
			if int(r) == n {
				q := delta
				for k := base; ; k += base {
					t := k - bias
					if t < tMin {
						t = tMin
					} else if t > tMax {
						t = tMax
					}
					if q < t {
						break
					}
					out = append(out, digit(t+(q-t)%(base-t)))
					q = (q - t) / (base - t)
				}
				out = append(out, digit(q))
				bias = adapt(delta, h+1, h == b)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	if !utf8.Valid(out) {
		return "", fmt.Errorf("punycode: invalid output")
	}
	return string(out), nil
}
