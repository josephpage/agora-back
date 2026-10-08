package consultation

import (
	"testing"
	"time"

	"agora/internal/jsonjava"
	"agora/internal/strapi"
)

// TestParseConsultationStrapiDTO is ConsultationStrapiDTOTest.`parse ConsultationStrapiDTO`.
func TestParseConsultationStrapiDTO(t *testing.T) {
	const doc = `{
  "data": [
    {
      "documentId": "agri-pro-1",
      "url_image_de_couverture": "https://content.agora.beta.gouv.fr/consultation_covers/agriculteurs_professionnels.jpg",
      "url_image_page_de_contenu": "https://content.agora.beta.gouv.fr/consultation_covers/agriculteurs_consommateurs.jpg",
      "image_de_couverture": {"formats": {"medium": {"url": "https://r2.dev/medium_20240215_095657_ef8160b276.jpg"}}, "url": "https://r2.dev/20240215_095657_ef8160b276.jpg"},
      "image_page_de_contenu": {"formats": {"medium": {"url": "https://r2.dev/medium_IMG_4029_f2bca5feed.jpg"}}, "url": "https://r2.dev/20240215_095657_ef8160b276.jpg"},
      "nombre_de_questions": 3, "estimation_nombre_de_questions": "2 à 3 questions", "estimation_temps": "8 minutes", "nombre_participants_cible": 12000,
      "createdAt": "2024-05-29T09:37:12.407Z", "updatedAt": "2024-08-23T13:04:14.174Z", "publishedAt": "2024-07-09T12:25:24.599Z",
      "datetime_de_debut": "2024-07-01T22:00:00.000Z", "datetime_de_fin": "2024-07-10T22:00:00.000Z",
      "titre_consultation": "Agriculture professionnel", "slug": "agriculture-professionnel", "territoire": "Nord",
      "thematique": {"documentId": "thema-9", "label": "Démocratie", "pictogramme": "🗳", "createdAt": "2024-05-29T10:19:13.352Z"},
      "questions": [{"id": 1, "__component": "question-de-consultation.question-a-choix-unique", "titre": "Qu'en pensez-vous ?", "numero": 1,
                     "popup_explication": null, "question_suivante": null, "choix": [{"id": 2, "label": "J'adore !", "ouvert": true}]}],
      "consultation_avant_reponse": {"documentId": "avant-rep-1", "template_partage": "Comme moi : {title} {url}", "historique_titre": "Lancement",
        "historique_call_to_action": "Voir les objectifs", "slug": "lancement",
        "commanditaire": [{"type": "paragraph", "children": [{"text": "Gouvernement", "type": "text"}]}],
        "objectif": [{"type": "paragraph", "children": [{"text": "sdsdsd", "type": "text", "italic": true}, {"bold": true, "text": "qdsdsds", "type": "text"}]}],
        "axe_gouvernemental": [{"type": "paragraph", "children": [{"text": "Culture", "type": "text"}]}],
        "presentation": [{"type": "paragraph", "children": [{"text": "Pouet", "type": "text"}]}],
        "nom_strapi": "Agriculture pro - objectifs", "sections": [{"id": 1, "__component": "consultation-section.section-titre", "titre": "Titre"}]},
      "consultation_apres_reponse_ou_terminee": {"documentId": "apres-rep-2", "historique_titre": "Fin de consultation", "historique_call_to_action": "Consulter",
        "slug": "fin-de-la-consultation", "feedback_message": "Satisfait(e) ?", "nom_strapi": "x", "template_partage": "Comme moi",
        "sections": [{"id": 1, "__component": "consultation-section.section-chiffre", "titre": "12%", "description": [{"type": "paragraph", "children": [{"text": "Bienvenue", "type": "text"}]}]}]},
      "consultation_contenu_autres": [{"documentId": "autre-2", "template_partage": "Cela", "historique_titre": "Nouveau contenu autre", "historique_call_to_action": "cliquez ici",
        "datetime_publication": "2024-08-22T15:00:00.000Z", "slug": "cliquez-ici", "feedback_message": "Satisfait(e) ?", "flamme_label": "Trop bien",
        "sections": [{"id": 1, "__component": "consultation-section.section-accordeon", "titre": "Accord", "description": [{"type": "paragraph", "children": [{"text": "éon", "type": "text"}]}]}]}],
      "consultation_contenu_a_venir": {"documentId": "a-venir-3", "titre_historique": "Ca arrive"},
      "consultation_contenu_analyse_des_reponse": {"documentId": "analyse-3", "lien_telechargement_analyse": "https://pdf", "template_partage": "Cela",
        "datetime_publication": "2024-08-21T22:00:00.000Z", "slug": "analyse-disponible", "feedback_message": "Satisfait(e) ?", "historique_titre": "Analyse des réponses",
        "historique_call_to_action": "Consulter la synthèse", "flamme_label": "Venez répondre !!",
        "sections": [{"id": 1, "__component": "consultation-section.section-citation", "description": [{"type": "paragraph", "children": [{"text": "Oui", "type": "text"}]}]}],
        "pdf_analyse": {"url": "https://r2.dev/Contenu.doc"}},
      "contenu_reponse_du_commanditaires": {"documentId": "rep-cmd-2", "template_partage": "Cela", "datetime_publication": "2024-08-21T23:00:00.000Z", "slug": "reponse-du-gouvernement",
        "feedback_message": "Satisfait(e) ?", "historique_titre": "Réponse du Gouvernement", "historique_call_to_action": "Actions mises en place", "flamme_label": "Ils ont répondu !!", "sections": []},
      "titre_page_web": "Grande Consultation", "sous_titre_page_web": "par le Gouvernement"
    }
  ],
  "meta": {"pagination": {"page": 1, "pageSize": 25, "pageCount": 1, "total": 1}}
}`
	var env strapi.Envelope[*strapiConsultation]
	if err := jsonjava.Unmarshal([]byte(doc), &env); err != nil {
		t.Fatal(err)
	}
	c := env.Data[0]
	if c.Titre != "Agriculture professionnel" || len(c.ContenuAutres) != 1 || c.ContenuAVenir == nil {
		t.Fatalf("%+v", c)
	}
	if c.getImageCouverture() != "https://r2.dev/medium_20240215_095657_ef8160b276.jpg" || c.getImagePageContenu() != "https://r2.dev/medium_IMG_4029_f2bca5feed.jpg" {
		t.Fatal("pictures")
	}
	if c.AnalyseDesReponses.getAnalysePdfURL() != "https://r2.dev/Contenu.doc" {
		t.Fatal("pdf")
	}
	if c.DateDeDebut.Format() != "2024-07-01 22:00:00" {
		t.Fatal(c.DateDeDebut.Format())
	}
}

