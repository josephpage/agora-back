package consultation

import (
	"time"

	"agora/internal/common"
	"agora/internal/modules/thematique"
)

// Thematique is domain.Thematique (owned by the thematique module).
type Thematique = thematique.Thematique

// ConsultationInfo is usecase/consultation/repository/ConsultationInfo.
type ConsultationInfo struct {
	ID                   string
	Title                string
	Slug                 string
	CoverURL             string
	DetailsCoverURL      string
	StartDate            LocalDateTime
	EndDate              LocalDateTime
	QuestionCount        string
	EstimatedTime        string
	ParticipantCountGoal int
	Thematique           Thematique
	Territory            string
	TitreWeb             string
	SousTitreWeb         string
}

// IsOngoing is ConsultationInfo.isOngoing(now).
func (c *ConsultationInfo) IsOngoing(now LocalDateTime) bool { return now.Before(c.EndDate) }

// ConsultationPreview is domain.ConsultationPreview (an ongoing consultation).
type ConsultationPreview struct {
	ID         string
	Slug       string
	Title      string
	CoverURL   string
	Thematique Thematique
	EndDate    LocalDateTime
	Territory  string
}

// HighlightLabel is ConsultationPreview.highlightLabel(today): "Dernier jour !"
// or "Plus que N jours !" in the last week.
func (c ConsultationPreview) HighlightLabel(today LocalDateTime) *string {
	days := daysBetween(today, c.EndDate)
	if c.EndDate.Before(today) {
		return nil
	}
	switch {
	case days == 0:
		s := "Dernier jour !"
		return &s
	case days >= 1 && days <= 6:
		s := "Plus que " + itoa(days+1) + " jours !"
		return &s
	}
	return nil
}

// ConsultationPreviewFinished is domain.ConsultationPreviewFinished.
type ConsultationPreviewFinished struct {
	ID             string
	Slug           string
	Title          string
	CoverURL       string
	Thematique     Thematique
	UpdateLabel    *string
	LastUpdateDate LocalDateTime
	EndDate        LocalDateTime
	Territory      string
}

// ConsultationStatus is domain.ConsultationStatus.
type ConsultationStatus int

// The ConsultationStatus constants, in declaration order.
const (
	CollectingData ConsultationStatus = iota
	PoliticalCommitment
	Execution
)

// GetStep is ConsultationPreviewFinished.getStep(now).
func (c ConsultationPreviewFinished) GetStep(now LocalDateTime) ConsultationStatus {
	if now.Before(c.EndDate) {
		return CollectingData
	}
	return PoliticalCommitment
}

// GetUpdateLabel is ConsultationPreviewFinished.getUpdateLabel(now): the label
// is kept from the last update date to 90 days after it.
func (c ConsultationPreviewFinished) GetUpdateLabel(now LocalDateTime) *string {
	if c.UpdateLabel == nil {
		return nil
	}
	maxUpdateDateLabel := c.LastUpdateDate.PlusDays(90)
	if now.Before(c.LastUpdateDate) || now.After(maxUpdateDateLabel) {
		return nil
	}
	return c.UpdateLabel
}

// ConsultationPreviewPage is domain.ConsultationPreviewPage.
type ConsultationPreviewPage struct {
	Ongoing  []ConsultationPreview
	Finished []ConsultationPreviewFinished
	Answered []ConsultationPreviewFinished
}

// ConsultationWithUpdateInfo is usecase/consultation/repository/ConsultationWithUpdateInfo.
type ConsultationWithUpdateInfo struct {
	ID           string
	Slug         string
	Title        string
	CoverURL     string
	ThematiqueID string
	EndDate      LocalDateTime
	UpdateDate   LocalDateTime
	UpdateLabel  *string
	Territory    string
}

// ---------------------------------------------------------------------------
// Updates (domain/ConsultationUpdateInfoV2.kt)
// ---------------------------------------------------------------------------

// UpdateInfo is domain.ConsultationUpdateInfoV2.
type UpdateInfo struct {
	ID                   string
	Slug                 string
	UpdateDate           LocalDateTime
	ShareTextTemplate    string
	HasQuestionsInfo     bool
	HasParticipationInfo bool
	ResponsesInfo        *ResponsesInfo
	InfoHeader           *InfoHeader
	SectionsHeader       []Section
	Body                 []Section
	BodyPreview          []Section
	DownloadAnalysisURL  *string
	FeedbackQuestion     *FeedbackQuestion
	Footer               *Footer
	Goals                []Goal // nil = null (Kotlin List?)
}

// ResponsesInfo is ConsultationUpdateInfoV2.ResponsesInfo.
type ResponsesInfo struct{ Picto, Description, ActionText string }

// InfoHeader is ConsultationUpdateInfoV2.InfoHeader.
type InfoHeader struct{ Picto, Description string }

// FeedbackQuestion is ConsultationUpdateInfoV2.FeedbackQuestion.
type FeedbackQuestion struct {
	ConsultationUpdateID string
	Title                string
	Picto                string
	Description          string
}

