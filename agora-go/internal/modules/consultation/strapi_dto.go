package consultation

import (
	"errors"

	"agora/internal/common"
	"agora/internal/jsonjava"
	"agora/internal/strapi"
)

// Strapi DTOs of the consultations (infrastructure/consultation/dto/strapi/*).
// They are decoded with jsonjava (Jackson + jackson-module-kotlin rules): a
// missing required property, or a null for a non-null one, anywhere in the
// document makes the whole Strapi list undecodable, i.e. empty. A list of
// objects is a list of pointers: a JSON null element stays nil and, like the
// Kotlin NullPointerException, panics where Kotlin dereferences it.

// strapiThematique is ThematiqueStrapiDTO.
type strapiThematique struct {
	DocumentID  string `json:"documentId"`
	Label       string `json:"label"`
	Pictogramme string `json:"pictogramme"`
}

// strapiConsultation is ConsultationStrapiDTO.
type strapiConsultation struct {
	DocumentID                    string                      `json:"documentId"`
	Titre                         string                      `json:"titre_consultation"`
	Slug                          string                      `json:"slug"`
	DateDeDebut                   LocalDateTime               `json:"datetime_de_debut"`
	DateDeFin                     LocalDateTime               `json:"datetime_de_fin"`
	URLImageDeCouverture          string                      `json:"url_image_de_couverture"`
	URLImagePageDeContenu         string                      `json:"url_image_page_de_contenu"`
	NombreDeQuestion              int                         `json:"nombre_de_questions"`
	EstimationNombreDeQuestions   string                      `json:"estimation_nombre_de_questions"`
	EstimationTemps               string                      `json:"estimation_temps"`
	NombreParticipantsCible       int                         `json:"nombre_participants_cible"`
	Thematique                    strapiThematique            `json:"thematique"`
	Questions                     []*strapiQuestion           `json:"questions"`
	ContenuAvantReponse           strapiContenuAvant          `json:"consultation_avant_reponse"`
	ContenuApresReponseOuTerminee strapiContenuApres          `json:"consultation_apres_reponse_ou_terminee"`
	AnalyseDesReponses            *strapiAnalyse              `json:"consultation_contenu_analyse_des_reponse"`
	ReponseDuCommanditaire        *strapiReponseCommanditaire `json:"contenu_reponse_du_commanditaires"`
	ContenuAutres                 []*strapiContenuAutre       `json:"consultation_contenu_autres"`
	ContenuAVenir                 *strapiAVenir               `json:"consultation_contenu_a_venir"`
	Territoire                    string                      `json:"territoire"`
	TitrePageWeb                  string                      `json:"titre_page_web"`
	SousTitrePageWeb              string                      `json:"sous_titre_page_web"`
	ImageDeCouverture             *strapi.MediaPicture        `json:"image_de_couverture"`
	ImagePageDeContenu            *strapi.MediaPicture        `json:"image_page_de_contenu"`
}

// getImageCouverture is ConsultationStrapiDTO.getImageCouverture.
func (c *strapiConsultation) getImageCouverture() string {
	if c.ImageDeCouverture != nil {
		return c.ImageDeCouverture.MediaURL()
	}
	return c.URLImageDeCouverture
}

// getImagePageContenu is ConsultationStrapiDTO.getImagePageContenu.
func (c *strapiConsultation) getImagePageContenu() string {
	if c.ImagePageDeContenu != nil {
		return c.ImagePageDeContenu.MediaURL()
	}
	return c.URLImagePageDeContenu
}

// getLatestUpdateDate is ConsultationStrapiDTO.getLatestUpdateDate: the latest
// publication date before now among the other contents, the analysis, the
// sponsor's answer and the start date; nil when there is none.
func (c *strapiConsultation) getLatestUpdateDate(now LocalDateTime) *LocalDateTime {
	candidates := make([]LocalDateTime, 0, len(c.ContenuAutres)+3)
	for _, a := range c.ContenuAutres {
		candidates = append(candidates, a.DatetimePublication)
	}
	if c.AnalyseDesReponses != nil {
		candidates = append(candidates, c.AnalyseDesReponses.DatetimePublication)
	}
	if c.ReponseDuCommanditaire != nil {
		candidates = append(candidates, c.ReponseDuCommanditaire.DatetimePublication)
	}
	candidates = append(candidates, c.DateDeDebut)
	var max *LocalDateTime
	for i := range candidates {
		if !candidates[i].Before(now) {
			continue
		}
		if max == nil || max.Before(candidates[i]) {
			v := candidates[i]
			max = &v
		}
	}
	return max
}

