package fakestrapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The shipped fixtures must load and honour the shared ID plan.

const realFixtures = "../fixtures/strapi"

var collections = []string{
	"consultations", "theme-hebdos", "concertations", "thematiques", "charte-participations",
	"welcome-page-news", "fiche-inventaires", "reponse-du-gouvernements", "cluster-semaine-libres",
	"qa-g-headers-onglets",
}

var singleTypes = []string{
	"page-reponse-aux-questions-au-gouvernement", "page-poser-ma-question", "page-questions-au-gouvernement",
	"site-vitrine-accueil", "site-vitrine-conditions-generales-d-utilisation", "site-vitrine-consultation",
	"site-vitrine-declaration-d-accessibilite", "site-vitrine-mentions-legale",
	"site-vitrine-politique-de-confidentialite", "site-vitrine-question-au-gouvernement",
}

func realServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(realFixtures, refNow)
	if err != nil {
		t.Fatalf("load shipped fixtures: %v", err)
	}
	return s
}

func fetch(t *testing.T, s *Server, target string) (int, *Object) {
	t.Helper()
	rec, body := do(t, s, "GET", target, "", nil)
	v, err := ParseJSON([]byte(body))
	if err != nil {
		t.Fatalf("%s: invalid JSON: %v", target, err)
	}
	return rec.Code, v.(*Object)
}

func dataList(t *testing.T, env *Object) []*Object {
	t.Helper()
	d, _ := env.Get("data")
	var out []*Object
	for _, e := range d.([]any) {
		out = append(out, e.(*Object))
	}
	return out
}

func str(o *Object, k string) string {
	v, _ := o.Get(k)
	s, _ := v.(string)
	return s
}

func TestShippedFixturesLoad(t *testing.T) {
	s := realServer(t)
	have := map[string]bool{}
	for _, m := range s.Models() {
		have[m] = true
	}
	for _, m := range append(append([]string{}, collections...), singleTypes...) {
		if !have[m] {
			t.Errorf("missing fixture for model %q", m)
		}
	}
	for _, m := range collections {
		code, env := fetch(t, s, "/api/"+m+"?pagination[pageSize]=100&status=draft")
		if code != 200 || len(dataList(t, env)) == 0 {
			t.Errorf("%s: status %d / empty", m, code)
		}
	}
	for _, m := range singleTypes {
		code, env := fetch(t, s, "/api/"+m)
		if d, _ := env.Get("data"); code != 200 || d == nil {
			t.Errorf("%s: status %d / null data", m, code)
		}
	}
}

func TestShippedFixturesHaveNoUnresolvedTemplates(t *testing.T) {
	s := realServer(t)
	for _, m := range s.Models() {
		_, body := do(t, s, "GET", "/api/"+m+"?status=draft&pagination[pageSize]=100", "", nil)
		if strings.Contains(body, "{{") {
			t.Errorf("%s still contains a template", m)
		}
	}
}

