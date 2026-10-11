package acme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// This file implements the CA* interfaces with golang.org/x/crypto/acme (RFC
// 8555): directory discovery, nonce pool and RS256/ES* signing, new-order,
// order / authorization / challenge reads, challenge acceptance and
// certificate download all go through acme.Client.
//
// Two requests cannot be expressed with the library and are signed here with
// the same account key (see libSession.rawPost):
//   - newAccount: acme4j sends termsOfServiceAgreed unconditionally, derives
//     the EAB algorithm (HS256/HS384/HS512) from the size of the MAC key,
//     accepts 200 as well as 201 for "onlyReturnExisting" (the library only
//     accepts 200 and the stub answers 201);
//   - finalize: the library's CreateOrderCert always waits for the order and
//     downloads the certificate, while the use case needs the bare request
//     followed by its own polling.
// The library retries 5xx answers forever; acme4j never retries, hence
// badNonceOnlyBackoff. Retry-After is read through a RoundTripper because the
// library hides response headers.

const (
	// acme4j NetworkSettings: 10 s connect and 10 s read timeout.
	caDialTimeout = 10 * time.Second
	caReadTimeout = 10 * time.Second
	// Total deadline of one logical ACME operation (class C, see
	// parity/divergences/S11.md): Java only bounds each socket read.
	caOperationTimeout = 60 * time.Second
	// DefaultConnection.MAX_ATTEMPTS (bad nonce retries).
	caMaxAttempts = 10
)

var base64URLRe = regexp.MustCompile(`^[0-9A-Za-z_-]*$`)

// LibProvider is the CAProvider backed by golang.org/x/crypto/acme.
type LibProvider struct {
	// Transport overrides the HTTP transport (tests).
	Transport http.RoundTripper
}

// NewSession implements CAProvider.
func (p *LibProvider) NewSession(serverURL string) CASession {
	rt := p.Transport
	if rt == nil {
		rt = newOutboundTransport(caDialTimeout, caReadTimeout)
	}
	c := &capture{rt: rt}
	return &libSession{serverURL: serverURL, hc: &http.Client{Transport: c}, cap: c}
}

// newOutboundTransport builds a transport without proxy (acme4j uses
// Proxy.NO_PROXY, the JVM only honours -Dhttp.proxyHost) and with connect /
// response timeouts.
func newOutboundTransport(connect, read time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: connect}).DialContext,
		TLSHandshakeTimeout:   connect,
		ResponseHeaderTimeout: read,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
	}
}

// capture remembers the headers of the last response.
type capture struct {
	rt   http.RoundTripper
	mu   sync.Mutex
	last http.Header
}

func (c *capture) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := c.rt.RoundTrip(r)
	if err == nil {
		c.mu.Lock()
		c.last = res.Header.Clone()
		c.mu.Unlock()
	}
	return res, err
}

func (c *capture) lastHeader(name string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.last[http.CanonicalHeaderKey(name)]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

var digitsRe = regexp.MustCompile(`^[0-9]+$`)

// retryAfterOf is DefaultConnection.getRetryAfterHeader + handleRetryAfter
// applied to the last response: a purely numeric value (seconds, relative to
// the Date header) or a parsable HTTP date raises RetryAfterError; any other
// value is ignored; an int overflow is a protocol error.
func (c *capture) retryAfterOf(msg string) error {
	v, ok := c.lastHeader("Retry-After")
	if !ok {
		return nil
	}
	if digitsRe.MatchString(v) {
		secs, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return fmt.Errorf("AcmeProtocolException: Bad retry-after header value: %s", v)
		}
		base := time.Now()
		if d, ok := c.lastHeader("Date"); ok {
			if t, err := http.ParseTime(d); err == nil {
				base = t
			}
		}
		return &RetryAfterError{Msg: msg, RetryAfter: base.Add(time.Duration(secs) * time.Second)}
	}
	if t, err := http.ParseTime(v); err == nil && t.UnixMilli() != 0 {
		return &RetryAfterError{Msg: msg, RetryAfter: t}
	}
	return nil
}

// badNonceOnlyBackoff stops every retry of the library except the one that
// follows a "bad nonce" answer (HTTP 400), at most caMaxAttempts times.
func badNonceOnlyBackoff(n int, _ *http.Request, res *http.Response) time.Duration {
	if res.StatusCode == http.StatusBadRequest && n < caMaxAttempts {
		return time.Millisecond
	}
	return 0
}