// getNextQuestionID is ConsultationStrapiDTO.getNextQuestionId.
func (c *strapiConsultation) getNextQuestionID(q *strapiQuestion) *string {
	suivante := q.numeroQuestionSuivante()
	numero := q.numero()
	if suivante == nil {
		for _, other := range c.Questions {
			if int32(numero)+1 == int32(other.numero()) {
				id := other.id()
				return &id
			}
		}
		return nil
	}
	const numeroDerniereQuestion = 999
	if *suivante == numeroDerniereQuestion {
		return nil
	}
	for _, other := range c.Questions {
		if *suivante == other.numero() {
			id := other.id()
			return &id
		}
	}
	return nil
}

// getLastContenuAutre is ConsultationStrapiDTO.getLastContenuAutre: the other
// content with the latest publication date before now (the first one on a tie).
func (c *strapiConsultation) getLastContenuAutre(now LocalDateTime) *strapiContenuAutre {
	var last *strapiContenuAutre
	for _, a := range c.ContenuAutres {
		if !a.DatetimePublication.Before(now) {
			continue
		}
		if last == nil || last.DatetimePublication.Before(a.DatetimePublication) {
			last = a
		}
	}
	return last
}

// getFlammeLabel is ConsultationStrapiDTO.getFlammeLabel (elvis chain).
func (c *strapiConsultation) getFlammeLabel(now LocalDateTime) *string {
	if a := c.getLastContenuAutre(now); a != nil && a.FlammeLabel != nil {
		return a.FlammeLabel
	}
	if c.ReponseDuCommanditaire != nil && c.ReponseDuCommanditaire.FlammeLabel != nil {
		return c.ReponseDuCommanditaire.FlammeLabel
	}
	if c.AnalyseDesReponses != nil {
		return c.AnalyseDesReponses.FlammeLabel
	}
	return nil
}

// ---------------------------------------------------------------------------
// Contents (ConsultationContenuStrapiDTO.kt)
// ---------------------------------------------------------------------------

// strapiContenuAvant is StrapiConsultationContenuAvantReponse.
type strapiContenuAvant struct {
	DocumentID             string           `json:"documentId"`
	Slug                   string           `json:"slug"`
	TemplatePartage        string           `json:"template_partage"`
	HistoriqueTitre        string           `json:"historique_titre"`
	HistoriqueCallToAction string           `json:"historique_call_to_action"`
	Commanditaire          strapi.RichText  `json:"commanditaire"`
	Objectif               strapi.RichText  `json:"objectif"`
	AxeGouvernemental      strapi.RichText  `json:"axe_gouvernemental"`
	Presentation           strapi.RichText  `json:"presentation"`
	Sections               []*strapiSection `json:"sections,def"`
}

// strapiContenuApres is StrapiConsultationContenuApresReponse.
type strapiContenuApres struct {
	DocumentID             string           `json:"documentId"`
	Slug                   string           `json:"slug"`
	TemplatePartage        string           `json:"template_partage"`
	FeedbackMessage        string           `json:"feedback_message"`
	HistoriqueTitre        string           `json:"historique_titre"`
	HistoriqueCallToAction string           `json:"historique_call_to_action"`
	Sections               []*strapiSection `json:"sections,def"`
}

