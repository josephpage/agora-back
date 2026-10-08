package acme

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/modules/users"
	"agora/internal/store"
)

// The four repositories of usecase/acme/repository, ported from
// infrastructure/acme/repository/*RepositoryImpl.kt + the Spring Data
// interfaces (the SQL below is what Hibernate generates for the derived
// queries; every id is a fresh random UUID, like UUID.randomUUID() +
// merge() on a @GeneratedValue entity).

// AccountRepository is usecase/acme/repository/AcmeAccountRepository.
type AccountRepository interface {
	LoadAccount(ctx context.Context, serverURL string) (*Account, error)
	SaveAccount(ctx context.Context, account Account) error
}

// CertificateRepository is usecase/acme/repository/AcmeCertificateRepository.
type CertificateRepository interface {
	LoadCertificate(ctx context.Context, domain string) (*Certificate, error)
	SaveCertificate(ctx context.Context, certificate Certificate) error
	MarkAsDeployed(ctx context.Context, domain string, deployedAt time.Time) error
}

// ChallengeStore is usecase/acme/repository/AcmeChallengeStore.
type ChallengeStore interface {
	StoreChallenge(ctx context.Context, token, keyAuthorization string) error
	// GetChallenge returns nil for an unknown token.
	GetChallenge(ctx context.Context, token string) (*string, error)
	ClearChallenge(ctx context.Context, token string) error
}

// OrderRepository is usecase/acme/repository/AcmeOrderRepository.
type OrderRepository interface {
	LoadOrder(ctx context.Context, domain string) (*Order, error)
	SaveOrder(ctx context.Context, order Order) error
	UpdateOrderStatus(ctx context.Context, domain string, status OrderStatus) error
	DeleteOrder(ctx context.Context, domain string) error
}

// pgRepos implements the four repositories on PostgreSQL.
type pgRepos struct {
	db     store.Querier
	crypto *CryptoHelper
	now    func() time.Time // LocalDateTime.now()
}

func (r *pgRepos) stamp() time.Time { return store.Micros(r.now()) }

func newUUID() string { return users.RandomUUID() }

// --- accounts ---------------------------------------------------------------

type accountRow struct {
	id         string
	serverURL  string
	accountURL *string
	keyPEM     string
}

