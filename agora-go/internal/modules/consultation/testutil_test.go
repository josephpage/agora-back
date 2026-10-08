package consultation

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/jsonjava"
	"agora/internal/strapi"
)

// ---------------------------------------------------------------------------
// Consultation payloads
// ---------------------------------------------------------------------------

type obj = map[string]any

func richText(paras ...string) []any {
	out := []any{}
	for _, p := range paras {
		out = append(out, obj{"type": "paragraph", "children": []any{obj{"type": "text", "text": p}}})
	}
	return out
}

// tdt formats a LocalDateTime like Strapi does.
func tdt(t time.Time) string { return t.Format("2006-01-02T15:04:05.000") + "Z" }

type consultationSpec struct {
	id, slug      string
	start, end    time.Time
	autres        []obj
	analyse       obj
	commanditaire obj
	aVenir        obj
	questions     []any
	extra         obj
}

var baseTime = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func contenu(documentID, slug string, extra obj) obj {
	o := obj{
		"documentId": documentID, "slug": slug, "template_partage": "Participez : {title} {url}", "historique_titre": "Titre " + documentID,
		"historique_call_to_action": "Action " + documentID, "feedback_message": "Avis ?", "sections": []any{},
	}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

func autreContenu(documentID string, when time.Time, extra obj) obj {
	e := obj{"datetime_publication": tdt(when)}
	for k, v := range extra {
		e[k] = v
	}
	return contenu(documentID, "slug-"+documentID, e)
}

func (s consultationSpec) json() obj {
	if s.start.IsZero() {
		s.start = baseTime.AddDate(0, 0, -20)
	}
	if s.end.IsZero() {
		s.end = baseTime.AddDate(0, 0, 20)
	}
	autres := []any{}
	for _, a := range s.autres {
		autres = append(autres, a)
	}
	questions := s.questions
	if questions == nil {
		questions = []any{}
	}
	o := obj{
		"documentId": s.id, "titre_consultation": "Titre " + s.id, "slug": s.slug, "datetime_de_debut": tdt(s.start), "datetime_de_fin": tdt(s.end),
		"url_image_de_couverture": "https://img/cover.jpg", "url_image_page_de_contenu": "https://img/page.jpg", "nombre_de_questions": 3,
		"estimation_nombre_de_questions": "3 questions", "estimation_temps": "5 minutes", "nombre_participants_cible": 100,
		"thematique":                               obj{"documentId": "th1", "label": "Santé", "pictogramme": "S"},
		"questions":                                questions,
		"consultation_avant_reponse":               contenu("avant-"+s.id, "lancement", obj{"commanditaire": richText("Le Gouvernement"), "objectif": richText("Objectif"), "axe_gouvernemental": richText("Axe"), "presentation": richText("Premier.", "Second.")}),
		"consultation_apres_reponse_ou_terminee":   contenu("apres-"+s.id, "fin", nil),
		"consultation_contenu_analyse_des_reponse": s.analyse, "contenu_reponse_du_commanditaires": s.commanditaire,
		"consultation_contenu_autres": autres, "consultation_contenu_a_venir": s.aVenir,
		"territoire": "France", "titre_page_web": "Web", "sous_titre_page_web": "Sous-titre", "image_de_couverture": nil, "image_page_de_contenu": nil,
	}
	for k, v := range s.extra {
		o[k] = v
	}
	return o
}

func analyseContenu(documentID string, when time.Time, extra obj) obj {
	o := obj{"lien_telechargement_analyse": "https://pdf/" + documentID + ".pdf", "datetime_publication": tdt(when), "flamme_label": nil, "recap_emoji": nil, "recap_label": nil, "pdf_analyse": nil}
	for k, v := range extra {
		o[k] = v
	}
	return contenu(documentID, "slug-"+documentID, o)
}

func commanditaireContenu(documentID string, when time.Time, extra obj) obj {
	o := obj{"datetime_publication": tdt(when), "flamme_label": nil, "recap_emoji": nil, "recap_label": nil}
	for k, v := range extra {
		o[k] = v
	}
	return contenu(documentID, "slug-"+documentID, o)
}

func decodeConsultation(t testing.TB, o obj) *strapiConsultation {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var c strapiConsultation
	if err := jsonjava.Unmarshal(b, &c); err != nil {
		t.Fatalf("decoding the consultation: %v", err)
	}
	return &c
}

func ldt(y int, m time.Month, d, h, mi, s int) LocalDateTime {
	return LocalDateTime{T: time.Date(y, m, d, h, mi, s, 0, time.UTC)}
}

// ---------------------------------------------------------------------------
// Fake Strapi and App
// ---------------------------------------------------------------------------

// fakeStrapi serves the consultations it holds: a request filtered by
// documentId / slug gets the matching ones, any other request gets all of them.
type fakeStrapi struct {
	srv  *httptest.Server
	mu   sync.Mutex
	data []obj
	uris []string
	fail bool
}

func newFakeStrapi(t testing.TB) *fakeStrapi {
	t.Helper()
	f := &fakeStrapi{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.uris = append(f.uris, strings.TrimPrefix(r.RequestURI, "/api/"))
		fail := f.fail
		data := append([]obj(nil), f.data...)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			_, _ = io.WriteString(w, `{"data":null,"error":{"status":500}}`)
			return
		}
		q, _ := url.ParseQuery(strings.SplitN(r.RequestURI, "?", 2)[1])
		var out []any
		for _, c := range data {
			ok := true
			if ids := q["filters[documentId][$in]"]; len(ids) > 0 {
				ok = ok && contains(ids, c["documentId"])
			}
			if slugs := q["filters[slug][$in]"]; len(slugs) > 0 {
				ok = ok && contains(slugs, c["slug"])
			}
			if ok {
				out = append(out, c)
			}
		}
		if out == nil {
			out = []any{}
		}
		_ = json.NewEncoder(w).Encode(obj{"data": out, "meta": obj{"pagination": obj{"page": 1, "pageSize": 100, "pageCount": 1, "total": len(out)}}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func contains(l []string, v any) bool {
	s, _ := v.(string)
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (f *fakeStrapi) set(data ...obj) {
	f.mu.Lock()
	f.data = data
	f.mu.Unlock()
}

func (f *fakeStrapi) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.uris)
}

func (f *fakeStrapi) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uris) == 0 {
		return ""
	}
	return f.uris[len(f.uris)-1]
}

type testApp struct {
	*app.App
	strapi *fakeStrapi
	now    time.Time
}

func newTestApp(t testing.TB) *testApp {
	t.Helper()
	f := newFakeStrapi(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ta := &testApp{strapi: f, now: baseTime}
	ta.App = &app.App{
		Cfg:    &config.Config{MicroCacheTTL: 0},
		Cache:  cache.New(nil, log, false),
		Strapi: strapi.NewClient(strapi.Options{BaseURL: f.srv.URL + "/api/", Token: "token", Timeout: 5 * time.Second, Logger: log}),
		Log:    log,
		Clock:  func() time.Time { return ta.now },
	}
	return ta
}

func (ta *testApp) strapiRepo() *StrapiRepository {
	r := newStrapiRepository(ta.App)
	r.now = ta.App.Clock
	return r
}
