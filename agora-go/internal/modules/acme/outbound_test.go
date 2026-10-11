package acme

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The Kotlin tests of CloudflareCertificateDeployerImpl, CloudflareZoneCheckerImpl and
// AcmeServerCheckerImpl mock RestTemplate.exchange. Here the real RestClient (RestTemplate
// semantics) talks to an httptest server, which also checks what is sent.

type seen struct {
	method, path, auth, contentType, accept, body string
}

func fakeServer(t *testing.T, status int, contentType, body string, extra func(w http.ResponseWriter)) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*s = seen{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("Accept"), string(b)}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if extra != nil {
			extra(w)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func cfConfig(srv *httptest.Server) *Config {
	return &Config{CloudflareBaseURL: srv.URL + "/client/v4", CloudflareZoneID: "zone-abc123", CloudflareAPIToken: "token-xyz"}
}

// --- CloudflareCertificateDeployerImplTest -------------------------------------------------

func TestDeployCertificateSendsThePayloadAndTheBearerToken(t *testing.T) {
	srv, got := fakeServer(t, 200, "application/json", `{"success":true}`, nil)
	cfg := cfConfig(srv)
	cfg.CloudflareZoneID, cfg.CloudflareAPIToken = "zone-abc", "super-secret-token"
	mustNoErr(t, (&CloudflareDeployer{Cfg: cfg, Rest: NewRestClient()}).DeployCertificate(context.Background(), "my-cert", "my-key"))
	if got.method != "POST" || got.path != "/client/v4/zones/zone-abc/custom_certificates" {
		t.Errorf("request %s %s", got.method, got.path)
	}
	if got.auth != "Bearer super-secret-token" || got.contentType != "application/json" {
		t.Errorf("headers auth=%q content-type=%q", got.auth, got.contentType)
	}
	if want := `{"certificate":"my-cert","private_key":"my-key","bundle_method":"ubiquitous","type":"sni_custom"}`; got.body != want {
		t.Errorf("body %s, want %s", got.body, want)
	}
}

func TestDeployCertificateAcceptsAnEmptyBodyAndTwoHundredAndOne(t *testing.T) {
	for _, st := range []int{200, 201, 204} {
		srv, _ := fakeServer(t, st, "", "", nil)
		if err := (&CloudflareDeployer{Cfg: cfConfig(srv), Rest: NewRestClient()}).DeployCertificate(context.Background(), "c", "k"); err != nil {
			t.Errorf("status %d: %v", st, err)
		}
	}
}

// Jackson (UTF8JsonGenerator.writeString) writes the two halves of a supplementary character as \uXXXX escapes.
func TestDeployCertificateEscapesThePEMInTheJSON(t *testing.T) {
	srv, got := fakeServer(t, 200, "application/json", `{}`, nil)
	mustNoErr(t, (&CloudflareDeployer{Cfg: cfConfig(srv), Rest: NewRestClient()}).DeployCertificate(context.Background(), "-----BEGIN-----\nA\"B\\é😀\n", "k</"))
	if want := `{"certificate":"-----BEGIN-----\nA\"B\\é\uD83D\uDE00\n","private_key":"k</","bundle_method":"ubiquitous","type":"sni_custom"}`; got.body != want {
		t.Errorf("body %s, want %s", got.body, want)
	}
}

// RestTemplate throws for 4xx / 5xx answers before CloudflareDeploymentException is reached
// (the Kotlin tests mock a 400 / 403 ResponseEntity that the real client never returns).
func TestDeployCertificateFailsOnClientAndServerErrors(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{{400, `{"errors":["invalid certificate format"]}`}, {403, `{"message":"Unauthorized"}`}, {500, ``}, {503, `<html>`}} {
		srv, _ := fakeServer(t, c.status, "application/json", c.body, nil)
		err := (&CloudflareDeployer{Cfg: cfConfig(srv), Rest: NewRestClient()}).DeployCertificate(context.Background(), "bad-cert", "key")
		var he *HTTPStatusError
		if !errors.As(err, &he) || he.Status != c.status || he.Body != c.body || !strings.Contains(err.Error(), strconv.Itoa(c.status)) {
			t.Errorf("status %d: %v", c.status, err)
		}
	}
}

// A non 2xx status that is not an error for RestTemplate (3xx is never followed for a POST)
// reaches CloudflareDeploymentException.
func TestDeployCertificateFailsOnANon2xxStatus(t *testing.T) {
	srv, _ := fakeServer(t, 302, "application/json", `{"errors":["moved"]}`, func(w http.ResponseWriter) { w.Header().Set("Location", "/elsewhere") })
	cfg := cfConfig(srv)
	cfg.CloudflareZoneID = "zone-abc"
	err := (&CloudflareDeployer{Cfg: cfg, Rest: NewRestClient()}).DeployCertificate(context.Background(), "cert", "key")
	var de *CloudflareDeploymentError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "302") || !strings.Contains(err.Error(), "zone-abc") {
		t.Errorf("err %v", err)
	}
}

