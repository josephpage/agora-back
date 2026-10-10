package acme

// Runner-free parity tests against a running parity environment (slot 1 by default):
// the Kotlin reference and the Go application, each with its own PostgreSQL database
// and its own ACME / Cloudflare stub. They cover what a YAML scenario cannot express
// (random keys and certificates, stub orders created per side, rows written by one
// implementation and read by the other):
//
//	PARITY_SLOT=1 PARITY_EXTRA_ENV_REF=parity/env/acme-ref.env PARITY_EXTRA_ENV_GO=parity/env/acme-go.env ./parity/run.sh up
//	S11_PARITY=1 go test ./internal/modules/acme/ -run Parity -v -count=1
//
// Overrides: PARITY_REF_URL, PARITY_GO_URL, PARITY_REF_DB, PARITY_GO_DB.

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/acme"

	"agora/internal/auth"
	"agora/parity/seed"
)

type pside struct {
	name   string
	base   string
	dbURL  string
	port   string
	jwt    string
	crypto *CryptoHelper
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(k, "#") {
			m[k] = v
		}
	}
	return m
}

var parityOnce sync.Once

func paritySides(t *testing.T) (ref, gov *pside) {
	t.Helper()
	if os.Getenv("S11_PARITY") != "1" {
		t.Skip("S11_PARITY!=1 (needs the parity environment, see the header of parity_it_test.go)")
	}
	root := filepath.Join("..", "..", "..", "parity", "env")
	common := readEnvFile(t, filepath.Join(root, "common.env"))
	acmeEnv := readEnvFile(t, filepath.Join(root, "acme-ref.env"))
	j := auth.NewJWT(common["JWT_SECRET"], nil)
	tok, _, err := j.Generate(seed.UserAdmin)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name, url, db, port string) *pside {
		return &pside{name: name, base: url, dbURL: db, port: port, jwt: tok,
			crypto: &CryptoHelper{cfg: &Config{EncryptionKey: acmeEnv["ACME_ENCRYPTION_KEY"]}}}
	}
	ref = mk("ref", envOr("PARITY_REF_URL", "http://localhost:8111"), envOr("PARITY_REF_DB", "postgres://backend:agora_password@localhost:5432/agora_ref_1"), "8111")
	gov = mk("go", envOr("PARITY_GO_URL", "http://localhost:8112"), envOr("PARITY_GO_DB", "postgres://backend:agora_password@localhost:5432/agora_go_1"), "8112")
	parityOnce.Do(func() {
		for _, s := range []*pside{ref, gov} {
			conn, err := pgx.Connect(context.Background(), s.dbURL)
			if err != nil {
				t.Fatalf("%s db: %v", s.name, err)
			}
			if err := seed.Seed(context.Background(), conn, time.Now().UTC().Truncate(time.Second), 1); err != nil {
				t.Fatalf("%s seed: %v", s.name, err)
			}
			conn.Close(context.Background())
		}
	})
	return
}

func both(ref, gov *pside, f func(s *pside)) {
	var wg sync.WaitGroup
	for _, s := range []*pside{ref, gov} {
		wg.Add(1)
		go func() { defer wg.Done(); f(s) }()
	}
	wg.Wait()
}

func (s *pside) do(t *testing.T, method, path string, hdr map[string]string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, s.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, "/admin/") {
		req.Header.Set("Authorization", "Bearer "+s.jwt)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := (&http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatalf("%s %s %s: %v", s.name, method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

func (s *pside) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), s.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %s: %v", s.name, sql, err)
	}
}

func (s *pside) reset(t *testing.T) {
	t.Helper()
	s.exec(t, "TRUNCATE acme_account, acme_certificate, acme_order, acme_challenge")
	if st, _, _ := s.do(t, "DELETE", "/admin/stub/acme/calls/reset", nil, nil); st != 200 {
		t.Fatalf("%s: stub reset: %d", s.name, st)
	}
}

func (s *pside) renew(t *testing.T) int {
	st, _, _ := s.do(t, "POST", "/admin/acme/renew-certificate", nil, nil)
	return st
}

