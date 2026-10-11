package acme

// Tests of the PostgreSQL repositories (AcmeCertificateRepositoryImplTest,
// AcmeOrderRepositoryImplTest, AcmeChallengeStoreImplTest and the Spring Data queries they
// mock). They need the parity PostgreSQL (S11_PARITY=1, see parity_it_test.go): the tables of the Go
// database are emptied first.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type pgFixture struct {
	t     *testing.T
	conn  *pgx.Conn
	repos *pgRepos
	n     int
}

func newPGFixture(t *testing.T) *pgFixture {
	t.Helper()
	_, gov := paritySides(t)
	conn, err := pgx.Connect(context.Background(), gov.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	f := &pgFixture{t: t, conn: conn}
	f.exec("TRUNCATE acme_account, acme_certificate, acme_order, acme_challenge")
	// every call of stamp() is one second later than the previous one
	f.repos = &pgRepos{db: conn, crypto: &CryptoHelper{cfg: &Config{EncryptionKey: keyBytes(32)}},
		now: func() time.Time { f.n++; return time.Date(2026, 1, 1, 0, 0, f.n, 123456789, time.Local) }}
	return f
}

func (f *pgFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.conn.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *pgFixture) str(sql string, args ...any) string {
	f.t.Helper()
	var s *string
	if err := f.conn.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

var bg = context.Background()

// --- AcmeCertificateRepositoryImplTest -----------------------------------------------------

func TestPGSaveCertificateEncryptsThePrivateKey(t *testing.T) {
	f := newPGFixture(t)
	expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)
	mustNoErr(t, f.repos.SaveCertificate(bg, Certificate{Domain: "example.com", CertificatePEM: "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----",
		PrivateKeyPEM: "plaintext-key", ExpiresAt: expires, Status: StatusToDeploy}))
	enc := f.str("SELECT private_key FROM acme_certificate")
	if enc == "plaintext-key" || strings.Contains(enc, "plaintext") {
		t.Fatalf("the key is stored in clear: %q", enc)
	}
	if dec, err := f.repos.crypto.Decrypt(enc); err != nil || dec != "plaintext-key" {
		t.Errorf("decrypted %q %v", dec, err)
	}
	if got := f.str("SELECT status || '|' || domain || '|' || certificate || '|' || (deployed_at IS NULL)::text FROM acme_certificate"); got !=
		"TO_DEPLOY|example.com|-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----|true" {
		t.Errorf("row %q", got)
	}
	// a UUID id, created now (microseconds), expiry as given
	if got := f.str("SELECT (id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')::text || '|' || to_char(created_at, 'YYYY-MM-DD HH24:MI:SS.US') || '|' || to_char(expires_at, 'YYYY-MM-DD HH24:MI:SS.US') FROM acme_certificate"); got !=
		"true|2026-01-01 00:00:01.123456|2027-01-01 00:00:00.000000" {
		t.Errorf("row %q", got)
	}
}

func TestPGLoadCertificateReturnsTheLatestOfTheDomain(t *testing.T) {
	f := newPGFixture(t)
	for _, c := range []Certificate{
		{Domain: "example.com", CertificatePEM: "old", PrivateKeyPEM: "k1", Status: StatusDeployed},
		{Domain: "other.com", CertificatePEM: "other", PrivateKeyPEM: "k2", Status: StatusDeployed},
		{Domain: "example.com", CertificatePEM: "new", PrivateKeyPEM: "plaintext-key", Status: StatusToDeploy},
	} {
		c.ExpiresAt = time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)
		mustNoErr(t, f.repos.SaveCertificate(bg, c))
	}
	got, err := f.repos.LoadCertificate(bg, "example.com")
	mustNoErr(t, err)
	if got == nil || got.CertificatePEM != "new" || got.PrivateKeyPEM != "plaintext-key" || got.Status != StatusToDeploy || got.Domain != "example.com" ||
		got.DeployedAt != nil || !got.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)) {
		t.Errorf("loaded %+v", got)
	}
	if got, err := f.repos.LoadCertificate(bg, "unknown.com"); err != nil || got != nil {
		t.Errorf("unknown domain: %v %v", got, err)
	}
}

func TestPGMarkAsDeployedUpdatesOnlyTheLatestCertificate(t *testing.T) {
	f := newPGFixture(t)
	for _, pem := range []string{"old", "new"} {
		mustNoErr(t, f.repos.SaveCertificate(bg, Certificate{Domain: "example.com", CertificatePEM: pem, PrivateKeyPEM: "k", ExpiresAt: time.Now(), Status: StatusToDeploy}))
	}
	mustNoErr(t, f.repos.SaveCertificate(bg, Certificate{Domain: "other.com", CertificatePEM: "other", PrivateKeyPEM: "k", ExpiresAt: time.Now(), Status: StatusToDeploy}))
	at := time.Date(2026, 6, 25, 3, 0, 0, 0, time.Local)
	mustNoErr(t, f.repos.MarkAsDeployed(bg, "example.com", at))
	rows := f.str("SELECT string_agg(certificate || ':' || status || ':' || coalesce(to_char(deployed_at, 'YYYY-MM-DD HH24:MI:SS'), '-'), ',' ORDER BY certificate) FROM acme_certificate")
	if want := "new:DEPLOYED:2026-06-25 03:00:00,old:TO_DEPLOY:-,other:TO_DEPLOY:-"; rows != want {
		t.Errorf("rows %s, want %s", rows, want)
	}
	got, err := f.repos.LoadCertificate(bg, "example.com")
	mustNoErr(t, err)
	if got.Status != StatusDeployed || got.DeployedAt == nil || !got.DeployedAt.Equal(at) {
		t.Errorf("loaded %+v", got)
	}
	// nothing to update for an unknown domain: no error
	mustNoErr(t, f.repos.MarkAsDeployed(bg, "unknown.com", at))
}

