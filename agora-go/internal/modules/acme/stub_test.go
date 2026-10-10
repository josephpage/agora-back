package acme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/config"
	"agora/internal/httpx"
)

// In-process tests of the ACME / Cloudflare stubs (AcmeStubControllerTest,
// AcmeStubStateControllerTest, CloudflareStubControllerTest, AcmeStubCertificateGeneratorTest):
// the real httpx pipeline serves the stub routes, the Kotlin mocks of the stores are replaced
// by the stub state of the service.

type stubHarness struct {
	t   *testing.T
	svc *Service
	srv *httptest.Server
}

func newStubHarness(t *testing.T, domain string) *stubHarness {
	t.Helper()
	log := discardLog()
	a := &app.App{Cfg: &config.Config{}, Log: log, Clock: func() time.Time { return testNow }}
	a.Server = httpx.NewServer(httpx.Options{Now: a.Clock, Logger: log, Rules: []httpx.Rule{{Patterns: []string{"/**"}, PermitAll: true}}})
	cfg := &Config{Enabled: true, Domain: domain, Port: 8080, EncryptionKey: keyBytes(32)}
	svc := &Service{
		Cfg: cfg, Crypto: &CryptoHelper{cfg: cfg}, a: a,
		stub: &stubState{orders: map[string]*stubOrder{}, authzs: map[string]*stubAuthz{}, challengeToOrder: map[string]string{}, certs: map[string]string{}},
	}
	svc.registerStubRoutes()
	h := &stubHarness{t: t, svc: svc, srv: httptest.NewServer(a.Server)}
	t.Cleanup(h.srv.Close)
	return h
}

func (h *stubHarness) do(method, path, contentType string, body string) (int, http.Header, []byte) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

func (h *stubHarness) jose(path string) (int, http.Header, map[string]any) {
	h.t.Helper()
	st, hdr, b := h.do("POST", path, "application/jose+json", "{}")
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return st, hdr, m
}

func (h *stubHarness) calls() []StubCall {
	h.t.Helper()
	h.svc.stub.mu.Lock()
	defer h.svc.stub.mu.Unlock()
	return append([]StubCall(nil), h.svc.stub.calls...)
}

func (h *stubHarness) lastCall() StubCall {
	h.t.Helper()
	c := h.calls()
	if len(c) == 0 {
		h.t.Fatal("no call recorded")
	}
	return c[len(c)-1]
}

func assertNonce(t *testing.T, hdr http.Header) {
	t.Helper()
	n := hdr.Get("Replay-Nonce")
	if len(n) != 22 || strings.Trim(n, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
		t.Errorf("Replay-Nonce %q (16 random bytes, URL-safe Base64 without padding)", n)
	}
}

// --- directory -------------------------------------------------------------------------

func TestStubDirectory(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, _, b := h.do("GET", "/stub/acme/directory", "", "")
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	var m map[string]any
	mustNoErr(t, json.Unmarshal(b, &m))
	for _, k := range []string{"newNonce", "newAccount", "newOrder", "revokeCert", "keyChange"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	for k, want := range map[string]string{"newNonce": "new-nonce", "newAccount": "new-account", "newOrder": "new-order"} {
		if m[k] != "http://localhost:8080/stub/acme/"+want {
			t.Errorf("%s = %v", k, m[k])
		}
	}
	meta := m["meta"].(map[string]any)
	if meta["termsOfService"] != "http://localhost:8080/stub/acme/terms" || meta["externalAccountRequired"] != false {
		t.Errorf("meta %v", meta)
	}
	if c := h.lastCall(); c.Method != "GET" || c.Endpoint != "/stub/acme/directory" || c.Summary != "directory" {
		t.Errorf("recorded %+v", c)
	}
}

func TestStubDirectoryUsesTheConfiguredPort(t *testing.T) {
	h := newStubHarness(t, "test.local")
	h.svc.Cfg.Port = 9191
	_, _, b := h.do("GET", "/stub/acme/directory", "", "")
	if !strings.Contains(string(b), `"newNonce":"http://localhost:9191/stub/acme/new-nonce"`) {
		t.Errorf("body %s", b)
	}
}

func TestStubNonceAndTerms(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, hdr, b := h.do("HEAD", "/stub/acme/new-nonce", "", "")
	if st != 204 || len(b) != 0 || hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("HEAD: %d %v %q", st, hdr, b)
	}
	assertNonce(t, hdr)
	st, hdr, _ = h.do("GET", "/stub/acme/new-nonce", "", "")
	if st != 204 {
		t.Errorf("GET: %d", st)
	}
	assertNonce(t, hdr)
	if st, _, b = h.do("GET", "/stub/acme/terms", "", ""); st != 200 || !strings.Contains(string(b), "Stub ACME Terms of Service") {
		t.Errorf("terms %d %s", st, b)
	}
	if len(h.calls()) != 0 {
		t.Errorf("nonce / terms are not recorded: %v", h.calls())
	}
}

