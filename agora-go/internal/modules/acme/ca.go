package acme

import (
	"context"
	"crypto"
	"strings"
	"time"
)

// The CA* interfaces model the part of acme4j used by
// AcmeCertificateRenewalUseCase (Session, AccountBuilder, Login, Account,
// Order, Authorization, Http01Challenge, Certificate) with its lazy loading and
// "update() sets the data, then raises AcmeRetryAfterException" semantics, so
// that the use case can be a line-by-line port and unit-tested with a fake CA.

// RetryAfterError is org.shredzone.acme4j.exception.AcmeRetryAfterException:
// the CA answered an update() with a Retry-After header (the resource data has
// nevertheless been refreshed).
type RetryAfterError struct {
	Msg        string
	RetryAfter time.Time
}

func (e *RetryAfterError) Error() string { return e.Msg }

// CAStatus is org.shredzone.acme4j.Status.
type CAStatus string

// The acme4j Status values used by the use case.
const (
	CAStatusPending    CAStatus = "PENDING"
	CAStatusReady      CAStatus = "READY"
	CAStatusProcessing CAStatus = "PROCESSING"
	CAStatusValid      CAStatus = "VALID"
	CAStatusInvalid    CAStatus = "INVALID"
	CAStatusUnknown    CAStatus = "UNKNOWN"
)

// ParseCAStatus is Status.parse: upper-cased name, UNKNOWN when not a constant.
func ParseCAStatus(s string) CAStatus {
	switch u := strings.ToUpper(s); u {
	case "PENDING", "READY", "PROCESSING", "VALID", "INVALID", "REVOKED", "DEACTIVATED", "CANCELED", "EXPIRED":
		return CAStatus(u)
	}
	return CAStatusUnknown
}

// AccountOptions are the AccountBuilder settings of startNewOrder.
type AccountOptions struct {
	// OnlyExisting is AccountBuilder.onlyExisting().
	OnlyExisting bool
	// AgreeToTerms is AccountBuilder.agreeToTermsOfService().
	AgreeToTerms bool
	// EabKid / EabHmacKey are AccountBuilder.withKeyIdentifier(kid, encodedMacKey)
	// (the key is base64url encoded); applied when EabKid is not empty.
	EabKid, EabHmacKey string
}

// CAProvider creates sessions (Session(serverUrl)).
type CAProvider interface {
	NewSession(serverURL string) CASession
}

// CASession is org.shredzone.acme4j.Session.
type CASession interface {
	// CreateAccount is AccountBuilder().useKeyPair(key)...create(session).
	CreateAccount(ctx context.Context, key crypto.Signer, opts AccountOptions) (CAAccount, error)
	// Login is session.login(accountUrl, key): no request is sent.
	Login(accountURL string, key crypto.Signer) (CAAccount, error)
}

// CAAccount is an acme4j Account / Login.
type CAAccount interface {
	// Location is account.location.
	Location() string
	// Status is account.status (empty for a Login that was not loaded).
	Status() string
	// NewOrder is account.newOrder().domain(d)...create().
	NewOrder(ctx context.Context, domains []string) (CAOrder, error)
	// BindOrder is login.bindOrder(url): not loaded yet.
	BindOrder(orderURL string) CAOrder
}

// CAOrder is org.shredzone.acme4j.Order.
type CAOrder interface {
	Location() string
	// Update is order.update(): POST-as-GET, data refreshed, then
	// *RetryAfterError when the response carries a Retry-After header.
	Update(ctx context.Context) error
	// Execute is order.execute(csr): finalize request, then the order data is invalidated.
	Execute(ctx context.Context, csr []byte) error
	// Status is order.status (lazy loading).
	Status(ctx context.Context) (CAStatus, error)
	// Authorizations is order.authorizations: new, not yet loaded objects.
	Authorizations(ctx context.Context) ([]CAAuthorization, error)
	// Certificate is order.certificate: a new, not yet downloaded certificate
	// each call, nil when the order has none.
	Certificate(ctx context.Context) (CACertificate, error)
}

// CAAuthorization is org.shredzone.acme4j.Authorization.
type CAAuthorization interface {
	// Domain is authorization.identifier.domain (lazy loading).
	Domain(ctx context.Context) (string, error)
	// FindHTTP01 is authorization.findChallenge("http-01") on the CURRENT data:
	// the returned challenge keeps the data it was created from. nil when absent.
	FindHTTP01(ctx context.Context) (CAChallenge, error)
	// Update is authorization.update() (same Retry-After rule as the order).
	Update(ctx context.Context) error
}

// CAChallenge is org.shredzone.acme4j.challenge.Http01Challenge.
type CAChallenge interface {
	Token() string
	Location() string
	Status() CAStatus
	// KeyAuthorization is challenge.authorization.
	KeyAuthorization() (string, error)
	// Trigger is challenge.trigger() (data refreshed, no Retry-After check).
	Trigger(ctx context.Context) error
	// Update is challenge.update().
	Update(ctx context.Context) error
}

// CACertificate is org.shredzone.acme4j.Certificate.
type CACertificate interface {
	// Chain is certificate.certificateChain (DER, leaf first); downloaded once per object.
	Chain(ctx context.Context) ([][]byte, error)
}