func TestPGLoadCertificateRejectsAnUnknownStatus(t *testing.T) {
	f := newPGFixture(t)
	mustNoErr(t, f.repos.SaveCertificate(bg, Certificate{Domain: "example.com", CertificatePEM: "c", PrivateKeyPEM: "k", ExpiresAt: time.Now(), Status: StatusToDeploy}))
	for _, status := range []string{"deployed", "FOO", ""} {
		f.exec("UPDATE acme_certificate SET status = $1", status)
		if got, err := f.repos.LoadCertificate(bg, "example.com"); err == nil {
			t.Errorf("status %q accepted: %+v", status, got)
		}
	}
}

func TestPGLoadCertificateFailsWhenTheKeyCannotBeDecrypted(t *testing.T) {
	f := newPGFixture(t)
	mustNoErr(t, f.repos.SaveCertificate(bg, Certificate{Domain: "example.com", CertificatePEM: "c", PrivateKeyPEM: "k", ExpiresAt: time.Now(), Status: StatusToDeploy}))
	f.exec("UPDATE acme_certificate SET private_key = 'not-encrypted'")
	if _, err := f.repos.LoadCertificate(bg, "example.com"); err == nil {
		t.Error("a clear text key was accepted")
	}
}

// --- AcmeOrderRepositoryImplTest -----------------------------------------------------------

func TestPGOrders(t *testing.T) {
	f := newPGFixture(t)
	// no order: nil, and the crypto helper is not needed (an invalid key does not matter)
	bad := &pgRepos{db: f.conn, crypto: &CryptoHelper{cfg: &Config{EncryptionKey: ""}}, now: time.Now}
	if got, err := bad.LoadOrder(bg, "example.com"); err != nil || got != nil {
		t.Errorf("no order: %v %v", got, err)
	}

	// the createdAt of the object is ignored: the row is created now
	mustNoErr(t, f.repos.SaveOrder(bg, Order{Domain: "example.com", OrderURL: "https://acme.example.com/order/1", DomainKeyPEM: "plaintext-key",
		Status: OrderChallengePending, CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)}))
	if enc := f.str("SELECT domain_key_pem FROM acme_order"); enc == "plaintext-key" || len(enc) < 40 {
		t.Errorf("stored key %q", enc)
	}
	mustNoErr(t, f.repos.SaveOrder(bg, Order{Domain: "example.com", OrderURL: "https://acme.example.com/order/2", DomainKeyPEM: "key2", Status: OrderChallengePending}))
	mustNoErr(t, f.repos.SaveOrder(bg, Order{Domain: "other.com", OrderURL: "https://acme.example.com/order/3", DomainKeyPEM: "key3", Status: OrderChallengePending}))

	got, err := f.repos.LoadOrder(bg, "example.com")
	mustNoErr(t, err)
	if got == nil || got.OrderURL != "https://acme.example.com/order/2" || got.DomainKeyPEM != "key2" || got.Status != OrderChallengePending || got.Domain != "example.com" ||
		!got.CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 2, 123456000, time.Local)) {
		t.Errorf("loaded %+v", got)
	}

	// updateStatusForLatestByDomain
	mustNoErr(t, f.repos.UpdateOrderStatus(bg, "example.com", OrderFinalizing))
	if rows := f.str("SELECT string_agg(order_url || ':' || status, ',' ORDER BY order_url) FROM acme_order"); rows !=
		"https://acme.example.com/order/1:CHALLENGE_PENDING,https://acme.example.com/order/2:ORDER_FINALIZING,https://acme.example.com/order/3:CHALLENGE_PENDING" {
		t.Errorf("rows %s", rows)
	}

	// deleteByDomain deletes every order of the domain
	mustNoErr(t, f.repos.DeleteOrder(bg, "example.com"))
	if rows := f.str("SELECT string_agg(domain, ',') FROM acme_order"); rows != "other.com" {
		t.Errorf("rows %s", rows)
	}
	mustNoErr(t, f.repos.DeleteOrder(bg, "example.com")) // nothing left: no error
}

func TestPGLoadOrderRejectsAnUnknownStatus(t *testing.T) {
	f := newPGFixture(t)
	mustNoErr(t, f.repos.SaveOrder(bg, Order{Domain: "example.com", OrderURL: "u", DomainKeyPEM: "k", Status: OrderChallengePending}))
	f.exec("UPDATE acme_order SET status = 'WHATEVER'")
	if got, err := f.repos.LoadOrder(bg, "example.com"); err == nil {
		t.Errorf("accepted: %+v", got)
	}
}

