package httpx

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"agora/internal/auth"
	"agora/internal/jsonjava"
)

// UserLookup resolves the JWT subject into a user (LoginUseCase.findUser).
// It returns nil for unknown users.
type UserLookup func(r *http.Request, userID string) (*User, error)

// Options configures the server.
type Options struct {
	AllowedOrigins []string
	JWT            *auth.JWT
	IPHasher       *auth.IPHasher
	Users          UserLookup
	Rules          []Rule
	// ETagPaths are the exact paths handled by ShallowEtagHeaderFilter.
	ETagPaths []string
	Now       func() time.Time
	Logger    *slog.Logger
	// OnPanic reports unexpected errors (Sentry).
	OnPanic func(r *http.Request, v any, stack []byte)
}

// Server is the Spring-compatible HTTP pipeline.
type Server struct {
	opts      Options
	router    router
	rules     []Rule
	etagPaths map[string]bool
	ipHasher  *auth.IPHasher
	cors      corsConfig
	log       *slog.Logger
}

// NewServer creates the pipeline; routes are added with Handle.
func NewServer(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Rules == nil {
		o.Rules = AgoraRules()
	}
	s := &Server{opts: o, ipHasher: o.IPHasher, log: o.Logger}
	s.rules = append([]Rule(nil), o.Rules...)
	for i := range s.rules {
		s.rules[i].compile()
	}
	s.etagPaths = map[string]bool{}
	for _, p := range o.ETagPaths {
		s.etagPaths[p] = true
	}
	s.cors = newCORSConfig(o.AllowedOrigins)
	// BasicErrorController mapped on /error for every method: without error
	// attributes it answers 500 with status 999 "None" and no path.
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		s.Handle(m, "/error", func(c *Ctx) *Response {
			return &Response{Status: 500, Kind: BodyValue, Value: directErrorBody{
				Timestamp: jsonjava.EpochMillis(c.Now.UnixMilli()), Status: 999, Error: "None",
			}}
		})
	}
	return s
}

// directErrorBody is the /error output when called directly.
type directErrorBody struct {
	Timestamp jsonjava.EpochMillis `json:"timestamp"`
	Status    int                  `json:"status"`
	Error     string               `json:"error"`
}

// JavaName is the XML root element.
func (directErrorBody) JavaName() string { return "Map" }

// Handle registers a route (registration order matters for the Allow header).
func (s *Server) Handle(method, pattern string, h HandlerFunc) {
	s.router.add(&Route{Method: method, Pattern: pattern, Handler: h})
}

// GET, POST, PUT, DELETE are shorthands.
func (s *Server) GET(p string, h HandlerFunc)    { s.Handle("GET", p, h) }
func (s *Server) POST(p string, h HandlerFunc)   { s.Handle("POST", p, h) }
func (s *Server) PUT(p string, h HandlerFunc)    { s.Handle("PUT", p, h) }
func (s *Server) DELETE(p string, h HandlerFunc) { s.Handle("DELETE", p, h) }

// responseState accumulates headers in Tomcat's write order.
type responseState struct {
	headers []Header
	// headersWritten marks that Spring Security's HeaderWriterFilter applies.
	securityHeaders bool
}

func (st *responseState) add(name, value string) {
	st.headers = append(st.headers, Header{name, value})
}
func (st *responseState) has(name string) bool {
	for _, h := range st.headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

// ServeHTTP implements the Spring filter chain + DispatcherServlet.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := s.opts.Now()
	rawURI := r.RequestURI
	rawPath := rawURI
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		rawPath = rawPath[:i]
	}
	st := &responseState{}
	// CorsFilter / CorsInterceptor: the "/**" CORS mapping always adds Vary.
	st.add("Vary", "Origin")
	st.add("Vary", "Access-Control-Request-Method")
	st.add("Vary", "Access-Control-Request-Headers")

	// 0. Tomcat request-line validation (HTML error pages, no Spring headers).
	if s.tomcatReject(w, r) {
		return
	}

	// 1. StrictHttpFirewall (before HeaderWriterFilter: no security headers).
	if firewallReject(r.Method, rawURI) {
		s.writeSpringError(w, r, st, 400, rawPath, now)
		return
	}
	st.securityHeaders = true

	// 2. CORS (Spring Security CorsFilter with the MVC configuration).
	if done := s.handleCORS(w, r, st); done {
		return
	}

	// 3. AuthenticationTokenFilter
	user, authErr := s.authenticate(r)
	if authErr != nil {
		s.log.Error("uncaught exception in AuthenticationTokenFilter", "err", authErr)
		s.writeSpringError(w, r, st, 500, rawPath, now)
		return
	}

	path := r.URL.Path

	// 4. AuthorizationFilter
	switch decide(s.rules, r.Method, path, user) {
	case denyUnauthenticated:
		s.log.Warn(fmt.Sprintf("Authentication exception on resource '%s'", rawPath))
		if savesRequest(r) {
			// HttpSessionRequestCache saved the request → new HTTP session.
			st.add("Set-Cookie", "JSESSIONID="+newSessionID()+"; Path=/; HttpOnly")
		}
		s.writeSpringError(w, r, st, 401, rawPath, now)
		return
	case denyForbidden:
		s.writeSpringError(w, r, st, 403, rawPath, now)
		return
	}

	// 5. DispatcherServlet handler lookup.
	m := s.router.match(r.Method, path)
	if !m.pathMatched {
		s.writeSpringError(w, r, st, 404, rawPath, now)
		return
	}
	if m.route == nil {
		if r.Method == "OPTIONS" {
			st.add("Allow", strings.Join(optionsAllow(m.allowed), ","))
			st.add("Accept-Patch", "")
			s.finish(w, r, st, &Response{Status: 200, Kind: BodyNone}, now, rawPath)
			return
		}
		st.add("Allow", strings.Join(m.allowed, ","))
		s.writeSpringError(w, r, st, 405, rawPath, now)
		return
	}

	c := &Ctx{R: r, Vars: m.vars, User: user, Now: now, srv: s}
	resp := s.invoke(c, m.route.Handler)
	s.finish(w, r, st, resp, now, rawPath)
}