// strapiContenuAutre is StrapiConsultationContenuAutre.
type strapiContenuAutre struct {
	DocumentID             string           `json:"documentId"`
	Slug                   string           `json:"slug"`
	TemplatePartage        string           `json:"template_partage"`
	FeedbackMessage        string           `json:"feedback_message"`
	HistoriqueTitre        string           `json:"historique_titre"`
	HistoriqueCallToAction string           `json:"historique_call_to_action"`
	DatetimePublication    LocalDateTime    `json:"datetime_publication"`
	Sections               []*strapiSection `json:"sections,def"`
	FlammeLabel            *string          `json:"flamme_label"`
	RecapEmoji             *string          `json:"recap_emoji"`
	RecapLabel             *string          `json:"recap_label"`
}

// strapiAVenir is StrapiConsultationAVenir.
type strapiAVenir struct {
	TitreHistorique string `json:"titre_historique"`
}

// strapiAnalyse is StrapiConsultationAnalyseDesReponses.
type strapiAnalyse struct {
	DocumentID                string           `json:"documentId"`
	LienTelechargementAnalyse string           `json:"lien_telechargement_analyse"`
	Slug                      string           `json:"slug"`
	TemplatePartage           string           `json:"template_partage"`
	DatetimePublication       LocalDateTime    `json:"datetime_publication"`
	FeedbackMessage           string           `json:"feedback_message"`
	HistoriqueTitre           string           `json:"historique_titre"`
	HistoriqueCallToAction    string           `json:"historique_call_to_action"`
	FlammeLabel               *string          `json:"flamme_label"`
	Sections                  []*strapiSection `json:"sections,def"`
	RecapEmoji                *string          `json:"recap_emoji"`
	RecapLabel                *string          `json:"recap_label"`
	AnalysePdf                *strapi.MediaPdf `json:"pdf_analyse"`
}

// getAnalysePdfURL is getAnalysePdfUrl.
func (a *strapiAnalyse) getAnalysePdfURL() string {
	if a.AnalysePdf != nil {
		return a.AnalysePdf.URL
	}
	return a.LienTelechargementAnalyse
}

// strapiReponseCommanditaire is StrapiConsultationReponseCommanditaire.
type strapiReponseCommanditaire struct {
	DocumentID             string           `json:"documentId"`
	Slug                   string           `json:"slug"`
	TemplatePartage        string           `json:"template_partage"`
	DatetimePublication    LocalDateTime    `json:"datetime_publication"`
	FeedbackMessage        string           `json:"feedback_message"`
	HistoriqueTitre        string           `json:"historique_titre"`
	HistoriqueCallToAction string           `json:"historique_call_to_action"`
	FlammeLabel            *string          `json:"flamme_label"`
	Sections               []*strapiSection `json:"sections,def"`
	RecapEmoji             *string          `json:"recap_emoji"`
	RecapLabel             *string          `json:"recap_label"`
}

// ---------------------------------------------------------------------------
// Sections (ConsultationContenuSectionStrapiDTO.kt)
// ---------------------------------------------------------------------------