// --- accounts ---------------------------------------------------------------------------

func TestStubGetAccount(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, hdr, m := h.jose("/stub/acme/account/stub-account-001")
	if st != 200 || m["status"] != "valid" || !strings.Contains(m["orders"].(string), "stub-account-001") {
		t.Errorf("%d %v", st, m)
	}
	assertNonce(t, hdr)
}

func TestStubNewAccount(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, hdr, m := h.jose("/stub/acme/new-account")
	if st != 201 || m["status"] != "valid" {
		t.Errorf("%d %v", st, m)
	}
	if loc := hdr.Get("Location"); loc != "http://localhost:8080/stub/acme/account/stub-account-001" {
		t.Errorf("Location %q", loc)
	}
	assertNonce(t, hdr)
	if c := h.lastCall(); c.Method != "POST" || c.Endpoint != "/stub/acme/new-account" || c.Summary != "newAccount id=stub-account-001" {
		t.Errorf("recorded %+v", c)
	}
}

func TestStubBodyRules(t *testing.T) {
	h := newStubHarness(t, "test.local")
	// consumes = application/jose+json, application/json
	for _, ct := range []string{"application/jose+json", "application/json", "application/json; charset=utf-8", "APPLICATION/JSON"} {
		if st, _, _ := h.do("POST", "/stub/acme/new-account", ct, "{}"); st != 201 {
			t.Errorf("%q: %d", ct, st)
		}
	}
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/xml"} {
		st, hdr, _ := h.do("POST", "/stub/acme/new-account", ct, "{}")
		if st != 415 || hdr.Get("Accept") != "application/jose+json, application/json" {
			t.Errorf("%q: %d accept=%q", ct, st, hdr.Get("Accept"))
		}
	}
	// the body is a required String
	if st, _, _ := h.do("POST", "/stub/acme/new-account", "application/jose+json", ""); st != 400 {
		t.Errorf("empty body: %d", st)
	}
	// but its content is never looked at
	if st, _, _ := h.do("POST", "/stub/acme/new-account", "application/jose+json", "not json at all"); st != 201 {
		t.Errorf("garbage body: %d", st)
	}
	if st, _, _ := h.do("GET", "/stub/acme/new-account", "", ""); st != 405 {
		t.Errorf("GET: %d", st)
	}
}

// --- orders ---------------------------------------------------------------------------------

func TestStubNewOrder(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, hdr, m := h.jose("/stub/acme/new-order")
	if st != 201 || m["status"] != "pending" {
		t.Fatalf("%d %v", st, m)
	}
	if loc := hdr.Get("Location"); !strings.HasPrefix(loc, "http://localhost:8080/stub/acme/order/") || len(loc) != len("http://localhost:8080/stub/acme/order/")+8 {
		t.Errorf("Location %q", loc)
	}
	assertNonce(t, hdr)
	authz := m["authorizations"].([]any)
	if len(authz) != 1 || !strings.HasPrefix(authz[0].(string), "http://localhost:8080/stub/acme/authz/") {
		t.Errorf("authorizations %v", authz)
	}
	ids := m["identifiers"].([]any)
	if id := ids[0].(map[string]any); id["type"] != "dns" || id["value"] != "test.local" {
		t.Errorf("identifiers %v", ids)
	}
	orderID := strings.TrimPrefix(hdr.Get("Location"), "http://localhost:8080/stub/acme/order/")
	if m["finalize"] != "http://localhost:8080/stub/acme/order/"+orderID+"/finalize" {
		t.Errorf("finalize %v", m["finalize"])
	}
	if c := h.lastCall(); c.Endpoint != "/stub/acme/new-order" || c.Summary != "newOrder orderId="+orderID+" domain=test.local" {
		t.Errorf("recorded %+v", c)
	}
}

