package content

import (
	"context"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/jsonjava"
)

func ldt(y int64, m, d, h, mi, s, n int) LocalDateTime {
	return LocalDateTime{LocalDate{y, m, d}, h, mi, s, n}
}

// Port of GetLastNewsUseCaseTest (+ ties and bounds).
func TestLastNews(t *testing.T) {
	today := ldt(2024, 12, 25, 12, 0, 0, 0)
	mk := func(desc string, d LocalDateTime) news { return news{description: desc, beginDate: d} }

	t.Run("pasts and future news: the last one started (today included)", func(t *testing.T) {
		list := []news{
			mk("today", today),
			mk("future", ldt(2024, 12, 26, 12, 0, 0, 0)),
			mk("past", ldt(2024, 12, 24, 12, 0, 0, 0)),
		}
		got := lastNews(list, today)
		if got == nil || got.Description != "today" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("no news before today: null", func(t *testing.T) {
		if got := lastNews([]news{mk("future", ldt(2024, 12, 26, 12, 0, 0, 0))}, today); got != nil {
			t.Fatalf("%+v", got)
		}
		if got := lastNews(nil, today); got != nil {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("one nanosecond after now is not started", func(t *testing.T) {
		if got := lastNews([]news{mk("a", ldt(2024, 12, 25, 12, 0, 0, 1))}, today); got != nil {
			t.Fatalf("%+v", got)
		}
		if got := lastNews([]news{mk("a", ldt(2024, 12, 25, 11, 59, 59, 999_999_999))}, today); got == nil {
			t.Fatal("expected news")
		}
	})
	t.Run("maxByOrNull keeps the first maximum", func(t *testing.T) {
		d := ldt(2024, 1, 1, 0, 0, 0, 0)
		got := lastNews([]news{mk("first", d), mk("second", d), mk("older", ldt(2023, 1, 1, 0, 0, 0, 0))}, today)
		if got == nil || got.Description != "first" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("the fields of NewsJson", func(t *testing.T) {
		arg := "co1"
		got := lastNews([]news{{"d", "s", "c", "r", &arg, ldt(2020, 1, 1, 0, 0, 0, 0)}}, today)
		if got == nil || *got.RouteArgument != "co1" || got.ShortDescription != "s" || got.CallToActionText != "c" || got.RouteName != "r" {
			t.Fatalf("%+v", got)
		}
		b := jsonjava.MarshalString(*got)
		if b != `{"description":"d","short_description":"s","callToActionText":"c","routeName":"r","routeArgument":"co1"}` {
			t.Fatal(b)
		}
		got.RouteArgument = nil
		if b := jsonjava.MarshalString(*got); !strings.HasSuffix(b, `"routeArgument":null}`) {
			t.Fatal(b)
		}
	})
}

func newsDoc(date any, over map[string]any) map[string]any {
	d := map[string]any{
		"documentId": "n", "message": []any{para("Message")}, "short_message": "court", "call_to_action": "Go",
		"date_de_debut": date, "page_route_mobile_enum": "qag", "page_route_argument_mobile": nil,
	}
	for k, v := range over {
		d[k] = v
	}
	return d
}

func TestLastNewsRoute(t *testing.T) {
	ta := newTestApp(t, constBody(envelope(
		newsDoc("2026-10-07T12:00:00.000Z", map[string]any{"short_message": "future"}),
		newsDoc("2026-09-01T10:00:00.000Z", map[string]any{"short_message": "recent", "page_route_argument_mobile": "arg"}),
		newsDoc("2026-01-01T10:00:00.000Z", map[string]any{"short_message": "old"}),
	))).routes()
	r := ta.get("/welcome_page/last_news")
	if r.status != 200 || r.body != `{"description":"<p>Message</p>","short_description":"recent","callToActionText":"Go","routeName":"qag","routeArgument":"arg"}` {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if got, want := ta.strapi.lastURI(), "welcome-page-news?pagination[pageSize]=100&populate=*&sort[0]=date_de_debut:desc"; got != want {
		t.Fatalf("uri %s", got)
	}
	// 404 without a body, also when Strapi fails
	for _, body := range []string{envelope(), "garbage", `{"data":null}`, envelope(newsDoc("2099-01-01T00:00:00Z", nil)), envelope(newsDoc("bad date", nil))} {
		ta := newTestApp(t, constBody(body)).routes()
		if r := ta.get("/welcome_page/last_news"); r.status != 404 || r.body != "" {
			t.Fatalf("%s: %d %q", body, r.status, r.body)
		}
	}
	// a null element of data makes the mapper throw: 500
	ta = newTestApp(t, constBody(`{"data":[null],"meta":{"pagination":{"page":1,"pageSize":1,"pageCount":1,"total":1}}}`)).routes()
	if r := ta.get("/welcome_page/last_news"); r.status != 500 {
		t.Fatalf("%d", r.status)
	}
}

func TestCharterRoute(t *testing.T) {
	doc := func(date string) map[string]any {
		return map[string]any{"documentId": "c", "charte": []any{para("Charte")}, "charte_preview": []any{para("Aperçu")}, "datetime_debut": date}
	}
	ta := newTestApp(t, constBody(envelope(doc("2026-01-01T00:00:00.000Z"), doc("2025-01-01T00:00:00.000Z")))).routes()
	r := ta.get("/participation_charter")
	if r.status != 200 || r.body != `{"extraText":"<body><p>Charte</p></body>","previewText":"<body><p>Aperçu</p></body>"}` {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if len(r.header["ETag"]) == 0 {
		t.Fatal("missing ETag")
	}
	if got, want := ta.strapi.lastURI(), "charte-participations?pagination[pageSize]=100&populate=*&filters[datetime_debut][$lt]=2026-10-06T12:00:00&sort[0]=datetime_debut:desc"; got != want {
		t.Fatalf("uri %s", got)
	}
	if r2 := ta.do("GET", "/participation_charter", map[string]string{"If-None-Match": r.header["ETag"][0]}); r2.status != 304 {
		t.Fatalf("%d", r2.status)
	}
	// an empty list (also a Strapi failure) is a NoSuchElementException: 500
	for _, body := range []string{envelope(), "garbage", envelope(doc("not a date"))} {
		ta := newTestApp(t, constBody(body)).routes()
		if r := ta.get("/participation_charter"); r.status != 500 {
			t.Fatalf("%s: %d", body, r.status)
		}
	}
}

func TestMicroCache(t *testing.T) {
	// class B: a successful Strapi load is shared for AGORA_MICROCACHE_TTL, an empty news list or a failure never
	ta := newTestApp(t, constBody(envelope(newsDoc("2020-01-01T00:00:00Z", nil)))).routes()
	ta.Cfg.MicroCacheTTL = time.Minute
	for i := 0; i < 3; i++ {
		ta.get("/welcome_page/last_news")
	}
	if n := ta.strapi.count(); n != 1 {
		t.Fatalf("strapi called %d times", n)
	}
	ta = newTestApp(t, constBody(envelope())).routes()
	ta.Cfg.MicroCacheTTL = time.Minute
	for i := 0; i < 3; i++ {
		ta.get("/welcome_page/last_news")
	}
	if n := ta.strapi.count(); n != 3 {
		t.Fatalf("empty list cached: strapi called %d times", n)
	}
	ta = newTestApp(t, constBody("garbage")).routes()
	ta.Cfg.MicroCacheTTL = time.Minute
	for i := 0; i < 3; i++ {
		if r := ta.get("/content/page-poser-ma-question"); r.status != 500 {
			t.Fatal(r.status)
		}
	}
	if n := ta.strapi.count(); n != 3 {
		t.Fatalf("failure cached: strapi called %d times", n)
	}
}

func TestPageRoutes(t *testing.T) {
	rich := []any{map[string]any{"type": "heading", "level": 2, "children": []any{map[string]any{"type": "text", "text": "T", "bold": true}}}, para("a & <b>")}
	pages := map[string]struct {
		model string
		doc   map[string]any
		want  string
	}{
		"page-poser-ma-question": {"page-poser-ma-question", map[string]any{"texte_regles": rich},
			`{"regles":"<h2><b>T</b></h2><p>a & <b></p>"}`},
		"page-reponses-aux-qags": {"page-reponse-aux-questions-au-gouvernement", map[string]any{"information_reponse_a_venir_bottomsheet": "info"},
			`{"infoReponsesAVenir":"info"}`},
		"page-site-vitrine-accueil": {"site-vitrine-accueil", map[string]any{"titre_header": "h", "sous_titre_header": "sh", "titre_body": "b", "description_body": "d",
			"texte_image_1": rich, "texte_image_2": []any{}, "texte_image_3": []any{para("3")}},
			`{"titreHeader":"h","sousTitreHeader":"sh","titreBody":"b","descriptionBody":"d","texteImage1":"<h2><b>T</b></h2><p>a & <b></p>","texteImage2":"","texteImage3":"<p>3</p>"}`},
		"page-site-vitrine-conditions-generales": {"site-vitrine-conditions-generales-d-utilisation", map[string]any{"conditions_generales_d_utilisation": rich},
			`{"conditionsGeneralesDUtilisation":"<h2><b>T</b></h2><p>a & <b></p>"}`},
		"page-site-vitrine-consultation":              {"site-vitrine-consultation", map[string]any{"donnez_votre_avis": []any{para("x")}}, `{"donnezVotreAvis":"<p>x</p>"}`},
		"page-site-vitrine-declaration-accessibilite": {"site-vitrine-declaration-d-accessibilite", map[string]any{"declaration": []any{para("x")}}, `{"declaration":"<p>x</p>"}`},
		"page-site-vitrine-mentions-legales":          {"site-vitrine-mentions-legale", map[string]any{"mentions_legales": []any{para("x")}}, `{"mentionsLegales":"<p>x</p>"}`},
		"page-site-vitrine-politique-confidentialite": {"site-vitrine-politique-de-confidentialite", map[string]any{"politique_de_confidentialite": []any{para("x")}},
			`{"politiqueDeConfidentialite":"<p>x</p>"}`},
		"page-site-vitrine-question-au-gouvernement": {"site-vitrine-question-au-gouvernement", map[string]any{"titre": "t", "sous_titre": "st", "texte_soutien": []any{para("s")}},
			`{"titre":"t","sousTitre":"st","texteSoutien":"<p>s</p>"}`},
	}
	for route, p := range pages {
		t.Run(route, func(t *testing.T) {
			body := mustJSON(map[string]any{"data": p.doc, "meta": map[string]any{}})
			ta := newTestApp(t, constBody(body)).routes()
			r := ta.get("/content/" + route)
			if r.status != 200 || r.body != p.want || r.header.Get("Cache-Control") != "max-age=300, public" {
				t.Fatalf("%d %s %v", r.status, r.body, r.header)
			}
			if got := ta.strapi.lastURI(); got != p.model+"?pagination[pageSize]=100&populate=*" {
				t.Fatalf("uri %s", got)
			}
			// a single type failing is a 500
			for _, bad := range []string{"garbage", `{"data":null}`, `{"data":{}}`} {
				if r := newTestApp(t, constBody(bad)).routes().get("/content/" + route); r.status != 500 {
					t.Fatalf("%s: %d", bad, r.status)
				}
			}
			if r := ta.get("/content/" + route + "?mediaType=foo"); r.status != 406 || r.header.Get("Cache-Control") == "max-age=300, public" {
				t.Fatalf("%d %v", r.status, r.header)
			}
		})
	}

	t.Run("page-questions-au-gouvernement", func(t *testing.T) {
		old := qagCount
		qagCount = func(context.Context, *app.App) int { return 42 }
		defer func() { qagCount = old }()
		doc := func(over map[string]any) string {
			d := map[string]any{"information_bottomsheet": "i", "nombre_de_questions": "{} questions, {}!", "programme_du_mois": nil, "comment_ca_marche": nil}
			for k, v := range over {
				d[k] = v
			}
			return mustJSON(map[string]any{"data": d, "meta": map[string]any{}})
		}
		ta := newTestApp(t, constBody(doc(nil))).routes()
		if r := ta.get("/content/page-questions-au-gouvernement"); r.status != 200 ||
			r.body != `{"info":"i","texteTotalQuestions":"42 questions, 42!","programmeDuMois":null,"commentCaMarche":null}` {
			t.Fatalf("%d %s", r.status, r.body)
		}
		ta = newTestApp(t, constBody(doc(map[string]any{"programme_du_mois": []any{para("p")}, "comment_ca_marche": "c"}))).routes()
		if r := ta.get("/content/page-questions-au-gouvernement"); r.body != `{"info":"i","texteTotalQuestions":"42 questions, 42!","programmeDuMois":"<p>p</p>","commentCaMarche":"c"}` {
			t.Fatal(r.body)
		}
		// optional properties may be missing
		d := map[string]any{"data": map[string]any{"information_bottomsheet": "i", "nombre_de_questions": "n"}}
		ta = newTestApp(t, constBody(mustJSON(d))).routes()
		if r := ta.get("/content/page-questions-au-gouvernement"); r.body != `{"info":"i","texteTotalQuestions":"n","programmeDuMois":null,"commentCaMarche":null}` {
			t.Fatal(r.body)
		}
	})
}

func TestWithQagCount(t *testing.T) {
	for in, want := range map[string]string{"{}": "7", "a {} b {} c": "a 7 b 7 c", "{ }": "{ }", "{{}}": "{7}", "": "", "$1 {} \\1": "$1 7 \\1"} {
		if got := withQagCount(pageQuestionAuGouvernementContent{texteTotalQuestions: in}, 7).texteTotalQuestions; got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// AppFeedbackJsonMapper.toDomain
func TestFeedbackToDomain(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want appFeedbackType
		ok   bool
	}{
		{"bug", "bug", true}, {"BUG", "bug", true}, {"Bug", "bug", true}, {"feature", "feature", true}, {"FEATURE", "feature", true},
		{"comment", "comment", true}, {"CoMmEnT", "comment", true},
		{"", "", false}, {" bug", "", false}, {"bug ", "", false}, {"feature_request", "", false}, {"FEATURE_REQUEST", "", false}, {"bugs", "", false},
		{"BUG\u0000", "", false}, {"İ", "", false},
	} {
		got, ok := feedbackToDomain(AppFeedbackJSON{Type: tc.in, Description: "d"}, "u")
		if ok != tc.ok || got.typ != tc.want {
			t.Errorf("%q: %v %q, want %v %q", tc.in, ok, got.typ, tc.ok, tc.want)
		}
	}
	dev := &AppFeedbackDeviceInfoJSON{"m", "o", "a"}
	got, _ := feedbackToDomain(AppFeedbackJSON{"bug", "desc", dev}, "user")
	if got.userID != "user" || got.description != "desc" || got.deviceInfo != dev {
		t.Fatalf("%+v", got)
	}
}

// The request DTO decoding (Jackson + Kotlin module rules).
func TestFeedbackDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string // re-encoded, or "ERR"
	}{
		{`{"type":"bug","description":"d"}`, `{"type":"bug","description":"d","deviceInfo":null}`},
		{`{"type":"bug","description":"d","deviceInfo":null}`, `{"type":"bug","description":"d","deviceInfo":null}`},
		{`{"type":"bug","description":"d","deviceInfo":{"model":"m","osVersion":"o","appVersion":"a"},"extra":1}`,
			`{"type":"bug","description":"d","deviceInfo":{"model":"m","osVersion":"o","appVersion":"a"}}`},
		{`{"type":1,"description":2.5}`, `{"type":"1","description":"2.5","deviceInfo":null}`},
		{`{"type":null,"description":"d"}`, "ERR"},
		{`{"description":"d"}`, "ERR"},
		{`{"type":"bug"}`, "ERR"},
		{`{"type":"bug","description":"d","deviceInfo":{}}`, "ERR"},
		{`{"type":"bug","description":"d","deviceInfo":"x"}`, "ERR"},
		{`[]`, "ERR"}, {`null`, "ERR"}, {``, "ERR"}, {`{`, "ERR"},
		{`{"type":"bug","description":"d"} trailing`, `{"type":"bug","description":"d","deviceInfo":null}`},
	} {
		var body AppFeedbackJSON
		err := jsonjava.Unmarshal([]byte(tc.in), &body)
		got := "ERR"
		if err == nil {
			got = jsonjava.MarshalString(body)
		}
		if got != tc.want {
			t.Errorf("%s: %s, want %s", tc.in, got, tc.want)
		}
	}
}
