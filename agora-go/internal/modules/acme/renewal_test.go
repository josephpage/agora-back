package acme

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"
)

// Port of AcmeCertificateRenewalUseCaseTest. The Kotlin tests could not script the ACME
// server (acme4j classes are final) and stopped at the first network call; the fake CA of
// fakes_test.go lets the same scenarios run through the whole state machine.

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenewDisabledDoesNothing(t *testing.T) {
	f := newFixture(t)
	f.cfg.Enabled = false
	mustNoErr(t, f.run())
	if f.repos.ops() != "" || len(f.calls) != 0 {
		t.Errorf("interactions: %s %v", f.repos.ops(), f.calls)
	}
}

func TestRenewValidDeployedCertificateDoesNothing(t *testing.T) {
	f := newFixture(t)
	f.addCert(StatusDeployed, 60*24*time.Hour)
	mustNoErr(t, f.run())
	if f.repos.ops() != "loadCertificate" || len(f.calls) != 0 {
		t.Errorf("interactions: %s %v", f.repos.ops(), f.calls)
	}
}

func TestRenewToDeployCertificateIsDeployedWithoutProvisioning(t *testing.T) {
	f := newFixture(t)
	f.addCert(StatusToDeploy, 60*24*time.Hour)
	f.addOrder(OrderFinalizing, time.Hour)
	mustNoErr(t, f.run())
	if f.repos.deployed[0] != "cert-pem|key-pem" {
		t.Errorf("deployed %v", f.repos.deployed)
	}
	if got := f.repos.ops(); got != "loadCertificate,deploy,markAsDeployed:2026-06-25T03:00:00,deleteOrder" {
		t.Errorf("ops %s", got)
	}
	if len(f.calls) != 0 || len(f.repos.orders) != 0 || f.repos.certs[0].Status != StatusDeployed {
		t.Errorf("calls=%v orders=%v cert=%v", f.calls, f.repos.orders, f.repos.certs[0])
	}
}

func TestRenewCloudflareFailureLeavesCertificateToDeploy(t *testing.T) {
	f := newFixture(t)
	f.addCert(StatusToDeploy, 60*24*time.Hour)
	f.repos.deployErr = errors.New("Cloudflare error")
	if err := f.run(); err == nil {
		t.Fatal("expected the Cloudflare error")
	}
	if got := f.repos.ops(); got != "loadCertificate,deploy" {
		t.Errorf("ops %s", got)
	}
}

func TestRenewCloudflareDisabledSkipsDeployment(t *testing.T) {
	f := newFixture(t)
	f.cfg.CloudflareInteractionEnabled = false
	f.addCert(StatusToDeploy, 60*24*time.Hour)
	mustNoErr(t, f.run())
	if got := f.repos.ops(); got != "loadCertificate" {
		t.Errorf("ops %s", got)
	}
}

func TestRenewServerInteractionDisabledStillDeploysAStoredCertificate(t *testing.T) {
	f := newFixture(t)
	f.cfg.ServerInteractionEnabled = false
	f.addCert(StatusToDeploy, 60*24*time.Hour)
	mustNoErr(t, f.run())
	if !f.repos.has("deploy") || !f.repos.has("markAsDeployed") || !f.repos.has("deleteOrder") || len(f.calls) != 0 {
		t.Errorf("ops %s calls %v", f.repos.ops(), f.calls)
	}
}

func TestRenewServerInteractionDisabledSkipsProvisioning(t *testing.T) {
	for name, setup := range map[string]func(*renewFixture){
		"no certificate": func(*renewFixture) {},
		"expiring":       func(f *renewFixture) { f.addCert(StatusDeployed, 10*24*time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.ServerInteractionEnabled = false
			setup(f)
			mustNoErr(t, f.run())
			if got := f.repos.ops(); got != "loadCertificate" || len(f.calls) != 0 {
				t.Errorf("ops %s calls %v", got, f.calls)
			}
		})
	}
}

