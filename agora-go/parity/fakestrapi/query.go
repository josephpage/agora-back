package fakestrapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultPageSize and MaxPageSize mirror Strapi's defaults.
const (
	DefaultPageSize = 25
	MaxPageSize     = 100
)

// QueryError is a client error (HTTP 400, Strapi ValidationError).
type QueryError struct{ Msg string }

func (e *QueryError) Error() string { return e.Msg }

// Filter is one parsed `filters[a][b][$op]=v` entry. Several values (the
// parameter repeated in the URL) are combined: positive operators
// ($in/$eq/$contains/$containsi/$startsWith/$endsWith) are OR-ed, negative
// and range operators are AND-ed.
type Filter struct {
	Path   []string
	Op     string
	Values []string
}

// SortKey is one `sort[i]=field:dir` entry.
type SortKey struct {
	Path []string
	Desc bool
}

// Query is the subset of the Strapi REST query language the fake supports.
type Query struct {
	Filters []Filter
	Sorts   []SortKey
	// Draft is true for status=draft: draft versions of ALL documents,
	// published or not. Otherwise only documents with a non-null publishedAt.
	Draft bool

	Page     int
	PageSize int
	// Start/Limit are set when the offset-style pagination was requested.
	Start, Limit int
	OffsetStyle  bool
}

var (
	filterKeyRe = regexp.MustCompile(`^filters((?:\[[^\[\]]*\])+)$`)
	sortKeyRe   = regexp.MustCompile(`^sort(?:\[(\d+)\])?$`)
	bracketRe   = regexp.MustCompile(`\[([^\[\]]*)\]`)
)

var supportedOps = map[string]bool{
	"in": true, "notIn": true, "eq": true, "ne": true, "contains": true,
	"containsi": true, "notContains": true, "notContainsi": true,
	"startsWith": true, "endsWith": true,
	"lt": true, "lte": true, "gt": true, "gte": true,
	"null": true, "notNull": true,
}

// ParseQuery parses URL query values (already decoded with url.ParseQuery
// semantics: '+' and %XX are decoded). populate* and unknown top-level keys
// are accepted and ignored, because fixtures are stored fully populated.
func ParseQuery(values url.Values) (*Query, error) {
	q := &Query{Page: 1, PageSize: DefaultPageSize}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type indexedSort struct {
		idx  int
		seq  int
		keys []SortKey
	}
	var sorts []indexedSort

	var start, limit *int
	var page, pageSize *int

	for _, k := range keys {
		vals := values[k]
		switch {
		case k == "status":
			if len(vals) > 0 && vals[len(vals)-1] == "draft" {
				q.Draft = true
			}
		case strings.HasPrefix(k, "populate"), strings.HasPrefix(k, "fields"), k == "locale":
			// accepted and ignored
		case strings.HasPrefix(k, "pagination["):
			name := strings.TrimSuffix(strings.TrimPrefix(k, "pagination["), "]")
			if len(vals) == 0 {
				continue
			}
			v := vals[len(vals)-1]
			switch name {
			case "withCount":
				continue
			case "page", "pageSize", "start", "limit":
				n, err := strconv.Atoi(v)
				if err != nil {
					return nil, &QueryError{Msg: fmt.Sprintf("Invalid value for %s: %q", k, v)}
				}
				switch name {
				case "page":
					page = &n
				case "pageSize":
					pageSize = &n
				case "start":
					start = &n
				case "limit":
					limit = &n
				}
			default:
				return nil, &QueryError{Msg: fmt.Sprintf("Invalid key %s", k)}
			}
		case sortKeyRe.MatchString(k):
			m := sortKeyRe.FindStringSubmatch(k)
			idx := 0
			if m[1] != "" {
				idx, _ = strconv.Atoi(m[1])
			}
			for seq, v := range vals {
				var sk []SortKey
				for _, part := range strings.Split(v, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					field, dir := part, "asc"
					if i := strings.LastIndex(part, ":"); i >= 0 {
						field, dir = part[:i], strings.ToLower(part[i+1:])
					}
					if dir != "asc" && dir != "desc" {
						return nil, &QueryError{Msg: fmt.Sprintf("Invalid sort direction %q", dir)}
					}
					sk = append(sk, SortKey{Path: strings.Split(field, "."), Desc: dir == "desc"})
				}
				sorts = append(sorts, indexedSort{idx: idx, seq: seq, keys: sk})
			}
		case filterKeyRe.MatchString(k):
			segs := bracketRe.FindAllStringSubmatch(filterKeyRe.FindStringSubmatch(k)[1], -1)
			var path []string
			for _, s := range segs {
				path = append(path, s[1])
			}
			op := "eq"
			if n := len(path); n > 0 && strings.HasPrefix(path[n-1], "$") {
				op = strings.TrimPrefix(path[n-1], "$")
				path = path[:n-1]
			}
			if len(path) == 0 {
				return nil, &QueryError{Msg: fmt.Sprintf("Invalid key %s", k)}
			}
			for _, p := range path {
				if p == "" || strings.HasPrefix(p, "$") {
					return nil, &QueryError{Msg: fmt.Sprintf("Unsupported filter key %s", k)}
				}
				if _, err := strconv.Atoi(p); err == nil {
					return nil, &QueryError{Msg: fmt.Sprintf("Unsupported filter key %s", k)}
				}
			}
			if !supportedOps[op] {
				return nil, &QueryError{Msg: fmt.Sprintf("Invalid operator $%s in %s", op, k)}
			}
			q.Filters = append(q.Filters, Filter{Path: path, Op: op, Values: append([]string(nil), vals...)})
		default:
			// unknown keys are tolerated (Strapi would 400; the fake is lenient)
		}
	}

	sort.SliceStable(sorts, func(i, j int) bool {
		if sorts[i].idx != sorts[j].idx {
			return sorts[i].idx < sorts[j].idx
		}
		return sorts[i].seq < sorts[j].seq
	})
	for _, s := range sorts {
		q.Sorts = append(q.Sorts, s.keys...)
	}

	if start != nil || limit != nil {
		q.OffsetStyle = true
		q.Start, q.Limit = 0, DefaultPageSize
		if start != nil {
			q.Start = *start
		}
		if limit != nil {
			q.Limit = *limit
		}
		if q.Start < 0 || q.Limit < 1 {
			return nil, &QueryError{Msg: "Invalid pagination: start must be >= 0 and limit >= 1"}
		}
		if q.Limit > MaxPageSize {
			q.Limit = MaxPageSize
		}
		return q, nil
	}
	if page != nil {
		q.Page = *page
	}
	if pageSize != nil {
		q.PageSize = *pageSize
	}
	if q.Page < 1 {
		return nil, &QueryError{Msg: "Invalid pagination: page must be greater than or equal to 1"}
	}
	if q.PageSize < 1 {
		return nil, &QueryError{Msg: "Invalid pagination: pageSize must be greater than or equal to 1"}
	}
	if q.PageSize > MaxPageSize {
		q.PageSize = MaxPageSize
	}
	return q, nil
}