// TestParseSections is ConsultationStrapiDTOTest.`parse sections`.
func TestParseSections(t *testing.T) {
	const doc = `[
 {"id": 2, "__component": "consultation-section.section-chiffre", "titre": "Chiffre", "description": [{"type": "paragraph", "children": [{"text": "Description chiffre", "type": "text"}]}]},
 {"id": 2, "__component": "consultation-section.section-citation", "description": [{"type": "paragraph", "children": [{"text": "Description citation", "type": "text"}]}]},
 {"id": 1, "__component": "consultation-section.section-image", "url": "url image", "description_accessible_de_l_image": "Description image",
  "image": {"formats": {"medium": {"url": "https://r2.dev/medium.jpg"}}, "url": "https://r2.dev/orig.jpg"}},
 {"id": 1, "__component": "consultation-section.section-texte-riche", "description": [{"type": "paragraph", "children": [{"text": "Description texte riche", "type": "text"}]}]},
 {"id": 2, "__component": "consultation-section.section-titre", "titre": "titre"},
 {"id": 1, "__component": "consultation-section.section-video", "url": "url vidéo", "largeur": 200, "hauteur": 300, "nom_auteur": "nom", "poste_auteur": "poste",
  "date_tournage": "2024-08-26", "transcription": "transcription vidéo", "video": {"url": "https://r2.dev/reponse.mp4"}},
 {"id": 1, "__component": "consultation-section.section-accordeon", "titre": "Accordéon", "description": [{"type": "paragraph", "children": [{"text": "Description accordéon", "type": "text"}]}]}
]`
	var sections []*strapiSection
	if err := jsonjava.Unmarshal([]byte(doc), &sections); err != nil {
		t.Fatal(err)
	}
	if len(sections) != 7 {
		t.Fatalf("%d sections", len(sections))
	}
	got := toSections(sections)
	if v := got[2].(SectionImage); v.URL != "https://r2.dev/medium.jpg" || *v.ContentDescription != "Description image" {
		t.Fatalf("%+v", v)
	}
	if v := got[5].(SectionVideo); v.URL != "https://r2.dev/reponse.mp4" || v.Width != 200 || v.Height != 300 || v.AuthorInfo.Date.Day != 26 {
		t.Fatalf("%+v", v)
	}
}

func TestSectionAndQuestionDecodingFailures(t *testing.T) {
	bad := []string{
		`[{"id":1,"__component":"consultation-section.nope","titre":"x"}]`,
		`[{"id":1,"titre":"x"}]`,
		`[{"id":1,"__component":null,"titre":"x"}]`,
		`[{"id":1,"__component":3,"titre":"x"}]`,
		`["x"]`,
		`[{"id":1,"__component":"consultation-section.section-titre"}]`,
	}
	for _, in := range bad {
		var s []*strapiSection
		if err := jsonjava.Unmarshal([]byte(in), &s); err == nil {
			t.Errorf("%s must fail", in)
		}
	}
	var qs []*strapiQuestion
	if err := jsonjava.Unmarshal([]byte(`[{"id":1,"__component":"question-de-consultation.question-ouverte","titre":"x","numero":1}]`), &qs); err != nil || qs[0].Ouverte == nil {
		t.Fatalf("%v", err)
	}
	if err := jsonjava.Unmarshal([]byte(`[{"id":1,"__component":"question-de-consultation.nope","titre":"x","numero":1}]`), &qs); err == nil {
		t.Fatal("unknown question component must fail")
	}
	// a null element is kept as nil (Kotlin keeps the null and fails later)
	var nulls []*strapiSection
	if err := jsonjava.Unmarshal([]byte(`[null]`), &nulls); err != nil || len(nulls) != 1 || nulls[0] != nil {
		t.Fatalf("%v %v", nulls, err)
	}
}