func TestRenewThresholdIs30Days(t *testing.T) {
	for _, tc := range []struct {
		expiresIn time.Duration
		status    CertificateStatus
		wantOrder bool
		wantNoop  bool
	}{
		{30*24*time.Hour + time.Second, StatusDeployed, false, true},
		{30 * 24 * time.Hour, StatusDeployed, true, false}, // not strictly after now+30d
		{29 * 24 * time.Hour, StatusDeployed, true, false},
		{30 * 24 * time.Hour, StatusToDeploy, true, false}, // renewal wins over the deployment retry
		{30*24*time.Hour + time.Second, StatusToDeploy, false, false},
		{-time.Hour, StatusDeployed, true, false},
	} {
		f := newFixture(t)
		f.cfg.ServerInteractionEnabled = false // provisioning is just observed through loadOrder
		f.addCert(tc.status, tc.expiresIn)
		f.cfg.ServerInteractionEnabled = true
		f.ca.createAccountErr = errBoom
		_ = f.run()
		gotOrder := f.repos.has("loadOrder")
		if gotOrder != tc.wantOrder {
			t.Errorf("%v %s: provisioning=%v, want %v (%s)", tc.expiresIn, tc.status, gotOrder, tc.wantOrder, f.repos.ops())
		}
		if tc.wantNoop && f.repos.ops() != "loadCertificate" {
			t.Errorf("%v %s: ops %s", tc.expiresIn, tc.status, f.repos.ops())
		}
	}
}

// --- resume / stale orders --------------------------------------------------------------------

func TestRenewStaleOrderIsDeletedAndANewOrderStarted(t *testing.T) {
	for _, age := range []time.Duration{169 * time.Hour, 168*time.Hour + time.Minute, 168 * time.Hour, 200 * time.Hour} {
		f := newFixture(t)
		f.addOrder(OrderChallengePending, age)
		f.ca.createAccountErr = errBoom // no account: a new one is attempted, which fails here
		if err := f.run(); !errors.Is(err, errBoom) {
			t.Fatalf("age %v: %v", age, err)
		}
		if !f.repos.has("deleteOrder") || !f.repos.has("loadAccount") || len(f.ca.accountOpts) != 1 {
			t.Errorf("age %v: ops %s", age, f.repos.ops())
		}
	}
}

func TestRenewRecentOrderIsResumedNotDeleted(t *testing.T) {
	for _, age := range []time.Duration{time.Hour, 167 * time.Hour, 167*time.Hour + 59*time.Minute + 59*time.Second} {
		f := newFixture(t)
		f.addOrder(OrderChallengePending, age)
		err := f.run() // no account in the database
		var is *IllegalStateError
		if !errors.As(err, &is) || !strings.Contains(is.Msg, "no account found") {
			t.Fatalf("age %v: %v", age, err)
		}
		if f.repos.has("deleteOrder") || len(f.repos.orders) != 1 || len(f.calls) != 0 {
			t.Errorf("age %v: ops %s", age, f.repos.ops())
		}
	}
}

func TestResumeWithAccountWithoutURLFails(t *testing.T) {
	f := newFixture(t)
	f.addOrder(OrderChallengePending, time.Hour)
	f.addAccount(nil)
	err := f.run()
	var is *IllegalStateError
	if !errors.As(err, &is) || !strings.Contains(is.Msg, "account has no URL") {
		t.Fatalf("%v", err)
	}
}

func TestResumeOrderReloadRetryAfterLeavesEverythingInPlace(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderChallengePending, 2*time.Hour)
	o := f.order("agora.gouv.fr")
	o.updates = []error{&RetryAfterError{Msg: "not ready", RetryAfter: testNow}}
	mustNoErr(t, f.run())
	if f.ca.loginURL != url || len(f.repos.orders) != 1 || f.repos.has("deploy") || f.repos.has("storeChallenge") {
		t.Errorf("ops %s calls %v", f.repos.ops(), f.calls)
	}
}