func TestStubNewOrderWithABlankDomainUsesStubLocal(t *testing.T) {
	for _, domain := range []string{"", "   "} {
		h := newStubHarness(t, domain)
		_, _, m := h.jose("/stub/acme/new-order")
		if v := m["identifiers"].([]any)[0].(map[string]any)["value"]; v != "stub.local" {
			t.Errorf("domain %q: identifier %v", domain, v)
		}
	}
}

func TestStubOrderLifecycle(t *testing.T) {
	h := newStubHarness(t, "test.local")
	_, hdr, order := h.jose("/stub/acme/new-order")
	orderPath := strings.TrimPrefix(hdr.Get("Location"), "http://localhost:8080")
	authzPath := strings.TrimPrefix(order["authorizations"].([]any)[0].(string), "http://localhost:8080")

	_, _, m := h.jose(authzPath)
	ch := m["challenges"].([]any)[0].(map[string]any)
	token := ch["token"].(string)
	if m["status"] != "pending" || ch["status"] != "pending" || ch["type"] != "http-01" || len(token) != 22 ||
		ch["url"] != "http://localhost:8080/stub/acme/challenge/"+token {
		t.Errorf("pending authz %v", m)
	}
	if _, has := ch["validated"]; has {
		t.Errorf("a pending challenge has no validated date: %v", ch)
	}
	if _, _, o := h.jose(orderPath); o["status"] != "pending" || o["certificate"] != nil {
		t.Errorf("pending order %v", o)
	}

	// the challenge is accepted: the authorization becomes valid
	st, _, c := h.jose("/stub/acme/challenge/" + token)
	if st != 200 || c["status"] != "valid" || c["token"] != token || c["type"] != "http-01" || c["validated"] != "2026-01-01T00:00:00Z" {
		t.Errorf("challenge %d %v", st, c)
	}
	_, _, m = h.jose(authzPath)
	if m["status"] != "valid" || m["challenges"].([]any)[0].(map[string]any)["validated"] != "2026-01-01T00:00:00Z" {
		t.Errorf("valid authz %v", m)
	}

	// finalize: the order is valid and carries the certificate URL
	st, hdr, f := h.jose(orderPath + "/finalize")
	certURL, _ := f["certificate"].(string)
	if st != 200 || f["status"] != "valid" || !strings.HasPrefix(certURL, "http://localhost:8080/stub/acme/certificate/") {
		t.Fatalf("finalize %d %v", st, f)
	}
	assertNonce(t, hdr)
	if _, _, o := h.jose(orderPath); o["status"] != "valid" || o["certificate"] != certURL {
		t.Errorf("valid order %v", o)
	}

	// the certificate chain
	st, hdr, b := h.do("POST", strings.TrimPrefix(certURL, "http://localhost:8080"), "application/jose+json", "{}")
	if st != 200 || hdr.Get("Content-Type") != "application/pem-certificate-chain" {
		t.Fatalf("certificate %d %q", st, hdr.Get("Content-Type"))
	}
	assertNonce(t, hdr)
	blk, rest := pem.Decode(b)
	if blk == nil || blk.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("certificate PEM %q", b)
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	mustNoErr(t, err)
	if cert.Subject.CommonName != "test.local" || !cert.NotAfter.Equal(testNow.Add(90*24*time.Hour).Truncate(time.Second)) {
		t.Errorf("certificate %q until %v", cert.Subject.CommonName, cert.NotAfter)
	}

	summaries := ""
	for _, c := range h.calls() {
		summaries += strings.Fields(c.Summary)[0] + ","
	}
	if want := "newOrder,getAuthz,getOrder,challenge,getAuthz,finalizeOrder,getOrder,downloadCertificate,"; summaries != want {
		t.Errorf("recorded %s, want %s", summaries, want)
	}
}