func TestDeployCertificateFailsOnAnUnreadableBody(t *testing.T) {
	srv, _ := fakeServer(t, 200, "text/plain", `ok`, nil)
	if err := (&CloudflareDeployer{Cfg: cfConfig(srv), Rest: NewRestClient()}).DeployCertificate(context.Background(), "c", "k"); err == nil {
		t.Error("a text/plain body cannot be extracted as a Map")
	}
	srv, _ = fakeServer(t, 200, "application/json", `[1,2]`, nil)
	if err := (&CloudflareDeployer{Cfg: cfConfig(srv), Rest: NewRestClient()}).DeployCertificate(context.Background(), "c", "k"); err == nil {
		t.Error("a JSON array cannot be extracted as a Map")
	}
}

func TestDeployCertificateFailsWhenTheServerIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	cfg := cfConfig(srv)
	srv.Close()
	if err := (&CloudflareDeployer{Cfg: cfg, Rest: NewRestClient()}).DeployCertificate(context.Background(), "c", "k"); err == nil || !strings.Contains(err.Error(), "ResourceAccessException") {
		t.Errorf("err %v", err)
	}
}

// --- CloudflareZoneCheckerImplTest ---------------------------------------------------------

func TestGetZoneInfoParsesTheZone(t *testing.T) {
	srv, got := fakeServer(t, 200, "application/json", `{"result":{"name":"example.gouv.fr","status":"active","plan":{"name":"Enterprise"}},"success":true}`, nil)
	info, err := (&ZoneChecker{Cfg: cfConfig(srv), Rest: NewRestClient()}).GetZoneInfo(context.Background())
	mustNoErr(t, err)
	if *info != (CloudflareZoneInfo{ZoneID: "zone-abc123", Name: "example.gouv.fr", Status: "active", Plan: "Enterprise"}) {
		t.Errorf("info %+v", info)
	}
	if got.method != "GET" || got.path != "/client/v4/zones/zone-abc123" || got.auth != "Bearer token-xyz" || got.contentType != "application/json" {
		t.Errorf("request %+v", got)
	}
}

func TestGetZoneInfoDefaultsToUnknown(t *testing.T) {
	for name, body := range map[string]string{
		"no plan":            `{"result":{"name":"example.gouv.fr","status":"active"}}`,
		"plan not an object": `{"result":{"name":"example.gouv.fr","status":"active","plan":"Enterprise"}}`,
		"plan name not text": `{"result":{"name":"example.gouv.fr","status":"active","plan":{"name":3}}}`,
		"plan without name":  `{"result":{"name":"example.gouv.fr","status":"active","plan":{}}}`,
	} {
		srv, _ := fakeServer(t, 200, "application/json", body, nil)
		info, err := (&ZoneChecker{Cfg: cfConfig(srv), Rest: NewRestClient()}).GetZoneInfo(context.Background())
		mustNoErr(t, err)
		if info.Plan != "unknown" || info.Name != "example.gouv.fr" {
			t.Errorf("%s: %+v", name, info)
		}
	}
	srv, _ := fakeServer(t, 200, "application/json", `{"result":{}}`, nil)
	info, err := (&ZoneChecker{Cfg: cfConfig(srv), Rest: NewRestClient()}).GetZoneInfo(context.Background())
	mustNoErr(t, err)
	if *info != (CloudflareZoneInfo{ZoneID: "zone-abc123", Name: "unknown", Status: "unknown", Plan: "unknown"}) {
		t.Errorf("info %+v", info)
	}
}

