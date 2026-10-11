package acme

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"hash"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/acme"

	"agora/internal/jsonjava"
	"agora/parity/oracle"
)

type m = map[string]any

func requireOracle(t *testing.T) {
	t.Helper()
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
}

func call[T any](t *testing.T, fn string, args any) T {
	t.Helper()
	var out T
	oracle.MustCall(t, fn, args, &out)
	return out
}

type keyDesc struct {
	Algorithm string `json:"algorithm"`
	Pkcs8B64  string `json:"pkcs8B64"`
	X509B64   string `json:"x509B64"`
	Pem       string `json:"pem"`
}

func pkcs8B64(t *testing.T, k crypto.Signer) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := CreateKeyPair(2048)
	if err != nil {
		t.Fatal(err)
	}
	return k.(*rsa.PrivateKey)
}

// KeyPairUtils.writeKeyPair / readKeyPair: the PEM formats are byte identical in both directions.
func TestOracleKeyPairPEM(t *testing.T) {
	requireOracle(t)
	for i := 0; i < 3; i++ {
		// Java → Go: read the PEM acme4j wrote, write it back
		jk := call[keyDesc](t, "acmeKeyPairCreate", m{"bits": 2048})
		if !strings.HasPrefix(jk.Pem, "-----BEGIN RSA PRIVATE KEY-----\n") || !strings.HasSuffix(jk.Pem, "-----END RSA PRIVATE KEY-----\n") {
			t.Fatalf("unexpected acme4j PEM header/footer: %q", jk.Pem)
		}
		gk, err := ReadKeyPair(jk.Pem)
		if err != nil {
			t.Fatalf("Go cannot read the acme4j PEM: %v", err)
		}
		if got := pkcs8B64(t, gk); got != jk.Pkcs8B64 {
			t.Errorf("private key differs after reading the acme4j PEM")
		}
		back, err := WriteKeyPair(gk)
		if err != nil {
			t.Fatal(err)
		}
		if back != jk.Pem {
			t.Errorf("Go write of a Java key differs from KeyPairUtils.writeKeyPair:\n%q\n%q", back, jk.Pem)
		}

		// Go → Java: acme4j reads the PEM written by Go and writes the same text back
		k := testKey(t)
		pem, err := WriteKeyPair(k)
		if err != nil {
			t.Fatal(err)
		}
		jr := call[keyDesc](t, "acmeKeyPairRead", m{"pem": pem})
		if jr.Pkcs8B64 != pkcs8B64(t, k) {
			t.Errorf("acme4j read a different private key from the Go PEM")
		}
		pub, _ := x509.MarshalPKIXPublicKey(k.Public())
		if jr.X509B64 != base64.StdEncoding.EncodeToString(pub) {
			t.Errorf("acme4j derived a different public key from the Go PEM")
		}
		if w := call[string](t, "acmeKeyPairWrite", m{"pkcs8B64": pkcs8B64(t, k)}); w != pem {
			t.Errorf("KeyPairUtils.writeKeyPair differs from WriteKeyPair:\n%q\n%q", w, pem)
		}
	}
}

// KeyPairUtils.readKeyPair refuses what the Go reader refuses (PKCS#8 is a ClassCastException).
func TestOracleKeyPairReadRejects(t *testing.T) {
	requireOracle(t)
	k := testKey(t)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(k)
	cases := map[string]string{
		"pkcs8":   string(pemBlock("PRIVATE KEY", pkcs8)),
		"empty":   "",
		"garbage": "not a pem",
		"public":  string(pemBlock("PUBLIC KEY", mustPub(t, k))),
	}
	for name, pem := range cases {
		if _, err := ReadKeyPair(pem); err == nil {
			t.Errorf("%s: Go accepted the PEM", name)
		}
		var out keyDesc
		if err := oracle.Call("acmeKeyPairRead", m{"pem": pem}, &out); err == nil {
			t.Errorf("%s: acme4j accepted the PEM", name)
		}
	}
}

