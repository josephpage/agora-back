package content

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func para(texts ...string) any {
	children := []any{}
	for _, t := range texts {
		children = append(children, map[string]any{"type": "text", "text": t})
	}
	return map[string]any{"type": "paragraph", "children": children}
}

// ficheDoc is a complete Strapi fiche document.
func ficheDoc(id string, over map[string]any) map[string]any {
	d := map[string]any{
		"id": 1, "documentId": id,
		"etape_1_lancement": []any{para("L")}, "etape_2_analyse": []any{para("A")}, "etape_3_suivi": []any{para("S")},
		"titre": "Fiche " + id, "debut": "2024-01-01", "fin": "2024-12-31", "porteur": "Ministère test", "lien_site": "https://site.fr",
		"condition_participation": "Être citoyen", "modalite_participation": "En ligne",
		"thematique":   map[string]any{"documentId": "thema-1", "label": "Démocratie", "pictogramme": "🗳", "createdAt": "x"},
		"illustration": map[string]any{"url": "https://illustration.jpg", "formats": nil},
		"etape":        "En cours", "annee_de_lancement": "2024", "type": "Consultation",
	}
	for k, v := range over {
		if v == missing {
			delete(d, k)
		} else {
			d[k] = v
		}
	}
	return d
}

var missing = new(int)

func envelope(docs ...map[string]any) string {
	data := make([]any, len(docs))
	for i, d := range docs {
		data[i] = d
	}
	return mustJSON(map[string]any{"data": data, "meta": map[string]any{"pagination": map[string]any{"page": 1, "pageSize": 100, "pageCount": 1, "total": len(docs)}}})
}