type libSession struct {
	serverURL string
	hc        *http.Client
	cap       *capture
}

func (s *libSession) client(key crypto.Signer, kid string) *acme.Client {
	return &acme.Client{
		Key:          key,
		KID:          acme.KeyID(kid),
		DirectoryURL: s.serverURL,
		HTTPClient:   s.hc,
		RetryBackoff: badNonceOnlyBackoff,
		UserAgent:    "agora-back",
	}
}

func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, caOperationTimeout)
}

// Login implements CASession (no request is sent).
func (s *libSession) Login(accountURL string, key crypto.Signer) (CAAccount, error) {
	if _, err := url.Parse(accountURL); err != nil {
		return nil, fmt.Errorf("MalformedURLException: %w", err)
	}
	return &libAccount{s: s, cl: s.client(key, accountURL), location: accountURL}, nil
}

// CreateAccount implements CASession.
func (s *libSession) CreateAccount(ctx context.Context, key crypto.Signer, o AccountOptions) (CAAccount, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	cl := s.client(key, "")
	dir, err := cl.Discover(ctx)
	if err != nil {
		return nil, err
	}
	var macKey []byte
	if o.EabKid != "" {
		if macKey, err = JavaBase64Decode(o.EabHmacKey, true); err != nil {
			return nil, err
		}
	}
	claims := newAccountClaims{}
	if o.AgreeToTerms {
		t := true
		claims.TermsOfServiceAgreed = &t
	}
	if o.EabKid != "" {
		eab, err := externalAccountBinding(o.EabKid, key.Public(), macKey, dir.RegURL)
		if err != nil {
			return nil, err
		}
		claims.ExternalAccountBinding = eab
	}
	if o.OnlyExisting {
		t := true
		claims.OnlyReturnExisting = &t
	}
	res, err := s.rawPost(ctx, dir, dir.RegURL, claims, key, "")
	if err != nil {
		return nil, err
	}
	loc := res.header.Get("Location")
	if loc == "" {
		return nil, errors.New("AcmeProtocolException: Server did not provide an account location")
	}
	loc = resolveAgainst(dir.RegURL, loc)
	data, err := readJSONBody(res)
	if err != nil {
		return nil, err
	}
	status, _ := data["status"].(string)
	cl.KID = acme.KeyID(loc)
	return &libAccount{s: s, cl: cl, location: loc, status: status}, nil
}

func resolveAgainst(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// --- raw JWS requests -----------------------------------------------------------

type newAccountClaims struct {
	TermsOfServiceAgreed   *bool          `json:"termsOfServiceAgreed,omitempty"`
	ExternalAccountBinding map[string]any `json:"externalAccountBinding,omitempty"`
	OnlyReturnExisting     *bool          `json:"onlyReturnExisting,omitempty"`
}

type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

// problemError is an AcmeServerException (application/problem+json answer).
type problemError struct {
	Status int
	Type   string
	Detail string
}

func (e *problemError) Error() string {
	return fmt.Sprintf("AcmeServerException: HTTP %d %s: %s", e.Status, e.Type, e.Detail)
}

// rawPost is DefaultConnection.sendSignedRequest: a JWS signed with the
// account key (kid form when kid is set, jwk form otherwise), a fresh nonce
// from newNonce for every attempt, bad nonce retried up to MAX_ATTEMPTS
// times, 200 and 201 accepted.
func (s *libSession) rawPost(ctx context.Context, dir acme.Directory, target string, claims any, key crypto.Signer, kid string) (*rawResponse, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		nonce, err := s.newNonce(ctx, dir)
		if err != nil {
			return nil, err
		}
		body, err := signJWS(key, kid, nonce, target, payload)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/jose+json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "agora-back")
		resp, err := s.hc.Do(req)
		if err != nil {
			return nil, err
		}
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		raw := &rawResponse{status: resp.StatusCode, header: resp.Header, body: b}
		if resp.StatusCode == 200 || resp.StatusCode == 201 {
			return raw, nil
		}
		perr := problemOf(raw)
		var pe *problemError
		if errors.As(perr, &pe) && pe.Type == "urn:ietf:params:acme:error:badNonce" && attempt < caMaxAttempts {
			continue
		}
		return nil, perr
	}
}