var idRe = regexp.MustCompile(`/stub/acme/(order|authz|challenge|certificate)/[^/ ]+`)
var kvRe = regexp.MustCompile(`(orderId|authzId|token|certId|certPreview)=\S+`)

// calls returns the stub calls without GET /directory (acme4j re-reads an uncacheable
// directory several times per session, the Go client once: divergence S11-N1), with the
// random identifiers masked.
func (s *pside) calls(t *testing.T) []string {
	t.Helper()
	st, _, body := s.do(t, "GET", "/admin/stub/acme/calls", nil, nil)
	if st != 200 {
		t.Fatalf("%s: calls: %d", s.name, st)
	}
	var raw []struct{ Method, Endpoint, Summary string }
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range raw {
		if c.Method == "GET" && c.Endpoint == "/stub/acme/directory" {
			continue
		}
		ep := idRe.ReplaceAllString(c.Endpoint, "/stub/acme/$1/ID")
		ep = strings.Replace(ep, "/ID/finalize", "/ID/finalize", 1)
		sum := kvRe.ReplaceAllString(c.Summary, "$1=*")
		out = append(out, c.Method+" "+ep+" | "+sum)
	}
	return out
}

func sameSeq(t *testing.T, what string, a, b []string) {
	t.Helper()
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("%s: stub call sequences differ\n--- ref\n%s\n--- go\n%s", what, strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
}

type accountRowT struct {
	ServerURL, KeyPEM string
	AccountURL        *string
}
type certRowT struct {
	Domain, Cert, KeyPEM, Status string
	ExpiresAt                    time.Time
	DeployedAt                   *time.Time
}
type orderRowT struct {
	Domain, URL, KeyPEM, Status string
	CreatedAt                   time.Time
}
type dbState struct {
	Accounts []accountRowT
	Certs    []certRowT
	Orders   []orderRowT
	Chal     map[string]string
}

func (s *pside) state(t *testing.T) dbState {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, s.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var st dbState
	dec := func(v string) string {
		p, err := s.crypto.Decrypt(v)
		if err != nil {
			t.Fatalf("%s: decrypt: %v", s.name, err)
		}
		return p
	}
	rows, _ := conn.Query(ctx, "SELECT server_url, account_url, key_pem FROM acme_account ORDER BY created_at")
	for rows.Next() {
		var r accountRowT
		var k string
		if err := rows.Scan(&r.ServerURL, &r.AccountURL, &k); err != nil {
			t.Fatal(err)
		}
		r.KeyPEM = dec(k)
		st.Accounts = append(st.Accounts, r)
	}
	rows.Close()
	rows, _ = conn.Query(ctx, "SELECT domain, certificate, private_key, status, expires_at, deployed_at FROM acme_certificate ORDER BY created_at")
	for rows.Next() {
		var r certRowT
		var k string
		if err := rows.Scan(&r.Domain, &r.Cert, &k, &r.Status, &r.ExpiresAt, &r.DeployedAt); err != nil {
			t.Fatal(err)
		}
		r.KeyPEM = dec(k)
		st.Certs = append(st.Certs, r)
	}
	rows.Close()
	rows, _ = conn.Query(ctx, "SELECT domain, order_url, domain_key_pem, status, created_at FROM acme_order ORDER BY created_at")
	for rows.Next() {
		var r orderRowT
		var k string
		if err := rows.Scan(&r.Domain, &r.URL, &k, &r.Status, &r.CreatedAt); err != nil {
			t.Fatal(err)
		}
		r.KeyPEM = dec(k)
		st.Orders = append(st.Orders, r)
	}
	rows.Close()
	st.Chal = map[string]string{}
	rows, _ = conn.Query(ctx, "SELECT token, key_authorization FROM acme_challenge")
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		st.Chal[k] = v
	}
	rows.Close()
	return st
}