func TestStubUnknownIdentifiers(t *testing.T) {
	h := newStubHarness(t, "test.local")
	// an unknown order is reported as invalid
	if st, _, m := h.jose("/stub/acme/order/nope"); st != 200 || m["status"] != "invalid" {
		t.Errorf("order %d %v", st, m)
	}
	if c := h.lastCall(); c.Summary != "getOrder orderId=nope status=null" {
		t.Errorf("recorded %+v", c)
	}
	// an unknown authorization answers a valid one with the fallback token
	st, _, m := h.jose("/stub/acme/authz/nope")
	ch := m["challenges"].([]any)[0].(map[string]any)
	if st != 200 || m["status"] != "valid" || ch["token"] != "stub-token-fallback" || ch["status"] != "valid" {
		t.Errorf("authz %d %v", st, m)
	}
	// an unknown challenge is accepted anyway
	if st, _, m := h.jose("/stub/acme/challenge/my-token-xyz"); st != 200 || m["status"] != "valid" || m["token"] != "my-token-xyz" || m["type"] != "http-01" {
		t.Errorf("challenge %d %v", st, m)
	}
	if c := h.lastCall(); c.Method != "POST" || c.Endpoint != "/stub/acme/challenge/my-token-xyz" || c.Summary != "challenge token=my-token-xyz status=valid" {
		t.Errorf("recorded %+v", c)
	}
	// an unknown order is finalized with a certificate for the main domain
	st, _, f := h.jose("/stub/acme/order/nope/finalize")
	if st != 200 || f["status"] != "valid" {
		t.Errorf("finalize %d %v", st, f)
	}
	// an unknown certificate
	if st, _, b := h.do("POST", "/stub/acme/certificate/unknown-cert-id", "application/jose+json", "{}"); st != 404 || len(b) != 0 {
		t.Errorf("certificate %d %q", st, b)
	}
}

func TestStubAuthzReflectsTheStoredState(t *testing.T) {
	for _, status := range []string{"pending", "valid"} {
		h := newStubHarness(t, "test.local")
		h.svc.stub.authzs["authz1"] = &stubAuthz{id: "authz1", orderID: "order1", challengeToken: "tok123", status: status}
		st, _, m := h.jose("/stub/acme/authz/authz1")
		ch := m["challenges"].([]any)[0].(map[string]any)
		if st != 200 || m["status"] != status || ch["status"] != status || ch["token"] != "tok123" {
			t.Errorf("%s: %d %v", status, st, m)
		}
		if m["identifier"].(map[string]any)["value"] != "test.local" {
			t.Errorf("identifier %v", m["identifier"])
		}
	}
}

func TestStubFinalizeInjectedOrder(t *testing.T) {
	h := newStubHarness(t, "test.local")
	h.svc.stub.orders["order1"] = &stubOrder{id: "order1", domain: "test.local", authzID: "authz1", challengeToken: "token1"}
	_, _, f := h.jose("/stub/acme/order/order1/finalize")
	certID := f["certificate"].(string)[strings.LastIndex(f["certificate"].(string), "/")+1:]
	st, _, b := h.do("POST", "/stub/acme/certificate/"+certID, "application/jose+json", "{}")
	if st != 200 || !strings.Contains(strings.ToUpper(string(b)), "BEGIN CERTIFICATE") {
		t.Errorf("%d %s", st, b)
	}
}

// --- state endpoints -------------------------------------------------------------------------