// Result is a filtered, sorted, paginated slice of documents.
type Result struct {
	Docs  []*Object
	Total int
}

// Apply runs the query against docs (in fixture order).
func (q *Query) Apply(docs []*Object) Result {
	matched := make([]*Object, 0, len(docs))
	for _, d := range docs {
		if !q.Draft && !isPublished(d) {
			continue
		}
		ok := true
		for _, f := range q.Filters {
			if !f.matches(d) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, d)
		}
	}

	if len(q.Sorts) > 0 {
		sort.SliceStable(matched, func(i, j int) bool {
			for _, sk := range q.Sorts {
				c := compareSortValues(firstLeaf(matched[i], sk.Path), firstLeaf(matched[j], sk.Path), sk.Desc)
				if c != 0 {
					return c < 0
				}
			}
			return false
		})
	}

	total := len(matched)
	from, to := 0, total
	if q.OffsetStyle {
		from, to = q.Start, q.Start+q.Limit
	} else {
		from, to = (q.Page-1)*q.PageSize, q.Page*q.PageSize
	}
	if from > total {
		from = total
	}
	if to > total {
		to = total
	}
	return Result{Docs: matched[from:to], Total: total}
}

// Meta builds the `meta` envelope for the result.
func (q *Query) Meta(total int) *Object {
	meta := NewObject()
	pag := NewObject()
	if q.OffsetStyle {
		pag.Set("start", json.Number(strconv.Itoa(q.Start)))
		pag.Set("limit", json.Number(strconv.Itoa(q.Limit)))
		pag.Set("total", json.Number(strconv.Itoa(total)))
	} else {
		pageCount := 0
		if total > 0 {
			pageCount = (total + q.PageSize - 1) / q.PageSize
		}
		pag.Set("page", json.Number(strconv.Itoa(q.Page)))
		pag.Set("pageSize", json.Number(strconv.Itoa(q.PageSize)))
		pag.Set("pageCount", json.Number(strconv.Itoa(pageCount)))
		pag.Set("total", json.Number(strconv.Itoa(total)))
	}
	meta.Set("pagination", pag)
	return meta
}

// isPublished: an explicit null publishedAt marks a draft-only document; a
// document without the key at all is treated as published.
func isPublished(d *Object) bool {
	v, ok := d.Get("publishedAt")
	if !ok {
		return true
	}
	return v != nil
}

// resolvePath collects the leaf values reached by following path through
// nested objects; arrays are traversed transparently (any element may match).
func resolvePath(v any, path []string) []any {
	if len(path) == 0 {
		if arr, ok := v.([]any); ok {
			var out []any
			for _, e := range arr {
				out = append(out, resolvePath(e, nil)...)
			}
			return out
		}
		return []any{v}
	}
	switch t := v.(type) {
	case *Object:
		next, ok := t.Get(path[0])
		if !ok {
			return nil
		}
		return resolvePath(next, path[1:])
	case []any:
		var out []any
		for _, e := range t {
			out = append(out, resolvePath(e, path)...)
		}
		return out
	}
	return nil
}

