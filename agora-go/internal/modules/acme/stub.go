package acme

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/jsonjava"
	"agora/internal/modules/users"
)

// The local stubs (infrastructure/acme/stub/**): a simulated ACME server
// (RFC 8555, always valid), a simulated Cloudflare API and the inspection
// endpoints. They only exist when ACME_STUB_MODE=true (routes.go).

// stubState is AcmeStubStore + AcmeStubOrderStore + the certificate cache of
// AcmeStubController.
type stubState struct {
	mu               sync.Mutex
	calls            []StubCall
	lastCertPreview  *string
	lastDeployedAt   []int // LocalDateTime written as a Jackson array
	orders           map[string]*stubOrder
	authzs           map[string]*stubAuthz
	challengeToOrder map[string]string
	certs            map[string]string // certId → PEM
}

type stubOrder struct {
	id, domain, authzID, challengeToken string
	status                              string
	certID                              string
}

type stubAuthz struct {
	id, orderID, challengeToken string
	status                      string
}

// StubCall is AcmeStubStore.StubCall.
type StubCall struct {
	Timestamp []int  `json:"timestamp" xml:"timestamp,unwrapped"`
	Method    string `json:"method"`
	Endpoint  string `json:"endpoint"`
	Summary   string `json:"summary"`
}

// StubCalls is the List<StubCall> of GET /admin/stub/acme/calls.
type StubCalls []StubCall

// JavaName is the declared type (ResponseEntity<List<StubCall>>).
func (StubCalls) JavaName() string { return "List" }

// localDateTimeArray is Jackson's LocalDateTime array form (WRITE_DATES_AS_TIMESTAMPS,
// nanoseconds): seconds and nanoseconds are only written when not zero.
func localDateTimeArray(t time.Time) []int {
	out := []int{t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute()}
	if t.Second() > 0 || t.Nanosecond() > 0 {
		out = append(out, t.Second())
		if t.Nanosecond() > 0 {
			out = append(out, t.Nanosecond())
		}
	}
	return out
}

func (s *Service) stubRecord(method, endpoint, summary string) {
	st := s.stub
	st.mu.Lock()
	defer st.mu.Unlock()
	st.calls = append(st.calls, StubCall{Timestamp: localDateTimeArray(s.a.Now()), Method: method, Endpoint: endpoint, Summary: summary})
}

// --- response bodies (declared Map<String, Any> → XML root "Map") ------------

type stubMap struct{}

func (stubMap) JavaName() string { return "Map" }

// EncryptedKeyPairJSON is mapOf("encryptedKeyPair" to ...).
type EncryptedKeyPairJSON struct {
	stubMap
	EncryptedKeyPair string `json:"encryptedKeyPair"`
}

type stubDirectoryMeta struct {
	TermsOfService          string   `json:"termsOfService"`
	Website                 string   `json:"website"`
	CaaIdentities           []string `json:"caaIdentities" xml:"caaIdentities,unwrapped"`
	ExternalAccountRequired bool     `json:"externalAccountRequired"`
}

type stubDirectory struct {
	stubMap
	NewNonce   string            `json:"newNonce"`
	NewAccount string            `json:"newAccount"`
	NewOrder   string            `json:"newOrder"`
	RevokeCert string            `json:"revokeCert"`
	KeyChange  string            `json:"keyChange"`
	Meta       stubDirectoryMeta `json:"meta"`
}

type stubAccount struct {
	stubMap
	Status  string   `json:"status"`
	Contact []string `json:"contact" xml:"contact,unwrapped"`
	Orders  string   `json:"orders"`
}

type stubIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type stubNewOrder struct {
	stubMap
	Status         string           `json:"status"`
	Identifiers    []stubIdentifier `json:"identifiers" xml:"identifiers,unwrapped"`
	Authorizations []string         `json:"authorizations" xml:"authorizations,unwrapped"`
	Finalize       string           `json:"finalize"`
}

type stubOrderBody struct {
	stubMap
	Status         string           `json:"status"`
	Identifiers    []stubIdentifier `json:"identifiers" xml:"identifiers,unwrapped"`
	Authorizations []string         `json:"authorizations" xml:"authorizations,unwrapped"`
	Finalize       string           `json:"finalize"`
	Certificate    *string          `json:"certificate" xml:"certificate" omitnull:"true"`
}

type stubUnknownOrder struct {
	stubMap
	Status string `json:"status"`
}

type stubFinalized struct {
	stubMap
	Status      string `json:"status"`
	Certificate string `json:"certificate"`
}