func TestStubStateEndpoints(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, _, b := h.do("GET", "/admin/stub/acme/calls", "", "")
	if st != 200 || strings.TrimSpace(string(b)) != "[]" {
		t.Errorf("empty calls: %d %s", st, b)
	}
	st, _, b = h.do("GET", "/admin/stub/acme/last-deploy", "", "")
	if st != 200 || strings.TrimSpace(string(b)) != `{"lastDeployedCertPreview":null,"lastDeployedAt":null}` {
		t.Errorf("last-deploy: %d %s", st, b)
	}

	h.do("GET", "/stub/acme/directory", "", "")
	h.jose("/stub/acme/new-account")
	st, _, b = h.do("GET", "/admin/stub/acme/calls", "", "")
	var calls []map[string]any
	mustNoErr(t, json.Unmarshal(b, &calls))
	if st != 200 || len(calls) != 2 || calls[0]["method"] != "GET" || calls[1]["method"] != "POST" ||
		calls[1]["summary"] != "newAccount id=stub-account-001" || calls[0]["endpoint"] != "/stub/acme/directory" {
		t.Errorf("calls: %d %s", st, b)
	}
	// LocalDateTime as a Jackson array (seconds and nanoseconds omitted when zero)
	if ts := calls[0]["timestamp"].([]any); len(ts) != 5 || ts[0] != float64(2026) || ts[1] != float64(6) || ts[2] != float64(25) || ts[3] != float64(3) || ts[4] != float64(0) {
		t.Errorf("timestamp %v", ts)
	}

	st, _, b = h.do("DELETE", "/admin/stub/acme/calls/reset", "", "")
	if st != 200 || !strings.Contains(string(b), "réinitialisé") {
		t.Errorf("reset: %d %s", st, b)
	}
	if len(h.calls()) != 0 {
		t.Errorf("calls after reset: %v", h.calls())
	}
}

func TestStubLocalDateTimeArray(t *testing.T) {
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), "[2026 7 16 10 0]"},
		{time.Date(2026, 7, 16, 10, 1, 5, 0, time.UTC), "[2026 7 16 10 1 5]"},
		{time.Date(2026, 7, 16, 10, 1, 0, 7, time.UTC), "[2026 7 16 10 1 0 7]"},
		{time.Date(2026, 7, 16, 10, 1, 9, 123456000, time.UTC), "[2026 7 16 10 1 9 123456000]"},
	} {
		if got := fmt.Sprint(localDateTimeArray(c.t)); got != c.want {
			t.Errorf("%v: %s, want %s", c.t, got, c.want)
		}
	}
}

// --- Cloudflare stub ---------------------------------------------------------------------------

func TestStubCloudflareZone(t *testing.T) {
	h := newStubHarness(t, "example.gouv.fr")
	st, _, b := h.do("GET", "/stub/cloudflare/zones/zone-abc123", "", "")
	var m map[string]any
	mustNoErr(t, json.Unmarshal(b, &m))
	r := m["result"].(map[string]any)
	if st != 200 || m["success"] != true || r["id"] != "zone-abc123" || r["status"] != "active" || r["name"] != "example.gouv.fr" ||
		r["plan"].(map[string]any)["name"] != "stub-enterprise" {
		t.Errorf("%d %s", st, b)
	}
	if c := h.lastCall(); c.Method != "GET" || c.Endpoint != "/stub/cloudflare/zones/zone-abc123" || c.Summary != "getZone zoneId=zone-abc123" {
		t.Errorf("recorded %+v", c)
	}
	// the Cloudflare routes accept any content type and no body
	h2 := newStubHarness(t, "")
	_, _, b = h2.do("GET", "/stub/cloudflare/zones/z", "", "")
	if !strings.Contains(string(b), `"name":"stub.local"`) {
		t.Errorf("blank domain: %s", b)
	}
}