func problemOf(r *rawResponse) error {
	ct, _, _ := mime.ParseMediaType(r.header.Get("Content-Type"))
	if ct != "application/problem+json" {
		return fmt.Errorf("AcmeException: HTTP %d", r.status)
	}
	var p struct{ Type, Detail string }
	_ = json.Unmarshal(r.body, &p)
	return &problemError{Status: r.status, Type: p.Type, Detail: p.Detail}
}

func readJSONBody(r *rawResponse) (map[string]any, error) {
	if r.header.Get("Content-Length") == "0" {
		return nil, errors.New("AcmeProtocolException: Empty response")
	}
	ct, _, _ := mime.ParseMediaType(r.header.Get("Content-Type"))
	if ct != "application/json" && ct != "application/problem+json" {
		return nil, fmt.Errorf("AcmeProtocolException: Unexpected content type: %s", ct)
	}
	if len(r.body) == 0 {
		return nil, errors.New("AcmeProtocolException: JSON response is empty")
	}
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		return nil, fmt.Errorf("AcmeProtocolException: %w", err)
	}
	return m, nil
}

// newNonce is DefaultConnection.resetNonce: HEAD newNonce, 200 or 204.
func (s *libSession) newNonce(ctx context.Context, dir acme.Directory) (string, error) {
	if dir.NonceURL == "" {
		return "", errors.New("AcmeProtocolException: Server does not provide newNonce")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, dir.NonceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "agora-back")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return "", fmt.Errorf("AcmeException: HTTP %d", resp.StatusCode)
	}
	n := resp.Header.Get("Replay-Nonce")
	if n == "" {
		return "", errors.New("AcmeProtocolException: Server did not provide a nonce")
	}
	if !base64URLRe.MatchString(n) {
		return "", fmt.Errorf("AcmeProtocolException: Invalid replay nonce: %s", n)
	}
	return n, nil
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

type jwk map[string]string

func jwkOf(pub crypto.PublicKey) (jwk, string, error) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		e := big.NewInt(int64(k.E)).Bytes()
		return jwk{"kty": "RSA", "n": b64u(k.N.Bytes()), "e": b64u(e)}, "RS256", nil
	case *ecdsa.PublicKey:
		size := (k.Curve.Params().BitSize + 7) / 8
		pad := func(b *big.Int) []byte {
			x := b.Bytes()
			return append(make([]byte, size-len(x)), x...)
		}
		var alg string
		switch k.Curve.Params().Name {
		case "P-256":
			alg = "ES256"
		case "P-384":
			alg = "ES384"
		case "P-521":
			alg = "ES512"
		default:
			return nil, "", fmt.Errorf("IllegalArgumentException: Unknown EC name %s", k.Curve.Params().Name)
		}
		return jwk{"kty": "EC", "crv": k.Curve.Params().Name, "x": b64u(pad(k.X)), "y": b64u(pad(k.Y))}, alg, nil
	}
	return nil, "", errors.New("IllegalArgumentException: Unknown algorithm")
}

func signJWS(key crypto.Signer, kid, nonce, target string, payload []byte) ([]byte, error) {
	j, alg, err := jwkOf(key.Public())
	if err != nil {
		return nil, err
	}
	hdr := map[string]any{"alg": alg, "url": target, "nonce": nonce}
	if kid != "" {
		hdr["kid"] = kid
	} else {
		hdr["jwk"] = j
	}
	hb, err := json.Marshal(hdr)
	if err != nil {
		return nil, err
	}
	signing := b64u(hb) + "." + b64u(payload)
	var h crypto.Hash
	switch alg {
	case "RS256", "ES256":
		h = crypto.SHA256
	case "ES384":
		h = crypto.SHA384
	default:
		h = crypto.SHA512
	}
	hh := h.New()
	hh.Write([]byte(signing))
	sig, err := key.Sign(rand.Reader, hh.Sum(nil), h)
	if err != nil {
		return nil, err
	}
	if alg != "RS256" { // ASN.1 → R || S
		var rs struct{ R, S *big.Int }
		if _, err := asn1.Unmarshal(sig, &rs); err != nil {
			return nil, err
		}
		size := (key.Public().(*ecdsa.PublicKey).Curve.Params().BitSize + 7) / 8
		out := make([]byte, 2*size)
		rb, sb := rs.R.Bytes(), rs.S.Bytes()
		copy(out[size-len(rb):size], rb)
		copy(out[2*size-len(sb):], sb)
		sig = out
	}
	return json.Marshal(map[string]string{"protected": b64u(hb), "payload": b64u(payload), "signature": b64u(sig)})
}