func TestGetLatestUpdateDate(t *testing.T) {
	now := ldt(2026, 6, 1, 12, 0, 0)
	spec := consultationSpec{
		id: "c1", slug: "s1", start: baseTime.AddDate(0, 0, -30),
		autres:        []obj{autreContenu("a1", baseTime.AddDate(0, 0, -5), nil), autreContenu("a2", baseTime.AddDate(0, 0, 3), nil)},
		analyse:       analyseContenu("an", baseTime.AddDate(0, 0, -2), nil),
		commanditaire: commanditaireContenu("cm", baseTime.AddDate(0, 0, 9), nil),
	}
	c := decodeConsultation(t, spec.json())
	got := c.getLatestUpdateDate(now)
	if got == nil || got.Format() != "2026-05-30 12:00:00" {
		t.Fatalf("latest update date: %v", got)
	}
	// only the start date (in the past)
	c = decodeConsultation(t, consultationSpec{id: "c2", slug: "s2", start: baseTime.AddDate(0, 0, -3)}.json())
	if got := c.getLatestUpdateDate(now); got == nil || got.Format() != "2026-05-29 12:00:00" {
		t.Fatalf("start date: %v", got)
	}
	// nothing before now
	c = decodeConsultation(t, consultationSpec{id: "c3", slug: "s3", start: baseTime.AddDate(0, 0, 3)}.json())
	if got := c.getLatestUpdateDate(now); got != nil {
		t.Fatalf("no update date before now: %v", got)
	}
	// an update published exactly now is not before now
	c = decodeConsultation(t, consultationSpec{id: "c4", slug: "s4", start: baseTime}.json())
	if got := c.getLatestUpdateDate(now); got != nil {
		t.Fatalf("isBefore is strict: %v", got)
	}
}

func TestGetFlammeLabel(t *testing.T) {
	now := ldt(2026, 6, 1, 12, 0, 0)
	label := func(s string) *string { return &s }
	past := func(days int) time.Time { return baseTime.AddDate(0, 0, -days) }
	spec := consultationSpec{id: "c1", slug: "s1", start: past(30)}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}

	// the latest other content wins, then the sponsor's answer, then the analysis
	spec.autres = []obj{autreContenu("a1", past(5), obj{"flamme_label": "autre-old"}), autreContenu("a2", past(2), obj{"flamme_label": "autre-new"})}
	spec.commanditaire = commanditaireContenu("cm", past(1), obj{"flamme_label": "cmd"})
	spec.analyse = analyseContenu("an", past(1), obj{"flamme_label": "analyse"})
	if got := str(decodeConsultation(t, spec.json()).getFlammeLabel(now)); got != "autre-new" {
		t.Fatalf("got %s", got)
	}
	spec.autres = []obj{autreContenu("a1", past(2), obj{"flamme_label": nil})}
	if got := str(decodeConsultation(t, spec.json()).getFlammeLabel(now)); got != "cmd" {
		t.Fatalf("a null label falls through to the sponsor's answer: %s", got)
	}
	spec.commanditaire = commanditaireContenu("cm", past(1), nil)
	if got := str(decodeConsultation(t, spec.json()).getFlammeLabel(now)); got != "analyse" {
		t.Fatalf("got %s", got)
	}
	spec.analyse = nil
	if got := str(decodeConsultation(t, spec.json()).getFlammeLabel(now)); got != "<nil>" {
		t.Fatalf("got %s", got)
	}
	_ = label
	// the other contents published after now are ignored
	spec.autres = []obj{autreContenu("a1", baseTime.AddDate(0, 0, 2), obj{"flamme_label": "future"})}
	spec.commanditaire = commanditaireContenu("cm", past(1), obj{"flamme_label": "cmd"})
	if got := str(decodeConsultation(t, spec.json()).getFlammeLabel(now)); got != "cmd" {
		t.Fatalf("got %s", got)
	}
}

func TestGetNextQuestionID(t *testing.T) {
	q := func(id, numero int, next any) obj {
		return obj{"id": id, "__component": "question-de-consultation.question-ouverte", "titre": "q", "numero": numero, "question_suivante": next}
	}
	spec := consultationSpec{id: "c", slug: "s", questions: []any{q(10, 1, nil), q(11, 2, 4), q(12, 3, 999), q(13, 4, nil), q(14, 7, 42), q(15, 8, nil)}}
	c := decodeConsultation(t, spec.json())
	want := []string{"11", "13", "", "", "", ""}
	for i, question := range c.Questions {
		got := c.getNextQuestionID(question)
		switch {
		case want[i] == "" && got != nil:
			t.Errorf("question %d: got %s, want none", i, *got)
		case want[i] != "" && (got == nil || *got != want[i]):
			t.Errorf("question %d: got %v, want %s", i, got, want[i])
		}
	}
}