func TestStubCloudflareDeploy(t *testing.T) {
	h := newStubHarness(t, "test.local")
	cert := "-----BEGIN CERTIFICATE-----\nABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefghijklmnopqrst\n-----END CERTIFICATE-----"
	body, _ := json.Marshal(map[string]any{"certificate": cert, "private_key": "-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----", "bundle_method": "ubiquitous"})
	st, _, b := h.do("POST", "/stub/cloudflare/zones/zone-abc123/custom_certificates", "application/json", string(body))
	var m map[string]any
	mustNoErr(t, json.Unmarshal(b, &m))
	r := m["result"].(map[string]any)
	if st != 200 || m["success"] != true || r["status"] != "active" || r["bundle_method"] != "ubiquitous" || !strings.HasPrefix(r["id"].(string), "stub-cert-deploy-") {
		t.Errorf("%d %s", st, b)
	}
	// the preview is the first non header line cut to 40 characters
	want := "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcd"
	if c := h.lastCall(); c.Endpoint != "/stub/cloudflare/zones/zone-abc123/custom_certificates" ||
		c.Summary != "deployCertificate zoneId=zone-abc123 certPreview="+want {
		t.Errorf("recorded %+v", c)
	}
	_, _, b = h.do("GET", "/admin/stub/acme/last-deploy", "", "")
	var ld map[string]any
	mustNoErr(t, json.Unmarshal(b, &ld))
	if ld["lastDeployedCertPreview"] != want || ld["lastDeployedAt"] == nil {
		t.Errorf("last-deploy %s", b)
	}
	h.do("DELETE", "/admin/stub/acme/calls/reset", "", "")
	if _, _, b = h.do("GET", "/admin/stub/acme/last-deploy", "", ""); strings.Contains(string(b), want) {
		t.Errorf("reset keeps the last deployment: %s", b)
	}
}

func TestStubCloudflareDeployWithoutCertificate(t *testing.T) {
	h := newStubHarness(t, "test.local")
	st, _, _ := h.do("POST", "/stub/cloudflare/zones/z/custom_certificates", "application/json", `{}`)
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	if c := h.lastCall(); c.Summary != "deployCertificate zoneId=z certPreview=N/A" {
		t.Errorf("recorded %+v", c)
	}
}

// --- AcmeStubCertificateGeneratorTest -----------------------------------------------------------

func TestGenerateStubCertificate(t *testing.T) {
	now := time.Date(2026, 6, 25, 3, 0, 0, 0, time.UTC)
	a, err := generateStubCertificate("test.local", now)
	mustNoErr(t, err)
	if !strings.HasPrefix(a, "-----BEGIN CERTIFICATE-----\n") || !strings.HasSuffix(a, "-----END CERTIFICATE-----\n") {
		t.Errorf("PEM %q", a)
	}
	blk, _ := pem.Decode([]byte(a))
	cert, err := x509.ParseCertificate(blk.Bytes)
	mustNoErr(t, err)
	if cert.Subject.CommonName != "test.local" || !cert.NotAfter.Equal(now.Add(90*24*time.Hour)) || cert.NotBefore.After(now) {
		t.Errorf("subject %q valid %v - %v", cert.Subject.CommonName, cert.NotBefore, cert.NotAfter)
	}
	if cert.SignatureAlgorithm != x509.SHA256WithRSA {
		t.Errorf("signature %v", cert.SignatureAlgorithm)
	}
	mustNoErr(t, cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature)) // self-signed
	b, err := generateStubCertificate("test.local", now)
	mustNoErr(t, err)
	if a == b {
		t.Error("two generations returned the same certificate")
	}
}

// --- the whole flow: RenewalUseCase + golang.org/x/crypto/acme against the in-process stub --------

// The sequence of stub calls without the directory reads (S11-N1), identifiers masked.
func (h *stubHarness) sequence() []string {
	var out []string
	for _, c := range h.calls() {
		if c.Endpoint == "/stub/acme/directory" {
			continue
		}
		out = append(out, c.Method+" "+idRe.ReplaceAllString(c.Endpoint, "/stub/acme/$1/ID")+" | "+kvRe.ReplaceAllString(c.Summary, "$1=*"))
	}
	return out
}

