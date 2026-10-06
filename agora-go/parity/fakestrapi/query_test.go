package fakestrapi

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustDocs(t *testing.T, raw string) []*Object {
	t.Helper()
	v, err := ParseJSON([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out []*Object
	for _, e := range v.([]any) {
		out = append(out, e.(*Object))
	}
	return out
}

func ids(docs []*Object) string {
	var out []string
	for _, d := range docs {
		v, _ := d.Get("documentId")
		out = append(out, v.(string))
	}
	return strings.Join(out, ",")
}

func run(t *testing.T, docs []*Object, rawQuery string) Result {
	t.Helper()
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", rawQuery, err)
	}
	q, err := ParseQuery(vals)
	if err != nil {
		t.Fatalf("query %q: %v", rawQuery, err)
	}
	return q.Apply(docs)
}

const sample = `[
 {"documentId":"a","slug":"s-a","titre":"Hello World","territoire":"France","publishedAt":"2024-01-01T00:00:00.000Z",
  "start":"2026-10-01T10:00:00.000Z","end":"2026-10-20T10:00:00.000Z","n":3,
  "thematique":{"documentId":"t1","label":"Env"},"tags":[{"documentId":"x1"},{"documentId":"x2"}]},
 {"documentId":"b","slug":"s-b","titre":"hello there","territoire":"Nord","publishedAt":"2024-01-01T00:00:00.000Z",
  "start":"2026-10-05T10:00:00.000Z","end":"2026-10-06T10:00:00.000Z","n":10,
  "thematique":{"documentId":"t2","label":"Santé"},"tags":[{"documentId":"x3"}]},
 {"documentId":"c","slug":"s-c","titre":"Bonjour","territoire":"Ile-de-France","publishedAt":null,
  "start":"2026-09-01T10:00:00.000Z","end":"2026-12-01T10:00:00.000Z","n":2,
  "thematique":{"documentId":"t1","label":"Env"},"tags":[]},
 {"documentId":"d","slug":"s-d","titre":"Café crème","territoire":"France","publishedAt":"2024-01-01T00:00:00.000Z",
  "start":"2026-10-03","end":"2026-10-04","n":null,"thematique":null}
]`

func TestPublishedOnlyByDefault(t *testing.T) {
	docs := mustDocs(t, sample)
	if got := ids(run(t, docs, "pagination[pageSize]=100&populate=*").Docs); got != "a,b,d" {
		t.Errorf("default = %s, want a,b,d", got)
	}
	if got := ids(run(t, docs, "pagination[pageSize]=100&status=draft&populate=*").Docs); got != "a,b,c,d" {
		t.Errorf("status=draft = %s, want a,b,c,d", got)
	}
}

func TestMissingPublishedAtCountsAsPublished(t *testing.T) {
	docs := mustDocs(t, `[{"documentId":"x"},{"documentId":"y","publishedAt":null}]`)
	if got := ids(run(t, docs, "").Docs); got != "x" {
		t.Errorf("got %s, want x", got)
	}
}

func TestFilterIn(t *testing.T) {
	docs := mustDocs(t, sample)
	cases := []struct{ q, want string }{
		{"filters[slug][$in]=s-b", "b"},
		{"filters[documentId][$in]=a&filters[documentId][$in]=d", "a,d"},                    // repeated => OR
		{"filters[territoire][$in]=France&filters[territoire][$in]=Nord", "a,b,d"},          // OR
		{"filters[territoire][$in]=Ile-de-France&status=draft", "c"},                        // draft visible
		{"filters[territoire][$in]=Ile-de-France", ""},                                      // unpublished hidden
		{"filters[thematique][documentId][$in]=t1", "a"},                                    // nested object
		{"filters[thematique][documentId][$in]=t1&status=draft", "a,c"},                     // nested + draft
		{"filters[tags][documentId][$in]=x2", "a"},                                          // array of objects
		{"filters[tags][documentId][$in]=x3&filters[tags][documentId][$in]=x1", "a,b"},      // array + OR
		{"filters[thematique][documentId][$in]=t1&filters[territoire][$in]=Nord", ""},       // AND across fields
		{"filters[n][$in]=10", "b"},                                                         // numbers as strings
		{"filters[territoire][$in]=Ile%2Dde%2DFrance&status=draft", "c"},                    // percent-encoded
		{"filters[territoire][$in]=Ile-de-France&filters[slug][$in]=s-c&status=draft", "c"}, // AND of two fields
		{"filters[nope][$in]=x", ""},                                                        // unknown field matches nothing
	}
	for _, c := range cases {
		if got := ids(run(t, docs, c.q).Docs); got != c.want {
			t.Errorf("%s => %q, want %q", c.q, got, c.want)
		}
	}
}

func TestFilterContainsi(t *testing.T) {
	docs := mustDocs(t, sample)
	cases := []struct{ q, want string }{
		{"filters[titre][$containsi]=hello", "a,b"},
		{"filters[titre][$containsi]=HELLO", "a,b"},
		{"filters[titre][$containsi]=hello+world", "a"},   // '+' decoded as a space
		{"filters[titre][$containsi]=hello%20world", "a"}, // %20 as a space
		{"filters[titre][$containsi]=%C3%A9", "d"},        // é (UTF-8 percent-encoded)
		{"filters[titre][$containsi]=CAF%C3%89", "d"},     // É folds to é
		{"filters[titre][$contains]=hello", "b"},          // case-sensitive
		{"filters[thematique][label][$containsi]=sant", "b"},
	}
	for _, c := range cases {
		if got := ids(run(t, docs, c.q).Docs); got != c.want {
			t.Errorf("%s => %q, want %q", c.q, got, c.want)
		}
	}
}

func TestFilterDates(t *testing.T) {
	docs := mustDocs(t, sample)
	cases := []struct{ q, want string }{
		// Kotlin LocalDateTime.format(ISO_DATE_TIME): zone-less, micro precision => UTC
		{"filters[start][$lt]=2026-10-06T21:52:47.123456&filters[end][$gt]=2026-10-06T21:52:47.123456", "a"},
		{"filters[start][$lt]=2026-10-06T09:00:00.123456&filters[end][$gt]=2026-10-06T09:00:00.123456", "a,b"},
		{"filters[end][$lt]=2026-10-06T10:00:00", "d"},         // strictly lower: b ends exactly then
		{"filters[end][$lt]=2026-10-06T10:00:00.001", "b,d"},   // millis
		{"filters[end][$lt]=2026-10-06T10:00:00Z", "d"},        // with zone
		{"filters[end][$lt]=2026-10-06T12:00:00%2B02:00", "d"}, // explicit offset (= 10:00Z)
		{"filters[end][$lt]=2026-10-06T12:00:01%2B02:00", "b,d"},
		{"filters[start][$gt]=2026-10-02T00:00", "b,d"},        // no seconds; d has a date-only value
		{"filters[start][$gt]=2026-10-01T10:00:00.000", "b,d"}, // a starts exactly then: excluded
		{"filters[start][$gte]=2026-10-01T10:00:00.000", "a,b,d"},
		{"filters[end][$lte]=2026-10-04", "d"},
		{"filters[n][$gt]=2", "a,b"}, // numbers
		{"filters[n][$lt]=10", "a"},  // null never compares
	}
	for _, c := range cases {
		if got := ids(run(t, docs, c.q).Docs); got != c.want {
			t.Errorf("%s => %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSort(t *testing.T) {
	docs := mustDocs(t, sample)
	cases := []struct{ q, want string }{
		{"sort[0]=documentId:desc", "d,b,a"},
		{"sort[0]=documentId:asc", "a,b,d"},
		{"sort[0]=end:desc", "a,b,d"}, // c is draft; date-time compare, d is a plain date
		{"sort[0]=end:asc", "d,b,a"},
		{"sort[0]=start:asc&status=draft", "c,a,d,b"}, // mixed date-time / date-only values
		{"sort[0]=territoire:asc&sort[1]=documentId:desc", "d,a,b"},
		{"sort[0]=n:asc", "a,b,d"},  // nulls last when ascending
		{"sort[0]=n:desc", "d,b,a"}, // nulls first when descending
		{"sort=titre:asc", "d,a,b"}, // plain sort= form (byte-wise string order)
		{"sort[0]=thematique.label:asc", "a,b,d"},
		{"sort[0]=titre", "d,a,b"}, // default direction asc
	}
	for _, c := range cases {
		if got := ids(run(t, docs, c.q).Docs); got != c.want {
			t.Errorf("%s => %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSortKeepsFixtureOrderOnTies(t *testing.T) {
	docs := mustDocs(t, `[{"documentId":"1","k":"x"},{"documentId":"2","k":"x"},{"documentId":"3","k":"x"}]`)
	if got := ids(run(t, docs, "sort[0]=k:desc").Docs); got != "1,2,3" {
		t.Errorf("got %s", got)
	}
}

func TestNumericSortIsNumeric(t *testing.T) {
	docs := mustDocs(t, `[{"documentId":"1","k":10},{"documentId":"2","k":9},{"documentId":"3","k":100}]`)
	if got := ids(run(t, docs, "sort[0]=k:asc").Docs); got != "2,1,3" {
		t.Errorf("got %s", got)
	}
}

func TestPagination(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 7; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"documentId":"d` + string(rune('0'+i)) + `","publishedAt":"x"}`)
	}
	sb.WriteString("]")
	docs := mustDocs(t, sb.String())

	vals, _ := url.ParseQuery("pagination[pageSize]=3")
	q, err := ParseQuery(vals)
	if err != nil {
		t.Fatal(err)
	}
	res := q.Apply(docs)
	if ids(res.Docs) != "d0,d1,d2" || res.Total != 7 {
		t.Errorf("page 1 = %s total=%d", ids(res.Docs), res.Total)
	}
	meta, _ := Marshal(q.Meta(res.Total))
	if string(meta) != `{"pagination":{"page":1,"pageSize":3,"pageCount":3,"total":7}}` {
		t.Errorf("meta = %s", meta)
	}

	vals, _ = url.ParseQuery("pagination[pageSize]=3&pagination[page]=3")
	q, _ = ParseQuery(vals)
	res = q.Apply(docs)
	if ids(res.Docs) != "d6" {
		t.Errorf("page 3 = %s", ids(res.Docs))
	}
	vals, _ = url.ParseQuery("pagination[pageSize]=3&pagination[page]=9")
	q, _ = ParseQuery(vals)
	if res = q.Apply(docs); len(res.Docs) != 0 || res.Total != 7 {
		t.Errorf("page 9 = %v total=%d", res.Docs, res.Total)
	}

	// defaults, empty result and clamping
	q, _ = ParseQuery(url.Values{})
	if q.PageSize != DefaultPageSize || q.Page != 1 {
		t.Errorf("defaults = %d/%d", q.Page, q.PageSize)
	}
	meta, _ = Marshal(q.Meta(0))
	if string(meta) != `{"pagination":{"page":1,"pageSize":25,"pageCount":0,"total":0}}` {
		t.Errorf("empty meta = %s", meta)
	}
	vals, _ = url.ParseQuery("pagination[pageSize]=5000")
	q, _ = ParseQuery(vals)
	if q.PageSize != MaxPageSize {
		t.Errorf("pageSize clamp = %d", q.PageSize)
	}
	// offset style
	vals, _ = url.ParseQuery("pagination[start]=2&pagination[limit]=2")
	q, _ = ParseQuery(vals)
	res = q.Apply(docs)
	meta, _ = Marshal(q.Meta(res.Total))
	if ids(res.Docs) != "d2,d3" || string(meta) != `{"pagination":{"start":2,"limit":2,"total":7}}` {
		t.Errorf("offset = %s %s", ids(res.Docs), meta)
	}
}

func TestExactStrapiRequestBuilderURL(t *testing.T) {
	// What StrapiRequestBuilder emits for getConsultationsOngoing, after the
	// client replaced literal spaces with %20.
	raw := "pagination[pageSize]=100&populate[thematique]=*&populate[questions][populate]=*" +
		"&populate[image_de_couverture][fields][0]=url&filters[datetime_de_debut][$lt]=2026-10-06T21:52:47.123456" +
		"&filters[datetime_de_fin][$gt]=2026-10-06T21:52:47.123456&filters[territoire][$in]=Ile-de-France" +
		"&filters[territoire][$in]=Fran%C3%A7ais+de+l%27%C3%A9tranger&sort[0]=datetime_de_fin:asc"
	vals, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseQuery(vals)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Filters) != 3 || len(q.Sorts) != 1 || q.PageSize != 100 || q.Draft {
		t.Fatalf("parsed = %+v", q)
	}
	for _, f := range q.Filters {
		if f.Op == "in" && len(f.Values) != 2 {
			t.Errorf("territoire values = %v", f.Values)
		}
		if f.Op == "in" && f.Values[1] != "Français de l'étranger" {
			t.Errorf("decoded value = %q", f.Values[1])
		}
	}
}

func TestInvalidQueries(t *testing.T) {
	for _, raw := range []string{
		"filters[a][$bogus]=1",
		"filters[$or][0][a][$eq]=1",
		"pagination[pageSize]=0",
		"pagination[pageSize]=abc",
		"sort[0]=a:sideways",
	} {
		vals, _ := url.ParseQuery(raw)
		if _, err := ParseQuery(vals); err == nil {
			t.Errorf("%q: expected an error", raw)
		} else if _, ok := err.(*QueryError); !ok {
			t.Errorf("%q: error type %T", raw, err)
		}
	}
}

func TestNullFilters(t *testing.T) {
	docs := mustDocs(t, sample)
	if got := ids(run(t, docs, "filters[thematique][$null]=true").Docs); got != "d" {
		t.Errorf("$null = %s", got)
	}
	if got := ids(run(t, docs, "filters[thematique][$notNull]=true").Docs); got != "a,b" {
		t.Errorf("$notNull = %s", got)
	}
}

func TestResolveTemplate(t *testing.T) {
	now := time.Date(2026, 10, 6, 21, 52, 47, 0, time.UTC)
	cases := map[string]string{
		"{{now}}":          "2026-10-06T21:52:47.000Z",
		"{{now+3d}}":       "2026-10-09T21:52:47.000Z",
		"{{now-2h}}":       "2026-10-06T19:52:47.000Z",
		"{{now-30m}}":      "2026-10-06T21:22:47.000Z",
		"{{now+10s}}":      "2026-10-06T21:52:57.000Z",
		"{{now+1w}}":       "2026-10-13T21:52:47.000Z",
		"{{now+5d+5m}}":    "2026-10-11T21:57:47.000Z",
		"{{date:now+3d}}":  "2026-10-09",
		"{{date:now}}":     "2026-10-06",
		"{{date:now-10d}}": "2026-09-26",
	}
	for in, want := range cases {
		got, matched, err := ResolveTemplate(in, now)
		if err != nil || !matched || got != want {
			t.Errorf("%s => %q matched=%v err=%v, want %q", in, got, matched, err, want)
		}
	}
	if got, matched, _ := ResolveTemplate("plain", now); matched || got != "plain" {
		t.Errorf("plain string altered: %q", got)
	}
	if _, _, err := ResolveTemplate("{{later}}", now); err == nil {
		t.Error("unsupported template accepted")
	}
}

func TestOrderedJSONRoundTrip(t *testing.T) {
	in := `{"z":1,"a":[1.50,{"k":"é<>&"}],"m":null,"b":true}`
	v, err := ParseJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("round trip = %s", out)
	}
}
