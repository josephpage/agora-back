package consultation

import (
	"reflect"
	"testing"

	"agora/internal/jsonjava"
	"agora/internal/xmljava"
)

func sp(s string) *string { return &s }

var (
	choixDefault     = ChoixPossible{ID: "choixPossibleId", Label: "label", Ordre: 1, QuestionID: "questionId", HasOpenTextField: false}
	choixConditional = ChoixPossible{ID: "choixPossibleId", Label: "label", Ordre: 1, QuestionID: "questionId", HasOpenTextField: false, NextQuestionID: "nextQuestionId"}
)

func question(kind QuestionKind, id string) Question {
	return Question{Kind: kind, ID: id, Title: "title", PopupDescription: sp("popupDescription"), Order: 1, NextQuestionID: sp("nextQuestionId"), ConsultationID: "consultationId"}
}

// QuestionJsonMapperTest
func TestQuestionJSONMapper(t *testing.T) {
	jsonChoixDefault := ChoixPossibleJSON{ID: "choixPossibleId", Label: "label", Order: 1, HasOpenTextField: false}
	jsonChoixConditional := ChoixPossibleJSON{ID: "choixPossibleId", Label: "label", Order: 1, HasOpenTextField: false, NextQuestionID: sp("nextQuestionId")}
	empty := QuestionsJSON{
		QuestionsUniqueChoice: []QuestionUniqueChoiceJSON{}, QuestionsOpened: []QuestionOpenedJSON{}, QuestionsMultipleChoices: []QuestionMultipleChoicesJSON{},
		Chapters: []QuestionChapterJSON{}, QuestionsWithCondition: []QuestionConditionalJSON{},
	}

	unique := question(KindUniqueChoice, "questionUniqueChoiceId")
	unique.ChoixPossibleList = []ChoixPossible{choixDefault}
	want := empty
	want.QuestionCount = 10
	want.QuestionsUniqueChoice = []QuestionUniqueChoiceJSON{{ID: "questionUniqueChoiceId", Title: "title", PopupDescription: sp("popupDescription"), Order: 1,
		QuestionProgress: "Question 1/1", QuestionProgressA11y: "Question 1 sur 1", NextQuestionID: sp("nextQuestionId"), PossibleChoices: []ChoixPossibleJSON{jsonChoixDefault}}}
	if got := ToQuestionsJSON(Questions{QuestionCount: 10, Questions: []Question{unique}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("unique choice:\n%+v\n%+v", got, want)
	}

	multiple := question(KindMultipleChoices, "questionMultipleChoicesId")
	multiple.ChoixPossibleList = []ChoixPossible{choixDefault}
	multiple.MaxChoices = 2
	want = empty
	want.QuestionCount = 10
	want.QuestionsMultipleChoices = []QuestionMultipleChoicesJSON{{ID: "questionMultipleChoicesId", Title: "title", PopupDescription: sp("popupDescription"), Order: 1,
		QuestionProgress: "Question 1/1", QuestionProgressA11y: "Question 1 sur 1", MaxChoices: 2, NextQuestionID: sp("nextQuestionId"), PossibleChoices: []ChoixPossibleJSON{jsonChoixDefault}}}
	if got := ToQuestionsJSON(Questions{QuestionCount: 10, Questions: []Question{multiple}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("multiple choices:\n%+v\n%+v", got, want)
	}

	open := question(KindOpen, "questionOpenId")
	want = empty
	want.QuestionCount = 11
	want.QuestionsOpened = []QuestionOpenedJSON{{ID: "questionOpenId", Title: "title", PopupDescription: sp("popupDescription"), Order: 1,
		QuestionProgress: "Question 1/1", QuestionProgressA11y: "Question 1 sur 1", NextQuestionID: sp("nextQuestionId")}}
	if got := ToQuestionsJSON(Questions{QuestionCount: 11, Questions: []Question{open}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("open:\n%+v\n%+v", got, want)
	}

	chapter := question(KindChapter, "questionChapterId")
	chapter.Description = "description"
	want = empty
	want.QuestionCount = 12
	want.Chapters = []QuestionChapterJSON{{ID: "questionChapterId", Title: "title", PopupDescription: sp("popupDescription"), Order: 1, Description: "description", NextQuestionID: sp("nextQuestionId")}}
	if got := ToQuestionsJSON(Questions{QuestionCount: 12, Questions: []Question{chapter}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("chapter:\n%+v\n%+v", got, want)
	}

	conditional := question(KindConditional, "questionConditionalId")
	conditional.ChoixPossibleList = []ChoixPossible{choixConditional}
	want = empty
	want.QuestionCount = 13
	want.QuestionsWithCondition = []QuestionConditionalJSON{{ID: "questionConditionalId", Title: "title", PopupDescription: sp("popupDescription"), Order: 1,
		QuestionProgress: "Question 1/1", QuestionProgressA11y: "Question 1 sur 1", PossibleChoices: []ChoixPossibleJSON{jsonChoixConditional}}}
	if got := ToQuestionsJSON(Questions{QuestionCount: 13, Questions: []Question{conditional}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("conditional:\n%+v\n%+v", got, want)
	}
}

func TestQuestionProgress(t *testing.T) {
	at := func(q Question, order int) Question { q.Order = order; return q }
	got := ToQuestionsJSON(Questions{QuestionCount: 10, Questions: []Question{
		at(question(KindOpen, "o"), 1), at(question(KindChapter, "c1"), 2), at(question(KindMultipleChoices, "m"), 3),
		at(question(KindChapter, "c2"), 4), at(question(KindUniqueChoice, "u"), 5),
	}})
	if got.QuestionsOpened[0].QuestionProgress != "Question 1/3" || got.QuestionsMultipleChoices[0].QuestionProgress != "Question 2/3" ||
		got.QuestionsUniqueChoice[0].QuestionProgress != "Question 3/3" || got.QuestionsUniqueChoice[0].QuestionProgressA11y != "Question 3 sur 3" {
		t.Fatalf("%+v", got)
	}
}

func TestQuestionsJSONBytes(t *testing.T) {
	// nextQuestionId is omitted (NON_NULL) from the choices of a unique choice question, null is written elsewhere
	unique := question(KindUniqueChoice, "u")
	unique.NextQuestionID = nil
	unique.PopupDescription = nil
	unique.ChoixPossibleList = []ChoixPossible{choixDefault}
	got := string(jsonjava.Marshal(ToQuestionsJSON(Questions{QuestionCount: 1, Questions: []Question{unique}})))
	want := `{"questionCount":1,"questionsUniqueChoice":[{"id":"u","title":"title","popupDescription":null,"order":1,"questionProgress":"Question 1/1",` +
		`"questionProgressA11y":"Question 1 sur 1","nextQuestionId":null,"possibleChoices":[{"id":"choixPossibleId","label":"label","order":1,"hasOpenTextField":false}]}],` +
		`"questionsOpened":[],"questionsMultipleChoices":[],"chapters":[],"questionsWithCondition":[]}`
	if got != want {
		t.Fatalf("%s", got)
	}
	x := string(xmljava.Marshal(ToQuestionsJSON(Questions{})))
	if x != `<QuestionsJson><questionCount>0</questionCount><questionsUniqueChoice/><questionsOpened/><questionsMultipleChoices/><chapters/><questionsWithCondition/></QuestionsJson>` {
		t.Fatalf("%s", x)
	}
}

// QuestionsMapperTest: every kind of question of Strapi is mapped.
func TestQuestionsMapper(t *testing.T) {
	const doc = `{
  "documentId": "abc123", "slug": "", "url_image_de_couverture": "", "url_image_page_de_contenu": "", "image_de_couverture": null, "image_page_de_contenu": null,
  "nombre_de_questions": 5, "estimation_nombre_de_questions": "5", "estimation_temps": "5 minutes", "nombre_participants_cible": 5000,
  "datetime_de_debut": "2024-07-22T22:00:00.000Z", "datetime_de_fin": "2038-07-14T22:00:00.000Z", "titre_consultation": "Test questions", "territoire": "Nord",
  "thematique": {"documentId": "thema1", "label": "Autonomie", "pictogramme": "👵"},
  "questions": [
    {"id": 10004, "__component": "question-de-consultation.description", "titre": "Description", "numero": 1, "description": [], "question_suivante": null,
     "image": {"formats": {"medium": {"url": "https://r2.dev/medium.jpg"}}, "url": "https://r2.dev/orig.jpg"}},
    {"id": 20002, "__component": "question-de-consultation.question-a-choix-multiples", "titre": "Question à choix multiples", "numero": 2, "nombre_maximum_de_choix": 1,
     "popup_explication": null, "question_suivante": null, "choix": [{"id": 80, "label": "choix multiple 1", "ouvert": false}, {"id": 81, "label": "choix multiple 2 ouvert", "ouvert": true}]},
    {"id": 30002, "__component": "question-de-consultation.question-a-choix-unique", "titre": "question à choix unique", "numero": 3, "popup_explication": null, "question_suivante": null,
     "choix": [{"id": 82, "label": "choix 1", "ouvert": false}, {"id": 83, "label": "choix 2 ouvert", "ouvert": true}]},
    {"id": 40002, "__component": "question-de-consultation.question-conditionnelle", "titre": "question conditionnelle", "numero": 4, "popup_explication": null,
     "choix": [{"id": 7, "label": "vers 5", "numero_de_la_question_suivante": 5, "ouvert": false}, {"id": 8, "label": "vers 6", "numero_de_la_question_suivante": 6, "ouvert": false}]},
    {"id": 50004, "__component": "question-de-consultation.question-ouverte", "titre": "question ouverte", "numero": 5, "popup_explication": null, "question_suivante": 7},
    {"id": 50006, "__component": "question-de-consultation.question-ouverte", "titre": "question ouverte", "numero": 6, "popup_explication": null, "question_suivante": 7},
    {"id": 50005, "__component": "question-de-consultation.question-ouverte", "titre": "question finale", "numero": 7, "popup_explication": null, "question_suivante": null}
  ],
  "consultation_avant_reponse": {"documentId": "avantRep1", "slug": "", "template_partage": " ", "historique_titre": " ", "historique_call_to_action": " ",
    "sections": [], "commanditaire": [], "objectif": [], "axe_gouvernemental": [], "presentation": []},
  "consultation_apres_reponse_ou_terminee": {"documentId": "apresRep1", "historique_titre": "", "historique_call_to_action": "", "slug": "", "feedback_message": "",
    "nom_strapi": "", "template_partage": "", "sections": []},
  "consultation_contenu_analyse_des_reponse": null, "contenu_reponse_du_commanditaires": null, "consultation_contenu_autres": [], "consultation_contenu_a_venir": null,
  "titre_page_web": "Grande Consultation", "sous_titre_page_web": "par le Gouvernement"
}`
	var c strapiConsultation
	if err := jsonjava.Unmarshal([]byte(doc), &c); err != nil {
		t.Fatal(err)
	}
	counts := map[QuestionKind]int{}
	got := questionsOf(&c)
	for _, q := range got {
		counts[q.Kind]++
	}
	if counts[KindConditional] != 1 || counts[KindMultipleChoices] != 1 || counts[KindChapter] != 1 || counts[KindUniqueChoice] != 1 || counts[KindOpen] != 3 {
		t.Fatalf("%v", counts)
	}
	// ids and choices
	chapter := got[0]
	if chapter.ID != "10004" || chapter.Description != "" || *chapter.URLImage != "https://r2.dev/medium.jpg" || chapter.PopupDescription != nil || chapter.NextQuestionID == nil || *chapter.NextQuestionID != "20002" {
		t.Fatalf("%+v", chapter)
	}
	multiple := got[1]
	if multiple.MaxChoices != 1 || len(multiple.ChoixPossibleList) != 2 || multiple.ChoixPossibleList[1] != (ChoixPossible{ID: "81", Label: "choix multiple 2 ouvert", Ordre: 1, QuestionID: "20002", HasOpenTextField: true}) {
		t.Fatalf("%+v", multiple)
	}
	conditional := got[3]
	if conditional.NextQuestionID != nil || conditional.ChoixPossibleList[0].NextQuestionID != "50004" || conditional.ChoixPossibleList[1].NextQuestionID != "50006" {
		t.Fatalf("%+v", conditional)
	}
	// question 5 jumps to question 7, which is the last one (no next question)
	if got[4].NextQuestionID == nil || *got[4].NextQuestionID != "50005" || got[6].NextQuestionID != nil {
		t.Fatalf("%+v %+v", got[4], got[6])
	}
}

func TestQuestionsMapperFailures(t *testing.T) {
	run := func(questions []any) (r any) {
		defer func() { r = recover() }()
		questionsOf(decodeConsultation(t, consultationSpec{id: "c", slug: "s", questions: questions}.json()))
		return nil
	}
	cond := obj{"id": 1, "__component": "question-de-consultation.question-conditionnelle", "titre": "q", "numero": 1,
		"choix": []any{obj{"id": 1, "label": "l", "ouvert": false, "numero_de_la_question_suivante": 99}}}
	if run([]any{cond}) == nil {
		t.Fatal("a conditional question without target question: NoSuchElementException")
	}
	if run([]any{nil}) == nil {
		t.Fatal("a null question: NoWhenBranchMatchedException")
	}
	open := obj{"id": 1, "__component": "question-de-consultation.question-ouverte", "titre": "q", "numero": 1}
	if run([]any{open}) != nil {
		t.Fatal("a plain question")
	}
}