// checkCertRow validates the formats written by either implementation.
func checkCertRow(t *testing.T, who string, c certRowT, wantStatus string) {
	t.Helper()
	if c.Status != wantStatus {
		t.Errorf("%s: cert status %s, want %s", who, c.Status, wantStatus)
	}
	blk, rest := pem.Decode([]byte(c.Cert))
	if blk == nil || blk.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("%s: certificate column is not one CERTIFICATE block: %q", who, c.Cert)
	}
	if !strings.HasPrefix(c.Cert, "-----BEGIN CERTIFICATE-----\n") || !strings.HasSuffix(c.Cert, "\n-----END CERTIFICATE-----\n") {
		t.Errorf("%s: certificate framing: %q", who, c.Cert)
	}
	for _, l := range strings.Split(strings.TrimSuffix(c.Cert, "\n"), "\n") {
		if len(l) > 64 && !strings.HasPrefix(l, "-----") {
			t.Errorf("%s: certificate line longer than 64 columns", who)
		}
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("%s: %v", who, err)
	}
	key, err := ReadKeyPair(c.KeyPEM)
	if err != nil {
		t.Fatalf("%s: private key: %v", who, err)
	}
	if !strings.HasPrefix(c.KeyPEM, "-----BEGIN RSA PRIVATE KEY-----\n") {
		t.Errorf("%s: private key is not PKCS#1: %q", who, c.KeyPEM[:40])
	}
	_ = key
	if cert.Subject.CommonName != "local.test" {
		t.Errorf("%s: CN %q", who, cert.Subject.CommonName)
	}
	if d := time.Until(cert.NotAfter); d < 85*24*time.Hour || d > 95*24*time.Hour {
		t.Errorf("%s: notAfter in %v", who, d)
	}
	// expires_at is notAfter in the process zone (TZ=UTC for the parity apps)
	if got := c.ExpiresAt.UTC(); !got.Equal(cert.NotAfter.UTC()) {
		t.Errorf("%s: expires_at %v != notAfter %v", who, got, cert.NotAfter.UTC())
	}
}

func (r accountRowT) key(t *testing.T) *rsa.PrivateKey {
	k, err := ReadKeyPair(r.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return k.(*rsa.PrivateKey)
}

func TestParityFirstIssuanceAndReuse(t *testing.T) {
	ref, gov := paritySides(t)
	both(ref, gov, func(s *pside) { s.reset(t) })
	var codes [2]int
	both(ref, gov, func(s *pside) {
		c := s.renew(t)
		if s == ref {
			codes[0] = c
		} else {
			codes[1] = c
		}
	})
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("renew statuses ref=%d go=%d", codes[0], codes[1])
	}
	rc, gc := ref.calls(t), gov.calls(t)
	sameSeq(t, "first issuance", rc, gc)
	if len(rc) != 11 {
		t.Errorf("unexpected number of stub calls: %d\n%s", len(rc), strings.Join(rc, "\n"))
	}
	for _, s := range []*pside{ref, gov} {
		st := s.state(t)
		if len(st.Accounts) != 1 || len(st.Certs) != 1 || len(st.Orders) != 0 || len(st.Chal) != 0 {
			t.Fatalf("%s: rows accounts=%d certs=%d orders=%d challenges=%d", s.name, len(st.Accounts), len(st.Certs), len(st.Orders), len(st.Chal))
		}
		a := st.Accounts[0]
		if want := fmt.Sprintf("http://localhost:%s/stub/acme/directory", s.port); a.ServerURL != want {
			t.Errorf("%s: server_url %q", s.name, a.ServerURL)
		}
		if a.AccountURL == nil || *a.AccountURL != fmt.Sprintf("http://localhost:%s/stub/acme/account/stub-account-001", s.port) {
			t.Errorf("%s: account_url %v", s.name, a.AccountURL)
		}
		if !strings.HasPrefix(a.KeyPEM, "-----BEGIN RSA PRIVATE KEY-----\n") || a.key(t).N.BitLen() != 2048 {
			t.Errorf("%s: account key is not a PKCS#1 RSA-2048 PEM", s.name)
		}
		checkCertRow(t, s.name, st.Certs[0], "DEPLOYED")
		if st.Certs[0].DeployedAt == nil || time.Since(*st.Certs[0].DeployedAt) > time.Minute {
			t.Errorf("%s: deployed_at %v", s.name, st.Certs[0].DeployedAt)
		}
	}

	// a valid deployed certificate: nothing happens
	both(ref, gov, func(s *pside) {
		if c := s.renew(t); c != 200 {
			t.Errorf("%s: second renew %d", s.name, c)
		}
	})
	sameSeq(t, "second renew (no-op)", ref.calls(t), gov.calls(t))

	// the certificate expires soon: a new order reusing the stored account (onlyReturnExisting)
	both(ref, gov, func(s *pside) {
		s.exec(t, "UPDATE acme_certificate SET expires_at = now() + interval '10 days'")
		if c := s.renew(t); c != 200 {
			t.Errorf("%s: renewal %d", s.name, c)
		}
	})
	rc, gc = ref.calls(t), gov.calls(t)
	sameSeq(t, "renewal with the stored account", rc, gc)
	for _, s := range []*pside{ref, gov} {
		st := s.state(t)
		if len(st.Accounts) != 1 || len(st.Certs) != 2 || len(st.Orders) != 0 {
			t.Fatalf("%s: after renewal accounts=%d certs=%d orders=%d", s.name, len(st.Accounts), len(st.Certs), len(st.Orders))
		}
		checkCertRow(t, s.name, st.Certs[1], "DEPLOYED")
	}
}