func mustPub(t *testing.T, k *rsa.PrivateKey) []byte {
	b, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// AcmeCryptoHelper, both directions.
func TestOracleAESGCMInterop(t *testing.T) {
	requireOracle(t)
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	h := &CryptoHelper{cfg: &Config{EncryptionKey: key}}
	for _, plain := range []string{"", "x", "héllo 😀", strings.Repeat("-----BEGIN RSA PRIVATE KEY-----\nMIIE\n", 80)} {
		enc, err := h.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		if got := call[string](t, "aesGcmDecrypt", m{"key": key, "ciphertext": enc}); got != plain {
			t.Errorf("Java cannot read the Go ciphertext of %q: %q", plain, got)
		}
		jenc := call[string](t, "aesGcmEncrypt", m{"key": key, "plaintext": plain})
		got, err := h.Decrypt(jenc)
		if err != nil || got != plain {
			t.Errorf("Go cannot read the Java ciphertext of %q: %q %v", plain, got, err)
		}
	}
	// key validation: same failures
	for _, bad := range []string{"", " ", "***", base64.StdEncoding.EncodeToString(make([]byte, 16)), base64.StdEncoding.EncodeToString(make([]byte, 33))} {
		_, gerr := (&CryptoHelper{cfg: &Config{EncryptionKey: bad}}).Encrypt("x")
		jerr := oracle.Call("aesGcmEncrypt", m{"key": bad, "plaintext": "x"}, new(string))
		if (gerr == nil) != (jerr == nil) {
			t.Errorf("key %q: go err=%v, java err=%v", bad, gerr, jerr)
		}
		if _, ok := gerr.(*IllegalStateError); !ok {
			t.Errorf("key %q: Go error %T is not an IllegalStateError", bad, gerr)
		}
	}
	// corrupted values fail on both sides
	enc, _ := h.Encrypt("secret")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 1
	bad := base64.StdEncoding.EncodeToString(raw)
	if _, err := h.Decrypt(bad); err == nil {
		t.Error("Go decrypted a tampered value")
	}
	if err := oracle.Call("aesGcmDecrypt", m{"key": key, "ciphertext": bad}, new(string)); err == nil {
		t.Error("Java decrypted a tampered value")
	}
}

// java.util.Base64 decoding rules (used for the encryption key, the stored values and the EAB key).
func TestOracleJavaBase64Decode(t *testing.T) {
	requireOracle(t)
	check := func(s string, url bool) {
		t.Helper()
		var want string
		jerr := oracle.Call("javaBase64Decode", m{"s": s, "url": url}, &want)
		got, gerr := JavaBase64Decode(s, url)
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("%q (url=%v): java err=%v, go err=%v", s, url, jerr, gerr)
			return
		}
		if jerr == nil && hex.EncodeToString(got) != want {
			t.Errorf("%q (url=%v): java %s, go %x", s, url, want, got)
		}
	}
	fixed := []string{"", "A", "AA", "AAA", "AAAA", "AA==", "AAA=", "A===", "AA=A", "AAAA=", "AAAA==", "=", "==", "AB", "ABC", "ABCD", "ABCDE", "ABCDEF", "ABCDEFG", "ABCDEFGH",
		"AA==AAAA", "A=", "AA=", " AAAA", "AAAA ", "AA AA", "AA\nAA", "+/+/", "-_-_", "é", "AAAé", "ĀAAA", "Zm9v", "Zm9vYg==", "Zm9vYg", "Zm9vYmE=", "Zm9vYmE", "Zm9vYmFy", "Zm9vYmFy=", "Zm9vYmFy===="}
	for _, s := range fixed {
		check(s, false)
		check(s, true)
	}
	r := rand.New(rand.NewSource(11))
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/-_=. \n"
	for i := 0; i < 2000; i++ {
		n := r.Intn(14)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteByte(alphabet[r.Intn(len(alphabet))])
		}
		check(b.String(), false)
		check(b.String(), true)
	}
}

// AcmeUtils.toAce
func TestOracleToACE(t *testing.T) {
	requireOracle(t)
	for _, s := range []string{"", "example.com", "Example.COM", " example.com ", "a.b.c.d", "www.agora.gouv.fr", "example.com.", ".", "a..b", "ünicode.fr", "bücher.example", "日本語.jp",
		"xn--bcher-kva.example", strings.Repeat("a", 63) + ".com", strings.Repeat("a", 64) + ".com", "A\tB", "ａｂｃ.com", "ex。ample．com", "-lead.com", "under_score.com", "wild*.com"} {
		var want string
		jerr := oracle.Call("acmeToAce", m{"s": s}, &want)
		got, gerr := ToACE(s)
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("%q: java err=%v, go err=%v", s, jerr, gerr)
			continue
		}
		if jerr == nil && got != want {
			t.Errorf("%q: java %q, go %q", s, want, got)
		}
	}
}