func TestResumeChallengePendingRestoresTokensThenRetriggers(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderChallengePending, 24*time.Hour)
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr")
	// first challenge pending, second already PROCESSING: only the first is re-triggered
	o.authz[1].challenge.status = CAStatusProcessing
	mustNoErr(t, f.run())
	got := strings.Join(f.calls, ",")
	want := "login,bindOrder:https://acme.test/order/12345,order.update,authz.update:agora.gouv.fr,authz.update:www.agora.gouv.fr,challenge.trigger:tok1"
	if got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
	// both tokens are re-stored BEFORE any authorization.update()
	if f.repos.count("storeChallenge") != 2 || len(f.repos.challenges) != 2 || f.repos.challenges["tok1"] != "tok1.THUMB" {
		t.Errorf("ops %s challenges %v", f.repos.ops(), f.repos.challenges)
	}
	if f.repos.has("clearChallenge") || f.repos.has("updateOrderStatus") || f.repos.has("deploy") || len(f.repos.orders) != 1 {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestResumeAuthorizationRetryAfterRetriggersUnlessProcessing(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderChallengePending, time.Hour)
	o := f.order("agora.gouv.fr")
	o.authz[0].challenge.status = CAStatusProcessing // status unknown after a Retry-After: re-triggered anyway
	o.authz[0].updateErr = &RetryAfterError{Msg: "x", RetryAfter: testNow}
	mustNoErr(t, f.run())
	if !strings.Contains(strings.Join(f.calls, ","), "challenge.trigger:tok1") {
		t.Errorf("calls %v", f.calls)
	}
}

func TestResumeInvalidChallengeClearsTokensAndFails(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderChallengePending, time.Hour)
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr")
	o.authz[1].challenge.status = CAStatusInvalid
	err := f.run()
	var failed *ChallengeFailedError
	if !errors.As(err, &failed) || !strings.Contains(failed.Msg, "www.agora.gouv.fr during resume") {
		t.Fatalf("%v", err)
	}
	if len(f.repos.challenges) != 0 || f.repos.count("clearChallenge") != 2 || len(f.repos.orders) != 1 {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestResumeValidChallengesFinalizeAndDeploy(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderChallengePending, time.Hour)
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr")
	f.cfg.Sans = "www.agora.gouv.fr"
	for _, a := range o.authz {
		a.challenge.status = CAStatusValid
	}
	o.statuses = []CAStatus{CAStatusPending /* reload */, CAStatusValid}
	o.chain = [][]byte{testCertDER(t, "agora.gouv.fr", testNow.Add(90*24*time.Hour).Truncate(time.Second))}
	mustNoErr(t, f.run())
	wantOps := "loadCertificate,loadOrder,loadAccount,storeChallenge:tok1,storeChallenge:tok2,clearChallenge:tok1,clearChallenge:tok2,updateOrderStatus:ORDER_FINALIZING," +
		"saveCertificate:TO_DEPLOY,deploy,markAsDeployed:2026-06-25T03:00:00,deleteOrder"
	if got := f.repos.ops(); got != wantOps {
		t.Errorf("ops\n got %s\nwant %s", got, wantOps)
	}
	if o.downloads != 2 { // order.certificate is a new object each time: two downloads
		t.Errorf("downloads %d", o.downloads)
	}
	// the CSR is signed with the stored domain key and covers all the domains
	csr, err := x509.ParseCertificateRequest(o.csrs[0])
	mustNoErr(t, err)
	if csr.Subject.CommonName != "agora.gouv.fr" || strings.Join(csr.DNSNames, ",") != "agora.gouv.fr,www.agora.gouv.fr" {
		t.Errorf("csr %q %v", csr.Subject.CommonName, csr.DNSNames)
	}
	// the chain is stored as PEM blocks and the certificate expiry comes from the leaf
	c := f.repos.certs[0]
	if !strings.HasPrefix(c.CertificatePEM, "-----BEGIN CERTIFICATE-----\n") || strings.Count(c.CertificatePEM, "BEGIN CERTIFICATE") != 1 {
		t.Errorf("pem %q", c.CertificatePEM)
	}
	if !c.ExpiresAt.Equal(testNow.Add(90 * 24 * time.Hour).Truncate(time.Second)) {
		t.Errorf("expiresAt %v", c.ExpiresAt)
	}
}