// Rows written by one implementation are read by the other: the Go rows are copied (with the
// stub URLs rewritten) into the Kotlin database and conversely; an expiring certificate then
// makes the reader reuse the stored account and issue a new certificate.
func TestParityCrossReadRows(t *testing.T) {
	ref, gov := paritySides(t)
	both(ref, gov, func(s *pside) {
		s.reset(t)
		if c := s.renew(t); c != 200 {
			t.Errorf("%s: issuance %d", s.name, c)
		}
	})
	exchange := func(from, to *pside) {
		src := from.state(t)
		a, c := src.Accounts[0], src.Certs[0]
		// the encrypted columns are copied as written by the source implementation
		var accEnc, certEnc string
		sc, err := pgx.Connect(context.Background(), from.dbURL)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close(context.Background())
		if err := sc.QueryRow(context.Background(), "SELECT key_pem FROM acme_account").Scan(&accEnc); err != nil {
			t.Fatal(err)
		}
		if err := sc.QueryRow(context.Background(), "SELECT private_key FROM acme_certificate").Scan(&certEnc); err != nil {
			t.Fatal(err)
		}
		to.reset(t)
		rw := func(v string) string { return strings.ReplaceAll(v, ":"+from.port, ":"+to.port) }
		to.exec(t, `INSERT INTO acme_account (id, server_url, account_url, key_pem, created_at) VALUES (gen_random_uuid(), $1, $2, $3, now())`,
			rw(a.ServerURL), rw(*a.AccountURL), accEnc)
		to.exec(t, `INSERT INTO acme_certificate (id, domain, certificate, private_key, expires_at, created_at, status, deployed_at)
			VALUES (gen_random_uuid(), $1, $2, $3, now() + interval '5 days', now(), 'DEPLOYED', now())`, c.Domain, c.Cert, certEnc)
		if code := to.renew(t); code != 200 {
			t.Fatalf("%s reading the rows of %s: renew %d", to.name, from.name, code)
		}
		st := to.state(t)
		if len(st.Accounts) != 1 || len(st.Certs) != 2 {
			t.Fatalf("%s: accounts=%d certs=%d", to.name, len(st.Accounts), len(st.Certs))
		}
		checkCertRow(t, to.name, st.Certs[1], "DEPLOYED")
		if calls := strings.Join(to.calls(t), "\n"); !strings.Contains(calls, "newAccount") {
			t.Errorf("%s: the stored account was not reused through new-account:\n%s", to.name, calls)
		}
	}
	exchange(gov, ref) // Go-written rows, read by Kotlin
	exchange(ref, gov) // Kotlin-written rows, read by Go
}

