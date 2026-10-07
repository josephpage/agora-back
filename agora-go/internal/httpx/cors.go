package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

// corsConfig is the global CorsRegistration("/**") of CrossOriginConfig:
// allowedOrigins from ALLOWED_ORIGINS, methods GET/HEAD/POST/PUT/DELETE,
// allowedHeaders "*", maxAge 1800, no credentials, no exposed headers.
type corsConfig struct {
	origins []string // trailing slash trimmed
	any     bool
}

var corsAllowedMethods = []string{"GET", "HEAD", "POST", "PUT", "DELETE"}

func newCORSConfig(origins []string) corsConfig {
	c := corsConfig{}
	for _, o := range origins {
		if o == "*" {
			c.any = true
		}
		c.origins = append(c.origins, strings.TrimSuffix(o, "/"))
	}
	return c
}

// checkOrigin reproduces CorsConfiguration.checkOrigin.
func (c corsConfig) checkOrigin(origin string) (string, bool) {
	if strings.TrimSpace(origin) == "" {
		return "", false
	}
	if c.any {
		return "*", true
	}
	o := strings.TrimSuffix(origin, "/")
	for _, a := range c.origins {
		if strings.EqualFold(o, a) {
			return origin, true
		}
	}
	return "", false
}

func methodAllowed(m string) bool {
	for _, a := range corsAllowedMethods {
		if a == m {
			return true
		}
	}
	return false
}

// isSameOrigin reproduces CorsUtils.isCorsRequest's same-origin test against
// the request's scheme/host/port (Host header as seen behind nginx).
func isSameOrigin(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !strings.EqualFold(u.Scheme, scheme) {
		return false
	}
	host, port := splitHostPortDefault(r.Host, scheme)
	oh, op := splitHostPortDefault(u.Host, u.Scheme)
	return strings.EqualFold(host, oh) && port == op
}

func splitHostPortDefault(hp, scheme string) (string, string) {
	host, port := hp, ""
	if i := strings.LastIndexByte(hp, ':'); i >= 0 && !strings.Contains(hp[i:], "]") {
		host, port = hp[:i], hp[i+1:]
	}
	if port == "" {
		if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port
}

// corsRequest reproduces CorsUtils.isCorsRequest. An Origin value the
// firewall rejects counts as cross-origin (reading it is what fails).
func corsRequest(r *http.Request) (string, bool) {
	vals, ok := r.Header["Origin"]
	if !ok || len(vals) == 0 {
		return "", false
	}
	if !headerValueOK(vals[0]) {
		return vals[0], true
	}
	return vals[0], !isSameOrigin(r, vals[0])
}

// isPreflight reproduces CorsUtils.isPreFlightRequest (header presence only:
// an empty Access-Control-Request-Method still makes a preflight).
func isPreflight(r *http.Request) bool {
	_, has := r.Header["Access-Control-Request-Method"]
	return r.Method == "OPTIONS" && has
}

// corsHandlerFound reports whether HandlerMappingIntrospector finds a CORS
// configuration for the request at the Spring Security CorsFilter: a mapped
// handler for (path, method) — any method for a preflight — or the springdoc
// resource handler.
func (s *Server) corsHandlerFound(r *http.Request, m matchResult, preflight bool) bool {
	if s.isResourcePath(r.URL.Path) {
		return true
	}
	if preflight {
		return m.pathMatched
	}
	return m.route != nil || r.Method == "OPTIONS" && m.pathMatched
}

// handleCORS implements Spring Security's CorsFilter + DefaultCorsProcessor.
// When no handler is found, the request goes on without CORS processing and
// the MVC CorsInterceptor of the /error handler processes it while the error
// is rendered (deferred, see writeSpringError). It returns true when the
// response has been fully written.
func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request, st *responseState, m matchResult) bool {
	origin, cors := corsRequest(r)
	if !cors {
		return false
	}
	// isCorsRequest / isPreFlightRequest read Origin and Access-Control-Request-Method
	if !headerValueOK(origin) || r.Method == "OPTIONS" && !firstHeaderOK(r, "Access-Control-Request-Method") {
		s.writeRejectedEmpty(w, r, st)
		return true
	}
	preflight := isPreflight(r)
	if !s.corsHandlerFound(r, m, preflight) {
		if preflight {
			s.rejectCORS(w, r, st)
			return true
		}
		st.corsDeferred, st.corsOrigin = true, origin
		return false
	}
	// handleInternal starts with ServletServerHttpRequest.getHeaders()
	switch springHeadersStatus(r) {
	case 400:
		s.writeRejectedEmpty(w, r, st)
		return true
	case 500:
		s.writeTomcatPage(w, r, st, 500)
		return true
	}
	allowOrigin, ok := s.cors.checkOrigin(origin)
	if !ok {
		s.rejectCORS(w, r, st)
		return true
	}
	method := r.Method
	if preflight {
		method = r.Header.Get("Access-Control-Request-Method")
	}
	if !methodAllowed(method) {
		s.rejectCORS(w, r, st)
		return true
	}
	var allowHeaders []string
	if preflight {
		for _, v := range r.Header.Values("Access-Control-Request-Headers") {
			for _, h := range strings.Split(v, ",") {
				if t := strings.TrimSpace(h); t != "" {
					allowHeaders = append(allowHeaders, t)
				}
			}
		}
	}
	st.add("Access-Control-Allow-Origin", allowOrigin)
	if preflight {
		st.add("Access-Control-Allow-Methods", strings.Join(corsAllowedMethods, ","))
		if len(allowHeaders) > 0 {
			st.add("Access-Control-Allow-Headers", strings.Join(allowHeaders, ", "))
		}
		st.add("Access-Control-Max-Age", "1800")
		s.finish(w, r, st, Empty(200), s.opts.Now(), "")
		return true
	}
	return false
}

// firstHeaderOK checks the first value of a header (getHeader).
func firstHeaderOK(r *http.Request, name string) bool {
	vs := r.Header[http.CanonicalHeaderKey(name)]
	return len(vs) == 0 || headerValueOK(vs[0])
}

// isResourcePath matches springdoc's resource handler mapping (/swagger-ui*/**).
func (s *Server) isResourcePath(p string) bool {
	return strings.HasPrefix(p, "/swagger-ui")
}

func (s *Server) rejectCORS(w http.ResponseWriter, r *http.Request, st *responseState) {
	s.finish(w, r, st, Bytes(403, "", []byte("Invalid CORS request")), s.opts.Now(), "")
}

// writeRejectedEmpty writes the answer to a firewall rejection raised while a
// cross-origin request is processed: the error dispatch is rejected again, so
// the 400 has no body.
func (s *Server) writeRejectedEmpty(w http.ResponseWriter, r *http.Request, st *responseState) {
	s.finish(w, r, st, Empty(400), s.opts.Now(), "")
}

// writeTomcatPage writes Tomcat's own error page: the exception was raised
// again while the error page was rendered.
func (s *Server) writeTomcatPage(w http.ResponseWriter, r *http.Request, st *responseState, status int) {
	page := tomcatErrorPage(status, ReasonPhrase(status))
	s.finish(w, r, st, Bytes(status, "text/html;charset=utf-8", []byte(page)).With("Content-Language", "en"), s.opts.Now(), "")
}