// CSRBuilder
func TestOracleCSR(t *testing.T) {
	requireOracle(t)
	k := testKey(t)
	for _, domains := range [][]string{{"agora.gouv.fr"}, {"agora.gouv.fr", "www.agora.gouv.fr"}, {"a.example", "b.example", "C.Example"}, {"bücher.example", "x.example"}} {
		jder, err := base64.StdEncoding.DecodeString(call[string](t, "acmeCsr", m{"domains": domains, "pkcs8B64": pkcs8B64(t, k)}))
		if err != nil {
			t.Fatal(err)
		}
		gder, err := BuildCSR(domains, k)
		if err != nil {
			t.Fatal(err)
		}
		jc, err := x509.ParseCertificateRequest(jder)
		if err != nil {
			t.Fatalf("parse Java CSR: %v", err)
		}
		gc, err := x509.ParseCertificateRequest(gder)
		if err != nil {
			t.Fatalf("parse Go CSR: %v", err)
		}
		if err := gc.CheckSignature(); err != nil {
			t.Errorf("%v: Go CSR signature: %v", domains, err)
		}
		if gc.Subject.CommonName != jc.Subject.CommonName {
			t.Errorf("%v: CN go %q java %q", domains, gc.Subject.CommonName, jc.Subject.CommonName)
		}
		if !reflect.DeepEqual(gc.DNSNames, jc.DNSNames) {
			t.Errorf("%v: SANs go %v java %v", domains, gc.DNSNames, jc.DNSNames)
		}
		if gc.SignatureAlgorithm != jc.SignatureAlgorithm {
			t.Errorf("%v: signature algorithm go %v java %v", domains, gc.SignatureAlgorithm, jc.SignatureAlgorithm)
		}
		if !reflect.DeepEqual(gc.PublicKey, jc.PublicKey) {
			t.Errorf("%v: public key differs", domains)
		}
	}
}

// External account binding: algorithm chosen from the MAC key size, same JWK payload, valid MAC.
func TestOracleEAB(t *testing.T) {
	requireOracle(t)
	k := testKey(t)
	pubDER := mustPub(t, k)
	const url = "https://acme.example/new-account"
	for _, size := range []int{32, 48, 64, 16, 20, 33, 0} {
		raw := make([]byte, size)
		rand.New(rand.NewSource(int64(size))).Read(raw)
		macKey := base64.RawURLEncoding.EncodeToString(raw)
		var jv map[string]string
		jerr := oracle.Call("acmeEab", m{"kid": "kid-1", "macKey": macKey, "x509B64": base64.StdEncoding.EncodeToString(pubDER), "url": url}, &jv)
		gv, gerr := externalAccountBinding("kid-1", k.Public(), raw, url)
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("size %d: java err=%v, go err=%v", size, jerr, gerr)
			continue
		}
		if jerr != nil {
			continue
		}
		var jh, gh map[string]string
		decodeSegment(t, jv["protected"], &jh)
		decodeSegment(t, gv["protected"].(string), &gh)
		if !reflect.DeepEqual(jh, gh) {
			t.Errorf("size %d: header java %v go %v", size, jh, gh)
		}
		var jp, gp map[string]string
		decodeSegment(t, jv["payload"], &jp)
		decodeSegment(t, gv["payload"].(string), &gp)
		if !reflect.DeepEqual(jp, gp) {
			t.Errorf("size %d: JWK java %v go %v", size, jp, gp)
		}
		var newH func() hash.Hash
		switch jh["alg"] {
		case "HS256":
			newH = sha256.New
		case "HS384":
			newH = sha512.New384
		default:
			newH = sha512.New
		}
		mac := hmac.New(newH, raw)
		mac.Write([]byte(jv["protected"] + "." + jv["payload"]))
		if got := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); got != jv["signature"] {
			t.Errorf("size %d: the Java MAC is not %s over protected.payload", size, jh["alg"])
		}
		mac = hmac.New(newH, raw)
		mac.Write([]byte(gv["protected"].(string) + "." + gv["payload"].(string)))
		if got := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); got != gv["signature"] {
			t.Errorf("size %d: the Go MAC is wrong", size)
		}
	}
}

func decodeSegment(t *testing.T, seg string, v any) {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// Http01Challenge.getAuthorization == x/crypto/acme's HTTP01ChallengeResponse.
func TestOracleKeyAuthorization(t *testing.T) {
	requireOracle(t)
	k := testKey(t)
	cl := &acme.Client{Key: k}
	for _, token := range []string{"abc", "a-b_c", "evaGxfADs6pSRb2LAv9IZf17Dt3juxGJ-PCt92wr-oA", ""} {
		want := call[string](t, "acmeKeyAuthorization", m{"pkcs8B64": pkcs8B64(t, k), "token": token})
		got, err := cl.HTTP01ChallengeResponse(token)
		if err != nil || got != want {
			t.Errorf("token %q: java %q, go %q (%v)", token, want, got, err)
		}
	}
}

// AcmeConfig.allDomains
func TestOracleAllDomains(t *testing.T) {
	requireOracle(t)
	for _, c := range [][2]string{{"agora.gouv.fr", ""}, {"agora.gouv.fr", "www.agora.gouv.fr"}, {"a", " b , ,c,,\t"}, {"", "x"}, {"a", ","}, {"a", " b "}, {"a", " "}} {
		want := call[[]string](t, "acmeAllDomains", m{"domain": c[0], "sans": c[1]})
		got := (&Config{Domain: c[0], Sans: c[1]}).AllDomains()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q/%q: java %q, go %q", c[0], c[1], want, got)
		}
	}
}

