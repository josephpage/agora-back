package consultation

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	fixedNow   = ldt(2026, 6, 1, 12, 0, 0)
	pastDate   = baseTime.AddDate(0, 0, -10)
	futureDate = baseTime.AddDate(0, 0, 10)
)

func titleSections(n int) []any {
	out := []any{}
	for i := 0; i < n; i++ {
		out = append(out, obj{"id": i, "__component": "consultation-section.section-titre", "titre": "T" + string(rune('A'+i))})
	}
	return out
}

// ConsultationUpdateInfoV2MapperTest

func TestToDomainUnanswered(t *testing.T) {
	m := updateMapper{}
	spec := consultationSpec{id: "c", slug: "s"}
	o := spec.json()
	o["consultation_avant_reponse"].(obj)["documentId"] = "avant-rep-123"
	got := m.toDomainUnanswered(decodeConsultation(t, o))
	if got.ID != "avant-rep-123" || !got.HasQuestionsInfo || got.HasParticipationInfo || got.DownloadAnalysisURL != nil || got.FeedbackQuestion != nil ||
		got.ResponsesInfo != nil || got.InfoHeader != nil || got.Footer != nil || len(got.SectionsHeader) != 0 {
		t.Fatalf("%+v", got)
	}
	// "Pourquoi cette consultation ?", the presentation, then the sections; the preview is the first paragraph
	if len(got.Body) != 2 || got.Body[0] != (SectionTitle{Title: "Pourquoi cette consultation ?"}) {
		t.Fatalf("%+v", got.Body)
	}
	if got.Body[1] != (SectionRichText{Description: "<p>Premier.</p><p>Second.</p>"}) {
		t.Fatalf("%+v", got.Body[1])
	}
	if got.BodyPreview[1] != (SectionRichText{Description: "<p>Premier.</p>"}) {
		t.Fatalf("%+v", got.BodyPreview[1])
	}
	want := []Goal{{"\U0001F5E3️", "Le Gouvernement"}, {"\U0001F3AF", "Objectif"}, {"\U0001F680", "Axe"}}
	if len(got.Goals) != 3 || got.Goals[0] != want[0] || got.Goals[1] != want[1] || got.Goals[2] != want[2] {
		t.Fatalf("%+v", got.Goals)
	}
}

func TestToDomainUnansweredPreviewAndGoalsEdgeCases(t *testing.T) {
	m := updateMapper{}
	o := consultationSpec{id: "c", slug: "s"}.json()
	avant := o["consultation_avant_reponse"].(obj)

	// no "</p>": the whole text is closed with "</p>"; goals keep what is not <p>...</p>
	avant["presentation"] = []any{obj{"type": "heading", "level": 2, "children": []any{obj{"type": "text", "text": "Titre"}}}}
	avant["commanditaire"] = []any{}
	avant["objectif"] = richText("<p>", "x")
	got := m.toDomainUnanswered(decodeConsultation(t, o))
	if got.BodyPreview[1] != (SectionRichText{Description: "<h2>Titre</h2></p>"}) {
		t.Fatalf("%+v", got.BodyPreview[1])
	}
	if got.Goals[0].Description != "" || got.Goals[1].Description != "<p></p><p>x" {
		t.Fatalf("%+v", got.Goals)
	}
	// an empty presentation
	avant["presentation"] = []any{}
	got = m.toDomainUnanswered(decodeConsultation(t, o))
	if got.Body[1] != (SectionRichText{Description: ""}) || got.BodyPreview[1] != (SectionRichText{Description: "</p>"}) {
		t.Fatalf("%+v", got)
	}
}

func TestToDomainAnsweredOrEnded(t *testing.T) {
	m := updateMapper{}
	o := consultationSpec{id: "c", slug: "s", end: pastDate}.json()
	o["consultation_apres_reponse_ou_terminee"].(obj)["documentId"] = "apres-rep-456"
	got := m.toDomainAnsweredOrEnded(decodeConsultation(t, o), fixedNow)
	if got.ID != "apres-rep-456" || got.HasQuestionsInfo || got.ResponsesInfo.Picto != "\U0001F3C1" || got.ResponsesInfo.ActionText != "Voir tous les résultats" {
		t.Fatalf("%+v", got)
	}
	if got.FeedbackQuestion.Description != "<body>Avis ?</body>" || got.FeedbackQuestion.Title != "Donnez votre avis" || got.FeedbackQuestion.ConsultationUpdateID != "apres-rep-456" {
		t.Fatalf("%+v", got.FeedbackQuestion)
	}
	ongoing := m.toDomainAnsweredOrEnded(decodeConsultation(t, consultationSpec{id: "c", slug: "s", end: futureDate}.json()), fixedNow)
	if ongoing.ResponsesInfo.Picto != "\U0001F64C" || !strings.Contains(ongoing.ResponsesInfo.Description, "Merci pour votre participation") {
		t.Fatalf("%+v", ongoing.ResponsesInfo)
	}
	// the end date equal to now is not before now: still ongoing
	equal := m.toDomainAnsweredOrEnded(decodeConsultation(t, consultationSpec{id: "c", slug: "s", end: baseTime}.json()), fixedNow)
	if equal.ResponsesInfo.Picto != "\U0001F64C" {
		t.Fatal("an end date equal to now is not before now")
	}
}