// strapiSection is the sealed interface StrapiConsultationSection, read from
// the "__component" property (@JsonTypeInfo NAME / EXISTING_PROPERTY, visible,
// no default implementation: an unknown or missing name fails the decoding).
// Exactly one pointer is set.
type strapiSection struct {
	Titre     *strapiSectionTitre
	RichText  *strapiSectionRichText
	Citation  *strapiSectionCitation
	Image     *strapiSectionImage
	Video     *strapiSectionVideo
	Chiffre   *strapiSectionChiffre
	Accordeon *strapiSectionAccordeon
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler.
func (s *strapiSection) UnmarshalJavaTree(tree any) error {
	obj, ok := tree.(map[string]any)
	if !ok {
		return errors.New("section: expected object")
	}
	name, _ := obj["__component"].(string)
	var out strapiSection
	var target any
	switch name {
	case "consultation-section.section-titre":
		out.Titre = &strapiSectionTitre{}
		target = out.Titre
	case "consultation-section.section-texte-riche":
		out.RichText = &strapiSectionRichText{}
		target = out.RichText
	case "consultation-section.section-citation":
		out.Citation = &strapiSectionCitation{}
		target = out.Citation
	case "consultation-section.section-image":
		out.Image = &strapiSectionImage{}
		target = out.Image
	case "consultation-section.section-video":
		out.Video = &strapiSectionVideo{}
		target = out.Video
	case "consultation-section.section-chiffre":
		out.Chiffre = &strapiSectionChiffre{}
		target = out.Chiffre
	case "consultation-section.section-accordeon":
		out.Accordeon = &strapiSectionAccordeon{}
		target = out.Accordeon
	default:
		return errors.New("section: unknown or missing __component")
	}
	if err := jsonjava.UnmarshalTree(tree, target); err != nil {
		return err
	}
	*s = out
	return nil
}

type strapiSectionCitation struct {
	ID          string          `json:"id"`
	Description strapi.RichText `json:"description"`
}

type strapiSectionImage struct {
	ID               string               `json:"id"`
	URL              string               `json:"url"`
	DescriptionImage string               `json:"description_accessible_de_l_image"`
	Image            *strapi.MediaPicture `json:"image"`
}

func (s *strapiSectionImage) getImageURL() string {
	if s.Image != nil {
		return s.Image.MediaURL()
	}
	return s.URL
}

type strapiSectionRichText struct {
	ID          string          `json:"id"`
	Description strapi.RichText `json:"description"`
}

type strapiSectionTitre struct {
	ID    string `json:"id"`
	Titre string `json:"titre"`
}

type strapiSectionChiffre struct {
	ID          string          `json:"id"`
	Titre       string          `json:"titre"`
	Description strapi.RichText `json:"description"`
}

type strapiSectionAccordeon struct {
	ID          string          `json:"id"`
	Titre       string          `json:"titre"`
	Description strapi.RichText `json:"description"`
}

type strapiSectionVideo struct {
	ID            string             `json:"id"`
	URL           string             `json:"url"`
	Largeur       int                `json:"largeur"`
	Hauteur       int                `json:"hauteur"`
	NomAuteur     string             `json:"nom_auteur"`
	PosteAuteur   string             `json:"poste_auteur"`
	DateTournage  common.LocalDate   `json:"date_tournage"`
	Transcription string             `json:"transcription"`
	Video         *strapi.MediaVideo `json:"video"`
}

func (s *strapiSectionVideo) getVideoURL() string {
	if s.Video != nil {
		return s.Video.URL
	}
	return s.URL
}

// ---------------------------------------------------------------------------
// Questions (ConsultationQuestionStrapiDTO.kt)
// ---------------------------------------------------------------------------

// strapiQuestion is the sealed interface StrapiConsultationQuestion (same
// polymorphic rules as strapiSection). Exactly one pointer is set.
type strapiQuestion struct {
	Multiple    *strapiQuestionMultiple
	Unique      *strapiQuestionUnique
	Ouverte     *strapiQuestionOuverte
	Description *strapiQuestionDescription
	Conditional *strapiQuestionConditional
}

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler.
func (q *strapiQuestion) UnmarshalJavaTree(tree any) error {
	obj, ok := tree.(map[string]any)
	if !ok {
		return errors.New("question: expected object")
	}
	name, _ := obj["__component"].(string)
	var out strapiQuestion
	var target any
	switch name {
	case "question-de-consultation.question-a-choix-multiples":
		out.Multiple = &strapiQuestionMultiple{}
		target = out.Multiple
	case "question-de-consultation.question-a-choix-unique":
		out.Unique = &strapiQuestionUnique{}
		target = out.Unique
	case "question-de-consultation.question-ouverte":
		out.Ouverte = &strapiQuestionOuverte{}
		target = out.Ouverte
	case "question-de-consultation.description":
		out.Description = &strapiQuestionDescription{}
		target = out.Description
	case "question-de-consultation.question-conditionnelle":
		out.Conditional = &strapiQuestionConditional{}
		target = out.Conditional
	default:
		return errors.New("question: unknown or missing __component")
	}
	if err := jsonjava.UnmarshalTree(tree, target); err != nil {
		return err
	}
	*q = out
	return nil
}

// The common properties of the sealed interface (id, numero, numeroQuestionSuivante).

func (q *strapiQuestion) id() string {
	switch {
	case q.Multiple != nil:
		return q.Multiple.ID
	case q.Unique != nil:
		return q.Unique.ID
	case q.Ouverte != nil:
		return q.Ouverte.ID
	case q.Description != nil:
		return q.Description.ID
	}
	return q.Conditional.ID
}

func (q *strapiQuestion) numero() int {
	switch {
	case q.Multiple != nil:
		return q.Multiple.Numero
	case q.Unique != nil:
		return q.Unique.Numero
	case q.Ouverte != nil:
		return q.Ouverte.Numero
	case q.Description != nil:
		return q.Description.Numero
	}
	return q.Conditional.Numero
}

func (q *strapiQuestion) numeroQuestionSuivante() *int {
	switch {
	case q.Multiple != nil:
		return q.Multiple.NumeroQuestionSuivante
	case q.Unique != nil:
		return q.Unique.NumeroQuestionSuivante
	case q.Ouverte != nil:
		return q.Ouverte.NumeroQuestionSuivante
	case q.Description != nil:
		return q.Description.NumeroQuestionSuivante
	}
	return q.Conditional.NumeroQuestionSuivante
}

type strapiQuestionMultiple struct {
	ID                     string               `json:"id"`
	Titre                  string               `json:"titre"`
	Numero                 int                  `json:"numero"`
	NombreMaximumDeChoix   int                  `json:"nombre_maximum_de_choix"`
	Choix                  []*strapiChoixSimple `json:"choix,def"`
	PopupExplication       *strapi.RichText     `json:"popup_explication"`
	NumeroQuestionSuivante *int                 `json:"question_suivante"`
}

type strapiQuestionUnique struct {
	ID                     string               `json:"id"`
	Titre                  string               `json:"titre"`
	Numero                 int                  `json:"numero"`
	Choix                  []*strapiChoixSimple `json:"choix,def"`
	PopupExplication       *strapi.RichText     `json:"popup_explication"`
	NumeroQuestionSuivante *int                 `json:"question_suivante"`
}

type strapiQuestionOuverte struct {
	ID                     string           `json:"id"`
	Titre                  string           `json:"titre"`
	Numero                 int              `json:"numero"`
	PopupExplication       *strapi.RichText `json:"popup_explication"`
	NumeroQuestionSuivante *int             `json:"question_suivante"`
}

type strapiQuestionDescription struct {
	ID                     string               `json:"id"`
	Titre                  string               `json:"titre"`
	URLImage               *string              `json:"url_image"`
	TranscriptionImage     *string              `json:"transcription_image"`
	Numero                 int                  `json:"numero"`
	Description            strapi.RichText      `json:"description"`
	NumeroQuestionSuivante *int                 `json:"question_suivante"`
	Image                  *strapi.MediaPicture `json:"image"`
}

// getImageURL is StrapiConsultationQuestionDescription.getImageUrl.
func (q *strapiQuestionDescription) getImageURL() *string {
	if q.Image != nil {
		u := q.Image.MediaURL()
		return &u
	}
	return q.URLImage
}

// strapiQuestionConditional is StrapiConsultationQuestionConditionnelle; its
// numeroQuestionSuivante has no @JsonProperty (the property is named after the
// Kotlin field, which Strapi never sends).
type strapiQuestionConditional struct {
	ID                     string                     `json:"id"`
	Titre                  string                     `json:"titre"`
	Numero                 int                        `json:"numero"`
	Choix                  []*strapiChoixConditionnel `json:"choix,def"`
	PopupExplication       *strapi.RichText           `json:"popup_explication"`
	NumeroQuestionSuivante *int                       `json:"numeroQuestionSuivante"`
}

type strapiChoixSimple struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Ouvert bool   `json:"ouvert"`
}

type strapiChoixConditionnel struct {
	ID                         string `json:"id"`
	Label                      string `json:"label"`
	Ouvert                     bool   `json:"ouvert"`
	NumeroDeLaQuestionSuivante int    `json:"numero_de_la_question_suivante"`
}