func TestShippedIDPlan(t *testing.T) {
	s := realServer(t)

	_, env := fetch(t, s, "/api/thematiques?pagination[pageSize]=100")
	th := dataList(t, env)
	if len(th) != 6 {
		t.Fatalf("thematiques = %d", len(th))
	}
	for i, d := range th {
		if want := fmt.Sprintf("th%022d", i+1); str(d, "documentId") != want || str(d, "label") == "" || str(d, "pictogramme") == "" {
			t.Errorf("thematique %d = %s", i+1, str(d, "documentId"))
		}
	}

	// consultations: published ones vs draft listing
	_, env = fetch(t, s, "/api/consultations?pagination[pageSize]=100")
	if n := len(dataList(t, env)); n != 6 {
		t.Errorf("published consultations = %d, want 6 (co6 is unpublished)", n)
	}
	_, env = fetch(t, s, "/api/consultations?pagination[pageSize]=100&status=draft")
	cons := dataList(t, env)
	if len(cons) != 7 {
		t.Fatalf("draft consultations = %d, want 7", len(cons))
	}
	for i, c := range cons {
		n := i + 1
		if str(c, "documentId") != fmt.Sprintf("co%022d", n) || str(c, "slug") != fmt.Sprintf("consultation-%d", n) {
			t.Errorf("consultation %d = %s / %s", n, str(c, "documentId"), str(c, "slug"))
		}
		qs, _ := c.Get("questions")
		for qi, qv := range qs.([]any) {
			q := qv.(*Object)
			id, _ := q.Get("id")
			if string(id.(json.Number)) != fmt.Sprint(n*100+qi+1) {
				t.Errorf("co%d question %d id = %v", n, qi+1, id)
			}
			if ch, ok := q.Get("choix"); ok {
				for ki, cv := range ch.([]any) {
					cid, _ := cv.(*Object).Get("id")
					if string(cid.(json.Number)) != fmt.Sprint(n*1000+(qi+1)*10+ki+1) {
						t.Errorf("co%d q%d choice %d id = %v", n, qi+1, ki+1, cid)
					}
				}
			}
		}
	}

	// ongoing = datetime_de_debut < now < datetime_de_fin, the exact request the Kotlin backend sends
	q := "/api/consultations?pagination[pageSize]=100&populate=*&filters[datetime_de_debut][$lt]=2026-10-06T21:52:47.123456" +
		"&filters[datetime_de_fin][$gt]=2026-10-06T21:52:47.123456"
	_, env = fetch(t, s, q)
	if got := slugs(dataList(t, env)); got != "consultation-1,consultation-2,consultation-3" {
		t.Errorf("ongoing = %s", got)
	}
	_, env = fetch(t, s, q+"&status=draft")
	if got := slugs(dataList(t, env)); got != "consultation-1,consultation-2,consultation-3,consultation-6" {
		t.Errorf("ongoing (draft) = %s", got)
	}
	_, env = fetch(t, s, "/api/consultations?filters[datetime_de_fin][$lt]=2026-10-06T21:52:47.123456")
	if got := slugs(dataList(t, env)); got != "consultation-4,consultation-5,consultation-7" {
		t.Errorf("finished = %s", got)
	}
	_, env = fetch(t, s, "/api/consultations?filters[datetime_de_fin][$lt]=2026-09-22T21:52:47.123456") // today - 14 days
	if got := slugs(dataList(t, env)); got != "consultation-7" {
		t.Errorf("ended > 14 days ago = %s", got)
	}

	_, env = fetch(t, s, "/api/reponse-du-gouvernements?pagination[pageSize]=100&sort[0]=reponseDate:desc")
	var qids []string
	for _, d := range dataList(t, env) {
		qids = append(qids, str(d, "questionId"))
	}
	want := "00000000-0000-4000-8000-0000000000a1,00000000-0000-4000-8000-0000000000a2,00000000-0000-4000-8000-0000000000a3"
	if strings.Join(qids, ",") != want {
		t.Errorf("responses = %v", qids)
	}
	_, env = fetch(t, s, "/api/reponse-du-gouvernements?filters[questionId][$in]=00000000-0000-4000-8000-0000000000a2")
	if len(dataList(t, env)) != 1 {
		t.Error("questionId filter")
	}
}

func slugs(docs []*Object) string {
	var out []string
	for _, d := range docs {
		out = append(out, str(d, "slug"))
	}
	return strings.Join(out, ",")
}

func TestShippedThemeHebdoHasCurrentPastAndFuture(t *testing.T) {
	s := realServer(t)
	_, env := fetch(t, s, "/api/theme-hebdos?pagination[pageSize]=100")
	var past, current, future, libre int
	for _, d := range dataList(t, env) {
		debut, _ := ParseTime(str(d, "date_debut"))
		fin, _ := ParseTime(str(d, "date_fin"))
		switch {
		case fin.Before(refNow):
			past++
		case debut.After(refNow):
			future++
		default:
			current++
		}
		if v, _ := d.Get("est_theme_libre"); v == true {
			libre++
		}
	}
	if past < 1 || current != 1 || future < 3 || libre < 1 {
		t.Errorf("past=%d current=%d future=%d libre=%d", past, current, future, libre)
	}
}

func TestShippedFicheInventaireFilters(t *testing.T) {
	s := realServer(t)
	count := func(q string) int {
		code, env := fetch(t, s, "/api/fiche-inventaires?pagination[pageSize]=100&populate=*&"+q)
		if code != 200 {
			t.Fatalf("%s => %d", q, code)
		}
		return len(dataList(t, env))
	}
	if n := count(""); n < 4 {
		t.Errorf("fiches = %d", n)
	}
	if count("filters[etape][$in]=Lancement") == count("filters[etape][$in]=Analyse") && count("filters[etape][$in]=Suivi") == 0 {
		t.Error("etape filter does not discriminate")
	}
	if n := count("filters[thematique][documentId][$in]=th0000000000000000000001"); n != 2 {
		t.Errorf("thematique filter = %d", n)
	}
	if n := count("filters[annee_de_lancement][$in]=2024"); n != 2 {
		t.Errorf("annee filter = %d", n)
	}
	if n := count("filters[titre][$containsi]=consultation"); n < 2 {
		t.Errorf("titre filter = %d", n)
	}
	_, env := fetch(t, s, "/api/fiche-inventaires?sort[0]=debut:desc")
	list := dataList(t, env)
	for i := 1; i < len(list); i++ {
		if str(list[i-1], "debut") < str(list[i], "debut") {
			t.Errorf("fiches not sorted by debut desc: %s then %s", str(list[i-1], "debut"), str(list[i], "debut"))
		}
	}
}

