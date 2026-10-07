package httpx

import (
	"context"
	"errors"
	"io"
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

// Header returns the first value of a request header as the application sees
// it (Tomcat getHeader: ISO-8859-1 decoded, checked by the firewall).
func (c *Ctx) Header(name string) (string, bool) {
	v, ok := c.R.Header[http.CanonicalHeaderKey(name)]
	if !ok || len(v) == 0 {
		return "", false
	}
	if !headerValueOK(v[0]) {
		panic(&headerRejected{name: name})
	}
	return javacompat.Latin1(v[0]), true
}

// requestHeader is @RequestHeader's value: every value of the header (checked
// by the firewall), several values joined with "," (String[] → String).
func (c *Ctx) requestHeader(name string) (string, bool) {
	vs, ok := c.R.Header[http.CanonicalHeaderKey(name)]
	if !ok || len(vs) == 0 {
		return "", false
	}
	for _, v := range vs {
		if !headerValueOK(v) {
			panic(&headerRejected{name: name})
		}
	}
	if len(vs) == 1 {
		return javacompat.Latin1(vs[0]), true
	}
	return javacompat.Latin1(strings.Join(vs, ",")), true
}

// RequiredHeader is @RequestHeader("name") on a non-null String: absent → 400.
func (c *Ctx) RequiredHeader(name string) string {
	v, ok := c.requestHeader(name)
	if !ok {
		panic(&SpringError{Status: 400, Cause: "MissingRequestHeaderException: " + name})
	}
	return v
}

// OptionalHeader is @RequestHeader(name, required=false) String?.
func (c *Ctx) OptionalHeader(name string) *string {
	v, ok := c.requestHeader(name)
	if !ok {
		return nil
	}
	return &v
}

func (c *Ctx) params() url.Values {
	if c.query == nil {
		c.query = tomcatParseQuery(c.R.URL.RawQuery)
	}
	return c.query
}

// tomcatParseQuery reproduces Tomcat's Parameters.processParameters: pairs
// split on '&' then on the first '=', percent-decoded ('+' is a space), a pair
// with a malformed escape or an empty name is skipped, and the bytes are
// decoded as UTF-8 with Java's replacement rules.
func tomcatParseQuery(raw string) url.Values {
	out := url.Values{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		kk, err1 := url.QueryUnescape(k)
		vv, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil || kk == "" {
			continue
		}
		kk = javacompat.DecodeUTF8Java([]byte(kk))
		out[kk] = append(out[kk], javacompat.DecodeUTF8Java([]byte(vv)))
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
// X-Remote-Address is only read (and checked by the firewall) when
// X-Forwarded-For yields no address, like the Kotlin elvis chain.
func (c *Ctx) ClientIP() string {
	host, _, err := net.SplitHostPort(c.R.RemoteAddr)
	if err != nil {
		host = c.R.RemoteAddr
	}
	xff, _ := c.Header("X-Forwarded-For")
	if ip := auth.ClientIP(xff, "", ""); ip != "" {
		return ip
	}
	xra, _ := c.Header("X-Remote-Address")
	return auth.ClientIP("", xra, host)
}

// BindBody is @RequestBody: decodes the request body into v like
// RequestResponseBodyMethodProcessor + MappingJackson2HttpMessageConverter.
//
//   - the request headers are built (ServletServerHttpRequest.getHeaders()):
//     firewall check of every header (400) and Content-Type enrichment (500
//     for a wildcard type without charset);
//   - an invalid Content-Type is a 415 (InvalidMediaTypeException);
//   - no Content-Type means application/octet-stream: 415, or 400 "Required
//     request body is missing" when there is no body either (and for methods
//     other than POST/PUT/PATCH, whatever the content type);
//   - an empty body is a 400; an XML body is refused (400, class C);
//   - the JSON is decoded with the Content-Type charset (UTF-8 by default),
//     Jackson's encoding detection and parsing rules (jsonjava.UnmarshalRequest).
func (c *Ctx) BindBody(v any) {
	r := c.R
	switch springHeadersStatus(r) {
	case 400:
		panic(&headerRejected{name: "(all)"})
	case 500:
		panic(&SpringError{Status: 500, Cause: "IllegalArgumentException: Content-Type cannot contain wildcard type"})
	}
	ct := requestContentType(r)
	var mt mediaType
	if ct != "" {
		var ok bool
		if mt, ok = parseSpringMediaType(ct); !ok {
			panic(&SpringError{Status: 415, Cause: "InvalidMediaTypeException: " + ct, Headers: []Header{{"Accept", acceptForBodies}}})
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		panic(&SpringError{Status: 400, Cause: "body read: " + err.Error()})
	}
	isJSON, isXML := ct != "" && mt.readableJSON(), ct != "" && mt.readableXML()
	if !isJSON && !isXML {
		if !(r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH") || ct == "" && len(body) == 0 {
			panic(&SpringError{Status: 400, Cause: "Required request body is missing"})
		}
		panic(&SpringError{Status: 415, Cause: "unsupported content type " + ct, Headers: []Header{{"Accept", acceptForBodies}}})
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
	charset := mt.charset
	if charset == "" {
		charset = "UTF-8"
	}
	if err := jsonjava.UnmarshalRequest(body, charset, v); err != nil {
		if errors.Is(err, jsonjava.ErrUnsupportedCharset) {
			// a JVM charset Go does not decode (class C: C-CHARSET-EXOTIC)
			panic(&SpringError{Status: 415, Cause: "unsupported charset " + charset, Headers: []Header{{"Accept", acceptForBodies}}})
		}
		panic(&SpringError{Status: 400, Cause: "HttpMessageNotReadableException: " + err.Error()})
	}
}

// acceptForBodies is the Accept header of a 415 for a DTO body: the media
// types of the JSON and XML converters, sorted by specificity (captured).
const acceptForBodies = "application/xml;charset=UTF-8, text/xml;charset=UTF-8, application/json, application/*+xml;charset=UTF-8, application/*+json"