// Footer is ConsultationUpdateInfoV2.Footer.
type Footer struct {
	Title       *string
	Description string
}

// Goal is ConsultationUpdateInfoV2.Goal.
type Goal struct{ Picto, Description string }

// Section is the sealed class ConsultationUpdateInfoV2.Section.
type Section interface{ isSection() }

// The Section subclasses.
type (
	SectionTitle    struct{ Title string }
	SectionRichText struct{ Description string }
	SectionImage    struct {
		URL                string
		ContentDescription *string
	}
	SectionVideo struct {
		URL           string
		Width, Height int
		AuthorInfo    *VideoAuthorInfo
		Transcription string
	}
	SectionFocusNumber struct{ Title, Description string }
	SectionQuote       struct{ Description string }
	SectionAccordion   struct {
		Title    string
		Sections []Section
	}
)

// VideoAuthorInfo is ConsultationUpdateInfoV2.Section.Video.AuthorInfo.
type VideoAuthorInfo struct {
	Name, Message string
	Date          common.LocalDate
}

func (SectionTitle) isSection()       {}
func (SectionRichText) isSection()    {}
func (SectionImage) isSection()       {}
func (SectionVideo) isSection()       {}
func (SectionFocusNumber) isSection() {}
func (SectionQuote) isSection()       {}
func (SectionAccordion) isSection()   {}

// ---------------------------------------------------------------------------
// Details, history, feedback
// ---------------------------------------------------------------------------

// FeedbackStats is domain.FeedbackConsultationUpdateStats.
type FeedbackStats struct {
	PositiveRatio int
	NegativeRatio int
	ResponseCount int
}

// FeedbackResults is domain.FeedbackConsultationUpdateResults.
type FeedbackResults struct {
	UserResponse bool
	Stats        *FeedbackStats
}

// FeedbackInserting is domain.FeedbackConsultationUpdateInserting.
type FeedbackInserting struct {
	UserID               string
	ConsultationID       string
	ConsultationUpdateID string
	IsPositive           bool
}

// Details is domain.ConsultationDetailsV2: what the details caches hold.
// A value is shared between requests and must never be modified.
type Details struct {
	Consultation  *ConsultationInfo
	Update        *UpdateInfo
	FeedbackStats *FeedbackStats
}

// DetailsWithInfo is domain.ConsultationDetailsV2WithInfo.
type DetailsWithInfo struct {
	Consultation           *ConsultationInfo
	Update                 *UpdateInfo
	FeedbackStats          *FeedbackStats
	History                []UpdateHistory
	ParticipantCount       int
	IsUserFeedbackPositive *bool
	IsAnsweredByUser       bool
}

// HistoryType is domain.ConsultationUpdateHistoryType.
type HistoryType int

// The ConsultationUpdateHistoryType constants.
const (
	HistoryUpdate HistoryType = iota
	HistoryResults
)

// HistoryStatus is domain.ConsultationUpdateHistoryStatus.
type HistoryStatus int

// The ConsultationUpdateHistoryStatus constants.
const (
	HistoryDone HistoryStatus = iota
	HistoryCurrent
	HistoryIncoming
)

// UpdateHistory is domain.ConsultationUpdateHistory.
type UpdateHistory struct {
	Type                 HistoryType
	ConsultationUpdateID *string
	Status               HistoryStatus
	Title                string
	Slug                 *string
	UpdateDate           *time.Time // java.util.Date (an instant)
	ActionText           *string
}

// ---------------------------------------------------------------------------
// Questions (domain/Question.kt, ChoixPossible.kt)
// ---------------------------------------------------------------------------

// Questions is domain.Questions.
type Questions struct {
	QuestionCount int
	Questions     []Question
}

// ChoixPossible is the sealed class ChoixPossible (default and conditional
// choices share the same fields; NextQuestionID is set for the conditional ones).
type ChoixPossible struct {
	ID               string
	Label            string
	Ordre            int
	QuestionID       string
	HasOpenTextField bool
	NextQuestionID   string // ChoixPossibleConditional only
}

// QuestionKind tells which subclass of the sealed class Question a value is.
type QuestionKind int

// The Question subclasses.
const (
	KindUniqueChoice QuestionKind = iota
	KindMultipleChoices
	KindConditional
	KindOpen
	KindChapter
)

// Question is the sealed class Question and its subclasses (QuestionUniqueChoice,
// QuestionMultipleChoices, QuestionConditional, QuestionOpen, QuestionChapter).
type Question struct {
	Kind             QuestionKind
	ID               string
	Title            string
	PopupDescription *string
	Order            int
	NextQuestionID   *string
	ConsultationID   string
	// QuestionWithChoices
	ChoixPossibleList []ChoixPossible
	// QuestionMultipleChoices
	MaxChoices int
	// QuestionChapter
	URLImage           *string
	Description        string
	TranscriptionImage *string
}