// externalAccountBinding is JoseUtils.createExternalAccountBinding: a JWS over
// the JSON of the account public key, MACed with HS256 / HS384 / HS512 chosen
// from the size of the key (256 / 384 / 512 bits, anything else is refused).
func externalAccountBinding(kid string, pub crypto.PublicKey, macKey []byte, newAccountURL string) (map[string]any, error) {
	j, _, err := jwkOf(pub)
	if err != nil {
		return nil, err
	}
	var alg string
	var newHash func() hash.Hash
	switch len(macKey) * 8 {
	case 256:
		alg, newHash = "HS256", sha256.New
	case 384:
		alg, newHash = "HS384", sha512.New384
	case 512:
		alg, newHash = "HS512", sha512.New
	default:
		return nil, fmt.Errorf("IllegalArgumentException: Bad key size: %d", len(macKey)*8)
	}
	hb, err := json.Marshal(map[string]string{"alg": alg, "kid": kid, "url": newAccountURL})
	if err != nil {
		return nil, err
	}
	pb, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	m := hmac.New(newHash, macKey)
	m.Write([]byte(b64u(hb) + "." + b64u(pb)))
	return map[string]any{"protected": b64u(hb), "signature": b64u(m.Sum(nil)), "payload": b64u(pb)}, nil
}

// --- account --------------------------------------------------------------------

type libAccount struct {
	s        *libSession
	cl       *acme.Client
	location string
	status   string
}

func (a *libAccount) Location() string { return a.location }
func (a *libAccount) Status() string   { return a.status }

func (a *libAccount) NewOrder(ctx context.Context, domains []string) (CAOrder, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	ids := make([]acme.AuthzID, 0, len(domains))
	for _, d := range domains {
		ace, err := ToACE(d)
		if err != nil {
			return nil, err
		}
		ids = append(ids, acme.AuthzID{Type: "dns", Value: ace})
	}
	o, err := a.cl.AuthorizeOrder(ctx, ids)
	if err != nil {
		return nil, err
	}
	if o.URI == "" {
		return nil, errors.New("AcmeProtocolException: Server did not provide a Location header")
	}
	return &libOrder{a: a, location: o.URI, data: o, loaded: true}, nil
}

func (a *libAccount) BindOrder(orderURL string) CAOrder {
	return &libOrder{a: a, location: orderURL}
}

// --- order ----------------------------------------------------------------------

type libOrder struct {
	a        *libAccount
	location string
	data     *acme.Order
	loaded   bool // false until the first load, and again after Execute (invalidate())
}

func (o *libOrder) Location() string { return o.location }

func (o *libOrder) load(ctx context.Context) error {
	if o.loaded {
		return nil
	}
	// AcmeJsonResource.getJSON(): update(), a Retry-After is only logged
	if err := o.Update(ctx); err != nil {
		var ra *RetryAfterError
		if !errors.As(err, &ra) {
			return fmt.Errorf("AcmeLazyLoadingException: %w", err)
		}
	}
	return nil
}

func (o *libOrder) Update(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	d, err := o.a.cl.GetOrder(ctx, o.location)
	if err != nil {
		return err
	}
	o.data, o.loaded = d, true
	return o.a.s.cap.retryAfterOf("Order is not completed yet")
}

