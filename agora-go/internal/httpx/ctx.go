package httpx

import (
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agora/internal/auth"
	"agora/internal/javacompat"
	"agora/internal/jsonjava"
)

// Authorities (UserAuthorizationJWT).
const (
	AuthViewConsultation            = "VIEW_CONSULTATION"
	AuthViewUnpublishedConsultation = "VIEW_UNPUBLISHED_CONSULTATION"
	AuthAnswerConsultation          = "ANSWER_CONSULTATION"
	AuthViewQag                     = "VIEW_QAG"
	AuthSupportQag                  = "SUPPORT_QAG"
	AuthFeedbackQagResponse         = "FEEDBACK_QAG_RESPONSE"
	AuthAddQag                      = "ADD_QAG"
	AuthModerateQag                 = "MODERATE_QAG"
	AuthAdminAPIs                   = "ADMIN_APIS"
)

// User is the authenticated principal (UserInfoJwt).
type User struct {
	ID          string
	IsBanned    bool
	Authorities []string
}

// Has reports whether the user holds the authority.
func (u *User) Has(authority string) bool {
	if u == nil {
		return false
	}
	for _, a := range u.Authorities {
		if a == authority {
			return true
		}
	}
	return false
}

// Ctx is the per-request context handed to handlers.
type Ctx struct {
	R    *http.Request
	Vars map[string]string
	User *User
	// Now is the request start time (handlers should prefer the clock).
	Now time.Time

	srv   *Server
	query url.Values
}

// Context returns the request context.
func (c *Ctx) Context() context.Context { return c.R.Context() }

// PathVar returns a path variable (always present when the route matched).
func (c *Ctx) PathVar(name string) string { return c.Vars[name] }