func (r *pgRepos) latestAccount(ctx context.Context, serverURL string) (*accountRow, error) {
	var row accountRow
	err := r.db.QueryRow(ctx,
		`SELECT id::text, server_url, account_url, key_pem FROM acme_account WHERE server_url = $1 ORDER BY created_at DESC LIMIT 1`,
		serverURL).Scan(&row.id, &row.serverURL, &row.accountURL, &row.keyPEM)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// LoadAccount is AcmeAccountRepositoryImpl.loadAccount.
func (r *pgRepos) LoadAccount(ctx context.Context, serverURL string) (*Account, error) {
	row, err := r.latestAccount(ctx, serverURL)
	if err != nil || row == nil {
		return nil, err
	}
	key, err := r.crypto.Decrypt(row.keyPEM)
	if err != nil {
		return nil, err
	}
	return &Account{ServerURL: row.serverURL, AccountURL: row.accountURL, KeyPEM: key}, nil
}

// SaveAccount is AcmeAccountRepositoryImpl.saveAccount: the key is encrypted
// first, then the newest account of the server is updated, or a row inserted.
func (r *pgRepos) SaveAccount(ctx context.Context, account Account) error {
	enc, err := r.crypto.Encrypt(account.KeyPEM)
	if err != nil {
		return err
	}
	existing, err := r.latestAccount(ctx, account.ServerURL)
	if err != nil {
		return err
	}
	if existing != nil {
		_, err = r.db.Exec(ctx, `UPDATE acme_account SET account_url = $1, key_pem = $2 WHERE id = $3::uuid`,
			account.AccountURL, enc, existing.id)
		return err
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO acme_account (id, account_url, created_at, key_pem, server_url) VALUES ($1::uuid, $2, $3, $4, $5)`,
		newUUID(), account.AccountURL, r.stamp(), enc, account.ServerURL)
	return err
}

// --- certificates -----------------------------------------------------------

// LoadCertificate is AcmeCertificateRepositoryImpl.loadCertificate.
func (r *pgRepos) LoadCertificate(ctx context.Context, domain string) (*Certificate, error) {
	var (
		c          Certificate
		priv       string
		expires    time.Time
		status     string
		deployedAt *time.Time
	)
	err := r.db.QueryRow(ctx,
		`SELECT domain, certificate, private_key, expires_at, status, deployed_at FROM acme_certificate WHERE domain = $1 ORDER BY created_at DESC LIMIT 1`,
		domain).Scan(&c.Domain, &c.CertificatePEM, &priv, &expires, &status, &deployedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c.PrivateKeyPEM, err = r.crypto.Decrypt(priv); err != nil {
		return nil, err
	}
	if c.Status, err = parseCertificateStatus(status); err != nil {
		return nil, err
	}
	c.ExpiresAt = store.Local(expires)
	c.DeployedAt = store.LocalPtr(deployedAt)
	return &c, nil
}

func parseCertificateStatus(s string) (CertificateStatus, error) {
	switch CertificateStatus(s) {
	case StatusToDeploy, StatusDeployed:
		return CertificateStatus(s), nil
	}
	return "", fmt.Errorf("IllegalArgumentException: No enum constant AcmeCertificateStatus.%s", s)
}

func parseOrderStatus(s string) (OrderStatus, error) {
	switch OrderStatus(s) {
	case OrderChallengePending, OrderFinalizing:
		return OrderStatus(s), nil
	}
	return "", fmt.Errorf("IllegalArgumentException: No enum constant AcmeOrderStatus.%s", s)
}

// SaveCertificate is AcmeCertificateRepositoryImpl.saveCertificate: always a
// new row (created now, deployedAt not written), the private key encrypted.
func (r *pgRepos) SaveCertificate(ctx context.Context, c Certificate) error {
	enc, err := r.crypto.Encrypt(c.PrivateKeyPEM)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO acme_certificate (id, certificate, created_at, deployed_at, domain, expires_at, private_key, status)
		 VALUES ($1::uuid, $2, $3, NULL, $4, $5, $6, $7)`,
		newUUID(), c.CertificatePEM, r.stamp(), c.Domain, store.Micros(c.ExpiresAt), enc, string(c.Status))
	return err
}

// MarkAsDeployed is markAsDeployedForLatestByDomain.
func (r *pgRepos) MarkAsDeployed(ctx context.Context, domain string, deployedAt time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE acme_certificate SET status = $1, deployed_at = $2
		 WHERE id = (SELECT c2.id FROM acme_certificate c2 WHERE c2.domain = $3 ORDER BY c2.created_at DESC LIMIT 1)`,
		string(StatusDeployed), store.Micros(deployedAt), domain)
	return err
}

// --- orders -----------------------------------------------------------------

// LoadOrder is AcmeOrderRepositoryImpl.loadOrder.
func (r *pgRepos) LoadOrder(ctx context.Context, domain string) (*Order, error) {
	var (
		o       Order
		key     string
		status  string
		created time.Time
	)
	err := r.db.QueryRow(ctx,
		`SELECT domain, order_url, domain_key_pem, status, created_at FROM acme_order WHERE domain = $1 ORDER BY created_at DESC LIMIT 1`,
		domain).Scan(&o.Domain, &o.OrderURL, &key, &status, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if o.DomainKeyPEM, err = r.crypto.Decrypt(key); err != nil {
		return nil, err
	}
	if o.Status, err = parseOrderStatus(status); err != nil {
		return nil, err
	}
	o.CreatedAt = store.Local(created)
	return &o, nil
}

// SaveOrder is AcmeOrderRepositoryImpl.saveOrder: a new row created now (the
// createdAt of the domain object is ignored), the domain key encrypted.
func (r *pgRepos) SaveOrder(ctx context.Context, o Order) error {
	enc, err := r.crypto.Encrypt(o.DomainKeyPEM)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO acme_order (id, created_at, domain, domain_key_pem, order_url, status) VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		newUUID(), r.stamp(), o.Domain, enc, o.OrderURL, string(o.Status))
	return err
}

// UpdateOrderStatus is updateStatusForLatestByDomain.
func (r *pgRepos) UpdateOrderStatus(ctx context.Context, domain string, status OrderStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE acme_order SET status = $1
		 WHERE id = (SELECT o2.id FROM acme_order o2 WHERE o2.domain = $2 ORDER BY o2.created_at DESC LIMIT 1)`,
		string(status), domain)
	return err
}

// DeleteOrder is deleteByDomain (every order of the domain).
func (r *pgRepos) DeleteOrder(ctx context.Context, domain string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM acme_order WHERE domain = $1`, domain)
	return err
}

// --- challenges -------------------------------------------------------------

// StoreChallenge is AcmeChallengeStoreImpl.storeChallenge: save() of an
// entity with an assigned id (insert, or update of key_authorization and
// created_at when the token exists).
func (r *pgRepos) StoreChallenge(ctx context.Context, token, keyAuthorization string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO acme_challenge (token, created_at, key_authorization) VALUES ($1, $2, $3)
		 ON CONFLICT (token) DO UPDATE SET created_at = EXCLUDED.created_at, key_authorization = EXCLUDED.key_authorization`,
		token, r.stamp(), keyAuthorization)
	return err
}

// GetChallenge is AcmeChallengeStoreImpl.getChallenge (findById).
func (r *pgRepos) GetChallenge(ctx context.Context, token string) (*string, error) {
	var ka string
	err := r.db.QueryRow(ctx, `SELECT key_authorization FROM acme_challenge WHERE token = $1`, token).Scan(&ka)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ka, nil
}

// ClearChallenge is AcmeChallengeStoreImpl.clearChallenge (deleteById, no
// error for an unknown token).
func (r *pgRepos) ClearChallenge(ctx context.Context, token string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM acme_challenge WHERE token = $1`, token)
	return err
}