// --- AcmeChallengeStoreImplTest ------------------------------------------------------------

func TestPGChallengeStore(t *testing.T) {
	f := newPGFixture(t)
	if got, err := f.repos.GetChallenge(bg, "unknown-token"); err != nil || got != nil {
		t.Errorf("unknown token: %v %v", got, err)
	}
	mustNoErr(t, f.repos.StoreChallenge(bg, "abc123token", "abc123token.thumbprint"))
	got, err := f.repos.GetChallenge(bg, "abc123token")
	mustNoErr(t, err)
	if got == nil || *got != "abc123token.thumbprint" {
		t.Fatalf("loaded %v", got)
	}
	// save() of an existing id updates the row
	mustNoErr(t, f.repos.StoreChallenge(bg, "abc123token", "abc123token.other"))
	if n := f.str("SELECT count(*)::text FROM acme_challenge"); n != "1" {
		t.Errorf("rows %s", n)
	}
	if got, _ := f.repos.GetChallenge(bg, "abc123token"); got == nil || *got != "abc123token.other" {
		t.Errorf("updated %v", got)
	}
	if ca := f.str("SELECT to_char(created_at, 'SS.US') FROM acme_challenge"); ca != "02.123456" {
		t.Errorf("created_at refreshed to %s", ca)
	}
	// the token is case sensitive
	if got, _ := f.repos.GetChallenge(bg, "ABC123TOKEN"); got != nil {
		t.Errorf("case: %v", got)
	}
	mustNoErr(t, f.repos.ClearChallenge(bg, "abc123token"))
	mustNoErr(t, f.repos.ClearChallenge(bg, "non-existent-token"))
	if got, _ := f.repos.GetChallenge(bg, "abc123token"); got != nil {
		t.Errorf("cleared token still there: %v", got)
	}
}

// --- AcmeAccountRepositoryImpl -------------------------------------------------------------

func TestPGAccounts(t *testing.T) {
	f := newPGFixture(t)
	if got, err := f.repos.LoadAccount(bg, "https://acme.example.com/directory"); err != nil || got != nil {
		t.Errorf("no account: %v %v", got, err)
	}
	mustNoErr(t, f.repos.SaveAccount(bg, Account{ServerURL: "https://acme.example.com/directory", KeyPEM: "account-key"}))
	got, err := f.repos.LoadAccount(bg, "https://acme.example.com/directory")
	mustNoErr(t, err)
	if got == nil || got.AccountURL != nil || got.KeyPEM != "account-key" {
		t.Errorf("loaded %+v", got)
	}
	if enc := f.str("SELECT key_pem FROM acme_account"); enc == "account-key" {
		t.Error("account key stored in clear")
	}
	// the account is updated in place, not duplicated
	url := "https://acme.example.com/acct/1"
	mustNoErr(t, f.repos.SaveAccount(bg, Account{ServerURL: "https://acme.example.com/directory", AccountURL: &url, KeyPEM: "account-key-2"}))
	got, err = f.repos.LoadAccount(bg, "https://acme.example.com/directory")
	mustNoErr(t, err)
	if got.AccountURL == nil || *got.AccountURL != url || got.KeyPEM != "account-key-2" {
		t.Errorf("updated %+v", got)
	}
	if n := f.str("SELECT count(*)::text FROM acme_account"); n != "1" {
		t.Errorf("rows %s", n)
	}
	// another server: another account
	mustNoErr(t, f.repos.SaveAccount(bg, Account{ServerURL: "https://other.example.com/directory", KeyPEM: "other-key"}))
	if n := f.str("SELECT count(*)::text FROM acme_account"); n != "2" {
		t.Errorf("rows %s", n)
	}
	if other, _ := f.repos.LoadAccount(bg, "https://other.example.com/directory"); other == nil || other.KeyPEM != "other-key" {
		t.Errorf("other %+v", other)
	}
}

func TestPGSaveFailsWithoutAnEncryptionKey(t *testing.T) {
	f := newPGFixture(t)
	f.repos.crypto = &CryptoHelper{cfg: &Config{EncryptionKey: ""}}
	if err := f.repos.SaveCertificate(bg, Certificate{Domain: "d", PrivateKeyPEM: "k", Status: StatusToDeploy, ExpiresAt: time.Now()}); err == nil {
		t.Error("certificate saved without a key")
	}
	if err := f.repos.SaveOrder(bg, Order{Domain: "d", DomainKeyPEM: "k", Status: OrderChallengePending}); err == nil {
		t.Error("order saved without a key")
	}
	if err := f.repos.SaveAccount(bg, Account{ServerURL: "s", KeyPEM: "k"}); err == nil {
		t.Error("account saved without a key")
	}
	if n := f.str("SELECT (SELECT count(*) FROM acme_certificate) + (SELECT count(*) FROM acme_order) + (SELECT count(*) FROM acme_account) || ''"); n != "0" {
		t.Errorf("rows were written: %s", n)
	}
}