func TestResumeOrderFinalizingPollsDownloadsAndDeploys(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderFinalizing, time.Hour)
	o := f.order("agora.gouv.fr")
	o.statuses = []CAStatus{CAStatusPending, CAStatusPending, CAStatusValid}
	o.chain = [][]byte{testCertDER(t, "agora.gouv.fr", testNow.Add(90*24*time.Hour))}
	mustNoErr(t, f.run())
	// update #1 is the initial reload (no sleep); statuses after updates 2 and 3 are
	// PENDING then VALID, so the poll sleeps twice
	if f.sleeps != 2 {
		t.Errorf("sleeps %d", f.sleeps)
	}
	if strings.Contains(strings.Join(f.calls, ","), "challenge") || strings.Contains(strings.Join(f.calls, ","), "authz") {
		t.Errorf("calls %v", f.calls)
	}
	if !f.repos.has("saveCertificate:TO_DEPLOY") || !f.repos.has("deploy") || len(f.repos.orders) != 0 {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestResumeOrderFinalizingTimeout(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderFinalizing, time.Hour)
	o := f.order("agora.gouv.fr")
	o.statuses = []CAStatus{CAStatusPending}
	err := f.run()
	var to *ChallengeTimeoutError
	if !errors.As(err, &to) || f.sleeps != 10 || f.repos.has("saveCertificate") || len(f.repos.orders) != 1 {
		t.Fatalf("err=%v sleeps=%d ops=%s", err, f.sleeps, f.repos.ops())
	}
}

func TestResumeOrderFinalizingInvalid(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.addOrder(OrderFinalizing, time.Hour)
	o := f.order("agora.gouv.fr")
	o.statuses = []CAStatus{CAStatusPending, CAStatusInvalid}
	var failed *ChallengeFailedError
	if err := f.run(); !errors.As(err, &failed) || !strings.Contains(failed.Msg, "order became INVALID") {
		t.Fatalf("%v", err)
	}
}

// --- new orders ------------------------------------------------------------------------------