func TestGetZoneInfoFailures(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		contentType  string
		body         string
		wantContains string
	}{
		{"empty body", 200, "application/json", ``, "empty body"},
		{"json null", 200, "application/json", `null`, "empty body"},
		{"no content", 204, "", ``, "empty body"},
		{"result missing", 200, "application/json", `{"success":false}`, "missing 'result' field"},
		{"result null", 200, "application/json", `{"result":null}`, "missing 'result' field"},
		{"result not an object", 200, "application/json", `{"result":[1]}`, "missing 'result' field"},
		{"3xx with a body", 301, "application/json", `{"a":1}`, "returned 301 for zone zone-abc123"},
	}
	for _, c := range cases {
		srv, _ := fakeServer(t, c.status, c.contentType, c.body, nil)
		_, err := (&ZoneChecker{Cfg: cfConfig(srv), Rest: NewRestClient()}).GetZoneInfo(context.Background())
		var ce *CloudflareZoneCheckError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), c.wantContains) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// 4xx / 5xx are RestTemplate exceptions
	for _, st := range []int{401, 403, 404, 500} {
		srv, _ := fakeServer(t, st, "application/json", `{"success":false}`, nil)
		_, err := (&ZoneChecker{Cfg: cfConfig(srv), Rest: NewRestClient()}).GetZoneInfo(context.Background())
		var he *HTTPStatusError
		if !errors.As(err, &he) || he.Status != st {
			t.Errorf("status %d: %v", st, err)
		}
	}
}

// --- AcmeServerCheckerImplTest -------------------------------------------------------------

func acmeServerConfig(srv *httptest.Server, suffix string) *Config {
	return &Config{ServerURL: srv.URL + "/v2/DV90" + suffix}
}

func TestGetDirectoryInfoParsesTheDirectory(t *testing.T) {
	srv, got := fakeServer(t, 200, "application/json", `{"newAccount":"https://acme.zerossl.com/v2/DV90/newAccount","newOrder":"https://acme.zerossl.com/v2/DV90/newOrder","meta":{"termsOfService":"https://zerossl.com/terms"}}`, nil)
	cfg := acmeServerConfig(srv, "")
	info, err := (&ServerChecker{Cfg: cfg, Rest: NewRestClient()}).GetDirectoryInfo(context.Background())
	mustNoErr(t, err)
	if info.ServerURL != cfg.ServerURL || info.NewAccountURL != "https://acme.zerossl.com/v2/DV90/newAccount" ||
		info.NewOrderURL != "https://acme.zerossl.com/v2/DV90/newOrder" || info.TermsOfServiceURL == nil || *info.TermsOfServiceURL != "https://zerossl.com/terms" {
		t.Errorf("info %+v", info)
	}
	if got.method != "GET" || got.path != "/v2/DV90/directory" || got.accept != "application/json" || got.auth != "" {
		t.Errorf("request %+v", got)
	}
}

func TestGetDirectoryInfoTrimsEveryTrailingSlash(t *testing.T) {
	for _, suffix := range []string{"/", "///"} {
		srv, got := fakeServer(t, 200, "application/json", `{"newAccount":"a","newOrder":"o"}`, nil)
		info, err := (&ServerChecker{Cfg: acmeServerConfig(srv, suffix), Rest: NewRestClient()}).GetDirectoryInfo(context.Background())
		mustNoErr(t, err)
		if got.path != "/v2/DV90/directory" || info.TermsOfServiceURL != nil {
			t.Errorf("suffix %q: path %q info %+v", suffix, got.path, info)
		}
		// the server URL of the result is the configured one, untouched
		if !strings.HasSuffix(info.ServerURL, "/v2/DV90"+suffix) {
			t.Errorf("serverUrl %q", info.ServerURL)
		}
	}
}

