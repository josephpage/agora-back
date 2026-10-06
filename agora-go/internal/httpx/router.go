package httpx

import (
	"sort"
	"strings"
)

// HandlerFunc handles a matched request. Domain errors are raised with panic
// (SpringError / AdviceError / any other value → 500), mirroring Kotlin
// exceptions; the dispatcher recovers them.
type HandlerFunc func(c *Ctx) *Response

// Route is one @XxxMapping.
type Route struct {
	Method  string
	Pattern string
	Handler HandlerFunc
	// Public documents the matching permitAll rule (informational only).
	segs []segment
}

type segment struct {
	literal  string
	variable string // non-empty for {var}
	wildcard bool   // "**" (only allowed as last segment)
}

func parsePattern(p string) []segment {
	if p == "/" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	segs := make([]segment, len(parts))
	for i, part := range parts {
		switch {
		case part == "**":
			segs[i] = segment{wildcard: true}
		case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			segs[i] = segment{variable: part[1 : len(part)-1]}
		default:
			segs[i] = segment{literal: part}
		}
	}
	return segs
}

// splitPath splits a decoded request path into segments. A trailing slash
// yields an empty last segment (Spring 6: no trailing slash matching).
func splitPath(path string) []string {
	if path == "/" || path == "" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// matchSegments matches like Spring's PathPattern: {var} captures exactly one
// NON-EMPTY segment, "**" (last) matches zero or more segments.
func matchSegments(segs []segment, parts []string) (map[string]string, bool) {
	var vars map[string]string
	for i, s := range segs {
		if s.wildcard {
			return vars, true
		}
		if i >= len(parts) {
			return nil, false
		}
		p := parts[i]
		if s.variable != "" {
			if p == "" {
				return nil, false
			}
			if vars == nil {
				vars = map[string]string{}
			}
			vars[s.variable] = p
			continue
		}
		if s.literal != p {
			return nil, false
		}
	}
	if len(parts) != len(segs) {
		return nil, false
	}
	return vars, true
}

// specificity orders candidate patterns like PathPattern.SPECIFICITY_COMPARATOR
// for the shapes we use: fewer wildcards, then fewer variables, then longer.
func specificity(segs []segment) (wild, vars, length int) {
	for _, s := range segs {
		switch {
		case s.wildcard:
			wild++
		case s.variable != "":
			vars++
		}
	}
	return wild, vars, len(segs)
}

type router struct{ routes []*Route }

func (rt *router) add(r *Route) {
	r.segs = parsePattern(r.Pattern)
	rt.routes = append(rt.routes, r)
}

type matchResult struct {
	route *Route
	vars  map[string]string
	// allowed lists the methods of routes matching the path (for 405/OPTIONS).
	allowed []string
	// pathMatched is true when at least one route matches the path.
	pathMatched bool
}

func methodMatches(routeMethod, reqMethod string) bool {
	if routeMethod == reqMethod {
		return true
	}
	// Spring maps HEAD onto GET handlers.
	return reqMethod == "HEAD" && routeMethod == "GET"
}

func (rt *router) match(method, path string) matchResult {
	parts := splitPath(path)
	type cand struct {
		r    *Route
		vars map[string]string
	}
	var byPath []cand
	for _, r := range rt.routes {
		if vars, ok := matchSegments(r.segs, parts); ok {
			byPath = append(byPath, cand{r, vars})
		}
	}
	res := matchResult{pathMatched: len(byPath) > 0}
	if len(byPath) == 0 {
		return res
	}
	var ok []cand
	seen := map[string]bool{}
	for _, c := range byPath {
		if !seen[c.r.Method] {
			seen[c.r.Method] = true
			res.allowed = append(res.allowed, c.r.Method)
		}
		if methodMatches(c.r.Method, method) {
			ok = append(ok, c)
		}
	}
	if len(ok) == 0 {
		return res
	}
	sort.SliceStable(ok, func(i, j int) bool {
		wi, vi, li := specificity(ok[i].r.segs)
		wj, vj, lj := specificity(ok[j].r.segs)
		if wi != wj {
			return wi < wj
		}
		if vi != vj {
			return vi < vj
		}
		return li > lj
	})
	res.route, res.vars = ok[0].r, ok[0].vars
	return res
}

// optionsAllow reproduces HttpOptionsHandler: declared methods in order, HEAD
// right after GET, OPTIONS last.
func optionsAllow(declared []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, m := range declared {
		add(m)
		if m == "GET" {
			add("HEAD")
		}
	}
	add("OPTIONS")
	return out
}