func TestShippedFixturesCoverEveryRichTextAndSectionType(t *testing.T) {
	s := realServer(t)
	var all strings.Builder
	for _, m := range s.Models() {
		_, body := do(t, s, "GET", "/api/"+m+"?status=draft&pagination[pageSize]=100", "", nil)
		all.WriteString(body)
	}
	text := all.String()
	for _, needle := range []string{
		`"type":"text"`, `"bold":true`, `"italic":true`, `"underline":true`, `"strikethrough":true`, `"code":true`,
		`"type":"link"`, `"type":"list-item"`, `"format":"ordered"`, `"format":"unordered"`, `"type":"paragraph"`,
		`"type":"quote"`, `"type":"heading"`, `"level":1`, `"level":2`, `"level":3`, `"level":4`, `"level":5`,
		`"level":6`, `"level":7`, `"type":"code"`,
		`consultation-section.section-titre`, `consultation-section.section-texte-riche`,
		`consultation-section.section-citation`, `consultation-section.section-image`,
		`consultation-section.section-video`, `consultation-section.section-chiffre`,
		`consultation-section.section-accordeon`,
		`question-de-consultation.question-a-choix-unique`, `question-de-consultation.question-a-choix-multiples`,
		`question-de-consultation.question-ouverte`, `question-de-consultation.question-conditionnelle`,
		`question-de-consultation.description`, `reponse.reponse-video`, `reponse.reponsetextuelle`,
		`"slug":"fin-de-la-consultation"`,
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("fixtures never contain %s", needle)
		}
	}
}

// ---------------------------------------------------------------------------
// Structural check: every non-nullable field of the Kotlin Strapi DTOs
// (infrastructure/**/Strapi*DTO*.kt) must be present and non-null in the
// fixtures. When the Kotlin side silently returns empty lists, a missing
// required field is almost always the reason.

var requiredTop = map[string][]string{
	"thematiques": {"documentId", "label", "pictogramme"},
	"consultations": {"documentId", "titre_consultation", "slug", "datetime_de_debut", "datetime_de_fin",
		"url_image_de_couverture", "url_image_page_de_contenu", "nombre_de_questions",
		"estimation_nombre_de_questions", "estimation_temps", "nombre_participants_cible", "thematique",
		"questions", "consultation_avant_reponse", "consultation_apres_reponse_ou_terminee",
		"consultation_contenu_autres", "territoire", "titre_page_web", "sous_titre_page_web"},
	"concertations":          {"documentId", "titre", "url", "image_url", "datetime_publication", "thematique"},
	"theme-hebdos":           {"theme", "date_debut", "date_fin"},
	"charte-participations":  {"charte", "charte_preview", "datetime_debut"},
	"welcome-page-news":      {"message", "short_message", "call_to_action", "date_de_debut", "page_route_mobile_enum"},
	"cluster-semaine-libres": {"titre"},
	"qa-g-headers-onglets":   {"documentId", "titre", "message", "type", "datetime_publication"},
	"fiche-inventaires": {"documentId", "etape_1_lancement", "etape_2_analyse", "etape_3_suivi", "titre", "debut", "fin",
		"porteur", "lien_site", "condition_participation", "modalite_participation", "thematique", "illustration",
		"etape", "annee_de_lancement", "type"},
	"reponse-du-gouvernements": {"auteur", "auteurPortraitUrl", "reponseDate", "feedbackQuestion", "questionId", "reponseType"},

	"page-reponse-aux-questions-au-gouvernement":      {"information_reponse_a_venir_bottomsheet"},
	"page-poser-ma-question":                          {"texte_regles"},
	"page-questions-au-gouvernement":                  {"information_bottomsheet", "nombre_de_questions"},
	"site-vitrine-accueil":                            {"titre_header", "sous_titre_header", "titre_body", "description_body", "texte_image_1", "texte_image_2", "texte_image_3"},
	"site-vitrine-conditions-generales-d-utilisation": {"conditions_generales_d_utilisation"},
	"site-vitrine-consultation":                       {"donnez_votre_avis"},
	"site-vitrine-declaration-d-accessibilite":        {"declaration"},
	"site-vitrine-mentions-legale":                    {"mentions_legales"},
	"site-vitrine-politique-de-confidentialite":       {"politique_de_confidentialite"},
	"site-vitrine-question-au-gouvernement":           {"titre", "sous_titre", "texte_soutien"},
}