// invoke runs a handler, translating panics like Spring's exception resolvers.
func (s *Server) invoke(c *Ctx, h HandlerFunc) (resp *Response) {
	defer func() {
		if v := recover(); v != nil {
			resp = s.panicToResponse(c.R, v)
		}
	}()
	resp = h(c)
	if resp == nil {
		// a Kotlin handler returning null ResponseEntity → 200 empty
		resp = Empty(200)
	}
	return resp
}

type errorResponse struct {
	status  int
	headers []Header
}

func (s *Server) panicToResponse(r *http.Request, v any) *Response {
	var se *SpringError
	var ae *AdviceError
	if err, ok := v.(error); ok {
		if errors.As(err, &se) {
			return &Response{Status: se.Status, Kind: BodyValue, Value: errorMarker{se.Status}, Headers: se.Headers}
		}
		if errors.As(err, &ae) {
			return JSON(ae.Status, AdviceBody{Title: ae.Title})
		}
	}
	stack := debug.Stack()
	s.log.Error("unhandled exception", "path", r.URL.Path, "err", fmt.Sprint(v), "stack", string(stack))
	if s.opts.OnPanic != nil {
		s.opts.OnPanic(r, v, stack)
	}
	return &Response{Status: 500, Kind: BodyValue, Value: errorMarker{500}}
}

// errorMarker asks finish() to render the Spring Boot error body.
type errorMarker struct{ status int }

// negotiate reproduces the ParameterContentNegotiationStrategy ("mediaType").
// Returns "json", "xml" or "" when not acceptable (→ 406).
func negotiate(r *http.Request) string {
	v, ok := r.URL.Query()["mediaType"]
	if !ok || len(v) == 0 {
		return "json"
	}
	switch strings.ToLower(v[0]) {
	case "json":
		return "json"
	case "xml":
		return "xml"
	case "":
		return "json"
	}
	return ""
}

func (s *Server) writeSpringError(w http.ResponseWriter, r *http.Request, st *responseState, status int, rawPath string, now time.Time) {
	s.finish(w, r, st, &Response{Status: status, Kind: BodyValue, Value: errorMarker{status}}, now, rawPath)
}

// finish serializes the response body, applies the ETag filter and the
// Spring Security header writers, then writes everything.
func (s *Server) finish(w http.ResponseWriter, r *http.Request, st *responseState, resp *Response, now time.Time, rawPath string) {
	for _, h := range resp.Headers {
		st.add(h.Name, h.Value)
	}
	status := resp.Status
	var body []byte
	contentType := ""
	format := negotiate(r)

	if em, ok := resp.Value.(errorMarker); ok {
		body, contentType = s.renderError(em.status, rawPath, now, format)
	} else {
		switch resp.Kind {
		case BodyNone:
		case BodyBytes:
			// preset Content-Type: Spring skips content negotiation
			body = resp.Bytes
			contentType = resp.ContentType
		case BodyValue, BodyString:
			if format == "" {
				// HttpMediaTypeNotAcceptableException after the handler ran.
				status = 406
				st.add("Accept", "application/xml, application/json")
				body = nil
				break
			}
			switch resp.Kind {
			case BodyValue:
				if format == "xml" {
					body = xmlMarshal(resp.Value)
					contentType = "application/xml;charset=UTF-8"
				} else {
					if resp.PrecomputedJSON != nil {
						body = resp.PrecomputedJSON
					} else {
						body = jsonjava.Marshal(resp.Value)
					}
					contentType = "application/json"
				}
			case BodyString:
				body = []byte(resp.Str)
				if format == "xml" {
					contentType = "application/xml;charset=UTF-8"
				} else {
					contentType = "application/json"
				}
			}
		}
	}

	// ShallowEtagHeaderFilter (exact paths, GET, 2xx, no "no-store").
	if s.etagPaths[r.URL.Path] && r.Method == "GET" && status >= 200 && status < 300 && !cacheControlNoStore(st) {
		sum := md5.Sum(body)
		etag := `"0` + hex.EncodeToString(sum[:]) + `"`
		st.add("ETag", etag)
		if ifNoneMatchMatches(r.Header.Values("If-None-Match"), etag) {
			status = http.StatusNotModified
			body = nil
			contentType = ""
		} else if body != nil {
			st.add("Content-Length", strconv.Itoa(len(body)))
		}
	}

	if body != nil && resp.Kind != BodyBytes && !st.has("Content-Disposition") && rfdContentDisposition(rawPath, status) {
		st.add("Content-Disposition", "inline;filename=f.txt")
	}
	if st.securityHeaders {
		s.addSecurityHeaders(st, status)
	}
	if contentType != "" {
		st.add("Content-Type", contentType)
	}
	h := w.Header()
	for _, hd := range st.headers {
		// keep Tomcat's exact header name casing
		h[hd.Name] = append(h[hd.Name], hd.Value)
	}
	if status == 406 && body == nil {
		h["Content-Length"] = []string{"0"}
	}
	if contentType == "" {
		// never let net/http sniff a Content-Type Tomcat would not send
		h["Content-Type"] = nil
	}
	w.WriteHeader(status)
	if r.Method != "HEAD" && len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func cacheControlNoStore(st *responseState) bool {
	for _, h := range st.headers {
		if strings.EqualFold(h.Name, "Cache-Control") && strings.Contains(h.Value, "no-store") {
			return true
		}
	}
	return false
}

// ifNoneMatchMatches reproduces ServletWebRequest.validateIfNoneMatch for GET.
func ifNoneMatchMatches(values []string, etag string) bool {
	strip := func(t string) string {
		t = strings.TrimSpace(t)
		return strings.TrimPrefix(t, "W/")
	}
	target := strip(etag)
	for _, v := range values {
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "*" {
				return true
			}
			if tok != "" && strip(tok) == target {
				return true
			}
		}
	}
	return false
}

