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

// handleCORS implements Spring Security's CorsFilter + DefaultCorsProcessor.
// Captured behaviour: the global "/**" configuration applies to every actual
// cross-origin request (even unknown paths); a preflight is only accepted
// when some handler matches the path, otherwise it is rejected (403).
// It returns true when the response has been fully written.
func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request, st *responseState) bool {
	origin, hasOrigin := r.Header["Origin"]
	if !hasOrigin || isSameOrigin(r, origin[0]) {
		return false
	}
	preflight := r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != ""
	if preflight && !s.router.match("OPTIONS", r.URL.Path).pathMatched && !s.isResourcePath(r.URL.Path) {
		s.rejectCORS(w, r, st)
		return true
	}
	allowOrigin, ok := s.cors.checkOrigin(origin[0])
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

// isResourcePath matches springdoc's resource handler mapping (/swagger-ui*/**).
func (s *Server) isResourcePath(p string) bool {
	return strings.HasPrefix(p, "/swagger-ui")
}

func (s *Server) rejectCORS(w http.ResponseWriter, r *http.Request, st *responseState) {
	s.finish(w, r, st, Bytes(403, "", []byte("Invalid CORS request")), s.opts.Now(), "")
}