var requiredContenu = map[string][]string{
	"consultation_avant_reponse": {"documentId", "slug", "template_partage", "historique_titre",
		"historique_call_to_action", "commanditaire", "objectif", "axe_gouvernemental", "presentation"},
	"consultation_apres_reponse_ou_terminee": {"documentId", "slug", "template_partage", "feedback_message",
		"historique_titre", "historique_call_to_action"},
	"consultation_contenu_autres": {"documentId", "slug", "template_partage", "feedback_message", "historique_titre",
		"historique_call_to_action", "datetime_publication"},
	"consultation_contenu_analyse_des_reponse": {"documentId", "lien_telechargement_analyse", "slug", "template_partage",
		"datetime_publication", "feedback_message", "historique_titre", "historique_call_to_action"},
	"contenu_reponse_du_commanditaires": {"documentId", "slug", "template_partage", "datetime_publication",
		"feedback_message", "historique_titre", "historique_call_to_action"},
	"consultation_contenu_a_venir": {"titre_historique"},
}

var requiredComponent = map[string][]string{
	"consultation-section.section-titre":       {"id", "titre"},
	"consultation-section.section-texte-riche": {"id", "description"},
	"consultation-section.section-citation":    {"id", "description"},
	"consultation-section.section-image":       {"id", "url", "description_accessible_de_l_image"},
	"consultation-section.section-video": {"id", "url", "largeur", "hauteur", "nom_auteur", "poste_auteur",
		"date_tournage", "transcription"},
	"consultation-section.section-chiffre":   {"id", "titre", "description"},
	"consultation-section.section-accordeon": {"id", "titre", "description"},

	"question-de-consultation.question-a-choix-unique":    {"id", "titre", "numero"},
	"question-de-consultation.question-a-choix-multiples": {"id", "titre", "numero", "nombre_maximum_de_choix"},
	"question-de-consultation.question-ouverte":           {"id", "titre", "numero"},
	"question-de-consultation.description":                {"id", "titre", "numero", "description"},
	"question-de-consultation.question-conditionnelle":    {"id", "titre", "numero", "choix"},
	"reponse.reponsetextuelle":                            {"label", "text"},
	"reponse.reponse-video": {"auteurDescription", "urlVideo", "videoWidth", "videoHeight", "transcription",
		"page_title"},
}

func requireKeys(t *testing.T, ctx string, o *Object, keys []string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := o.Get(k); !ok || v == nil {
			t.Errorf("%s: required field %q missing or null", ctx, k)
		}
	}
}

func checkComponents(t *testing.T, ctx string, list any) {
	t.Helper()
	arr, ok := list.([]any)
	if !ok {
		return
	}
	for i, e := range arr {
		c := e.(*Object)
		comp := str(c, "__component")
		keys, known := requiredComponent[comp]
		if !known {
			t.Errorf("%s[%d]: unknown component %q", ctx, i, comp)
			continue
		}
		requireKeys(t, fmt.Sprintf("%s[%d] %s", ctx, i, comp), c, keys)
		if ch, ok := c.Get("choix"); ok {
			for j, cv := range ch.([]any) {
				requireKeys(t, fmt.Sprintf("%s[%d].choix[%d]", ctx, i, j), cv.(*Object), []string{"id", "label", "ouvert"})
			}
		}
	}
}

func TestShippedFixturesSatisfyKotlinDTOs(t *testing.T) {
	s := realServer(t)
	for model, keys := range requiredTop {
		code, env := fetch(t, s, "/api/"+model+"?status=draft&pagination[pageSize]=100")
		if code != 200 {
			t.Errorf("%s: %d", model, code)
			continue
		}
		data, _ := env.Get("data")
		var docs []*Object
		switch d := data.(type) {
		case *Object:
			docs = []*Object{d}
		case []any:
			for _, e := range d {
				docs = append(docs, e.(*Object))
			}
		}
		for i, d := range docs {
			ctx := fmt.Sprintf("%s[%d]", model, i)
			requireKeys(t, ctx, d, keys)

			switch model {
			case "consultations":
				for field, ck := range requiredContenu {
					v, _ := d.Get(field)
					switch c := v.(type) {
					case *Object:
						requireKeys(t, ctx+"."+field, c, ck)
						if sec, ok := c.Get("sections"); ok {
							checkComponents(t, ctx+"."+field+".sections", sec)
						}
					case []any:
						for j, e := range c {
							requireKeys(t, fmt.Sprintf("%s.%s[%d]", ctx, field, j), e.(*Object), ck)
							if sec, ok := e.(*Object).Get("sections"); ok {
								checkComponents(t, fmt.Sprintf("%s.%s[%d].sections", ctx, field, j), sec)
							}
						}
					}
				}
				q, _ := d.Get("questions")
				checkComponents(t, ctx+".questions", q)
				th, _ := d.Get("thematique")
				requireKeys(t, ctx+".thematique", th.(*Object), requiredTop["thematiques"])
			case "reponse-du-gouvernements":
				rt, _ := d.Get("reponseType")
				checkComponents(t, ctx+".reponseType", rt)
			case "concertations", "fiche-inventaires":
				th, _ := d.Get("thematique")
				requireKeys(t, ctx+".thematique", th.(*Object), requiredTop["thematiques"])
			}
		}
	}
}