func TestNewOrderFullFlow(t *testing.T) {
	f := newFixture(t)
	f.cfg.Sans = "www.agora.gouv.fr, ,api.agora.gouv.fr"
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr", "api.agora.gouv.fr")
	f.validPolling(o)
	mustNoErr(t, f.run())
	wantOps := "loadCertificate,loadOrder,loadAccount,saveAccount,storeChallenge:tok1,storeChallenge:tok2,storeChallenge:tok3,saveOrder:CHALLENGE_PENDING," +
		"clearChallenge:tok1,clearChallenge:tok2,clearChallenge:tok3,updateOrderStatus:ORDER_FINALIZING,saveCertificate:TO_DEPLOY,deploy,markAsDeployed:2026-06-25T03:00:00,deleteOrder"
	if got := f.repos.ops(); got != wantOps {
		t.Errorf("ops\n got %s\nwant %s", got, wantOps)
	}
	if got := strings.Join(f.ca.newOrderDomains[0], ","); got != "agora.gouv.fr,www.agora.gouv.fr,api.agora.gouv.fr" {
		t.Errorf("order domains %s", got)
	}
	if opts := f.ca.accountOpts[0]; opts.OnlyExisting || !opts.AgreeToTerms || opts.EabKid != "" {
		t.Errorf("account options %+v", opts)
	}
	// triggers first, then the polls, in order
	got := strings.Join(f.calls, ",")
	want := "createAccount,newOrder,challenge.trigger:tok1,challenge.trigger:tok2,challenge.trigger:tok3,challenge.update:tok1,challenge.update:tok2,challenge.update:tok3," +
		"order.update,order.execute,order.update,certificate.download,certificate.download"
	if got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
	if f.sleeps != 4 { // 3 challenge polls + 1 order poll
		t.Errorf("sleeps %d", f.sleeps)
	}
	// the account and domain keys are stored as acme4j writes them
	if a := f.repos.accounts[0]; a.AccountURL == nil || *a.AccountURL != "https://acme.test/acct/1" || !strings.HasPrefix(a.KeyPEM, "-----BEGIN RSA PRIVATE KEY-----\n") {
		t.Errorf("account %+v", a)
	}
	if !strings.HasPrefix(f.repos.certs[0].PrivateKeyPEM, "-----BEGIN RSA PRIVATE KEY-----\n") {
		t.Errorf("private key %q", f.repos.certs[0].PrivateKeyPEM[:40])
	}
	blk, _ := pem.Decode([]byte(f.repos.certs[0].CertificatePEM))
	if blk == nil || strings.Count(f.repos.certs[0].CertificatePEM, "BEGIN CERTIFICATE") != 2 {
		t.Errorf("chain %q", f.repos.certs[0].CertificatePEM)
	}
	// LocalDateTime of notAfter in the process zone
	if !f.repos.certs[0].ExpiresAt.Equal(testNow.Add(90 * 24 * time.Hour).Truncate(time.Second)) {
		t.Errorf("expiresAt %v", f.repos.certs[0].ExpiresAt)
	}
}

func TestNewOrderReusesTheStoredAccountOnlyExisting(t *testing.T) {
	f := newFixture(t)
	url := "https://acme.test/acct/1"
	f.addAccount(&url)
	f.cfg.EabKid, f.cfg.EabHmacKey = "kid", "a2V5" // ignored when the account exists
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	mustNoErr(t, f.run())
	if opts := f.ca.accountOpts[0]; !opts.OnlyExisting || opts.AgreeToTerms || opts.EabKid != "" {
		t.Errorf("account options %+v", opts)
	}
}

func TestNewOrderStoredAccountWithoutURLCreatesAnAccount(t *testing.T) {
	f := newFixture(t)
	f.addAccount(nil)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	mustNoErr(t, f.run())
	if opts := f.ca.accountOpts[0]; opts.OnlyExisting || !opts.AgreeToTerms {
		t.Errorf("account options %+v", opts)
	}
	// the stored account keeps its key and gets its URL
	if len(f.repos.accounts) != 1 || f.repos.accounts[0].AccountURL == nil {
		t.Errorf("accounts %+v", f.repos.accounts)
	}
}

func TestNewOrderEAB(t *testing.T) {
	for _, tc := range []struct {
		kid, hmac string
		eab       bool
	}{
		{"kid", "a2V5", true}, {" ", "a2V5", false}, {"kid", "  ", false}, {"", "", false},
	} {
		f := newFixture(t)
		f.cfg.EabKid, f.cfg.EabHmacKey = tc.kid, tc.hmac
		o := f.order("agora.gouv.fr")
		f.validPolling(o)
		mustNoErr(t, f.run())
		if got := f.ca.accountOpts[0].EabKid != ""; got != tc.eab {
			t.Errorf("%q/%q: eab=%v", tc.kid, tc.hmac, got)
		}
		if tc.eab && (f.ca.accountOpts[0].EabKid != "kid" || f.ca.accountOpts[0].EabHmacKey != "a2V5") {
			t.Errorf("options %+v", f.ca.accountOpts[0])
		}
	}
}