func TestGetDirectoryInfoTermsOfServiceEdgeCases(t *testing.T) {
	for name, body := range map[string]string{
		"no meta":          `{"newAccount":"a","newOrder":"o"}`,
		"meta null":        `{"newAccount":"a","newOrder":"o","meta":null}`,
		"meta not object":  `{"newAccount":"a","newOrder":"o","meta":"x"}`,
		"tos not a string": `{"newAccount":"a","newOrder":"o","meta":{"termsOfService":3}}`,
		"meta empty":       `{"newAccount":"a","newOrder":"o","meta":{}}`,
	} {
		srv, _ := fakeServer(t, 200, "application/json", body, nil)
		info, err := (&ServerChecker{Cfg: acmeServerConfig(srv, ""), Rest: NewRestClient()}).GetDirectoryInfo(context.Background())
		mustNoErr(t, err)
		if info.TermsOfServiceURL != nil {
			t.Errorf("%s: %v", name, *info.TermsOfServiceURL)
		}
	}
}

func TestGetDirectoryInfoFailures(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"empty body", ``, "empty body"},
		{"newAccount missing", `{"newOrder":"o"}`, "missing 'newAccount' field"},
		{"newAccount not a string", `{"newAccount":1,"newOrder":"o"}`, "missing 'newAccount' field"},
		{"newOrder missing", `{"newAccount":"a"}`, "missing 'newOrder' field"},
		{"both missing", `{}`, "missing 'newAccount' field"},
	}
	for _, c := range cases {
		srv, _ := fakeServer(t, 200, "application/json", c.body, nil)
		_, err := (&ServerChecker{Cfg: acmeServerConfig(srv, ""), Rest: NewRestClient()}).GetDirectoryInfo(context.Background())
		var ce *ServerCheckError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "/v2/DV90/directory") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	srv, _ := fakeServer(t, 500, "application/json", `{"type":"serverInternal"}`, nil)
	_, err := (&ServerChecker{Cfg: acmeServerConfig(srv, ""), Rest: NewRestClient()}).GetDirectoryInfo(context.Background())
	var he *HTTPStatusError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("500: %v", err)
	}
}

// --- RestClient (RestTemplate.exchange with Map::class.java) --------------------------------

func TestRestClientBodyExtraction(t *testing.T) {
	for _, c := range []struct {
		name, ct, body string
		status         int
		wantErr        bool
		wantNilBody    bool
	}{
		{"json", "application/json", `{"a":1}`, 200, false, false},
		{"json with charset", "application/json; charset=utf-8", `{"a":1}`, 200, false, false},
		{"problem+json", "application/problem+json", `{"a":1}`, 200, false, false},
		{"vendor+json", "application/vnd.api+json", `{"a":1}`, 200, false, false},
		{"upper case type", "Application/JSON", `{"a":1}`, 200, false, false},
		{"text/plain", "text/plain", `{"a":1}`, 200, true, false},
		{"xml", "application/xml", `<a/>`, 200, true, false},
		{"no content type", "", `{"a":1}`, 200, true, false},
		{"json null", "application/json", `null`, 200, false, true},
		{"empty", "application/json", ``, 200, false, true},
		{"empty with wrong type", "text/plain", ``, 200, false, true},
		{"204", "", ``, 204, false, true},
		{"json array", "application/json", `[]`, 200, true, false},
		{"json string", "application/json", `"x"`, 200, true, false},
		{"broken json", "application/json", `{"a":`, 200, true, false},
		{"trailing garbage", "application/json", `{"a":1} x`, 200, false, false}, // Jackson reads the first value
	} {
		srv, _ := fakeServer(t, c.status, c.ct, c.body, nil)
		res, err := NewRestClient().Exchange(context.Background(), "GET", srv.URL, nil, nil)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err %v", c.name, err)
			continue
		}
		if err == nil && (res.Body == nil) != c.wantNilBody {
			t.Errorf("%s: body %v", c.name, res.Body)
		}
	}
}

func TestRestClientRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/final", http.StatusFound) })
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"method":"`+r.Method+`"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := NewRestClient().Exchange(context.Background(), "GET", srv.URL+"/start", nil, nil)
	mustNoErr(t, err)
	if res.Status != 200 || res.Body["method"] != "GET" {
		t.Errorf("GET: %+v", res)
	}
	// HttpURLConnection only follows redirects of GET requests
	res, err = NewRestClient().Exchange(context.Background(), "POST", srv.URL+"/start", nil, []byte(`{}`))
	mustNoErr(t, err)
	if res.Status != 302 {
		t.Errorf("POST: %+v", res)
	}
}