func (o *libOrder) Execute(ctx context.Context, csr []byte) error {
	if err := o.load(ctx); err != nil {
		return err
	}
	if o.data.FinalizeURL == "" {
		return errors.New("AcmeProtocolException: JSON key finalize is required")
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	dir, err := o.a.cl.Discover(ctx)
	if err != nil {
		return err
	}
	_, err = o.a.s.rawPost(ctx, dir, o.data.FinalizeURL, map[string]string{"csr": b64u(csr)}, o.a.cl.Key, o.a.location)
	o.loaded = false // invalidate()
	return err
}

func (o *libOrder) Status(ctx context.Context) (CAStatus, error) {
	if err := o.load(ctx); err != nil {
		return "", err
	}
	return ParseCAStatus(o.data.Status), nil
}

func (o *libOrder) Authorizations(ctx context.Context) ([]CAAuthorization, error) {
	if err := o.load(ctx); err != nil {
		return nil, err
	}
	out := make([]CAAuthorization, 0, len(o.data.AuthzURLs))
	for _, u := range o.data.AuthzURLs {
		out = append(out, &libAuthorization{a: o.a, location: u})
	}
	return out, nil
}

func (o *libOrder) Certificate(ctx context.Context) (CACertificate, error) {
	if err := o.load(ctx); err != nil {
		return nil, err
	}
	if o.data.CertURL == "" {
		return nil, nil
	}
	return &libCertificate{a: o.a, location: o.data.CertURL}, nil
}

// --- authorization ---------------------------------------------------------------

type libAuthorization struct {
	a        *libAccount
	location string
	data     *acme.Authorization
}

func (z *libAuthorization) load(ctx context.Context) error {
	if z.data != nil {
		return nil
	}
	if err := z.Update(ctx); err != nil {
		var ra *RetryAfterError
		if !errors.As(err, &ra) {
			return fmt.Errorf("AcmeLazyLoadingException: %w", err)
		}
	}
	return nil
}

func (z *libAuthorization) Update(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	d, err := z.a.cl.GetAuthorization(ctx, z.location)
	if err != nil {
		return err
	}
	z.data = d
	return z.a.s.cap.retryAfterOf("Authorization is not completed yet")
}

func (z *libAuthorization) Domain(ctx context.Context) (string, error) {
	if err := z.load(ctx); err != nil {
		return "", err
	}
	return z.data.Identifier.Value, nil
}

func (z *libAuthorization) FindHTTP01(ctx context.Context) (CAChallenge, error) {
	if err := z.load(ctx); err != nil {
		return nil, err
	}
	var found *acme.Challenge
	for _, c := range z.data.Challenges {
		if c.Type != "http-01" {
			continue
		}
		if found != nil {
			return nil, errors.New("AcmeProtocolException: Found more than one challenge of type http-01")
		}
		found = c
	}
	if found == nil {
		return nil, nil
	}
	cp := *found
	return &libChallenge{a: z.a, data: &cp}, nil
}

// --- challenge -------------------------------------------------------------------

type libChallenge struct {
	a    *libAccount
	data *acme.Challenge
}

func (c *libChallenge) Token() string    { return c.data.Token }
func (c *libChallenge) Location() string { return c.data.URI }
func (c *libChallenge) Status() CAStatus { return ParseCAStatus(c.data.Status) }
func (c *libChallenge) KeyAuthorization() (string, error) {
	if !base64URLRe.MatchString(c.data.Token) {
		return "", fmt.Errorf("AcmeProtocolException: Invalid token: %s", c.data.Token)
	}
	return c.a.cl.HTTP01ChallengeResponse(c.data.Token)
}

func (c *libChallenge) Trigger(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	d, err := c.a.cl.Accept(ctx, &acme.Challenge{URI: c.data.URI, Type: c.data.Type, Token: c.data.Token})
	if err != nil {
		return err
	}
	c.keep(d)
	return nil
}

func (c *libChallenge) Update(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	d, err := c.a.cl.GetChallenge(ctx, c.data.URI)
	if err != nil {
		return err
	}
	c.keep(d)
	return c.a.s.cap.retryAfterOf("Challenge is not completed yet")
}

// keep stores the refreshed data (the library fills the type only when the CA
// repeats it).
func (c *libChallenge) keep(d *acme.Challenge) {
	if d.URI == "" {
		d.URI = c.data.URI
	}
	if d.Type == "" {
		d.Type = c.data.Type
	}
	if d.Token == "" {
		d.Token = c.data.Token
	}
	c.data = d
}

// --- certificate -----------------------------------------------------------------

type libCertificate struct {
	a        *libAccount
	location string
	chain    [][]byte
}

func (c *libCertificate) Chain(ctx context.Context) ([][]byte, error) {
	if c.chain != nil {
		return c.chain, nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	chain, err := c.a.cl.FetchCert(ctx, c.location, true)
	if err != nil {
		return nil, err
	}
	c.chain = chain
	return chain, nil
}
