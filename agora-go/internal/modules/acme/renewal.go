package acme

import (
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"agora/internal/javacompat"
)

// Constants of AcmeCertificateRenewalUseCase.
const (
	renewalThresholdDays = 30
	pollingMaxAttempts   = 10
	pollingInterval      = 3 * time.Second
	orderExpiryHours     = 7 * 24 // 168 h
)

// ChallengeFailedError is AcmeChallengeFailedException.
type ChallengeFailedError struct{ Msg string }

func (e *ChallengeFailedError) Error() string { return e.Msg }

// ChallengeTimeoutError is AcmeChallengeTimeoutException.
type ChallengeTimeoutError struct{ Msg string }

func (e *ChallengeTimeoutError) Error() string { return e.Msg }

// CertificateDeployer is usecase/acme/repository/CloudflareCertificateDeployer.
type CertificateDeployer interface {
	DeployCertificate(ctx context.Context, certificatePEM, privateKeyPEM string) error
}

// RenewalUseCase is usecase/acme/AcmeCertificateRenewalUseCase.kt.
type RenewalUseCase struct {
	Cfg          *Config
	Certificates CertificateRepository
	Accounts     AccountRepository
	Challenges   ChallengeStore
	Orders       OrderRepository
	Deployer     CertificateDeployer
	CA           CAProvider
	// Now is LocalDateTime.now(clock).
	Now func() time.Time
	// Sleep is Thread.sleep (replaced in tests).
	Sleep func(ctx context.Context, d time.Duration)
	Log   *slog.Logger
	// NewKeyPair is KeyPairUtils.createKeyPair(2048) (replaced in tests).
	NewKeyPair func() (crypto.Signer, error)
}