func firstLeaf(d *Object, path []string) any {
	leaves := resolvePath(d, path)
	if len(leaves) == 0 {
		return nil
	}
	return leaves[0]
}

func (f Filter) matches(d *Object) bool {
	leaves := resolvePath(d, f.Path)

	switch f.Op {
	case "null":
		want := len(f.Values) == 0 || f.Values[len(f.Values)-1] != "false"
		isNull := true
		for _, l := range leaves {
			if l != nil {
				isNull = false
			}
		}
		return isNull == want
	case "notNull":
		want := len(f.Values) == 0 || f.Values[len(f.Values)-1] != "false"
		notNull := false
		for _, l := range leaves {
			if l != nil {
				notNull = true
			}
		}
		return notNull == want
	}

	anyLeaf := func(pred func(any, string) bool) bool {
		for _, l := range leaves {
			for _, v := range f.Values {
				if pred(l, v) {
					return true
				}
			}
		}
		return false
	}
	// every(value) must hold: no leaf may satisfy the positive predicate for
	// any of the values.
	noneLeaf := func(pred func(any, string) bool) bool { return !anyLeaf(pred) }

	switch f.Op {
	case "in", "eq":
		return anyLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && s == v })
	case "notIn":
		return noneLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && s == v })
	case "ne":
		return noneLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && s == v })
	case "contains":
		return anyLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && strings.Contains(s, v) })
	case "notContains":
		return noneLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && strings.Contains(s, v) })
	case "containsi":
		return anyLeaf(func(l any, v string) bool {
			s, ok := scalarString(l)
			return ok && strings.Contains(strings.ToLower(s), strings.ToLower(v))
		})
	case "notContainsi":
		return noneLeaf(func(l any, v string) bool {
			s, ok := scalarString(l)
			return ok && strings.Contains(strings.ToLower(s), strings.ToLower(v))
		})
	case "startsWith":
		return anyLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && strings.HasPrefix(s, v) })
	case "endsWith":
		return anyLeaf(func(l any, v string) bool { s, ok := scalarString(l); return ok && strings.HasSuffix(s, v) })
	case "lt", "lte", "gt", "gte":
		// every value must be satisfied by at least one leaf
		for _, v := range f.Values {
			sat := false
			for _, l := range leaves {
				c, ok := compareToValue(l, v)
				if !ok {
					continue
				}
				switch f.Op {
				case "lt":
					sat = sat || c < 0
				case "lte":
					sat = sat || c <= 0
				case "gt":
					sat = sat || c > 0
				case "gte":
					sat = sat || c >= 0
				}
			}
			if !sat {
				return false
			}
		}
		return len(f.Values) > 0
	}
	return false
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999", // zone-less LocalDateTime: treated as UTC
	"2006-01-02T15:04",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

// ParseTime parses the ISO date/date-time forms the fake understands.
// Zone-less values are interpreted as UTC.
func ParseTime(s string) (time.Time, bool) {
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// compareToValue compares a document leaf with a query-string value: as
// instants when both parse as dates, as numbers when both are numeric, else
// as strings. ok is false when the leaf is not comparable (null, object...).
func compareToValue(leaf any, v string) (int, bool) {
	switch l := leaf.(type) {
	case string:
		if lt, ok := ParseTime(l); ok {
			if vt, ok := ParseTime(v); ok {
				return cmpTime(lt, vt), true
			}
		}
		if lf, ok := toFloat(l); ok {
			if vf, ok := toFloat(v); ok {
				return cmpFloat(lf, vf), true
			}
		}
		return strings.Compare(l, v), true
	case json.Number:
		lf, ok := toFloat(l)
		vf, ok2 := toFloat(v)
		if ok && ok2 {
			return cmpFloat(lf, vf), true
		}
		return strings.Compare(l.String(), v), true
	case bool:
		s, _ := scalarString(l)
		return strings.Compare(s, v), true
	}
	return 0, false
}

func cmpTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareSortValues orders two leaves for sorting; nulls sort last when
// ascending and first when descending (PostgreSQL default). The result is
// already direction-adjusted.
func compareSortValues(a, b any, desc bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		if desc {
			return -1
		}
		return 1
	case b == nil:
		if desc {
			return 1
		}
		return -1
	}
	c := compareLeaves(a, b)
	if desc {
		c = -c
	}
	return c
}

func compareLeaves(a, b any) int {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		if at, ok := ParseTime(as); ok {
			if bt, ok := ParseTime(bs); ok {
				return cmpTime(at, bt)
			}
		}
		return strings.Compare(as, bs)
	}
	if af, ok := toFloatStrict(a); ok {
		if bf, ok := toFloatStrict(b); ok {
			return cmpFloat(af, bf)
		}
	}
	sa, _ := scalarString(a)
	sb, _ := scalarString(b)
	return strings.Compare(sa, sb)
}

func toFloatStrict(v any) (float64, bool) {
	if n, ok := v.(json.Number); ok {
		return toFloat(n)
	}
	return 0, false
}
