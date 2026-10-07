package content

// JSON response DTOs (ports of *Json.kt). Field order = Kotlin declaration
// order, names = Kotlin property names (or @JsonProperty).

// QuestionsAuGouvernementContentJSON is infrastructure.content.json.QuestionsAuGouvernementContentJson.
type QuestionsAuGouvernementContentJSON struct {
	Info                string  `json:"info"`
	TexteTotalQuestions string  `json:"texteTotalQuestions"`
	ProgrammeDuMois     *string `json:"programmeDuMois"`
	CommentCaMarche     *string `json:"commentCaMarche"`
}

// JavaName is the XML root element.
func (QuestionsAuGouvernementContentJSON) JavaName() string {
	return "QuestionsAuGouvernementContentJson"
}

// ReponseAuxQagsJSON is ReponseAuxQagsJson.
type ReponseAuxQagsJSON struct {
	InfoReponsesAVenir string `json:"infoReponsesAVenir"`
}

// JavaName is the XML root element.
func (ReponseAuxQagsJSON) JavaName() string { return "ReponseAuxQagsJson" }

// PoserMaQuestionJSON is PoserMaQuestionJson.
type PoserMaQuestionJSON struct {
	Regles string `json:"regles"`
}

// JavaName is the XML root element.
func (PoserMaQuestionJSON) JavaName() string { return "PoserMaQuestionJson" }

// SiteVitrineAccueilJSON is SiteVitrineAccueilJson.
type SiteVitrineAccueilJSON struct {
	TitreHeader     string `json:"titreHeader"`
	SousTitreHeader string `json:"sousTitreHeader"`
	TitreBody       string `json:"titreBody"`
	DescriptionBody string `json:"descriptionBody"`
	TexteImage1     string `json:"texteImage1"`
	TexteImage2     string `json:"texteImage2"`
	TexteImage3     string `json:"texteImage3"`
}

// JavaName is the XML root element.
func (SiteVitrineAccueilJSON) JavaName() string { return "SiteVitrineAccueilJson" }

// SiteVitrineConditionGeneralesJSON is SiteVitrineConditionGeneralesJson.
type SiteVitrineConditionGeneralesJSON struct {
	ConditionsGeneralesDUtilisation string `json:"conditionsGeneralesDUtilisation"`
}

// JavaName is the XML root element.
func (SiteVitrineConditionGeneralesJSON) JavaName() string {
	return "SiteVitrineConditionGeneralesJson"
}

// SiteVitrineConsultationJSON is SiteVitrineConsultationJson.
type SiteVitrineConsultationJSON struct {
	DonnezVotreAvis string `json:"donnezVotreAvis"`
}

// JavaName is the XML root element.
func (SiteVitrineConsultationJSON) JavaName() string { return "SiteVitrineConsultationJson" }

// SiteVitrineDeclarationAccessibiliteJSON is SiteVitrineDeclarationAccessibiliteJson.
type SiteVitrineDeclarationAccessibiliteJSON struct {
	Declaration string `json:"declaration"`
}

// JavaName is the XML root element.
func (SiteVitrineDeclarationAccessibiliteJSON) JavaName() string {
	return "SiteVitrineDeclarationAccessibiliteJson"
}

// SiteVitrineMentionsLegalesJSON is SiteVitrineMentionsLegalesJson.
type SiteVitrineMentionsLegalesJSON struct {
	MentionsLegales string `json:"mentionsLegales"`
}

// JavaName is the XML root element.
func (SiteVitrineMentionsLegalesJSON) JavaName() string { return "SiteVitrineMentionsLegalesJson" }

// SiteVitrinePolitiqueConfidentialiteJSON is SiteVitrinePolitiqueConfidentialiteJson.
type SiteVitrinePolitiqueConfidentialiteJSON struct {
	PolitiqueDeConfidentialite string `json:"politiqueDeConfidentialite"`
}

// JavaName is the XML root element.
func (SiteVitrinePolitiqueConfidentialiteJSON) JavaName() string {
	return "SiteVitrinePolitiqueConfidentialiteJson"
}

// SiteVitrineQuestionAuGouvernementJSON is SiteVitrineQuestionAuGouvernementJson.
type SiteVitrineQuestionAuGouvernementJSON struct {
	Titre        string `json:"titre"`
	SousTitre    string `json:"sousTitre"`
	TexteSoutien string `json:"texteSoutien"`
}

// JavaName is the XML root element.
func (SiteVitrineQuestionAuGouvernementJSON) JavaName() string {
	return "SiteVitrineQuestionAuGouvernementJson"
}

// NewsJSON is infrastructure.welcomePage.NewsJson.
type NewsJSON struct {
	Description      string  `json:"description"`
	ShortDescription string  `json:"short_description"`
	CallToActionText string  `json:"callToActionText"`
	RouteName        string  `json:"routeName"`
	RouteArgument    *string `json:"routeArgument"`
}

// JavaName is the XML root element.
func (NewsJSON) JavaName() string { return "NewsJson" }

// ParticipationCharterJSON is infrastructure.participationCharter.ParticipationCharterJson.
type ParticipationCharterJSON struct {
	ExtraText   string `json:"extraText"`
	PreviewText string `json:"previewText"`
}

// JavaName is the XML root element.
func (ParticipationCharterJSON) JavaName() string { return "ParticipationCharterJson" }