func TestNewOrderAccountFailureLeavesNoRow(t *testing.T) {
	f := newFixture(t)
	f.ca.createAccountErr = errBoom
	if err := f.run(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if f.repos.has("saveAccount") || f.repos.has("saveOrder") || f.repos.has("storeChallenge") {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestNewOrderNoHTTP01ChallengeFails(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	o.authz[0].challenge = nil
	err := f.run()
	var is *IllegalStateError
	if !errors.As(err, &is) || !strings.Contains(is.Msg, "No HTTP-01 challenge available for identifier agora.gouv.fr") {
		t.Fatalf("%v", err)
	}
	if !f.repos.has("saveAccount") || f.repos.has("saveOrder") {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestNewOrderChallengeTimeoutKeepsTokensAndOrder(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr")
	f.cfg.Sans = "www.agora.gouv.fr"
	o.authz[0].challenge.updates = []fakeChallengeUpdate{{status: CAStatusValid}}
	o.authz[1].challenge.updates = []fakeChallengeUpdate{{status: CAStatusProcessing}}
	err := f.run()
	var to *ChallengeTimeoutError
	if !errors.As(err, &to) || !strings.Contains(to.Msg, "timed out after 10 attempts for domain www.agora.gouv.fr") {
		t.Fatalf("%v", err)
	}
	if f.repos.has("clearChallenge") || len(f.repos.challenges) != 2 || len(f.repos.orders) != 1 || f.repos.orders[0].Status != OrderChallengePending {
		t.Errorf("ops %s orders %v", f.repos.ops(), f.repos.orders)
	}
	if f.sleeps != 11 { // 1 + 10 polls
		t.Errorf("sleeps %d", f.sleeps)
	}
}

func TestNewOrderRetryAfterWhilePollingChallengeKeepsPolling(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	ra := &RetryAfterError{Msg: "not completed", RetryAfter: testNow}
	o.authz[0].challenge.updates = []fakeChallengeUpdate{{err: ra, status: CAStatusPending}, {err: ra, status: CAStatusPending}, {status: CAStatusValid}}
	mustNoErr(t, f.run())
	if f.repos.count("clearChallenge") != 1 || !f.repos.has("deploy") {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestNewOrderRetryAfterWithValidStatusStillSucceeds(t *testing.T) {
	// update() sets the data before raising: the VALID status of that very answer is used
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	o.authz[0].challenge.updates = []fakeChallengeUpdate{{err: &RetryAfterError{Msg: "x", RetryAfter: testNow}, status: CAStatusValid}}
	mustNoErr(t, f.run())
	if f.sleeps != 2 {
		t.Errorf("sleeps %d", f.sleeps)
	}
}

func TestNewOrderInvalidChallengeClearsTokens(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr", "www.agora.gouv.fr")
	f.cfg.Sans = "www.agora.gouv.fr"
	o.authz[0].challenge.updates = []fakeChallengeUpdate{{status: CAStatusValid}}
	o.authz[1].challenge.updates = []fakeChallengeUpdate{{status: CAStatusInvalid}}
	var failed *ChallengeFailedError
	if err := f.run(); !errors.As(err, &failed) {
		t.Fatal(err)
	}
	if len(f.repos.challenges) != 0 || f.repos.has("updateOrderStatus") || len(f.repos.orders) != 1 || f.repos.orders[0].Status != OrderChallengePending {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestNewOrderTriggerFailureClearsTokens(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	o.authz[0].challenge.triggerErr = errBoom
	if err := f.run(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if len(f.repos.challenges) != 0 || f.repos.count("clearChallenge") != 1 {
		t.Errorf("ops %s", f.repos.ops())
	}
}

func TestNewOrderRetryAfterBeforeTheCSRKeepsTheOrderAndFlagsItFinalizing(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	o.updates = []error{&RetryAfterError{Msg: "not ready", RetryAfter: testNow}}
	err := f.run()
	var to *ChallengeTimeoutError
	if !errors.As(err, &to) || !strings.Contains(to.Msg, "order not ready before CSR submission") && !strings.Contains(to.Msg, "ACME order not ready before CSR submission") {
		t.Fatalf("%v", err)
	}
	// tokens were cleared by the successful polling, the status already moved on (a quirk of the original)
	if len(f.repos.orders) != 1 || f.repos.orders[0].Status != OrderFinalizing || len(o.csrs) != 0 || f.repos.has("saveCertificate") {
		t.Errorf("ops %s orders %v", f.repos.ops(), f.repos.orders)
	}
}

func TestNewOrderPollTimeoutAndInvalid(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	o.statuses = []CAStatus{CAStatusPending, CAStatusProcessing}
	var to *ChallengeTimeoutError
	if err := f.run(); !errors.As(err, &to) || !strings.Contains(to.Msg, "order timed out after 10 attempts") {
		t.Fatal(err)
	}
	if f.repos.has("saveCertificate") || f.repos.has("deploy") || o.downloads != 0 {
		t.Errorf("ops %s", f.repos.ops())
	}

	f = newFixture(t)
	o = f.order("agora.gouv.fr")
	f.validPolling(o)
	o.statuses = []CAStatus{CAStatusPending, CAStatusInvalid}
	var failed *ChallengeFailedError
	if err := f.run(); !errors.As(err, &failed) {
		t.Fatal(err)
	}
}

func TestNewOrderRetryAfterWhilePollingOrderSkipsTheStatusCheck(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	ra := &RetryAfterError{Msg: "x", RetryAfter: testNow}
	// the first poll raises Retry-After although the status is VALID: the check is skipped, the next poll counts
	o.updates = []error{nil, ra, nil}
	o.statuses = []CAStatus{CAStatusPending, CAStatusValid, CAStatusValid}
	mustNoErr(t, f.run())
	if f.sleeps != 1+2 {
		t.Errorf("sleeps %d", f.sleeps)
	}
}

func TestNewOrderExecuteAndDownloadErrorsPropagate(t *testing.T) {
	f := newFixture(t)
	o := f.order("agora.gouv.fr")
	f.validPolling(o)
	o.execErr = errBoom
	if err := f.run(); !errors.Is(err, errBoom) || f.repos.has("saveCertificate") {
		t.Fatal(err)
	}

	f = newFixture(t)
	o = f.order("agora.gouv.fr")
	f.validPolling(o)
	o.chain = nil // order completed without certificate
	err := f.run()
	var is *IllegalStateError
	if !errors.As(err, &is) || !strings.Contains(is.Msg, "certificate is null") {
		t.Fatal(err)
	}
}

func TestRepositoryFailuresPropagateAtEachStep(t *testing.T) {
	for _, op := range []string{"loadCertificate", "loadOrder", "loadAccount", "saveAccount", "storeChallenge", "saveOrder", "clearChallenge", "updateOrderStatus", "saveCertificate", "markAsDeployed", "deleteOrder"} {
		f := newFixture(t)
		o := f.order("agora.gouv.fr")
		f.validPolling(o)
		f.repos.failures[op] = errBoom
		if err := f.run(); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", op, err)
		}
	}
}

func TestRenewLocalDateTimeIgnoresTheZone(t *testing.T) {
	// 168 hours between two LocalDateTime values are 168 wall clock hours, whatever the DST changes between them
	zone, err := time.LoadLocation("Europe/Paris")
	mustNoErr(t, err)
	f := newFixture(t)
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, zone)
	f.uc.Now = func() time.Time { return now }
	f.repos.orders = append(f.repos.orders, Order{Domain: f.cfg.Domain, OrderURL: "u", DomainKeyPEM: f.keyPEM(), Status: OrderChallengePending,
		CreatedAt: time.Date(2026, 3, 23, 12, 0, 0, 0, zone)}) // spans the March 29 DST change: 167 absolute hours, 168 wall hours
	f.ca.createAccountErr = errBoom
	_ = f.run()
	if !f.repos.has("deleteOrder") {
		t.Errorf("a 168 wall clock hours old order must be stale: %s", f.repos.ops())
	}
}