// Port of FicheInventaireRepositoryImplTest (getAll / get / illustration).
func TestRepositoryGetAll(t *testing.T) {
	ctx := context.Background()

	t.Run("cache empty and strapi returns an empty list: empty list", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope()))
		got, err := getAllFiches(ctx, ta.App, newFicheFilters(nil, nil, nil, nil, nil, nil))
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v %v", got, err)
		}
	})

	t.Run("cache empty and strapi returns fiches: mapped domain objects", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("fiche-1", map[string]any{"titre": "Ma fiche"}))))
		got, err := getAllFiches(ctx, ta.App, ficheFilters{})
		if err != nil || len(got) != 1 || got[0].id != "fiche-1" || got[0].titre != "Ma fiche" {
			t.Fatalf("got %+v %v", got, err)
		}
		// the thematique is the one embedded in the fiche (ThematiqueMapper.toDomain(dto))
		if th := got[0].thematique; th.ID != "thema-1" || th.Label != "Démocratie" || th.Picto != "🗳" {
			t.Fatalf("thematique %+v", th)
		}
		if got[0].debut != (LocalDate{2024, 1, 1}) || got[0].fin != (LocalDate{2024, 12, 31}) {
			t.Fatalf("dates %+v %+v", got[0].debut, got[0].fin)
		}
		if got[0].etapeLancement != "<p>L</p>" {
			t.Fatalf("html %q", got[0].etapeLancement)
		}
	})

	t.Run("the result is stored in the cache: the second call does not call strapi", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("fiche-1", nil))))
		f := ficheFilters{}
		for i := 0; i < 3; i++ {
			if _, err := getAllFiches(ctx, ta.App, f); err != nil {
				t.Fatal(err)
			}
		}
		if n := ta.strapi.count(); n != 1 {
			t.Fatalf("strapi called %d times", n)
		}
		// another filter, another key
		title := "x"
		if _, err := getAllFiches(ctx, ta.App, newFicheFilters(&title, nil, nil, nil, nil, nil)); err != nil {
			t.Fatal(err)
		}
		if n := ta.strapi.count(); n != 2 {
			t.Fatalf("strapi called %d times", n)
		}
	})

	t.Run("cache populated: cached result without calling strapi", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("fiche-strapi", nil))))
		cached := []ficheInventaire{{id: "fiche-cached", titre: "Fiche en cache"}}
		ta.Cache.Put(fichesCacheName, ficheFilters{}.cacheKey(), cached, fichesCacheTTL)
		got, err := getAllFiches(ctx, ta.App, ficheFilters{})
		if err != nil || !reflect.DeepEqual(got, cached) {
			t.Fatalf("got %+v %v", got, err)
		}
		if ta.strapi.count() != 0 {
			t.Fatal("strapi was called")
		}
	})

	t.Run("a strapi failure is cached as an empty list", func(t *testing.T) {
		ta := newTestApp(t, constBody("not json"))
		for i := 0; i < 2; i++ {
			got, err := getAllFiches(ctx, ta.App, ficheFilters{})
			if err != nil || len(got) != 0 {
				t.Fatalf("got %v %v", got, err)
			}
		}
		if ta.strapi.count() != 1 {
			t.Fatalf("strapi called %d times", ta.strapi.count())
		}
	})

	t.Run("a mapping exception is not cached", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("f", map[string]any{"etape_1_lancement": []any{nil}}))))
		for i := 0; i < 2; i++ {
			if _, err := getAllFiches(ctx, ta.App, ficheFilters{}); err == nil {
				t.Fatal("expected the NPE of the null rich text element")
			}
		}
		if ta.strapi.count() != 2 {
			t.Fatalf("strapi called %d times", ta.strapi.count())
		}
	})

	t.Run("a null element of data is an NPE", func(t *testing.T) {
		ta := newTestApp(t, constBody(`{"data":[null],"meta":{"pagination":{"page":1,"pageSize":1,"pageCount":1,"total":1}}}`))
		if _, err := getAllFiches(ctx, ta.App, ficheFilters{}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestRepositoryGet(t *testing.T) {
	ctx := context.Background()
	t.Run("strapi returns nothing: null", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope()))
		got, err := getFiche(ctx, ta.App, "fiche-unknown")
		if got != nil || err != nil {
			t.Fatalf("%v %v", got, err)
		}
		if uri := ta.strapi.lastURI(); uri != "fiche-inventaires?pagination[pageSize]=100&populate=*&filters[documentId][$in]=fiche-unknown" {
			t.Fatal(uri)
		}
	})
	t.Run("strapi returns a fiche: mapped", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("fiche-42", map[string]any{"titre": "Fiche inventaire"}))))
		got, err := getFiche(ctx, ta.App, "fiche-42")
		if err != nil || got == nil || got.id != "fiche-42" || got.titre != "Fiche inventaire" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("only the first element counts (firstOrNull)", func(t *testing.T) {
		ta := newTestApp(t, constBody(envelope(ficheDoc("a", nil), ficheDoc("b", nil))))
		got, _ := getFiche(ctx, ta.App, "b")
		if got == nil || got.id != "a" {
			t.Fatalf("%+v", got)
		}
		ta = newTestApp(t, constBody(`{"data":[null,`+mustJSON(ficheDoc("b", nil))+`],"meta":{"pagination":{"page":1,"pageSize":1,"pageCount":1,"total":1}}}`))
		if got, err := getFiche(ctx, ta.App, "b"); got != nil || err != nil {
			t.Fatalf("a null first element is a miss: %+v %v", got, err)
		}
	})
	t.Run("a strapi failure is a miss", func(t *testing.T) {
		ta := newTestApp(t, constBody("garbage"))
		if got, err := getFiche(ctx, ta.App, "x"); got != nil || err != nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestIllustration(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		ill  map[string]any
		want string
	}{
		{"medium format", map[string]any{"url": "https://original.jpg", "formats": map[string]any{"medium": map[string]any{"url": "https://medium.jpg"}}}, "https://medium.jpg"},
		{"no medium format", map[string]any{"url": "https://original.jpg", "formats": map[string]any{"small": map[string]any{"url": "s"}}}, "https://original.jpg"},
		{"null medium", map[string]any{"url": "https://original.jpg", "formats": map[string]any{"medium": nil}}, "https://original.jpg"},
		{"null formats", map[string]any{"url": "https://original.jpg", "formats": nil}, "https://original.jpg"},
		{"missing formats", map[string]any{"url": "https://original.jpg"}, "https://original.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta := newTestApp(t, constBody(envelope(ficheDoc("fiche-1", map[string]any{"illustration": tc.ill}))))
			got, err := getFiche(ctx, ta.App, "fiche-1")
			if err != nil || got == nil || got.illustration != tc.want {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func sp(s string) *string { return &s }

// FicheInventaireFilters: derived properties, cache key and Strapi request.
func TestFilters(t *testing.T) {
	for _, tc := range []struct {
		name        string
		f           ficheFilters
		key, uriQry string
	}{
		{"none", newFicheFilters(nil, nil, nil, nil, nil, nil),
			"titre=null|thematique=null|etape=null|condition=null|modalite=null|annee=null", ""},
		{"blank strings and empty lists are null", newFicheFilters(sp("  "), sp(""), []string{}, []string{" ", ""}, []string{"\t"}, sp(" ")),
			"titre=null|thematique=null|etape=null|condition=null|modalite=null|annee=null", ""},
		{"strings are not trimmed", newFicheFilters(sp(" a b "), sp(" t "), nil, nil, nil, sp(" 2024 ")),
			"titre= a b |thematique= t |etape=null|condition=null|modalite=null|annee= 2024 ",
			"&filters[titre][$containsi]=+a+b+&filters[thematique][documentId][$in]=+t+&filters[annee_de_lancement][$in]=+2024+"},
		{"lists are trimmed, sorted in the key only", newFicheFilters(nil, nil, []string{" b ", "a", ""}, []string{"z", "y"}, []string{"m"}, nil),
			"titre=null|thematique=null|etape=a,b|condition=y,z|modalite=m|annee=null",
			"&filters[etape][$in]=b&filters[etape][$in]=a&filters[condition_participation][$in]=z&filters[condition_participation][$in]=y&filters[modalite_participation][$in]=m"},
		{"null literal collides with null", newFicheFilters(sp("null"), nil, nil, nil, nil, nil),
			"titre=null|thematique=null|etape=null|condition=null|modalite=null|annee=null", "&filters[titre][$containsi]=null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.cacheKey(); got != tc.key {
				t.Errorf("key %q, want %q", got, tc.key)
			}
			uri, err := tc.f.strapiRequest().Build()
			want := "fiche-inventaires?pagination[pageSize]=100&populate=*" + tc.uriQry + "&sort[0]=debut:desc"
			if err != nil || uri != want {
				t.Errorf("uri %q %v, want %q", uri, err, want)
			}
		})
	}
	// String.compareTo order (UTF-16 code units) in the key
	f := newFicheFilters(nil, nil, []string{"\U0001F600", "～", "é", "e", "Z"}, nil, nil, nil)
	if got, want := f.cacheKey(), "titre=null|thematique=null|etape=Z,e,é,\U0001F600,～|condition=null|modalite=null|annee=null"; got != want {
		t.Errorf("key %q, want %q", got, want)
	}
}

// Spring's binding of @RequestParam String? / List<String>?.
func TestParamBinding(t *testing.T) {
	q := func(s string) url.Values {
		v, err := url.ParseQuery(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return "'" + *p + "'"
	}
	for _, tc := range []struct{ query, name, wantStr string }{
		{"", "titre", "<nil>"},
		{"titre=", "titre", "''"},
		{"titre=a", "titre", "'a'"},
		{"titre=a&titre=b", "titre", "'a,b'"},
		{"titre=a,b", "titre", "'a,b'"},
		{"titre=&titre=", "titre", "','"},
	} {
		if got := str(stringParam(q(tc.query), tc.name)); got != tc.wantStr {
			t.Errorf("stringParam(%q): %s, want %s", tc.query, got, tc.wantStr)
		}
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", nil},
		{"etape=", []string{}},
		{"etape=a", []string{"a"}},
		{"etape=a,b", []string{"a", "b"}},
		{"etape=%20a%20,b%20", []string{"a", "b"}},
		{"etape=a,,b", []string{"a", "", "b"}},
		{"etape=,", []string{"", ""}},
		{"etape=a,b&etape=c,d", []string{"a,b", "c,d"}},
		{"etape=%20a&etape=b%20", []string{" a", "b "}},
		{"etape=&etape=", []string{"", ""}},
		{"etape=%C2%85a", []string{"\u0085a"}},
	} {
		got := listParam(q(tc.query), "etape")
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("listParam(%q) = %#v, want %#v", tc.query, got, tc.want)
		}
	}
}

func TestReplaceInvalidUTF8(t *testing.T) {
	for in, want := range map[string]string{"abc": "abc", "caf\xe9": "caf�", "\xff\xfe": "��", "é": "é"} {
		if got := replaceInvalidUTF8(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestCompareUTF16(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"a", "a", 0}, {"a", "b", -1}, {"b", "a", 1}, {"", "a", -1}, {"a", "", 1}, {"ab", "a", 1},
		{"\U0001F600", "～", -1}, // D83D < FF5E although the UTF-8 bytes order the other way
		{"～", "\U0001F600", 1}, {"\U0001F600", "\U0001F601", -1},
	} {
		if got := compareUTF16(tc.a, tc.b); got != tc.want {
			t.Errorf("compareUTF16(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// The routes, end to end through the httpx pipeline (no database).
func TestFicheRoutes(t *testing.T) {
	ta := newTestApp(t, func(uri string) string {
		if strings.Contains(uri, "unknown") {
			return envelope()
		}
		return envelope(ficheDoc("fi-1", nil))
	}).routes()

	r := ta.get("/fiches_inventaire?etape=Lancement,Analyse&titre=a&titre=b")
	if r.status != 200 || !strings.HasPrefix(r.body, `[{"id":"fi-1","etapeLancementHtml":"<p>L</p>"`) {
		t.Fatalf("%d %s", r.status, r.body)
	}
	want := "fiche-inventaires?pagination[pageSize]=100&populate=*&filters[titre][$containsi]=a%2Cb&filters[etape][$in]=Lancement&filters[etape][$in]=Analyse&sort[0]=debut:desc"
	if got := ta.strapi.lastURI(); got != want {
		t.Fatalf("uri %s", got)
	}
	if !strings.Contains(r.body, `"debut":"2024-01-01 00:00:00","fin":"2024-12-31 00:00:00"`) || !strings.Contains(r.body, `"thematique":{"label":"Démocratie","picto":"\uD83D\uDDF3"}`) {
		t.Fatal(r.body)
	}

	r = ta.get("/fiches_inventaire/fi-1")
	if r.status != 200 || !strings.HasPrefix(r.body, `{"id":"fi-1"`) {
		t.Fatalf("%d %s", r.status, r.body)
	}
	r = ta.get("/fiches_inventaire/unknown")
	if r.status != 404 || r.body != `{"title":"Veuillez renseigner un id de fiche inventaire existant."}` {
		t.Fatalf("%d %s", r.status, r.body)
	}
	// ?mediaType=foo: the 404 survives with an empty body
	r = ta.get("/fiches_inventaire/unknown?mediaType=foo")
	if r.status != 404 || r.body != "" {
		t.Fatalf("%d %q", r.status, r.body)
	}
	r = ta.get("/fiches_inventaire/unknown?mediaType=xml")
	if r.status != 404 || r.body != `<ErrorResponse><title>Veuillez renseigner un id de fiche inventaire existant.</title></ErrorResponse>` {
		t.Fatalf("%d %s", r.status, r.body)
	}
	r = ta.get("/fiches_inventaire?mediaType=xml")
	if r.status != 200 || !strings.HasPrefix(r.body, "<List><item><id>fi-1</id>") {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if r := ta.get("/fiches_inventaire/a%2Fb"); r.status != 400 { // firewall
		t.Fatalf("%d", r.status)
	}
}