func (s *pside) newStubOrder(t *testing.T) (orderURL string, authz string) {
	t.Helper()
	jose := map[string]string{"Content-Type": "application/jose+json"}
	body := []byte(`{"payload":"","protected":"","signature":""}`)
	st, h, b := s.do(t, "POST", "/stub/acme/new-order", jose, body)
	if st != 201 {
		t.Fatalf("%s: new-order %d", s.name, st)
	}
	var o struct{ Authorizations []string }
	_ = json.Unmarshal(b, &o)
	return h.Get("Location"), o.Authorizations[0]
}

func localPath(s *pside, u string) string {
	return strings.TrimPrefix(u, s.base)
}

func (s *pside) generatedKeyPEM(t *testing.T, fromKotlin bool) string {
	t.Helper()
	if fromKotlin {
		// the Kotlin reference writes the key (KeyPairUtils) and encrypts it (AcmeCryptoHelper)
		st, _, b := (&pside{name: "ref", base: envOr("PARITY_REF_URL", "http://localhost:8111"), jwt: s.jwt}).do(t, "POST", "/stub/acme/generate-keypair", nil, nil)
		if st != 200 {
			t.Fatalf("generate-keypair %d", st)
		}
		var o struct{ EncryptedKeyPair string }
		_ = json.Unmarshal(b, &o)
		return o.EncryptedKeyPair
	}
	k, err := CreateKeyPair(2048)
	if err != nil {
		t.Fatal(err)
	}
	pemText, _ := WriteKeyPair(k)
	enc, err := s.crypto.Encrypt(pemText)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// Resume after an incident, with the rows written by the OTHER implementation
// (Kotlin-encrypted keys for the Go application and the opposite).
func TestParityResumeOrders(t *testing.T) {
	ref, gov := paritySides(t)
	jose := map[string]string{"Content-Type": "application/jose+json"}
	body := []byte(`{"payload":"","protected":"","signature":""}`)
	setup := func(s *pside, status string) {
		s.reset(t)
		orderURL, authzURL := s.newStubOrder(t)
		kotlinKeys := s == gov
		s.exec(t, `INSERT INTO acme_account (id, server_url, account_url, key_pem, created_at) VALUES (gen_random_uuid(), $1, $2, $3, now())`,
			fmt.Sprintf("http://localhost:%s/stub/acme/directory", s.port), fmt.Sprintf("http://localhost:%s/stub/acme/account/stub-account-001", s.port), s.generatedKeyPEM(t, kotlinKeys))
		s.exec(t, `INSERT INTO acme_order (id, domain, order_url, domain_key_pem, status, created_at) VALUES (gen_random_uuid(), 'local.test', $1, $2, $3, now())`,
			orderURL, s.generatedKeyPEM(t, kotlinKeys), status)
		if status == "ORDER_FINALIZING" {
			// challenge validated and order finalized on the CA side
			_, _, ab := s.do(t, "POST", localPath(s, authzURL), jose, body)
			var az struct{ Challenges []struct{ Token string } }
			_ = json.Unmarshal(ab, &az)
			s.do(t, "POST", "/stub/acme/challenge/"+az.Challenges[0].Token, jose, body)
			s.do(t, "POST", localPath(s, orderURL)+"/finalize", jose, body)
		}
		s.do(t, "DELETE", "/admin/stub/acme/calls/reset", nil, nil)
	}

	t.Run("CHALLENGE_PENDING", func(t *testing.T) {
		both(ref, gov, func(s *pside) { setup(s, "CHALLENGE_PENDING") })
		// run 1: the challenge is still pending in the stub → re-triggered, nothing deployed
		both(ref, gov, func(s *pside) {
			if c := s.renew(t); c != 200 {
				t.Errorf("%s: run 1 status %d", s.name, c)
			}
		})
		sameSeq(t, "resume run 1", ref.calls(t), gov.calls(t))
		for _, s := range []*pside{ref, gov} {
			st := s.state(t)
			if len(st.Certs) != 0 || len(st.Orders) != 1 || st.Orders[0].Status != "CHALLENGE_PENDING" {
				t.Fatalf("%s: after run 1 certs=%d orders=%v", s.name, len(st.Certs), st.Orders)
			}
			// the token was restored in the store before authorization.update(), with the key
			// authorization of the (other implementation's) account key
			if len(st.Chal) != 1 {
				t.Fatalf("%s: challenges %v", s.name, st.Chal)
			}
			thumb, _ := acme.JWKThumbprint(st.Accounts[0].key(t).Public())
			for tok, ka := range st.Chal {
				if ka != tok+"."+thumb {
					t.Errorf("%s: key authorization %q, want %q", s.name, ka, tok+"."+thumb)
				}
				// the HTTP-01 route serves it
				if code, _, b := s.do(t, "GET", "/.well-known/acme-challenge/"+tok, nil, nil); code != 200 || string(b) != ka {
					t.Errorf("%s: challenge route %d %q", s.name, code, b)
				}
			}
		}
		// run 2: the challenge is now valid → finalized, certificate downloaded and deployed
		both(ref, gov, func(s *pside) { s.do(t, "DELETE", "/admin/stub/acme/calls/reset", nil, nil) })
		both(ref, gov, func(s *pside) {
			if c := s.renew(t); c != 200 {
				t.Errorf("%s: run 2 status %d", s.name, c)
			}
		})
		sameSeq(t, "resume run 2", ref.calls(t), gov.calls(t))
		for _, s := range []*pside{ref, gov} {
			st := s.state(t)
			if len(st.Certs) != 1 || len(st.Orders) != 0 || len(st.Chal) != 0 {
				t.Fatalf("%s: after run 2 certs=%d orders=%d challenges=%d", s.name, len(st.Certs), len(st.Orders), len(st.Chal))
			}
			checkCertRow(t, s.name, st.Certs[0], "DEPLOYED")
		}
	})

	t.Run("ORDER_FINALIZING", func(t *testing.T) {
		both(ref, gov, func(s *pside) { setup(s, "ORDER_FINALIZING") })
		both(ref, gov, func(s *pside) {
			if c := s.renew(t); c != 200 {
				t.Errorf("%s: status %d", s.name, c)
			}
		})
		sameSeq(t, "resume finalizing", ref.calls(t), gov.calls(t))
		for _, s := range []*pside{ref, gov} {
			st := s.state(t)
			if len(st.Certs) != 1 || len(st.Orders) != 0 {
				t.Fatalf("%s: certs=%d orders=%d", s.name, len(st.Certs), len(st.Orders))
			}
			checkCertRow(t, s.name, st.Certs[0], "DEPLOYED")
			// the key is the one stored with the order (written by the other implementation)
			if _, err := ReadKeyPair(st.Certs[0].KeyPEM); err != nil {
				t.Errorf("%s: %v", s.name, err)
			}
		}
	})
}

// The challenge row is cleared when the order is validated: the route answers 404 afterwards.
func TestParityChallengeClearedAfterIssuance(t *testing.T) {
	ref, gov := paritySides(t)
	both(ref, gov, func(s *pside) {
		s.reset(t)
		s.renew(t)
		_, _, b := s.do(t, "GET", "/admin/stub/acme/calls", nil, nil)
		var calls []struct{ Summary string }
		_ = json.Unmarshal(b, &calls)
		for _, c := range calls {
			if strings.HasPrefix(c.Summary, "challenge token=") {
				tok := strings.Fields(c.Summary)[1][len("token="):]
				if code, _, _ := s.do(t, "GET", "/.well-known/acme-challenge/"+tok, nil, nil); code != 404 {
					t.Errorf("%s: cleared challenge answers %d", s.name, code)
				}
				return
			}
		}
		t.Errorf("%s: no challenge call", s.name)
	})
}