// The certificate chain PEM built by downloadCertificate().
func TestOracleChainPEM(t *testing.T) {
	requireOracle(t)
	r := rand.New(rand.NewSource(5))
	for _, sizes := range [][]int{{1}, {47, 48, 49}, {500, 3}, {1, 2, 3, 4}} {
		var ders [][]byte
		var b64s []string
		for _, n := range sizes {
			b := make([]byte, n)
			r.Read(b)
			ders = append(ders, b)
			b64s = append(b64s, base64.StdEncoding.EncodeToString(b))
		}
		if want, got := call[string](t, "acmeChainPem", m{"der": b64s}), CertificateChainPEM(ders); got != want {
			t.Errorf("%v: java %q go %q", sizes, want, got)
		}
	}
}

// The request body of POST /admin/acme/upload-certificate (LocalDateTime read by the Spring mapper).
func TestOracleUploadBodyDecoding(t *testing.T) {
	requireOracle(t)
	const class = "fr.gouv.agora.infrastructure.acme.AcmeCertificateUploadJson"
	exp := []string{`"2027-01-01T00:00:00"`, `"2027-01-01T00:00"`, `"2027-01-01T10:20:30.123456789"`, `"2027-01-01T10:20:30.1234567891"`, `" 2027-01-01T00:00:00 "`,
		`"2027-01-01T00:00:00Z"`, `"2027-01-01T00:00:00+02:00"`, `"2027-01-01"`, `"2027-13-01T00:00:00"`, `"2027-02-30T00:00:00"`, `"2027-01-01T24:00:00"`, `"2027-01-01t10:00:00"`,
		`""`, `" "`, `"not a date"`, `null`, `[2027,1,1,10,20]`, `[2027,1,1,10,20,30]`, `[2027,1,1,10,20,30,123456789]`, `[2027,1,1]`, `[2027,1,1,10,20,30,1,5]`, `[2027,1,1,25,0]`,
		`[2027.5,1,1,10,20]`, `["2027",1,1,10,20]`, `1798761600`, `1798761600.5`, `true`, `{}`, `{"year":2027}`, `"+12027-01-01T00:00:00"`, `"-0001-01-01T00:00:00"`, `"0000-01-01T00:00:00"`, `"999999999-01-01T00:00:00"`}
	var rows []string
	for _, e := range exp {
		rows = append(rows, `{"certificatePem":"c","privateKeyPem":"k","expiresAt":`+e+`}`)
	}
	rows = append(rows,
		`{"certificatePem":"c","privateKeyPem":"k"}`, `{"certificatePem":"c","expiresAt":"2027-01-01T00:00:00"}`, `{"privateKeyPem":"k","expiresAt":"2027-01-01T00:00:00"}`,
		`{"certificatePem":null,"privateKeyPem":"k","expiresAt":"2027-01-01T00:00:00"}`, `{"certificatePem":1,"privateKeyPem":true,"expiresAt":"2027-01-01T00:00:00"}`,
		`{"certificatePem":"","privateKeyPem":"","expiresAt":"2027-01-01T00:00:00","extra":1}`, `{"certificatePem":["x"],"privateKeyPem":"k","expiresAt":"2027-01-01T00:00:00"}`,
		`[]`, `{}`, `null`, ``, `{"certificatePem":"c","privateKeyPem":"k","expiresAt":"2027-01-01T00:00:00"} trailing`)
	for _, body := range rows {
		var want string
		jerr := oracle.Call("jsonDecode", m{"className": class, "json": body}, &want)
		javaOK := jerr == nil && want != "null"
		var v AcmeCertificateUploadJSON
		gerr := jsonjava.UnmarshalRequest([]byte(body), "UTF-8", &v)
		if (gerr == nil) != javaOK {
			t.Errorf("%s: go err=%v, java ok=%v (%v)", body, gerr, javaOK, jerr)
		}
	}
}

func pemBlock(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}