func TestRenewalAgainstTheInProcessStub(t *testing.T) {
	h := newStubHarness(t, "agora.gouv.fr")
	u, _ := url.Parse(h.srv.URL)
	cfg := h.svc.Cfg
	cfg.Port, _ = strconv.Atoi(u.Port()) // the URLs of the stub point at the test server
	cfg.ServerURL = h.srv.URL + "/stub/acme/directory"
	cfg.ServerInteractionEnabled, cfg.CloudflareInteractionEnabled = true, true
	cfg.CloudflareBaseURL, cfg.CloudflareZoneID, cfg.CloudflareAPIToken = h.srv.URL+"/stub/cloudflare", "stub-zone-id", "stub-token"

	repos := newFakeRepos()
	uc := &RenewalUseCase{
		Cfg: cfg, Certificates: repos, Accounts: repos, Challenges: repos, Orders: repos,
		Deployer: &CloudflareDeployer{Cfg: cfg, Rest: NewRestClient()}, CA: &LibProvider{},
		Now: func() time.Time { return testNow }, Sleep: func(context.Context, time.Duration) {}, Log: discardLog(),
		NewKeyPair: func() (crypto.Signer, error) { return testKeyCached(t), nil },
	}
	run := func() { t.Helper(); mustNoErr(t, uc.RenewIfNeeded(context.Background())) }

	// first issuance: account, order, challenge (accepted, then re-read), finalization, download (twice), deployment
	wantSeq := []string{
		"POST /stub/acme/new-account | newAccount id=stub-account-001",
		"POST /stub/acme/new-order | newOrder orderId=* domain=agora.gouv.fr",
		"POST /stub/acme/authz/ID | getAuthz authzId=* status=pending",
		"POST /stub/acme/challenge/ID | challenge token=* status=valid",
		"POST /stub/acme/challenge/ID | challenge token=* status=valid",
		"POST /stub/acme/order/ID | getOrder orderId=* status=pending",
		"POST /stub/acme/order/ID/finalize | finalizeOrder orderId=*",
		"POST /stub/acme/order/ID | getOrder orderId=* status=valid",
		"POST /stub/acme/certificate/ID | downloadCertificate certId=*",
		"POST /stub/acme/certificate/ID | downloadCertificate certId=*",
		"POST /stub/cloudflare/zones/stub-zone-id/custom_certificates | deployCertificate zoneId=stub-zone-id certPreview=*",
	}
	run()
	got := h.sequence()
	if strings.Join(got, "\n") != strings.Join(wantSeq, "\n") {
		t.Fatalf("first issuance calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantSeq, "\n"))
	}
	if len(repos.certs) != 1 || repos.certs[0].Status != StatusDeployed || repos.certs[0].DeployedAt == nil {
		t.Fatalf("certificates %+v", repos.certs)
	}
	if len(repos.accounts) != 1 || repos.accounts[0].AccountURL == nil || *repos.accounts[0].AccountURL != "http://localhost:"+u.Port()+"/stub/acme/account/stub-account-001" {
		t.Errorf("accounts %+v", repos.accounts)
	}
	if len(repos.orders) != 0 || len(repos.challenges) != 0 {
		t.Errorf("orders=%d challenges=%d", len(repos.orders), len(repos.challenges))
	}
	if !repos.certs[0].ExpiresAt.Equal(testNow.Add(90 * 24 * time.Hour).Truncate(time.Second)) {
		t.Errorf("expiresAt %v", repos.certs[0].ExpiresAt)
	}
	_, _, b := h.do("GET", "/admin/stub/acme/last-deploy", "", "")
	if strings.Contains(string(b), `"lastDeployedCertPreview":null`) {
		t.Errorf("nothing deployed on the stub: %s", b)
	}

	// a valid deployed certificate: nothing happens
	before := len(h.calls())
	run()
	if len(h.calls()) != before {
		t.Errorf("a valid certificate triggered %d calls", len(h.calls())-before)
	}

	// expiring: a new order; the stored account is looked up again (onlyReturnExisting: the stub
	// sees a new-account request) and saves it again (an upsert: still one account row)
	repos.certs[0].ExpiresAt = testNow.Add(10 * 24 * time.Hour)
	run()
	if seq := h.sequence()[len(wantSeq):]; strings.Join(seq, "\n") != strings.Join(wantSeq, "\n") {
		t.Fatalf("renewal calls:\n%s", strings.Join(seq, "\n"))
	}
	if len(repos.certs) != 2 || repos.certs[1].Status != StatusDeployed || len(repos.accounts) != 1 {
		t.Errorf("certs=%d accounts=%d ops=%s", len(repos.certs), len(repos.accounts), repos.ops())
	}
}
