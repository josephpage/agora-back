package runner

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agora/internal/auth"
)

// Diff is one observed difference.
type Diff struct {
	Where string `json:"where"` // status | header:<name> | body:<path> | db:<table>
	Ref   string `json:"ref"`
	Go    string `json:"go"`
	Note  string `json:"note,omitempty"`
}

// Binder keeps the bijection between values generated independently by each
// side (UUIDs, JWTs, login tokens).
type Binder struct {
	mu      sync.Mutex
	refToGo map[string]string
	goToRef map[string]string
	tokens  *auth.LoginTokens
	now     func() time.Time
}

// NewBinder creates an empty binder.
func NewBinder(lt *auth.LoginTokens) *Binder {
	return &Binder{refToGo: map[string]string{}, goToRef: map[string]string{}, tokens: lt, now: time.Now}
}

var (
	uuidRe      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	jwtRe       = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
	sessionIDRe = regexp.MustCompile(`^[0-9A-F]{32}$`)
	epochMsRe   = regexp.MustCompile(`\b1[0-9]{12}\b`)
	dateRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}`)
)

// bind records ref↔go; returns false when it contradicts an existing pair.
func (b *Binder) bind(ref, gov string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if g, ok := b.refToGo[ref]; ok {
		return g == gov
	}
	if r, ok := b.goToRef[gov]; ok {
		return r == ref
	}
	b.refToGo[ref] = gov
	b.goToRef[gov] = ref
	return true
}

// GoToRef translates a go-side value into the ref-side one when bound.
func (b *Binder) GoToRef(v string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.goToRef[v]
	return r, ok
}

// Pairs returns a snapshot of bound go→ref pairs.
func (b *Binder) Pairs() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]string, len(b.goToRef))
	for k, v := range b.goToRef {
		out[k] = v
	}
	return out
}

// equivalentStrings decides whether two differing strings are the "same"
// generated value on each side.
func (b *Binder) equivalentStrings(ref, gov string) (bool, string) {
	if ref == gov {
		return true, ""
	}
	if sessionIDRe.MatchString(ref) && sessionIDRe.MatchString(gov) {
		return true, "" // random JSESSIONID
	}
	if uuidRe.MatchString(ref) && uuidRe.MatchString(gov) {
		if b.bind(strings.ToLower(ref), strings.ToLower(gov)) {
			return true, ""
		}
		return false, "uuid binding conflict"
	}
	if jwtRe.MatchString(ref) && jwtRe.MatchString(gov) {
		if ok, why := b.equivalentJWT(ref, gov); !ok {
			return false, why
		}
		b.bind(ref, gov)
		return true, ""
	}
	if b.tokens != nil && looksBase64(ref) && looksBase64(gov) {
		ru, err1 := b.tokens.Decode(ref)
		gu, err2 := b.tokens.Decode(gov)
		if err1 == nil && err2 == nil {
			if ok, _ := b.equivalentStrings(ru, gu); ok {
				b.bind(ref, gov)
				return true, ""
			}
			return false, "login tokens for different users"
		}
	}
	if dateRe.MatchString(ref) && dateRe.MatchString(gov) && b.nearNow(ref) && b.nearNow(gov) {
		rt, _ := parseLooseDate(ref)
		gt, _ := parseLooseDate(gov)
		if math.Abs(rt.Sub(gt).Seconds()) <= 5 && len(ref) == len(gov) {
			return true, ""
		}
	}
	return false, ""
}

func looksBase64(s string) bool {
	if len(s) < 16 || len(s)%4 != 0 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

func parseLooseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339Nano} {
		if t, err := time.ParseInLocation(layout, s[:min(len(s), len(layout))], time.Local); err == nil {
			return t, nil
		}
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable date %q", s)
}

func (b *Binder) nearNow(s string) bool {
	t, err := parseLooseDate(s)
	if err != nil {
		return false
	}
	return math.Abs(b.now().Sub(t).Hours()) < 25 && math.Abs(b.now().Sub(t).Minutes()) < 5
}

func (b *Binder) equivalentJWT(ref, gov string) (bool, string) {
	rp, err1 := jwtPayload(ref)
	gp, err2 := jwtPayload(gov)
	rh, _ := jwtHeader(ref)
	gh, _ := jwtHeader(gov)
	if err1 != nil || err2 != nil {
		return false, "not decodable JWTs"
	}
	if rh != gh {
		return false, "jwt header " + rh + " vs " + gh
	}
	rs, _ := rp["sub"].(string)
	gs, _ := gp["sub"].(string)
	if ok, _ := b.equivalentStrings(rs, gs); !ok {
		return false, "jwt sub differs"
	}
	rk, gk := keysOf(rp), keysOf(gp)
	if rk != gk {
		return false, "jwt claims " + rk + " vs " + gk
	}
	ri, _ := rp["iat"].(float64)
	re, _ := rp["exp"].(float64)
	gi, _ := gp["iat"].(float64)
	ge, _ := gp["exp"].(float64)
	if re-ri != ge-gi || math.Abs(ri-gi) > 5 {
		return false, "jwt iat/exp differ"
	}
	return true, ""
}

func keysOf(m map[string]any) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func jwtPart(tok string, i int) ([]byte, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a jwt")
	}
	return base64.RawURLEncoding.DecodeString(parts[i])
}

func jwtHeader(tok string) (string, error) {
	b, err := jwtPart(tok, 0)
	return string(b), err
}

func jwtPayload(tok string) (map[string]any, error) {
	b, err := jwtPart(tok, 1)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal(b, &m)
	return m, err
}

// compareJSON compares two JSON documents (key order, raw escaping and
// numbers included) and returns the differences.
func (b *Binder) compareJSON(ref, gov []byte, c Compare) []Diff {
	rn, err1 := parseJSON(ref)
	gn, err2 := parseJSON(gov)
	if err1 != nil || err2 != nil {
		if bytes.Equal(ref, gov) {
			return nil
		}
		return b.compareText(ref, gov)
	}
	var diffs []Diff
	b.compareNode(rn, gn, "$", c, &diffs)
	return diffs
}

func pathMatches(path string, patterns []string) bool {
	for _, p := range patterns {
		if p == path {
			return true
		}
		// "[*]" wildcard for array indexes
		if strings.Contains(p, "[*]") {
			re := regexp.QuoteMeta(p)
			re = strings.ReplaceAll(re, `\[\*\]`, `\[\d+\]`)
			if regexp.MustCompile("^" + re + "$").MatchString(path) {
				return true
			}
		}
	}
	return false
}

func short(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}

func (b *Binder) compareNode(r, g *node, path string, c Compare, diffs *[]Diff) {
	if pathMatches(path, c.Ignore) {
		return
	}
	if r.kind != g.kind {
		*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: short(r.raw), Go: short(g.raw), Note: "kind"})
		return
	}
	switch r.kind {
	case 'o':
		if strings.Join(r.keys, ",") != strings.Join(g.keys, ",") {
			*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: strings.Join(r.keys, ","), Go: strings.Join(g.keys, ","), Note: "keys/order"})
		}
		gi := map[string]int{}
		for i, k := range g.keys {
			gi[k] = i
		}
		for i, k := range r.keys {
			if j, ok := gi[k]; ok {
				b.compareNode(r.vals[i], g.vals[j], path+"."+k, c, diffs)
			}
		}
	case 'a':
		if pathMatches(path, c.Unordered) {
			b.compareUnordered(r, g, path, c, diffs)
			return
		}
		if len(r.elems) != len(g.elems) {
			*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: fmt.Sprintf("len %d", len(r.elems)), Go: fmt.Sprintf("len %d", len(g.elems)), Note: short(r.raw) + " ||| " + short(g.raw)})
			return
		}
		for i := range r.elems {
			b.compareNode(r.elems[i], g.elems[i], fmt.Sprintf("%s[%d]", path, i), c, diffs)
		}
	case 's':
		if bytes.Equal(r.raw, g.raw) {
			return
		}
		if r.str == g.str {
			*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: string(r.raw), Go: string(g.raw), Note: "escaping"})
			return
		}
		if ok, why := b.equivalentStrings(r.str, g.str); !ok {
			*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: r.str, Go: g.str, Note: why})
		}
	case 'n':
		if bytes.Equal(r.raw, g.raw) {
			return
		}
		rv, _ := strconv.ParseFloat(string(r.raw), 64)
		gv, _ := strconv.ParseFloat(string(g.raw), 64)
		nowMs := float64(b.now().UnixMilli())
		if len(r.raw) == len(g.raw) && math.Abs(rv-nowMs) < 2*86400e3+1e5 && math.Abs(rv-gv) <= 5000 {
			return // epoch-ms timestamps generated "now" (+ JWT expiry)
		}
		*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: string(r.raw), Go: string(g.raw)})
	default:
		if !bytes.Equal(r.raw, g.raw) {
			*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: string(r.raw), Go: string(g.raw)})
		}
	}
}

func (b *Binder) compareUnordered(r, g *node, path string, c Compare, diffs *[]Diff) {
	if len(r.elems) != len(g.elems) {
		*diffs = append(*diffs, Diff{Where: "body:" + path, Ref: fmt.Sprintf("len %d", len(r.elems)), Go: fmt.Sprintf("len %d", len(g.elems)), Note: "unordered"})
		return
	}
	used := make([]bool, len(g.elems))
	for i, re := range r.elems {
		found := false
		for j, ge := range g.elems {
			if used[j] {
				continue
			}
			var d []Diff
			b.compareNode(re, ge, fmt.Sprintf("%s[%d]", path, i), c, &d)
			if len(d) == 0 {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			*diffs = append(*diffs, Diff{Where: fmt.Sprintf("body:%s[%d]", path, i), Ref: short(re.raw), Go: "(no matching element)", Note: "unordered"})
		}
	}
}

// compareText compares non-JSON bodies after substituting bound values.
func (b *Binder) compareText(ref, gov []byte) []Diff {
	g := string(gov)
	for gv, rv := range b.Pairs() {
		g = strings.ReplaceAll(g, gv, rv)
	}
	if string(ref) == g {
		return nil
	}
	// epoch-ms timestamps generated "now" (XML error bodies)
	nowMs := b.now().UnixMilli()
	mask := func(s string) string {
		return epochMsRe.ReplaceAllStringFunc(s, func(m string) string {
			v, _ := strconv.ParseInt(m, 10, 64)
			if v-nowMs < 120000 && nowMs-v < 120000 {
				return "<now-ms>"
			}
			return m
		})
	}
	if mask(string(ref)) == mask(g) {
		return nil
	}
	return []Diff{{Where: "body", Ref: short(ref), Go: short([]byte(g))}}
}

// default headers compared on every step (case-insensitive names).
var defaultHeaders = []string{
	"Content-Type", "Cache-Control", "Pragma", "Expires", "ETag", "Vary",
	"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers",
	"Access-Control-Max-Age", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers",
	"X-Content-Type-Options", "X-XSS-Protection", "X-Frame-Options", "Allow", "Accept", "Set-Cookie", "Location",
	"Content-Disposition", "Strict-Transport-Security",
}

var cookieExpiresRe = regexp.MustCompile(`Expires=[^;]+`)

func (b *Binder) compareHeaders(rh, gh http.Header, extra []string) []Diff {
	var diffs []Diff
	for _, name := range append(append([]string(nil), defaultHeaders...), extra...) {
		rv := strings.Join(rh.Values(name), " | ")
		gv := strings.Join(gh.Values(name), " | ")
		if rv == gv {
			continue
		}
		if strings.EqualFold(name, "Set-Cookie") {
			if b.equivalentCookies(rh.Values(name), gh.Values(name)) {
				continue
			}
		}
		if strings.EqualFold(name, "Allow") && sameMethodSet(rv, gv) {
			// the order of the methods follows the JVM's reflection order of
			// the controller methods, which changes between reference runs
			// (class N): same methods and same separator are equivalent
			continue
		}
		diffs = append(diffs, Diff{Where: "header:" + name, Ref: rv, Go: gv})
	}
	return diffs
}

func (b *Binder) equivalentCookies(r, g []string) bool {
	if len(r) != len(g) {
		return false
	}
	for i := range r {
		rn := cookieExpiresRe.ReplaceAllString(r[i], "Expires=*")
		gn := cookieExpiresRe.ReplaceAllString(g[i], "Expires=*")
		rk, rrest, _ := strings.Cut(rn, ";")
		gk, grest, _ := strings.Cut(gn, ";")
		if rrest != grest {
			return false
		}
		rname, rval, _ := strings.Cut(rk, "=")
		gname, gval, _ := strings.Cut(gk, "=")
		if rname != gname {
			return false
		}
		if ok, _ := b.equivalentStrings(rval, gval); !ok {
			return false
		}
	}
	return true
}

// sameMethodSet compares two Allow values as sets, with the same separator.
func sameMethodSet(a, b string) bool {
	if strings.Contains(a, ", ") != strings.Contains(b, ", ") {
		return false
	}
	split := func(s string) []string {
		var out []string
		for _, p := range strings.Split(s, ",") {
			out = append(out, strings.TrimSpace(p))
		}
		sort.Strings(out)
		return out
	}
	return strings.Join(split(a), ",") == strings.Join(split(b), ",")
}
