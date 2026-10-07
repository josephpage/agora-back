package httpx

import (
	"net/url"
	"strings"
)

// Rule is one entry of WebSecurityConfig.authorizeHttpRequests(), evaluated
// in order; the first matching rule decides.
type Rule struct {
	Method    string // "" = any method
	Patterns  []string
	Authority string // required authority; "" with PermitAll=false = authenticated
	PermitAll bool
	segs      [][]segment
}

// AgoraRules reproduces fr.gouv.agora.config.WebSecurityConfig exactly.
func AgoraRules() []Rule {
	return []Rule{
		{Patterns: []string{"/admin/**"}, Authority: AuthAdminAPIs},
		{Patterns: []string{"/moderate/**"}, Authority: AuthModerateQag},
		{Patterns: []string{"/swagger-config.json", "/swagger-ui.html", "/swagger-ui/**", "/v3/api-docs/**"}, PermitAll: true},
		{Patterns: []string{"/signup", "/login"}, PermitAll: true},
		{Patterns: []string{"/moderatus/**"}, PermitAll: true},
		{Patterns: []string{"/api/public/**"}, PermitAll: true},
		{Patterns: []string{"/welcome_page/last_news"}, PermitAll: true},
		{Patterns: []string{"/consultations"}, PermitAll: true},
		{Patterns: []string{"/consultations/{consultationId}/questions"}, PermitAll: true},
		{Patterns: []string{"/qags/responses/**"}, PermitAll: true},
		{Patterns: []string{"/error"}, PermitAll: true},
		{Patterns: []string{"/referentiels/regions-et-departements"}, PermitAll: true},
		{Patterns: []string{"/content/**"}, PermitAll: true},
		{Patterns: []string{"/participation_charter"}, PermitAll: true},
		{Patterns: []string{"/fiches_inventaire/**"}, PermitAll: true},
		{Method: "GET", Patterns: []string{"/v2/consultations/**"}, PermitAll: true},
		{Patterns: []string{"/thematiques/**"}, PermitAll: true},
		{Patterns: []string{"/theme_hebdo"}, PermitAll: true},
		{Patterns: []string{"/.well-known/acme-challenge/**"}, PermitAll: true},
		{Patterns: []string{"/stub/**"}, PermitAll: true},
		// anyRequest().authenticated()
		{Patterns: []string{"/**"}},
	}
}

func (r *Rule) compile() {
	r.segs = make([][]segment, len(r.Patterns))
	for i, p := range r.Patterns {
		r.segs[i] = parsePattern(p)
	}
}

func (r *Rule) matches(method string, parts []string) bool {
	if r.Method != "" && r.Method != method {
		return false
	}
	for _, s := range r.segs {
		if _, ok := matchSegments(s, parts); ok {
			return true
		}
	}
	return false
}

// decision is the AuthorizationFilter outcome.
type decision int

const (
	allow decision = iota
	denyUnauthenticated
	denyForbidden
)

func decide(rules []Rule, method, path string, user *User) decision {
	parts := splitPath(path)
	for i := range rules {
		r := &rules[i]
		if !r.matches(method, parts) {
			continue
		}
		switch {
		case r.PermitAll:
			return allow
		case r.Authority != "":
			if user == nil {
				return denyUnauthenticated
			}
			if !user.Has(r.Authority) {
				return denyForbidden
			}
			return allow
		default:
			if user == nil {
				return denyUnauthenticated
			}
			return allow
		}
	}
	return allow
}

// firewallReject reproduces Spring Security's StrictHttpFirewall checks on
// the raw request URI. Returns true when the request must be rejected (400).
func firewallReject(method, rawURI string) bool {
	rawPath := rawURI
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		rawPath = rawPath[:i]
	}
	lower := strings.ToLower(rawPath)
	// encoded/decoded blocklists: semicolon, encoded percent, double slash, encoded period
	for _, bad := range []string{";", "%3b", "%25", "//", "%2e"} {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	// path must be normalized (no "." / ".." segments)
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		decoded = rawPath
	}
	if !isNormalized(rawPath) || !isNormalized(decoded) {
		return true
	}
	// encoded and decoded line feed, carriage return, line and paragraph
	// separators (allowUrlEncodedLineFeed & co. default to false)
	if strings.ContainsAny(decoded, "\n\r\u2028\u2029") {
		return true
	}
	return false
}

func isNormalized(path string) bool {
	if path == "" {
		return true
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