// Header returns the first value of a request header (Tomcat getHeader).
func (c *Ctx) Header(name string) (string, bool) {
	v, ok := c.R.Header[http.CanonicalHeaderKey(name)]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// RequiredHeader is @RequestHeader("name") on a non-null String: absent → 400.
func (c *Ctx) RequiredHeader(name string) string {
	v, ok := c.Header(name)
	if !ok {
		panic(&SpringError{Status: 400, Cause: "MissingRequestHeaderException: " + name})
	}
	return v
}

// OptionalHeader is @RequestHeader(name, required=false) String?.
func (c *Ctx) OptionalHeader(name string) *string {
	v, ok := c.Header(name)
	if !ok {
		return nil
	}
	return &v
}

func (c *Ctx) params() url.Values {
	if c.query == nil {
		q, err := url.ParseQuery(c.R.URL.RawQuery)
		if err != nil {
			// Tomcat ignores malformed pairs; keep what parsed.
			q = lenientParseQuery(c.R.URL.RawQuery)
		}
		c.query = q
	}
	return c.query
}

func lenientParseQuery(raw string) url.Values {
	out := url.Values{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		kk, err1 := url.QueryUnescape(k)
		vv, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil {
			continue
		}
		out[kk] = append(out[kk], vv)
	}
	return out
}

// Param returns the first value of a query parameter (getParameter).
func (c *Ctx) Param(name string) (string, bool) {
	v, ok := c.params()[name]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// RequiredParam is @RequestParam("name") on a non-null String: absent → 400.
func (c *Ctx) RequiredParam(name string) string {
	v, ok := c.Param(name)
	if !ok {
		panic(&SpringError{Status: 400, Cause: "MissingServletRequestParameterException: " + name})
	}
	return v
}

// OptionalParam is @RequestParam(name) on a nullable Kotlin type (String?).
func (c *Ctx) OptionalParam(name string) *string {
	v, ok := c.Param(name)
	if !ok {
		return nil
	}
	return &v
}

// ParamDefault is @RequestParam(name, defaultValue = def): absent or empty → def.
func (c *Ctx) ParamDefault(name, def string) string {
	v, ok := c.Param(name)
	if !ok || v == "" {
		return def
	}
	return v
}

// ParamList is @RequestParam List<String>? : every value, each split on ','
// (Spring's StringToCollectionConverter on single values), nil if absent.
func (c *Ctx) ParamList(name string) []string {
	vals, ok := c.params()[name]
	if !ok {
		return nil
	}
	if len(vals) == 1 {
		// a single value is split on commas and each element trimmed
		parts := strings.Split(vals[0], ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		if len(out) == 1 && out[0] == "" {
			return []string{}
		}
		return out
	}
	return append([]string(nil), vals...)
}

// SpringBoolParam converts a @RequestParam Boolean like StringToBooleanConverter
// (true/on/yes/1, false/off/no/0); anything else → 400.
func SpringBoolParam(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "on", "yes", "1":
		return true
	case "false", "off", "no", "0":
		return false
	}
	panic(&SpringError{Status: 400, Cause: "MethodArgumentTypeMismatchException: boolean " + v})
}

// SpringIntPathVar converts an Int @PathVariable/@RequestParam like Spring's
// StringToNumberConverterFactory (NumberUtils.parseNumber, trimmed): invalid → 400.
func SpringIntPathVar(v string) int {
	n, ok := javacompat.KotlinToIntOrNull(strings.TrimSpace(v))
	if !ok {
		// Spring's NumberUtils also accepts hex "0x.." / "#.." prefixes
		if i, ok2 := parseSpringHexInt(strings.TrimSpace(v)); ok2 {
			return i
		}
		panic(&SpringError{Status: 400, Cause: "MethodArgumentTypeMismatchException: int " + v})
	}
	return n
}

func parseSpringHexInt(s string) (int, bool) {
	neg := strings.HasPrefix(s, "-")
	t := strings.TrimPrefix(s, "-")
	var digits string
	switch {
	case strings.HasPrefix(t, "0x"), strings.HasPrefix(t, "0X"):
		digits = t[2:]
	case strings.HasPrefix(t, "#"):
		digits = t[1:]
	default:
		return 0, false
	}
	if digits == "" {
		return 0, false
	}
	var v int64
	for _, r := range digits {
		d := javacompat.JavaCharacterDigit(r, 16)
		if d < 0 {
			return 0, false
		}
		v = v*16 + int64(d)
		if v > 1<<31 {
			return 0, false
		}
	}
	if neg {
		v = -v
	}
	if v > 1<<31-1 || v < -(1<<31) {
		return 0, false
	}
	return int(v), true
}

// UserID is authentificationHelper.getUserId()!! : the Kotlin `!!` throws an
// NPE (→ 500) when the request is anonymous. Only reachable on routes that
// are permitAll but read the user id.
func (c *Ctx) UserID() string {
	if c.User == nil {
		panic("NullPointerException: authentificationHelper.getUserId()!!")
	}
	return c.User.ID
}

// OptionalUserID is authentificationHelper.getUserId() (nullable).
func (c *Ctx) OptionalUserID() *string {
	if c.User == nil {
		return nil
	}
	id := c.User.ID
	return &id
}

// CanViewUnpublishedConsultations is AuthentificationHelper.canViewUnpublishedConsultations.
func (c *Ctx) CanViewUnpublishedConsultations() bool {
	return c.User.Has(AuthViewUnpublishedConsultation)
}

// IPHash is IpAddressUtils.retrieveIpAddressHash(request) (misconfiguration → 500).
func (c *Ctx) IPHash() string {
	h, err := c.srv.ipHasher.Hash(c.ClientIP())
	if err != nil {
		panic(err)
	}
	return h
}

// ClientIP is IpAddressUtils.retrieveIpAddress(request).
func (c *Ctx) ClientIP() string {
	xff, _ := c.Header("X-Forwarded-For")
	xra, _ := c.Header("X-Remote-Address")
	host, _, err := net.SplitHostPort(c.R.RemoteAddr)
	if err != nil {
		host = c.R.RemoteAddr
	}
	return auth.ClientIP(xff, xra, host)
}

// isJSONOrXMLContentType tells whether a request body can be read by the
// Jackson JSON or XML converters (application/json, application/*+json,
// application/xml, text/xml, application/*+xml).
func isReadableContentType(ct string) (json bool, xml bool) {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false, false
	}
	mt = strings.ToLower(mt)
	switch {
	case mt == "application/json" || strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json"):
		return true, false
	case mt == "application/xml" || mt == "text/xml" || strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+xml"):
		return false, true
	}
	return false, false
}

// BindBody is @RequestBody: decodes the request body into v with Jackson
// semantics. Unsupported content type → 415, missing/unreadable body → 400.
func (c *Ctx) BindBody(v any) {
	ct := c.R.Header.Get("Content-Type")
	if ct == "" {
		// Spring assumes application/octet-stream → no converter → 415
		panic(&SpringError{Status: 415, Cause: "no content type", Headers: []Header{{"Accept", acceptForBodies}}})
	}
	isJSON, isXML := isReadableContentType(ct)
	if !isJSON && !isXML {
		panic(&SpringError{Status: 415, Cause: "unsupported content type " + ct, Headers: []Header{{"Accept", acceptForBodies}}})
	}
	body, err := io.ReadAll(io.LimitReader(c.R.Body, 16<<20))
	if err != nil {
		panic(&SpringError{Status: 400, Cause: "body read: " + err.Error()})
	}
	if len(body) == 0 {
		panic(&SpringError{Status: 400, Cause: "Required request body is missing"})
	}
	if isXML {
		if err := xmlDecodeBody(body, v); err != nil {
			panic(&SpringError{Status: 400, Cause: "HttpMessageNotReadableException(xml): " + err.Error()})
		}
		return
	}
	if err := jsonjava.Unmarshal(body, v); err != nil {
		panic(&SpringError{Status: 400, Cause: "HttpMessageNotReadableException: " + err.Error()})
	}
}

// acceptForBodies is the Accept header Spring adds to 415 responses
// (supported media types of the registered converters). Verified by capture.
const acceptForBodies = "application/json, application/*+json, application/xml, text/xml, application/*+xml"
