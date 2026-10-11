package acme

import "time"

// CertificateStatus is domain/AcmeCertificateStatus.kt (stored by name).
type CertificateStatus string

const (
	StatusToDeploy CertificateStatus = "TO_DEPLOY"
	StatusDeployed CertificateStatus = "DEPLOYED"
)

// OrderStatus is domain/AcmeOrderStatus.kt (stored by name).
type OrderStatus string

const (
	// OrderChallengePending: challenge triggered, waiting for the CA.
	OrderChallengePending OrderStatus = "CHALLENGE_PENDING"
	// OrderFinalizing: challenge validated, CSR sent, waiting for the final polling.
	OrderFinalizing OrderStatus = "ORDER_FINALIZING"
)

// Account is domain/AcmeAccount.kt. KeyPEM is the clear text in memory
// (encrypted in the database).
type Account struct {
	ServerURL  string
	AccountURL *string // nil until registered with the CA
	KeyPEM     string
}

// Certificate is domain/AcmeCertificate.kt. PrivateKeyPEM is the clear text in
// memory (encrypted in the database); the times are wall clock LocalDateTime
// values in the process zone.
type Certificate struct {
	Domain         string
	CertificatePEM string // full chain (certificate + intermediates)
	PrivateKeyPEM  string
	ExpiresAt      time.Time
	Status         CertificateStatus
	DeployedAt     *time.Time
}

// Order is domain/AcmeOrder.kt. DomainKeyPEM is the clear text in memory.
type Order struct {
	Domain       string
	OrderURL     string
	DomainKeyPEM string
	Status       OrderStatus
	CreatedAt    time.Time
}