func TestToDomainAnalyseDesReponses(t *testing.T) {
	m := updateMapper{}
	spec := consultationSpec{id: "c", slug: "s"}
	if got := m.toDomainAnalyseDesReponses(decodeConsultation(t, spec.json())); got != nil {
		t.Fatal("no analysis")
	}
	spec.analyse = analyseContenu("analyse-789", pastDate, obj{"pdf_analyse": obj{"url": "https://analyse.pdf"}, "recap_emoji": "\U0001F525", "recap_label": "Résultats disponibles"})
	got := m.toDomainAnalyseDesReponses(decodeConsultation(t, spec.json()))
	if got.ID != "analyse-789" || *got.DownloadAnalysisURL != "https://analyse.pdf" || got.InfoHeader == nil || got.InfoHeader.Picto != "\U0001F525" || got.InfoHeader.Description != "Résultats disponibles" {
		t.Fatalf("%+v", got)
	}
	spec.analyse = analyseContenu("analyse-789", pastDate, obj{"lien_telechargement_analyse": "https://fallback.pdf", "recap_emoji": nil, "recap_label": "label"})
	got = m.toDomainAnalyseDesReponses(decodeConsultation(t, spec.json()))
	if *got.DownloadAnalysisURL != "https://fallback.pdf" || got.InfoHeader != nil || got.FeedbackQuestion == nil {
		t.Fatalf("%+v", got)
	}
}

func TestToDomainReponseDuCommanditaireAndContenuAutre(t *testing.T) {
	m := updateMapper{}
	spec := consultationSpec{id: "c", slug: "s"}
	if got := m.toDomainReponseDuCommanditaire(decodeConsultation(t, spec.json())); got != nil {
		t.Fatal("no sponsor's answer")
	}
	spec.commanditaire = commanditaireContenu("commanditaire-101", pastDate, nil)
	spec.autres = []obj{autreContenu("autre-202", pastDate, obj{"recap_emoji": "E", "recap_label": "L"})}
	c := decodeConsultation(t, spec.json())
	got := m.toDomainReponseDuCommanditaire(c)
	if got.ID != "commanditaire-101" || got.FeedbackQuestion == nil || got.HasQuestionsInfo || got.InfoHeader != nil {
		t.Fatalf("%+v", got)
	}
	autre := m.toDomainContenuAutre(c, c.ContenuAutres[0])
	if autre.ID != "autre-202" || autre.FeedbackQuestion == nil || autre.HasQuestionsInfo || autre.Goals != nil || autre.InfoHeader.Picto != "E" {
		t.Fatalf("%+v", autre)
	}
	if autre.UpdateDate != ldt(2026, 5, 22, 12, 0, 0) {
		t.Fatalf("the date of the content: %v", autre.UpdateDate)
	}
}

func TestSectionsMapping(t *testing.T) {
	sections := []any{
		obj{"id": 1, "__component": "consultation-section.section-titre", "titre": "Titre"},
		obj{"id": 2, "__component": "consultation-section.section-texte-riche", "description": richText("Riche")},
		obj{"id": 3, "__component": "consultation-section.section-citation", "description": richText("Citation")},
		obj{"id": 4, "__component": "consultation-section.section-image", "url": "u", "description_accessible_de_l_image": "d", "image": nil},
		obj{"id": 5, "__component": "consultation-section.section-video", "url": "v", "largeur": 1, "hauteur": 2, "nom_auteur": "n", "poste_auteur": "p", "date_tournage": "2026-01-02", "transcription": "t", "video": nil},
		obj{"id": 6, "__component": "consultation-section.section-chiffre", "titre": "12", "description": richText("Chiffre")},
		obj{"id": 7, "__component": "consultation-section.section-accordeon", "titre": "Acc", "description": richText("Accordéon")},
	}
	o := consultationSpec{id: "c", slug: "s"}.json()
	o["consultation_apres_reponse_ou_terminee"].(obj)["sections"] = sections
	c := decodeConsultation(t, o)
	got := updateMapper{}.toDomainAnsweredOrEnded(c, fixedNow)
	if len(got.Body) != 7 || len(got.BodyPreview) != 0 {
		t.Fatalf("under 8 sections there is no preview: %d %d", len(got.Body), len(got.BodyPreview))
	}
	if !reflect.DeepEqual(got.Body[3], SectionImage{URL: "u", ContentDescription: ptr("d")}) || got.Body[4].(SectionVideo).AuthorInfo.Date.Month != 1 ||
		got.Body[6].(SectionAccordion).Sections[0] != (SectionRichText{Description: "<p>Accordéon</p>"}) {
		t.Fatalf("%+v", got.Body)
	}
	// 8 sections: the preview is the first five
	o["consultation_apres_reponse_ou_terminee"].(obj)["sections"] = append(sections, obj{"id": 8, "__component": "consultation-section.section-titre", "titre": "H"})
	got = updateMapper{}.toDomainAnsweredOrEnded(decodeConsultation(t, o), fixedNow)
	if len(got.Body) != 8 || len(got.BodyPreview) != 5 || !reflect.DeepEqual(got.BodyPreview[4], got.Body[4]) {
		t.Fatalf("%d %d", len(got.Body), len(got.BodyPreview))
	}
}

func TestSectionsWithNullElementPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a null section is a NoWhenBranchMatchedException")
		}
	}()
	toSections([]*strapiSection{nil})
}

// keep the compiler honest about helpers used by the other test files
var _ = time.Second
