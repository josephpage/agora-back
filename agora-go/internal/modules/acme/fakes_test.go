package acme

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- in-memory repositories (the Mockito mocks of the Kotlin tests) -------------------

type fakeRepos struct {
	mu         sync.Mutex
	log        []string
	certs      []Certificate
	accounts   []Account
	orders     []Order
	challenges map[string]string
	failures   map[string]error // operation name → error
	deployed   []string
	deployErr  error
	nowFn      func() time.Time
}

func newFakeRepos() *fakeRepos {
	return &fakeRepos{challenges: map[string]string{}, failures: map[string]error{}}
}

func (r *fakeRepos) rec(op string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, op)
	return r.failures[strings.SplitN(op, ":", 2)[0]]
}

func (r *fakeRepos) ops() string { return strings.Join(r.log, ",") }

func (r *fakeRepos) has(prefix string) bool {
	for _, l := range r.log {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func (r *fakeRepos) count(prefix string) int {
	n := 0
	for _, l := range r.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (r *fakeRepos) LoadCertificate(_ context.Context, domain string) (*Certificate, error) {
	if err := r.rec("loadCertificate"); err != nil {
		return nil, err
	}
	for i := len(r.certs) - 1; i >= 0; i-- {
		if r.certs[i].Domain == domain {
			c := r.certs[i]
			return &c, nil
		}
	}
	return nil, nil
}

func (r *fakeRepos) SaveCertificate(_ context.Context, c Certificate) error {
	if err := r.rec("saveCertificate:" + string(c.Status)); err != nil {
		return err
	}
	r.certs = append(r.certs, c)
	return nil
}

func (r *fakeRepos) MarkAsDeployed(_ context.Context, domain string, at time.Time) error {
	if err := r.rec("markAsDeployed:" + at.Format("2006-01-02T15:04:05")); err != nil {
		return err
	}
	for i := len(r.certs) - 1; i >= 0; i-- {
		if r.certs[i].Domain == domain {
			r.certs[i].Status = StatusDeployed
			r.certs[i].DeployedAt = &at
			break
		}
	}
	return nil
}

func (r *fakeRepos) LoadAccount(_ context.Context, serverURL string) (*Account, error) {
	if err := r.rec("loadAccount"); err != nil {
		return nil, err
	}
	for i := len(r.accounts) - 1; i >= 0; i-- {
		if r.accounts[i].ServerURL == serverURL {
			a := r.accounts[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (r *fakeRepos) SaveAccount(_ context.Context, a Account) error {
	if err := r.rec("saveAccount"); err != nil {
		return err
	}
	for i := range r.accounts {
		if r.accounts[i].ServerURL == a.ServerURL {
			r.accounts[i] = a
			return nil
		}
	}
	r.accounts = append(r.accounts, a)
	return nil
}

func (r *fakeRepos) StoreChallenge(_ context.Context, token, ka string) error {
	if err := r.rec("storeChallenge:" + token); err != nil {
		return err
	}
	r.challenges[token] = ka
	return nil
}

func (r *fakeRepos) GetChallenge(_ context.Context, token string) (*string, error) {
	if err := r.rec("getChallenge"); err != nil {
		return nil, err
	}
	if v, ok := r.challenges[token]; ok {
		return &v, nil
	}
	return nil, nil
}

func (r *fakeRepos) ClearChallenge(_ context.Context, token string) error {
	if err := r.rec("clearChallenge:" + token); err != nil {
		return err
	}
	delete(r.challenges, token)
	return nil
}

func (r *fakeRepos) LoadOrder(_ context.Context, domain string) (*Order, error) {
	if err := r.rec("loadOrder"); err != nil {
		return nil, err
	}
	for i := len(r.orders) - 1; i >= 0; i-- {
		if r.orders[i].Domain == domain {
			o := r.orders[i]
			return &o, nil
		}
	}
	return nil, nil
}

func (r *fakeRepos) SaveOrder(_ context.Context, o Order) error {
	if err := r.rec("saveOrder:" + string(o.Status)); err != nil {
		return err
	}
	r.orders = append(r.orders, o)
	return nil
}

func (r *fakeRepos) UpdateOrderStatus(_ context.Context, domain string, st OrderStatus) error {
	if err := r.rec("updateOrderStatus:" + string(st)); err != nil {
		return err
	}
	for i := len(r.orders) - 1; i >= 0; i-- {
		if r.orders[i].Domain == domain {
			r.orders[i].Status = st
			break
		}
	}
	return nil
}

func (r *fakeRepos) DeleteOrder(_ context.Context, domain string) error {
	if err := r.rec("deleteOrder"); err != nil {
		return err
	}
	var keep []Order
	for _, o := range r.orders {
		if o.Domain != domain {
			keep = append(keep, o)
		}
	}
	r.orders = keep
	return nil
}

func (r *fakeRepos) DeployCertificate(_ context.Context, cert, key string) error {
	r.mu.Lock()
	r.log = append(r.log, "deploy")
	r.deployed = append(r.deployed, cert+"|"+key)
	r.mu.Unlock()
	return r.deployErr
}

// --- scripted CA -----------------------------------------------------------------------

type fakeCA struct {
	log *[]string

	sessions         []string
	accountOpts      []AccountOptions
	loginURL         string
	createAccountErr error
	accountLocation  string
	order            *fakeOrder
	newOrderDomains  [][]string
}

func (c *fakeCA) rec(s string) { *c.log = append(*c.log, s) }

func (c *fakeCA) NewSession(serverURL string) CASession {
	c.sessions = append(c.sessions, serverURL)
	return c
}

func (c *fakeCA) CreateAccount(_ context.Context, _ crypto.Signer, o AccountOptions) (CAAccount, error) {
	c.rec("createAccount")
	c.accountOpts = append(c.accountOpts, o)
	if c.createAccountErr != nil {
		return nil, c.createAccountErr
	}
	return c, nil
}

func (c *fakeCA) Login(accountURL string, _ crypto.Signer) (CAAccount, error) {
	c.rec("login")
	c.loginURL = accountURL
	return c, nil
}

func (c *fakeCA) Location() string {
	if c.accountLocation == "" {
		return "https://acme.test/acct/1"
	}
	return c.accountLocation
}
func (c *fakeCA) Status() string { return "valid" }

func (c *fakeCA) NewOrder(_ context.Context, domains []string) (CAOrder, error) {
	c.rec("newOrder")
	c.newOrderDomains = append(c.newOrderDomains, domains)
	return c.order, nil
}

func (c *fakeCA) BindOrder(url string) CAOrder {
	c.rec("bindOrder:" + url)
	return c.order
}

type fakeOrder struct {
	ca        *fakeCA
	location  string
	updates   []error // results of successive Update calls (nil beyond the script)
	updateN   int
	statuses  []CAStatus // status after each Update (last repeated)
	status    CAStatus
	authz     []*fakeAuthz
	chain     [][]byte
	execErr   error
	csrs      [][]byte
	downloads int
}

func (o *fakeOrder) Location() string { return o.location }
func (o *fakeOrder) Update(context.Context) error {
	o.ca.rec("order.update")
	var err error
	if o.updateN < len(o.updates) {
		err = o.updates[o.updateN]
	}
	if o.updateN < len(o.statuses) {
		o.status = o.statuses[o.updateN]
	} else if len(o.statuses) > 0 {
		o.status = o.statuses[len(o.statuses)-1]
	}
	o.updateN++
	return err
}
func (o *fakeOrder) Execute(_ context.Context, csr []byte) error {
	o.ca.rec("order.execute")
	o.csrs = append(o.csrs, csr)
	return o.execErr
}
func (o *fakeOrder) Status(context.Context) (CAStatus, error) { return o.status, nil }
func (o *fakeOrder) Authorizations(context.Context) ([]CAAuthorization, error) {
	out := make([]CAAuthorization, len(o.authz))
	for i, a := range o.authz {
		out[i] = a
	}
	return out, nil
}
func (o *fakeOrder) Certificate(context.Context) (CACertificate, error) {
	if o.chain == nil {
		return nil, nil
	}
	return &fakeCert{o: o}, nil
}

type fakeCert struct{ o *fakeOrder }

func (c *fakeCert) Chain(context.Context) ([][]byte, error) {
	c.o.downloads++
	c.o.ca.rec("certificate.download")
	return c.o.chain, nil
}

type fakeAuthz struct {
	ca        *fakeCA
	domain    string
	challenge *fakeChallenge // nil: no HTTP-01 challenge
	updateErr error
}

func (a *fakeAuthz) Domain(context.Context) (string, error) { return a.domain, nil }
func (a *fakeAuthz) FindHTTP01(context.Context) (CAChallenge, error) {
	if a.challenge == nil {
		return nil, nil
	}
	cp := *a.challenge // the challenge keeps the data it was created from
	return &cp, nil
}
func (a *fakeAuthz) Update(context.Context) error {
	a.ca.rec("authz.update:" + a.domain)
	return a.updateErr
}

type fakeChallenge struct {
	ca         *fakeCA
	token      string
	status     CAStatus
	updates    []fakeChallengeUpdate // successive Update calls
	updateN    int
	triggered  *int
	triggerErr error
}

type fakeChallengeUpdate struct {
	err    error
	status CAStatus
}

func (c *fakeChallenge) Token() string                     { return c.token }
func (c *fakeChallenge) Location() string                  { return "https://acme.test/chall/" + c.token }
func (c *fakeChallenge) Status() CAStatus                  { return c.status }
func (c *fakeChallenge) KeyAuthorization() (string, error) { return c.token + ".THUMB", nil }
func (c *fakeChallenge) Trigger(context.Context) error {
	c.ca.rec("challenge.trigger:" + c.token)
	if c.triggered != nil {
		*c.triggered++
	}
	return c.triggerErr
}
func (c *fakeChallenge) Update(context.Context) error {
	c.ca.rec("challenge.update:" + c.token)
	var u fakeChallengeUpdate
	if c.updateN < len(c.updates) {
		u = c.updates[c.updateN]
	} else if len(c.updates) > 0 {
		u = c.updates[len(c.updates)-1]
	} else {
		u = fakeChallengeUpdate{status: c.status}
	}
	c.updateN++
	c.status = u.status
	return u.err
}

// --- fixtures ----------------------------------------------------------------------------

var testNow = time.Date(2026, 6, 25, 3, 0, 0, 0, time.Local)

type renewFixture struct {
	t      *testing.T
	cfg    *Config
	repos  *fakeRepos
	ca     *fakeCA
	uc     *RenewalUseCase
	calls  []string
	sleeps int
}

func newFixture(t *testing.T) *renewFixture {
	f := &renewFixture{t: t, repos: newFakeRepos()}
	f.cfg = &Config{
		Enabled: true, Domain: "agora.gouv.fr", ServerURL: "https://acme.sectigo.com/v2/DV",
		ServerInteractionEnabled: true, CloudflareInteractionEnabled: true,
	}
	f.ca = &fakeCA{log: &f.calls}
	f.uc = &RenewalUseCase{
		Cfg: f.cfg, Certificates: f.repos, Accounts: f.repos, Challenges: f.repos, Orders: f.repos, Deployer: f.repos,
		CA: f.ca, Now: func() time.Time { return testNow },
		Sleep:      func(context.Context, time.Duration) { f.sleeps++ },
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewKeyPair: func() (crypto.Signer, error) { return testKeyCached(t), nil },
	}
	return f
}

var (
	cachedKeyOnce sync.Once
	cachedKey     *rsa.PrivateKey
)

func testKeyCached(t *testing.T) crypto.Signer {
	cachedKeyOnce.Do(func() { cachedKey = testKey(t) })
	return cachedKey
}

func (f *renewFixture) keyPEM() string {
	p, err := WriteKeyPair(testKeyCached(f.t))
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *renewFixture) addAccount(url *string) {
	f.repos.accounts = append(f.repos.accounts, Account{ServerURL: f.cfg.ServerURL, AccountURL: url, KeyPEM: f.keyPEM()})
}

func (f *renewFixture) addOrder(status OrderStatus, age time.Duration) {
	f.repos.orders = append(f.repos.orders, Order{
		Domain: f.cfg.Domain, OrderURL: "https://acme.test/order/12345", DomainKeyPEM: f.keyPEM(), Status: status, CreatedAt: testNow.Add(-age),
	})
}

func (f *renewFixture) addCert(status CertificateStatus, expiresIn time.Duration) {
	f.repos.certs = append(f.repos.certs, Certificate{
		Domain: f.cfg.Domain, CertificatePEM: "cert-pem", PrivateKeyPEM: "key-pem", ExpiresAt: testNow.Add(expiresIn), Status: status,
	})
}

func testCertDER(t *testing.T, cn string, notAfter time.Time) []byte {
	t.Helper()
	k := testKeyCached(t).(*rsa.PrivateKey)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: cn}, NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// order wires a scripted order with one HTTP-01 challenge per domain.
func (f *renewFixture) order(domains ...string) *fakeOrder {
	o := &fakeOrder{ca: f.ca, location: "https://acme.test/order/12345", status: CAStatusPending}
	for i, d := range domains {
		ch := &fakeChallenge{ca: f.ca, token: fmt.Sprintf("tok%d", i+1), status: CAStatusPending}
		o.authz = append(o.authz, &fakeAuthz{ca: f.ca, domain: d, challenge: ch})
	}
	f.ca.order = o
	return o
}

// validPolling scripts every challenge VALID at the first poll and the order VALID after the CSR.
func (f *renewFixture) validPolling(o *fakeOrder) {
	for _, a := range o.authz {
		a.challenge.updates = []fakeChallengeUpdate{{status: CAStatusValid}}
	}
	o.statuses = []CAStatus{CAStatusPending /* pre-CSR reload */, CAStatusValid}
	o.chain = [][]byte{testCertDER(f.t, f.cfg.Domain, testNow.Add(90*24*time.Hour).Truncate(time.Second)), testCertDER(f.t, "intermediate", testNow.Add(365*24*time.Hour).Truncate(time.Second))}
}

func (f *renewFixture) run() error { return f.uc.RenewIfNeeded(context.Background()) }

func sorted(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

var errBoom = errors.New("boom")