type stubChallengeInAuthz struct {
	Type      string  `json:"type"`
	URL       string  `json:"url"`
	Token     string  `json:"token"`
	Status    string  `json:"status"`
	Validated *string `json:"validated,omitnull"`
}

type stubAuthzBody struct {
	stubMap
	Status     string                 `json:"status"`
	Identifier stubIdentifier         `json:"identifier"`
	Challenges []stubChallengeInAuthz `json:"challenges" xml:"challenges,unwrapped"`
}

type stubChallengeBody struct {
	stubMap
	Type      string `json:"type"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	Token     string `json:"token"`
	Validated string `json:"validated"`
}

type stubZonePlan struct {
	Name string `json:"name"`
}

type stubZoneResult struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status string       `json:"status"`
	Plan   stubZonePlan `json:"plan"`
}

type stubZone struct {
	stubMap
	Success bool           `json:"success"`
	Errors  []string       `json:"errors" xml:"errors,unwrapped"`
	Result  stubZoneResult `json:"result"`
}

type stubDeployResult struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	BundleMethod any    `json:"bundle_method"`
}

type stubDeploy struct {
	stubMap
	Success bool             `json:"success"`
	Errors  []string         `json:"errors" xml:"errors,unwrapped"`
	Result  stubDeployResult `json:"result"`
}

// LastDeployJSON is mapOf("lastDeployedCertPreview" to ..., "lastDeployedAt" to ...).
type LastDeployJSON struct {
	stubMap
	LastDeployedCertPreview *string `json:"lastDeployedCertPreview"`
	LastDeployedAt          *[]int  `json:"lastDeployedAt" xml:"lastDeployedAt,unwrapped"`
}

// --- helpers --------------------------------------------------------------------

func (s *Service) stubBase() string { return fmt.Sprintf("http://localhost:%d/stub/acme", s.Cfg.Port) }

// stubDomain is acmeConfig.domain.ifBlank { "stub.local" }.
func (s *Service) stubDomain() string {
	if javacompat.KotlinIsBlank(s.Cfg.Domain) {
		return "stub.local"
	}
	return s.Cfg.Domain
}

func randomHex8() string { return users.RandomUUID()[:8] }

// newNonce is AcmeStubNonceStore.generateNonce.
func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// stubBody checks `consumes = ["application/jose+json", "application/json"]`
// (415 otherwise) and reads the required `@RequestBody body: String`
// (400 when it is empty); the content is ignored.
func stubBody(c *httpx.Ctx) {
	ct, _ := c.Header("Content-Type")
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if ct == "" || (mt != "application/jose+json" && mt != "application/json") {
		panic(&httpx.SpringError{Status: 415, Cause: "unsupported content type " + ct,
			Headers: []httpx.Header{{Name: "Accept", Value: "application/jose+json, application/json"}}})
	}
	b, err := io.ReadAll(io.LimitReader(c.R.Body, 16<<20))
	if err != nil || len(b) == 0 {
		panic(&httpx.SpringError{Status: 400, Cause: "Required request body is missing"})
	}
}

func replayNonce(r *httpx.Response) *httpx.Response { return r.With("Replay-Nonce", newNonce()) }

// --- ACME stub ----------------------------------------------------------------------

func (s *Service) registerStubRoutes() {
	a := s.a.Server
	a.POST("/stub/acme/generate-keypair", s.stubGenerateKeypair)
	a.GET("/stub/acme/directory", s.stubDirectory)
	a.Handle("HEAD", "/stub/acme/new-nonce", s.stubNewNonce)
	a.GET("/stub/acme/new-nonce", s.stubNewNonce)
	a.POST("/stub/acme/account/{accountId}", s.stubGetAccount)
	a.POST("/stub/acme/new-account", s.stubNewAccount)
	a.POST("/stub/acme/new-order", s.stubNewOrder)
	a.POST("/stub/acme/order/{orderId}", s.stubGetOrder)
	a.POST("/stub/acme/order/{orderId}/finalize", s.stubFinalize)
	a.POST("/stub/acme/authz/{authzId}", s.stubGetAuthz)
	a.POST("/stub/acme/challenge/{token}", s.stubChallenge)
	a.POST("/stub/acme/certificate/{certId}", s.stubGetCertificate)
	a.GET("/stub/acme/terms", func(c *httpx.Ctx) *httpx.Response {
		return httpx.String(200, "Stub ACME Terms of Service — local test only")
	})
	a.GET("/stub/cloudflare/zones/{zoneId}", s.stubCloudflareZone)
	a.POST("/stub/cloudflare/zones/{zoneId}/custom_certificates", s.stubCloudflareDeploy)
	a.GET("/admin/stub/acme/calls", s.stubCalls)
	a.DELETE("/admin/stub/acme/calls/reset", s.stubReset)
	a.GET("/admin/stub/acme/last-deploy", s.stubLastDeploy)
}

func (s *Service) stubGenerateKeypair(c *httpx.Ctx) *httpx.Response {
	key, err := CreateKeyPair(2048)
	if err != nil {
		panic(err)
	}
	pem, err := WriteKeyPair(key)
	if err != nil {
		panic(err)
	}
	enc, err := s.Crypto.Encrypt(pem)
	if err != nil {
		panic(err)
	}
	s.a.Log.Info("[STUB] generate-keypair → keypair générée et chiffrée")
	return httpx.OK(EncryptedKeyPairJSON{EncryptedKeyPair: enc})
}

func (s *Service) stubDirectory(c *httpx.Ctx) *httpx.Response {
	base := s.stubBase()
	s.stubRecord("GET", "/stub/acme/directory", "directory")
	return httpx.OK(stubDirectory{
		NewNonce: base + "/new-nonce", NewAccount: base + "/new-account", NewOrder: base + "/new-order",
		RevokeCert: base + "/revoke-cert", KeyChange: base + "/key-change",
		Meta: stubDirectoryMeta{TermsOfService: base + "/terms", Website: "http://localhost:8080",
			CaaIdentities: []string{"localhost"}, ExternalAccountRequired: false},
	})
}

func (s *Service) stubNewNonce(c *httpx.Ctx) *httpx.Response {
	return httpx.Empty(204).With("Replay-Nonce", newNonce()).With("Cache-Control", "no-store")
}

func (s *Service) stubGetAccount(c *httpx.Ctx) *httpx.Response {
	accountID := c.PathVar("accountId")
	stubBody(c)
	return replayNonce(httpx.OK(stubAccount{Status: "valid", Orders: s.stubBase() + "/account/" + accountID + "/orders"}))
}

func (s *Service) stubNewAccount(c *httpx.Ctx) *httpx.Response {
	stubBody(c)
	const accountID = "stub-account-001"
	base := s.stubBase()
	s.stubRecord("POST", "/stub/acme/new-account", "newAccount id="+accountID)
	return replayNonce(httpx.JSON(201, stubAccount{Status: "valid", Orders: base + "/account/" + accountID + "/orders"})).
		With("Location", base+"/account/"+accountID)
}

func (s *Service) stubNewOrder(c *httpx.Ctx) *httpx.Response {
	stubBody(c)
	domain := s.stubDomain()
	orderID := randomHex8()
	authzID := randomHex8()
	token := strings.ReplaceAll(users.RandomUUID(), "-", "")[:22]
	base := s.stubBase()
	st := s.stub
	st.mu.Lock()
	st.orders[orderID] = &stubOrder{id: orderID, domain: domain, authzID: authzID, challengeToken: token, status: "pending"}
	st.authzs[authzID] = &stubAuthz{id: authzID, orderID: orderID, challengeToken: token, status: "pending"}
	st.challengeToOrder[token] = orderID
	st.mu.Unlock()
	s.a.Log.Info(fmt.Sprintf("[STUB] ACME POST /new-order → orderId=%s authzId=%s token=%s", orderID, authzID, token))
	s.stubRecord("POST", "/stub/acme/new-order", "newOrder orderId="+orderID+" domain="+domain)
	return replayNonce(httpx.JSON(201, stubNewOrder{
		Status:         "pending",
		Identifiers:    []stubIdentifier{{Type: "dns", Value: domain}},
		Authorizations: []string{base + "/authz/" + authzID},
		Finalize:       base + "/order/" + orderID + "/finalize",
	})).With("Location", base+"/order/"+orderID)
}

func (s *Service) stubGetOrder(c *httpx.Ctx) *httpx.Response {
	orderID := c.PathVar("orderId")
	stubBody(c)
	base := s.stubBase()
	st := s.stub
	st.mu.Lock()
	var snapshot *stubOrder
	if o := st.orders[orderID]; o != nil {
		cp := *o
		snapshot = &cp
	}
	st.mu.Unlock()
	status := "null"
	if snapshot != nil {
		status = snapshot.status
	}
	s.a.Log.Info(fmt.Sprintf("[STUB] ACME POST /order/%s → status=%s", orderID, status))
	s.stubRecord("POST", "/stub/acme/order/"+orderID, "getOrder orderId="+orderID+" status="+status)
	if snapshot == nil {
		return replayNonce(httpx.OK(stubUnknownOrder{Status: "invalid"}))
	}
	body := stubOrderBody{
		Status:         snapshot.status,
		Identifiers:    []stubIdentifier{{Type: "dns", Value: snapshot.domain}},
		Authorizations: []string{base + "/authz/" + snapshot.authzID},
		Finalize:       base + "/order/" + snapshot.id + "/finalize",
	}
	if snapshot.certID != "" {
		u := base + "/certificate/" + snapshot.certID
		body.Certificate = &u
	}
	return replayNonce(httpx.OK(body))
}

func (s *Service) stubFinalize(c *httpx.Ctx) *httpx.Response {
	orderID := c.PathVar("orderId")
	stubBody(c)
	base := s.stubBase()
	st := s.stub
	st.mu.Lock()
	o := st.orders[orderID]
	domain := s.Cfg.AllDomains()[0]
	if o != nil {
		domain = o.domain
	}
	st.mu.Unlock()
	s.a.Log.Info("[STUB] ACME POST /order/" + orderID + "/finalize")
	s.stubRecord("POST", "/stub/acme/order/"+orderID+"/finalize", "finalizeOrder orderId="+orderID)

	certID := randomHex8()
	certPEM, err := generateStubCertificate(domain, s.a.Now())
	if err != nil {
		panic(err)
	}
	st.mu.Lock()
	st.certs[certID] = certPEM
	if o != nil {
		o.status = "valid"
		o.certID = certID
	}
	st.mu.Unlock()
	return replayNonce(httpx.OK(stubFinalized{Status: "valid", Certificate: base + "/certificate/" + certID}))
}

func (s *Service) stubGetAuthz(c *httpx.Ctx) *httpx.Response {
	authzID := c.PathVar("authzId")
	stubBody(c)
	base := s.stubBase()
	st := s.stub
	st.mu.Lock()
	token, status := "stub-token-fallback", "valid"
	if z := st.authzs[authzID]; z != nil {
		token, status = z.challengeToken, z.status
	}
	st.mu.Unlock()
	domain := s.stubDomain()
	s.a.Log.Info(fmt.Sprintf("[STUB] ACME POST /authz/%s → status=%s token=%s", authzID, status, token))
	s.stubRecord("POST", "/stub/acme/authz/"+authzID, "getAuthz authzId="+authzID+" status="+status)
	challengeStatus := "pending"
	var validated *string
	if status == "valid" {
		challengeStatus = "valid"
		v := "2026-01-01T00:00:00Z"
		validated = &v
	}
	return replayNonce(httpx.OK(stubAuthzBody{
		Status:     status,
		Identifier: stubIdentifier{Type: "dns", Value: domain},
		Challenges: []stubChallengeInAuthz{{Type: "http-01", URL: base + "/challenge/" + token, Token: token, Status: challengeStatus, Validated: validated}},
	}))
}

func (s *Service) stubChallenge(c *httpx.Ctx) *httpx.Response {
	token := c.PathVar("token")
	stubBody(c)
	s.a.Log.Info("[STUB] ACME POST /challenge/" + token + " → VALID (stub toujours valide)")
	s.stubRecord("POST", "/stub/acme/challenge/"+token, "challenge token="+token+" status=valid")
	st := s.stub
	st.mu.Lock()
	if orderID, ok := st.challengeToOrder[token]; ok {
		if o := st.orders[orderID]; o != nil {
			if z := st.authzs[o.authzID]; z != nil {
				z.status = "valid"
			}
		}
	}
	st.mu.Unlock()
	base := s.stubBase()
	return replayNonce(httpx.OK(stubChallengeBody{Type: "http-01", URL: base + "/challenge/" + token, Status: "valid", Token: token, Validated: "2026-01-01T00:00:00Z"}))
}

func (s *Service) stubGetCertificate(c *httpx.Ctx) *httpx.Response {
	certID := c.PathVar("certId")
	stubBody(c)
	st := s.stub
	st.mu.Lock()
	pem, found := st.certs[certID]
	st.mu.Unlock()
	state := "not found"
	if found {
		state = "found"
	}
	s.a.Log.Info("[STUB] ACME POST /certificate/" + certID + " → " + state)
	s.stubRecord("POST", "/stub/acme/certificate/"+certID, "downloadCertificate certId="+certID)
	if !found {
		return httpx.Empty(404)
	}
	return httpx.Bytes(200, "application/pem-certificate-chain", []byte(pem)).With("Replay-Nonce", newNonce())
}

// generateStubCertificate is AcmeStubCertificateGenerator.generate: a 90 days
// self-signed RSA-2048 / SHA256withRSA certificate with subject CN=<domain>,
// as a one block PEM (MIME base64, 64 columns).
func generateStubCertificate(domain string, now time.Time) (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	tpl := &x509.Certificate{
		SerialNumber:       big.NewInt(time.Now().UnixMilli()),
		Subject:            pkix.Name{CommonName: domain},
		NotBefore:          now.Truncate(time.Millisecond),
		NotAfter:           now.Add(90 * 24 * time.Hour).Truncate(time.Millisecond),
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	return CertificateChainPEM([][]byte{der}), nil
}

// --- Cloudflare stub ---------------------------------------------------------------------

func (s *Service) stubCloudflareZone(c *httpx.Ctx) *httpx.Response {
	zoneID := c.PathVar("zoneId")
	s.a.Log.Info("[STUB] Cloudflare GET /zones/" + zoneID)
	s.stubRecord("GET", "/stub/cloudflare/zones/"+zoneID, "getZone zoneId="+zoneID)
	return httpx.OK(stubZone{Success: true, Result: stubZoneResult{ID: zoneID, Name: s.stubDomain(), Status: "active", Plan: stubZonePlan{Name: "stub-enterprise"}}})
}

func (s *Service) stubCloudflareDeploy(c *httpx.Ctx) *httpx.Response {
	zoneID := c.PathVar("zoneId")
	var body map[string]any
	c.BindBody(&body)
	certPem, _ := body["certificate"].(string)
	preview := "N/A"
	for _, line := range strings.Split(strings.ReplaceAll(certPem, "\r\n", "\n"), "\n") {
		if !javacompat.KotlinIsBlank(line) && !strings.HasPrefix(line, "-----") {
			preview = javacompat.Take16(line, 40)
			break
		}
	}
	s.a.Log.Info("[STUB] Cloudflare POST /zones/" + zoneID + "/custom_certificates (cert preview: " + preview + "...)")
	s.stubRecord("POST", "/stub/cloudflare/zones/"+zoneID+"/custom_certificates",
		"deployCertificate zoneId="+zoneID+" certPreview="+preview)
	st := s.stub
	st.mu.Lock()
	st.lastCertPreview = &preview
	st.lastDeployedAt = localDateTimeArray(s.a.Now())
	st.mu.Unlock()
	var bundle any = "ubiquitous"
	if v, ok := body["bundle_method"]; ok && v != nil {
		bundle = javaValue(v)
	}
	return httpx.OK(stubDeploy{Success: true, Result: stubDeployResult{
		ID: fmt.Sprintf("stub-cert-deploy-%d", time.Now().UnixMilli()), Status: "active", BundleMethod: bundle}})
}

// javaValue converts a decoded JSON value to what Jackson would write back
// after reading it into a Kotlin Any (Integer/Long/BigInteger/Double, Map, List).
func javaValue(v any) any {
	switch t := v.(type) {
	case json.Number:
		return jsonjava.Raw(javaNumberJSON(t.String()))
	case map[string]any:
		m := make(jsonjava.OrderedMap, 0, len(t))
		// object member order of the request is not tracked by the tree
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m = append(m, jsonjava.Entry{Key: k, Value: javaValue(t[k])})
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = javaValue(e)
		}
		return out
	}
	return v
}

// javaNumberJSON writes a JSON number as Jackson writes the Kotlin value read
// from it: Int / Long / BigInteger keep their digits, a decimal or exponent
// literal becomes a Double formatted like Double.toString.
func javaNumberJSON(lit string) []byte {
	if i, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return strconv.AppendInt(nil, i, 10)
	}
	if !strings.ContainsAny(lit, ".eE") {
		return []byte(lit)
	}
	f, _ := strconv.ParseFloat(lit, 64)
	return jsonjava.AppendJavaDouble(nil, f)
}

// --- state endpoints ----------------------------------------------------------------------

func (s *Service) stubCalls(c *httpx.Ctx) *httpx.Response {
	st := s.stub
	st.mu.Lock()
	out := make(StubCalls, len(st.calls))
	copy(out, st.calls)
	st.mu.Unlock()
	return httpx.OK(out)
}

func (s *Service) stubReset(c *httpx.Ctx) *httpx.Response {
	st := s.stub
	st.mu.Lock()
	st.calls = nil
	st.lastCertPreview = nil
	st.lastDeployedAt = nil
	st.mu.Unlock()
	return httpx.String(200, "Historique des bouchons réinitialisé")
}

func (s *Service) stubLastDeploy(c *httpx.Ctx) *httpx.Response {
	st := s.stub
	st.mu.Lock()
	defer st.mu.Unlock()
	out := LastDeployJSON{LastDeployedCertPreview: st.lastCertPreview}
	if st.lastDeployedAt != nil {
		at := append([]int(nil), st.lastDeployedAt...)
		out.LastDeployedAt = &at
	}
	return httpx.OK(out)
}