func (u *RenewalUseCase) sleep(ctx context.Context, d time.Duration) {
	if u.Sleep != nil {
		u.Sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (u *RenewalUseCase) newKeyPair() (crypto.Signer, error) {
	if u.NewKeyPair != nil {
		return u.NewKeyPair()
	}
	return CreateKeyPair(2048)
}

// wall reinterprets a LocalDateTime-like time as a plain wall clock value so
// that arithmetic and comparisons ignore the zone (java.time.LocalDateTime).
func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// RenewIfNeeded is AcmeCertificateRenewalUseCase.renewIfNeeded.
func (u *RenewalUseCase) RenewIfNeeded(ctx context.Context) error {
	log := u.Log
	cfg := u.Cfg
	if !cfg.Enabled {
		log.Info("ACME renewal is disabled (ACME_ENABLED=false). Skipping.")
		return nil
	}
	start := time.Now()
	domain := cfg.Domain
	allDomains := cfg.AllDomains()
	serverURL := cfg.ServerURL
	now := u.Now()
	log.Info(fmt.Sprintf("Starting ACME renewal check for domain=%s, SANs=%v [acmeServerInteraction=%t, cloudflareInteraction=%t]",
		domain, allDomains[1:], cfg.ServerInteractionEnabled, cfg.CloudflareInteractionEnabled))

	// 1. expiration + deployment status
	existing, err := u.Certificates.LoadCertificate(ctx, domain)
	if err != nil {
		return err
	}
	if existing != nil {
		log.Info(fmt.Sprintf("Loaded existing certificate for %s: status=%s, expiresAt=%s", domain, existing.Status, existing.ExpiresAt.Format("2006-01-02T15:04:05")))
	} else {
		log.Info("No existing certificate found in database for " + domain)
	}
	threshold := wall(now).AddDate(0, 0, renewalThresholdDays)
	var needsProvisioning bool
	switch {
	case existing == nil:
		log.Info("No existing certificate found for " + domain + ". Proceeding with first issuance.")
		needsProvisioning = true
	case wall(existing.ExpiresAt).After(threshold) && existing.Status == StatusDeployed:
		log.Info("Certificate for " + domain + " is still valid and already deployed. No action needed.")
		return nil
	case wall(existing.ExpiresAt).After(threshold) && existing.Status == StatusToDeploy:
		log.Info("Certificate for " + domain + " is valid but was not yet deployed to Cloudflare. Retrying deployment.")
		needsProvisioning = false
	default:
		log.Info("Certificate for " + domain + " expires soon. Renewal needed.")
		needsProvisioning = true
	}

	err = u.provisionAndDeploy(ctx, now, domain, allDomains, serverURL, existing, needsProvisioning)
	if err != nil {
		log.Error(fmt.Sprintf("ACME renewal failed for domain %s after %dms: %v", domain, time.Since(start).Milliseconds(), err))
		return err
	}
	return nil
}

// provisionAndDeploy is the try block of renewIfNeeded.
func (u *RenewalUseCase) provisionAndDeploy(ctx context.Context, now time.Time, domain string, allDomains []string, serverURL string, existing *Certificate, needsProvisioning bool) error {
	log, cfg := u.Log, u.Cfg
	if needsProvisioning && !cfg.ServerInteractionEnabled {
		log.Info("ACME server interaction is disabled (ACME_SERVER_INTERACTION_ENABLED=false). Skipping certificate provisioning.")
		return nil
	}
	var certPEM, keyPEM string
	if needsProvisioning {
		// resume after an incident: is an order already in progress?
		pending, err := u.Orders.LoadOrder(ctx, domain)
		if err != nil {
			return err
		}
		switch {
		case pending == nil:
			if certPEM, keyPEM, err = u.startNewOrder(ctx, domain, allDomains, serverURL); err != nil {
				return err
			}
		default:
			ageHours := int64(wall(now).Sub(wall(pending.CreatedAt)) / time.Hour)
			if ageHours >= orderExpiryHours {
				log.Warn(fmt.Sprintf("Stale ACME order for %s (age %dh > %dh). Deleting and starting fresh.", domain, ageHours, orderExpiryHours))
				if err := u.Orders.DeleteOrder(ctx, domain); err != nil {
					return err
				}
				if certPEM, keyPEM, err = u.startNewOrder(ctx, domain, allDomains, serverURL); err != nil {
					return err
				}
			} else {
				log.Info(fmt.Sprintf("Found pending ACME order for %s with status %s. Attempting resume.", domain, pending.Status))
				var ok bool
				if certPEM, keyPEM, ok, err = u.resumeOrder(ctx, *pending, domain, allDomains, serverURL); err != nil {
					return err
				}
				if !ok {
					log.Info("ACME challenge still pending for " + domain + ". No further action this run. Will retry on next scheduled execution.")
					return nil
				}
			}
		}
	} else {
		// no new provisioning: reuse the certificate already stored
		certPEM, keyPEM = existing.CertificatePEM, existing.PrivateKeyPEM
	}

	// Cloudflare deployment
	if !cfg.CloudflareInteractionEnabled {
		log.Info("Cloudflare interaction is disabled (ACME_CLOUDFLARE_INTERACTION_ENABLED=false). Skipping certificate deployment to Cloudflare.")
		return nil
	}
	log.Info("Deploying certificate to Cloudflare for " + domain + "...")
	if err := u.Deployer.DeployCertificate(ctx, certPEM, keyPEM); err != nil {
		return err
	}
	log.Info("Certificate successfully deployed to Cloudflare for " + domain)

	// DEPLOYED in database + order cleanup
	if err := u.Certificates.MarkAsDeployed(ctx, domain, u.Now()); err != nil {
		return err
	}
	log.Info("Certificate status updated to DEPLOYED in database for " + domain)
	if err := u.Orders.DeleteOrder(ctx, domain); err != nil {
		return err
	}
	log.Info("ACME order cleaned up from database for " + domain)
	return nil
}

type domainChallenge struct {
	domain    string
	challenge CAChallenge
}

// startNewOrder is AcmeCertificateRenewalUseCase.startNewOrder.
func (u *RenewalUseCase) startNewOrder(ctx context.Context, domain string, allDomains []string, serverURL string) (string, string, error) {
	log, cfg := u.Log, u.Cfg
	log.Info(fmt.Sprintf("=== [startNewOrder] Starting new ACME order for domain=%s, allDomains=%v, serverUrl=%s ===", domain, allDomains, serverURL))

	// 2. account key pair
	stored, err := u.Accounts.LoadAccount(ctx, serverURL)
	if err != nil {
		return "", "", err
	}
	var accountKey crypto.Signer
	if stored != nil {
		if accountKey, err = ReadKeyPair(stored.KeyPEM); err != nil {
			return "", "", err
		}
	} else {
		log.Info("[startNewOrder] No ACME account found for " + serverURL + ". Generating new RSA-2048 key pair.")
		if accountKey, err = u.newKeyPair(); err != nil {
			return "", "", err
		}
	}

	// 3. session
	session := u.CA.NewSession(serverURL)

	// 4. ACME account
	var account CAAccount
	if stored != nil && stored.AccountURL != nil {
		log.Info("[startNewOrder] Reconnecting to existing ACME account: " + *stored.AccountURL)
		account, err = session.CreateAccount(ctx, accountKey, AccountOptions{OnlyExisting: true})
	} else {
		opts := AccountOptions{AgreeToTerms: true}
		if !javacompat.KotlinIsBlank(cfg.EabKid) && !javacompat.KotlinIsBlank(cfg.EabHmacKey) {
			log.Info("[startNewOrder] Creating new ACME account with EAB credentials (kid=" + cfg.EabKid + ").")
			opts.EabKid, opts.EabHmacKey = cfg.EabKid, cfg.EabHmacKey
		} else {
			log.Info("[startNewOrder] Creating new ACME account without EAB (stub/test mode).")
		}
		account, err = session.CreateAccount(ctx, accountKey, opts)
	}
	if err != nil {
		return "", "", err
	}

	// immediate persistence of the account (before challenge.trigger) so the
	// key pair is not lost if the ACME flow fails (DEBT-02)
	accountKeyPEM, err := WriteKeyPair(accountKey)
	if err != nil {
		return "", "", err
	}
	loc := account.Location()
	if err := u.Accounts.SaveAccount(ctx, Account{ServerURL: serverURL, AccountURL: &loc, KeyPEM: accountKeyPEM}); err != nil {
		return "", "", err
	}
	log.Info(fmt.Sprintf("[startNewOrder] ACME account persisted to database (serverUrl=%s, accountUrl=%s)", serverURL, loc))

	// 5. multi-domain order (CN = main domain, SANs = additional domains)
	order, err := account.NewOrder(ctx, allDomains)
	if err != nil {
		return "", "", err
	}
	log.Info(fmt.Sprintf("[startNewOrder] ACME order created: orderUrl=%s, domains=%v", order.Location(), allDomains))

	// 6. HTTP-01 challenges: one authorization per domain
	authorizations, err := order.Authorizations(ctx)
	if err != nil {
		return "", "", err
	}
	var challenges []domainChallenge
	for _, authz := range authorizations {
		authzDomain, err := authz.Domain(ctx)
		if err != nil {
			return "", "", err
		}
		challenge, err := authz.FindHTTP01(ctx)
		if err != nil {
			return "", "", err
		}
		if challenge == nil {
			return "", "", &IllegalStateError{"No HTTP-01 challenge available for identifier " + authzDomain}
		}
		ka, err := challenge.KeyAuthorization()
		if err != nil {
			return "", "", err
		}
		// stored in the database (shared between instances)
		if err := u.Challenges.StoreChallenge(ctx, challenge.Token(), ka); err != nil {
			return "", "", err
		}
		log.Info(fmt.Sprintf("[startNewOrder] Challenge token stored in database for %s (token=%s)", authzDomain, challenge.Token()))
		challenges = append(challenges, domainChallenge{authzDomain, challenge})
	}

	// 9. domain key pair (persisted before the polling)
	domainKey, err := u.newKeyPair()
	if err != nil {
		return "", "", err
	}
	domainKeyPEM, err := WriteKeyPair(domainKey)
	if err != nil {
		return "", "", err
	}
	if err := u.Orders.SaveOrder(ctx, Order{
		Domain: domain, OrderURL: order.Location(), DomainKeyPEM: domainKeyPEM,
		Status: OrderChallengePending, CreatedAt: u.Now(),
	}); err != nil {
		return "", "", err
	}
	log.Info(fmt.Sprintf("[startNewOrder] Order persisted to database for %s (status=CHALLENGE_PENDING, orderUrl=%s)", domain, order.Location()))

	if err := u.triggerAndPoll(ctx, domain, challenges); err != nil {
		return "", "", err
	}

	if err := u.Orders.UpdateOrderStatus(ctx, domain, OrderFinalizing); err != nil {
		return "", "", err
	}
	log.Info("[startNewOrder] Order status updated to ORDER_FINALIZING in database for " + domain)

	// 10. order finalization
	csr, err := BuildCSR(allDomains, domainKey)
	if err != nil {
		return "", "", err
	}

	// reload the order before the CSR (RFC 8555 7.4: it must be "ready")
	if err := order.Update(ctx); err != nil {
		var ra *RetryAfterError
		if errors.As(err, &ra) {
			log.Info(fmt.Sprintf("[startNewOrder] Order not ready yet for %s (Retry-After: %s) before CSR submission. Leaving order in CHALLENGE_PENDING state for resume on next scheduled execution.", domain, ra.RetryAfter.Format(time.RFC3339)))
			return "", "", &ChallengeTimeoutError{fmt.Sprintf("ACME order not ready before CSR submission for domain %s (Retry-After: %s)", domain, ra.RetryAfter.UTC().Format("2006-01-02T15:04:05.999999999Z"))}
		}
		return "", "", err
	}
	log.Info(fmt.Sprintf("[startNewOrder] Submitting CSR to ACME server for %s (orderUrl=%s)...", domain, order.Location()))
	if err := order.Execute(ctx, csr); err != nil {
		return "", "", err
	}
	if err := u.pollUntilOrderValid(ctx, order, domain); err != nil {
		return "", "", err
	}

	// 11. certificate download
	certPEM, err := u.downloadCertificate(ctx, order, domain)
	if err != nil {
		return "", "", err
	}

	// 15. certificate persistence (TO_DEPLOY): a second order.certificate
	// object, hence a second download, like the Kotlin code
	expiresAt, err := u.leafNotAfter(ctx, order)
	if err != nil {
		return "", "", err
	}
	if err := u.Certificates.SaveCertificate(ctx, Certificate{
		Domain: domain, CertificatePEM: certPEM, PrivateKeyPEM: domainKeyPEM, ExpiresAt: expiresAt, Status: StatusToDeploy,
	}); err != nil {
		return "", "", err
	}
	log.Info(fmt.Sprintf("[startNewOrder] Certificate saved to database for %s (status=TO_DEPLOY, expiresAt=%s)", domain, expiresAt.Format("2006-01-02T15:04:05")))
	return certPEM, domainKeyPEM, nil
}

// triggerAndPoll is the try / catch block around the challenges of
// startNewOrder: a timeout keeps the tokens (the CA may validate later), any
// other failure clears them all.
func (u *RenewalUseCase) triggerAndPoll(ctx context.Context, domain string, challenges []domainChallenge) error {
	log := u.Log
	err := func() error {
		// 7. trigger every challenge
		for _, dc := range challenges {
			log.Info(fmt.Sprintf("[startNewOrder] Triggering HTTP-01 challenge for %s (challengeUrl=%s)...", dc.domain, dc.challenge.Location()))
			if err := dc.challenge.Trigger(ctx); err != nil {
				return err
			}
		}
		// 8. poll every challenge until VALID
		for _, dc := range challenges {
			if err := u.pollUntilChallengeValid(ctx, dc.challenge, dc.domain); err != nil {
				return err
			}
		}
		// clear the challenges on total success only
		for _, dc := range challenges {
			if err := u.Challenges.ClearChallenge(ctx, dc.challenge.Token()); err != nil {
				return err
			}
			log.Info("[startNewOrder] Challenge token cleared from store (token=" + dc.challenge.Token() + ")")
		}
		return nil
	}()
	if err == nil {
		return nil
	}
	var timeout *ChallengeTimeoutError
	if errors.As(err, &timeout) {
		// keep the challenges: the CA may validate later, the order stays in
		// the database for the next run
		log.Warn("[startNewOrder] Challenge timed out for " + domain + ". Leaving all challenge tokens in store for next retry run.")
		return err
	}
	// definitive error (e.g. INVALID): clear every token
	for _, dc := range challenges {
		if cerr := u.Challenges.ClearChallenge(ctx, dc.challenge.Token()); cerr != nil {
			return cerr
		}
	}
	log.Info("[startNewOrder] All challenge tokens cleared from store after error for " + domain)
	return err
}

type authzResult struct {
	domain    string
	challenge CAChallenge
	updated   bool
}

// resumeOrder is AcmeCertificateRenewalUseCase.resumeOrder; ok=false is the
// Kotlin `null` result (nothing more to do this run).
func (u *RenewalUseCase) resumeOrder(ctx context.Context, pending Order, domain string, allDomains []string, serverURL string) (certPEM, keyPEM string, ok bool, err error) {
	log := u.Log
	log.Info(fmt.Sprintf("=== [resumeOrder] Resuming ACME order for domain=%s at status=%s (orderUrl=%s) ===", domain, pending.Status, pending.OrderURL))

	stored, err := u.Accounts.LoadAccount(ctx, serverURL)
	if err != nil {
		return "", "", false, err
	}
	if stored == nil {
		return "", "", false, &IllegalStateError{"Cannot resume ACME order: no account found for " + serverURL}
	}
	accountKey, err := ReadKeyPair(stored.KeyPEM)
	if err != nil {
		return "", "", false, err
	}
	session := u.CA.NewSession(serverURL)
	if stored.AccountURL == nil {
		return "", "", false, &IllegalStateError{"Cannot resume ACME order: account has no URL for " + serverURL}
	}
	login, err := session.Login(*stored.AccountURL, accountKey)
	if err != nil {
		return "", "", false, err
	}
	order := login.BindOrder(pending.OrderURL)
	if err := order.Update(ctx); err != nil {
		var ra *RetryAfterError
		if errors.As(err, &ra) {
			log.Info(fmt.Sprintf("[resumeOrder] Order not ready yet for %s (Retry-After: %s) during initial order reload. Will retry on next scheduled execution.", domain, ra.RetryAfter.Format(time.RFC3339)))
			return "", "", false, nil
		}
		return "", "", false, err
	}
	domainKeyPEM := pending.DomainKeyPEM

	switch pending.Status {
	case OrderChallengePending:
		authorizations, err := order.Authorizations(ctx)
		if err != nil {
			return "", "", false, err
		}
		// collect every challenge, restoring it in the store BEFORE
		// authorization.update() (covers a lost acme_challenge row)
		type collected struct {
			authz     CAAuthorization
			domain    string
			challenge CAChallenge
		}
		var cs []collected
		for _, authz := range authorizations {
			d, err := authz.Domain(ctx)
			if err != nil {
				return "", "", false, err
			}
			ch, err := authz.FindHTTP01(ctx)
			if err != nil {
				return "", "", false, err
			}
			if ch == nil {
				return "", "", false, &IllegalStateError{"No HTTP-01 challenge available for " + d + " during resume"}
			}
			ka, err := ch.KeyAuthorization()
			if err != nil {
				return "", "", false, err
			}
			if err := u.Challenges.StoreChallenge(ctx, ch.Token(), ka); err != nil {
				return "", "", false, err
			}
			cs = append(cs, collected{authz, d, ch})
		}
		// refresh every authorization
		var results []authzResult
		for _, c := range cs {
			updated := true
			if err := c.authz.Update(ctx); err != nil {
				var ra *RetryAfterError
				if !errors.As(err, &ra) {
					return "", "", false, err
				}
				updated = false
			}
			results = append(results, authzResult{c.domain, c.challenge, updated})
		}
		// one INVALID authorization: unrecoverable error
		for _, r := range results {
			if r.updated && r.challenge.Status() == CAStatusInvalid {
				for _, c := range cs {
					if err := u.Challenges.ClearChallenge(ctx, c.challenge.Token()); err != nil {
						return "", "", false, err
					}
				}
				return "", "", false, &ChallengeFailedError{"ACME HTTP-01 challenge INVALID for domain " + r.domain + " during resume"}
			}
		}
		allValid := true
		for _, r := range results {
			if !(r.updated && r.challenge.Status() == CAStatusValid) {
				allValid = false
				break
			}
		}
		if allValid {
			for _, c := range cs {
				if err := u.Challenges.ClearChallenge(ctx, c.challenge.Token()); err != nil {
					return "", "", false, err
				}
			}
			if err := u.Orders.UpdateOrderStatus(ctx, domain, OrderFinalizing); err != nil {
				return "", "", false, err
			}
			domainKey, err := ReadKeyPair(domainKeyPEM)
			if err != nil {
				return "", "", false, err
			}
			csr, err := BuildCSR(allDomains, domainKey)
			if err != nil {
				return "", "", false, err
			}
			if err := order.Execute(ctx, csr); err != nil {
				return "", "", false, err
			}
			if err := u.pollUntilOrderValid(ctx, order, domain); err != nil {
				return "", "", false, err
			}
			pem, err := u.downloadAndPersistCertificate(ctx, order, domain, domainKeyPEM)
			if err != nil {
				return "", "", false, err
			}
			return pem, domainKeyPEM, true, nil
		}
		// anything else (Retry-After, PENDING, PROCESSING): re-trigger the
		// challenges that are not PROCESSING and leave; the order stays in
		// CHALLENGE_PENDING for the next run
		for _, r := range results {
			if !r.updated || r.challenge.Status() != CAStatusProcessing {
				if err := r.challenge.Trigger(ctx); err != nil {
					return "", "", false, err
				}
			}
		}
		return "", "", false, nil

	case OrderFinalizing:
		if err := u.pollUntilOrderValid(ctx, order, domain); err != nil {
			return "", "", false, err
		}
		pem, err := u.downloadAndPersistCertificate(ctx, order, domain, domainKeyPEM)
		if err != nil {
			return "", "", false, err
		}
		return pem, domainKeyPEM, true, nil
	}
	return "", "", false, fmt.Errorf("unknown order status %q", pending.Status)
}

func (u *RenewalUseCase) pollUntilChallengeValid(ctx context.Context, ch CAChallenge, domain string) error {
	u.Log.Info(fmt.Sprintf("[pollChallenge] Starting challenge polling for %s (max %d attempts × %dms)", domain, pollingMaxAttempts, pollingInterval.Milliseconds()))
	for attempt := 0; attempt < pollingMaxAttempts; attempt++ {
		u.sleep(ctx, pollingInterval)
		if err := ch.Update(ctx); err != nil {
			var ra *RetryAfterError
			if !errors.As(err, &ra) {
				return err
			}
			// not ready yet: keep polling rather than letting the exception
			// reach the generic handler (which would clear the tokens)
			u.Log.Info(fmt.Sprintf("[pollChallenge] Challenge not ready yet for %s (Retry-After: %s), attempt %d/%d — continuing polling.", domain, ra.RetryAfter.Format(time.RFC3339), attempt+1, pollingMaxAttempts))
		}
		switch ch.Status() {
		case CAStatusValid:
			u.Log.Info(fmt.Sprintf("[pollChallenge] HTTP-01 challenge VALID after %d attempt(s) for domain %s", attempt+1, domain))
			return nil
		case CAStatusInvalid:
			u.Log.Error("[pollChallenge] HTTP-01 challenge INVALID for domain " + domain)
			return &ChallengeFailedError{"ACME HTTP-01 challenge INVALID for domain " + domain}
		}
		if attempt == pollingMaxAttempts-1 {
			u.Log.Warn(fmt.Sprintf("[pollChallenge] Challenge polling exhausted for %s after %d attempts. Last status: %s", domain, pollingMaxAttempts, ch.Status()))
			return &ChallengeTimeoutError{fmt.Sprintf("ACME HTTP-01 challenge timed out after %d attempts for domain %s", pollingMaxAttempts, domain)}
		}
	}
	return nil
}

func (u *RenewalUseCase) pollUntilOrderValid(ctx context.Context, order CAOrder, domain string) error {
	u.Log.Info(fmt.Sprintf("[pollOrder] Starting order polling for %s (max %d attempts × %dms, orderUrl=%s)", domain, pollingMaxAttempts, pollingInterval.Milliseconds(), order.Location()))
	for attempt := 0; attempt < pollingMaxAttempts; attempt++ {
		u.sleep(ctx, pollingInterval)
		if err := order.Update(ctx); err != nil {
			var ra *RetryAfterError
			if !errors.As(err, &ra) {
				return err
			}
			u.Log.Info(fmt.Sprintf("[pollOrder] Order not ready yet for %s (Retry-After: %s), attempt %d/%d — continuing polling.", domain, ra.RetryAfter.Format(time.RFC3339), attempt+1, pollingMaxAttempts))
			continue
		}
		status, err := order.Status(ctx)
		if err != nil {
			return err
		}
		switch status {
		case CAStatusValid:
			u.Log.Info(fmt.Sprintf("[pollOrder] ACME order VALID after %d attempt(s) for domain %s", attempt+1, domain))
			return nil
		case CAStatusInvalid:
			u.Log.Error("[pollOrder] ACME order INVALID for domain " + domain)
			return &ChallengeFailedError{"ACME order became INVALID for domain " + domain}
		}
	}
	u.Log.Warn(fmt.Sprintf("[pollOrder] Order polling exhausted for %s after %d attempts.", domain, pollingMaxAttempts))
	return &ChallengeTimeoutError{fmt.Sprintf("ACME order timed out after %d attempts for domain %s", pollingMaxAttempts, domain)}
}

// downloadCertificate is downloadCertificate(): the chain as PEM.
func (u *RenewalUseCase) downloadCertificate(ctx context.Context, order CAOrder, domain string) (string, error) {
	u.Log.Info("[downloadCert] Downloading certificate from ACME server for " + domain + " (orderUrl=" + order.Location() + ")")
	cert, err := order.Certificate(ctx)
	if err != nil {
		return "", err
	}
	if cert == nil {
		return "", &IllegalStateError{"ACME order completed but certificate is null for domain " + domain}
	}
	chain, err := cert.Chain(ctx)
	if err != nil {
		return "", err
	}
	u.Log.Info(fmt.Sprintf("[downloadCert] Certificate chain received for %s: %d certificate(s) in chain", domain, len(chain)))
	pem := CertificateChainPEM(chain)
	if len(chain) == 0 {
		return "", errors.New("NoSuchElementException: List is empty.")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return "", fmt.Errorf("CertificateException: %w", err)
	}
	u.Log.Info(fmt.Sprintf("[downloadCert] Certificate downloaded for %s: subject=%s, notBefore=%s, notAfter=%s, issuer=%s",
		domain, leaf.Subject, leaf.NotBefore, leaf.NotAfter, leaf.Issuer))
	return pem, nil
}

// leafNotAfter is `order.certificate!!.certificateChain.first()` converted to a
// LocalDateTime in the process zone (a new Certificate object: a new download).
func (u *RenewalUseCase) leafNotAfter(ctx context.Context, order CAOrder) (time.Time, error) {
	cert, err := order.Certificate(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if cert == nil {
		return time.Time{}, errors.New("NullPointerException: order.certificate is null")
	}
	chain, err := cert.Chain(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if len(chain) == 0 {
		return time.Time{}, errors.New("NoSuchElementException: List is empty.")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("CertificateException: %w", err)
	}
	return leaf.NotAfter.In(time.Local), nil
}

func (u *RenewalUseCase) downloadAndPersistCertificate(ctx context.Context, order CAOrder, domain, domainKeyPEM string) (string, error) {
	certPEM, err := u.downloadCertificate(ctx, order, domain)
	if err != nil {
		return "", err
	}
	expiresAt, err := u.leafNotAfter(ctx, order)
	if err != nil {
		return "", err
	}
	if err := u.Certificates.SaveCertificate(ctx, Certificate{
		Domain: domain, CertificatePEM: certPEM, PrivateKeyPEM: domainKeyPEM, ExpiresAt: expiresAt, Status: StatusToDeploy,
	}); err != nil {
		return "", err
	}
	u.Log.Info(fmt.Sprintf("[downloadCert] Certificate saved to database for %s (status=TO_DEPLOY, expiresAt=%s)", domain, expiresAt.Format("2006-01-02T15:04:05")))
	return certPEM, nil
}