// addSecurityHeaders reproduces Spring Security's default HeaderWriters
// (written on commit, after the controller's own headers).
func (s *Server) addSecurityHeaders(st *responseState, status int) {
	st.add("X-Content-Type-Options", "nosniff")
	st.add("X-XSS-Protection", "0")
	if !(st.has("Cache-Control") || st.has("Expires") || st.has("Pragma") || status == http.StatusNotModified) {
		st.add("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
		st.add("Pragma", "no-cache")
		st.add("Expires", "0")
	}
	st.add("X-Frame-Options", "DENY")
}

func (s *Server) renderError(status int, rawPath string, now time.Time, format string) ([]byte, string) {
	eb := newErrorBody(status, rawPath, now)
	if format == "xml" {
		return xmlMarshal(eb), "application/xml;charset=UTF-8"
	}
	if format == "" {
		return nil, ""
	}
	return jsonjava.Marshal(eb), "application/json"
}

// authenticate reproduces AuthenticationTokenFilter.extractJwt + validation.
func (s *Server) authenticate(r *http.Request) (*User, error) {
	var token string
	if vals, ok := r.Header["Authorization"]; ok && len(vals) > 0 {
		t, ok := auth.ExtractBearer(vals[0])
		if !ok {
			return nil, nil
		}
		token = t
	} else if c := firstCookie(r, auth.JWTCookieName); c != nil {
		token = *c
	} else {
		return nil, nil
	}
	userID, err := s.opts.JWT.Parse(token)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidJWT) {
			s.log.Error("JwtException")
			return nil, nil
		}
		return nil, err
	}
	if s.opts.Users == nil {
		return nil, nil
	}
	return s.opts.Users(r, userID)
}

func firstCookie(r *http.Request, name string) *string {
	for _, c := range r.Cookies() {
		if c.Name == name {
			v := c.Value
			return &v
		}
	}
	return nil
}

// savesRequest reproduces HttpSessionRequestCache's default matcher (CSRF
// disabled): not favicon, negotiated type not JSON, not XMLHttpRequest.
func savesRequest(r *http.Request) bool {
	if strings.Contains(r.URL.Path, "favicon.") {
		return false
	}
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return false
	}
	return negotiate(r) != "json"
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// rfdContentDisposition reproduces AbstractMessageConverterMethodProcessor.
// addContentDispositionHeader: when the last path segment of the raw request
// URI has an extension that is not "safe", Spring adds
// "Content-Disposition: inline;filename=f.txt" to bodies written by message
// converters (statuses 2xx and >= 400).
func rfdContentDisposition(rawPath string, status int) bool {
	if status < 200 || (status > 299 && status < 400) {
		return false
	}
	filename := rawPath[strings.LastIndexByte(rawPath, '/')+1:]
	pathParams := ""
	if i := strings.IndexByte(filename, ';'); i >= 0 {
		filename, pathParams = filename[:i], filename[i:]
	}
	unsafe := func(name string) bool {
		if dec, err := url.PathUnescape(name); err == nil {
			name = dec
		}
		dot := strings.LastIndexByte(name, '.')
		if dot < 0 || strings.LastIndexByte(name, '/') > dot {
			return false
		}
		ext := strings.ToLower(name[dot+1:])
		if strings.TrimSpace(ext) == "" {
			return false
		}
		return !rfdSafeExtensions[ext]
	}
	return unsafe(filename) || unsafe(pathParams)
}
