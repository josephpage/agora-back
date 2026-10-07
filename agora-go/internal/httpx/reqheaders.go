package httpx

import (
	"net/http"

	"agora/internal/javacompat"
)

// Request headers as the Kotlin application sees them.
//
// Tomcat decodes header bytes as ISO-8859-1, and Spring Security's
// StrictHttpFirewall wraps the request so that every header value READ by the
// application is checked: an ISO control character (C0, DEL, C1; a tab, or
// the bytes of most non-ASCII UTF-8 characters) raises RequestRejectedException
// → 400. The check is lazy: a header nobody reads is never rejected. What reads
// headers (captured on the reference):
//
//   - the CORS processing of a cross-origin request, and the body binding,
//     build ServletServerHttpRequest.getHeaders(): EVERY header is checked, and
//     a Content-Type without charset gets the request encoding added, which
//     Spring refuses for a wildcard type ("*/*", "application/*",
//     "application/*+json"): IllegalArgumentException → 500;
//   - the router function resources("/**") does the same for every request
//     that reaches the DispatcherServlet on an unknown path;
//   - AuthenticationTokenFilter reads Authorization, @RequestHeader reads its
//     header, and the return value handler reads If-None-Match for a 200
//     answer to GET/HEAD.

// headerRejected is panicked by handler-level header reads.
type headerRejected struct{ name string }

func (h *headerRejected) Error() string { return "RequestRejectedException: header " + h.name }

// headerValueOK applies the firewall predicate to one raw value.
func headerValueOK(v string) bool {
	for i := 0; i < len(v); i++ {
		if b := v[i]; b < 0x20 || b >= 0x7f && b <= 0x9f {
			return false
		}
	}
	return true
}

// allHeadersOK checks every header value (the Host header included).
func allHeadersOK(r *http.Request) bool {
	if !headerValueOK(r.Host) {
		return false
	}
	for _, vs := range r.Header {
		for _, v := range vs {
			if !headerValueOK(v) {
				return false
			}
		}
	}
	return true
}

// headerValuesOK checks the values of one header (getHeaders(name)).
func headerValuesOK(r *http.Request, name string) bool {
	for _, v := range r.Header[http.CanonicalHeaderKey(name)] {
		if !headerValueOK(v) {
			return false
		}
	}
	return true
}

// requestContentType returns the first Content-Type value as Tomcat decodes
// it, "" when absent or empty (HttpHeaders.getContentType → null).
func requestContentType(r *http.Request) string {
	vs := r.Header["Content-Type"]
	if len(vs) == 0 {
		return ""
	}
	return javacompat.Latin1(vs[0])
}

// wildcardContentTypeFails reports whether ServletServerHttpRequest.getHeaders()
// throws on this request's Content-Type (valid, without charset, wildcard).
func wildcardContentTypeFails(r *http.Request) bool {
	ct := requestContentType(r)
	if ct == "" {
		return false
	}
	m, ok := parseSpringMediaType(ct)
	return ok && m.charset == "" && (m.wildcardType() || m.wildcardSubtype())
}

// springHeadersStatus emulates ServletServerHttpRequest.getHeaders(): 0 when
// it succeeds, 400 when the firewall rejects a header, 500 when the
// Content-Type enrichment throws.
func springHeadersStatus(r *http.Request) int {
	if !allHeadersOK(r) {
		return 400
	}
	if wildcardContentTypeFails(r) {
		return 500
	}
	return 0
}
