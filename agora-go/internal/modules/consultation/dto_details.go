package consultation

import (
	"strings"
	"time"

	"agora/internal/modules/thematique"
)

// ConsultationDetailsV2JSON is ConsultationDetailsV2Json (GET /v2/consultations/{id}
// and /v2/consultations/{id}/updates/{updateId}).
type ConsultationDetailsV2JSON struct {
	ID                  string                        `json:"id"`
	Slug                string                        `json:"slug"`
	Title               string                        `json:"title"`
	LastUpdateSlug      string                        `json:"lastUpdateSlug"`
	UpdateID            string                        `json:"updateId"`
	CoverURL            string                        `json:"coverUrl"`
	ShareText           string                        `json:"shareText"`
	Thematique          thematique.ThematiqueNoIDJSON `json:"thematique"`
	QuestionsInfo       QuestionsInfoJSON             `json:"questionsInfo"`
	ConsultationDates   ConsultationDatesJSON         `json:"consultationDates"`
	ResponsesInfo       *ResponsesInfoJSON            `json:"responsesInfo"`
	InfoHeader          *InfoHeaderJSON               `json:"infoHeader"`
	Body                BodyJSON                      `json:"body"`
	ParticipationInfo   ParticipationInfoJSON         `json:"participationInfo"`
	DownloadAnalysisURL *string                       `json:"downloadAnalysisUrl"`
	FeedbackQuestion    *FeedbackQuestionJSON         `json:"feedbackQuestion"`
	Footer              *FooterJSON                   `json:"footer"`
	Goals               []GoalJSON                    `json:"goals,nullable"`
	History             []HistoryJSON                 `json:"history"`
	Territory           string                        `json:"territory"`
	TitrePageWeb        string                        `json:"titrePageWeb"`
	SousTitrePageWeb    string                        `json:"sousTitrePageWeb"`
	IsAnsweredByUser    bool                          `json:"isAnsweredByUser"`
}

// JavaName is the XML root element.
func (ConsultationDetailsV2JSON) JavaName() string { return "ConsultationDetailsV2Json" }

// QuestionsInfoJSON is ConsultationDetailsV2Json.QuestionsInfo.
type QuestionsInfoJSON struct {
	EndDate              string `json:"endDate"`
	QuestionCount        string `json:"questionCount"`
	EstimatedTime        string `json:"estimatedTime"`
	ParticipantCount     int    `json:"participantCount"`
	ParticipantCountGoal int    `json:"participantCountGoal"`
}

// ConsultationDatesJSON is ConsultationDetailsV2Json.ConsultationDates.
type ConsultationDatesJSON struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// ResponsesInfoJSON is ConsultationDetailsV2Json.ResponsesInfo.
type ResponsesInfoJSON struct {
	Picto       string `json:"picto"`
	Description string `json:"description"`
	ActionText  string `json:"actionText"`
}

// InfoHeaderJSON is ConsultationDetailsV2Json.InfoHeader.
type InfoHeaderJSON struct {
	Picto       string `json:"picto"`
	Description string `json:"description"`
}

// BodyJSON is ConsultationDetailsV2Json.Body.
type BodyJSON struct {
	HeaderSections  []SectionJSON `json:"headerSections"`
	SectionsPreview []SectionJSON `json:"sectionsPreview"`
	Sections        []SectionJSON `json:"sections"`
}

// SectionJSON is the sealed class ConsultationDetailsV2Json.Section: one of the
// Section*JSON structs below (subclass properties first, then "type").
type SectionJSON any

// SectionTitleJSON is Section.Title.
type SectionTitleJSON struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// SectionRichTextJSON is Section.RichText.
type SectionRichTextJSON struct {
	Description string `json:"description"`
	Type        string `json:"type"`
}

// SectionImageJSON is Section.Image.
type SectionImageJSON struct {
	URL                string  `json:"url"`
	ContentDescription *string `json:"contentDescription"`
	Type               string  `json:"type"`
}

// SectionVideoJSON is Section.Video.
type SectionVideoJSON struct {
	URL           string               `json:"url"`
	VideoWidth    int                  `json:"videoWidth"`
	VideoHeight   int                  `json:"videoHeight"`
	AuthorInfo    *VideoAuthorInfoJSON `json:"authorInfo"`
	Transcription string               `json:"transcription"`
	Type          string               `json:"type"`
}

// VideoAuthorInfoJSON is Section.Video.AuthorInfo.
type VideoAuthorInfoJSON struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Date    string `json:"date"`
}

// SectionFocusNumberJSON is Section.FocusNumber.
type SectionFocusNumberJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// SectionQuoteJSON is Section.Quote.
type SectionQuoteJSON struct {
	Description string `json:"description"`
	Type        string `json:"type"`
}

// SectionAccordionJSON is Section.Accordion.
type SectionAccordionJSON struct {
	Title    string        `json:"title"`
	Sections []SectionJSON `json:"sections"`
	Type     string        `json:"type"`
}

// ParticipationInfoJSON is ConsultationDetailsV2Json.ParticipationInfo.
type ParticipationInfoJSON struct {
	ParticipantCount     int `json:"participantCount"`
	ParticipantCountGoal int `json:"participantCountGoal"`
}

// FeedbackQuestionJSON is ConsultationDetailsV2Json.FeedbackQuestion.
type FeedbackQuestionJSON struct {
	UpdateID    string               `json:"updateId"`
	Title       string               `json:"title"`
	Picto       string               `json:"picto"`
	Description string               `json:"description"`
	Results     *FeedbackResultsJSON `json:"results"`
}

// FeedbackResultsJSON is ConsultationDetailsV2Json.FeedbackQuestion.Results.
type FeedbackResultsJSON struct {
	UserResponse *bool              `json:"userResponse"`
	Stats        *FeedbackStatsJSON `json:"stats"`
}

// FeedbackStatsJSON is ConsultationDetailsV2Json.FeedbackQuestion.Stats.
type FeedbackStatsJSON struct {
	PositiveRatio int `json:"positiveRatio"`
	NegativeRatio int `json:"negativeRatio"`
	ResponseCount int `json:"responseCount"`
}

// FooterJSON is ConsultationDetailsV2Json.Footer.
type FooterJSON struct {
	Title       *string `json:"title"`
	Description string  `json:"description"`
}

// GoalJSON is ConsultationDetailsV2Json.Goal.
type GoalJSON struct {
	Picto       string `json:"picto"`
	Description string `json:"description"`
}

// HistoryJSON is ConsultationDetailsV2Json.History.
type HistoryJSON struct {
	UpdateID   *string `json:"updateId"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Title      string  `json:"title"`
	Date       *string `json:"date"`
	ActionText *string `json:"actionText"`
	Slug       *string `json:"slug"`
}

const (
	shareTextReplaceTitlePattern = "{title}"
	shareTextReplaceURLPattern   = "{url}"
	shareTextConsultationPath    = "/consultations/"
)

// ToDetailsJSON is ConsultationDetailsV2JsonMapper.toJson. universalLink is
// System.getenv("UNIVERSAL_LINK_URL") ("null" when unset: Kotlin concatenates a null).
func ToDetailsJSON(d *DetailsWithInfo, universalLink string) ConsultationDetailsV2JSON {
	info := d.Consultation
	update := d.Update
	return ConsultationDetailsV2JSON{
		ID:             info.ID,
		Slug:           info.Slug,
		Title:          info.Title,
		LastUpdateSlug: update.Slug,
		UpdateID:       update.ID,
		CoverURL:       info.DetailsCoverURL,
		ShareText: strings.ReplaceAll(strings.ReplaceAll(update.ShareTextTemplate,
			shareTextReplaceURLPattern, universalLink+shareTextConsultationPath+info.Slug),
			shareTextReplaceTitlePattern, info.Title),
		Thematique: thematique.ToNoIDJSON(info.Thematique),
		QuestionsInfo: QuestionsInfoJSON{
			EndDate:              info.EndDate.Format(),
			QuestionCount:        info.QuestionCount,
			EstimatedTime:        info.EstimatedTime,
			ParticipantCount:     d.ParticipantCount,
			ParticipantCountGoal: info.ParticipantCountGoal,
		},
		ConsultationDates: ConsultationDatesJSON{StartDate: info.StartDate.Format(), EndDate: info.EndDate.Format()},
		ResponsesInfo:     responsesInfoJSON(update.ResponsesInfo),
		InfoHeader:        infoHeaderJSON(update.InfoHeader),
		Body: BodyJSON{
			HeaderSections:  sectionsJSON(update.SectionsHeader),
			SectionsPreview: sectionsJSON(update.BodyPreview),
			Sections:        sectionsJSON(update.Body),
		},
		ParticipationInfo:   ParticipationInfoJSON{ParticipantCount: d.ParticipantCount, ParticipantCountGoal: info.ParticipantCountGoal},
		DownloadAnalysisURL: update.DownloadAnalysisURL,
		FeedbackQuestion:    feedbackQuestionJSON(d),
		Footer:              footerJSON(update.Footer),
		Goals:               goalsJSON(update.Goals),
		History:             historyJSON(d.History),
		Territory:           info.Territory,
		TitrePageWeb:        info.TitreWeb,
		SousTitrePageWeb:    info.SousTitreWeb,
		IsAnsweredByUser:    d.IsAnsweredByUser,
	}
}

func responsesInfoJSON(r *ResponsesInfo) *ResponsesInfoJSON {
	if r == nil {
		return nil
	}
	return &ResponsesInfoJSON{Picto: r.Picto, Description: r.Description, ActionText: r.ActionText}
}

func infoHeaderJSON(h *InfoHeader) *InfoHeaderJSON {
	if h == nil {
		return nil
	}
	return &InfoHeaderJSON{Picto: h.Picto, Description: h.Description}
}

func footerJSON(f *Footer) *FooterJSON {
	if f == nil {
		return nil
	}
	return &FooterJSON{Title: f.Title, Description: f.Description}
}

func goalsJSON(goals []Goal) []GoalJSON {
	if goals == nil {
		return nil
	}
	out := make([]GoalJSON, len(goals))
	for i, g := range goals {
		out[i] = GoalJSON{Picto: g.Picto, Description: g.Description}
	}
	return out
}

// feedbackQuestionJSON: the results are only given to a user who answered the
// question; the statistics only when they were computed.
func feedbackQuestionJSON(d *DetailsWithInfo) *FeedbackQuestionJSON {
	q := d.Update.FeedbackQuestion
	if q == nil {
		return nil
	}
	out := &FeedbackQuestionJSON{UpdateID: q.ConsultationUpdateID, Title: q.Title, Picto: q.Picto, Description: q.Description}
	if d.IsUserFeedbackPositive != nil {
		results := &FeedbackResultsJSON{UserResponse: d.IsUserFeedbackPositive}
		if s := d.FeedbackStats; s != nil {
			results.Stats = &FeedbackStatsJSON{PositiveRatio: s.PositiveRatio, NegativeRatio: s.NegativeRatio, ResponseCount: s.ResponseCount}
		}
		out.Results = results
	}
	return out
}

func historyJSON(history []UpdateHistory) []HistoryJSON {
	out := make([]HistoryJSON, len(history))
	for i, h := range history {
		j := HistoryJSON{UpdateID: h.ConsultationUpdateID, Title: h.Title, ActionText: h.ActionText, Slug: h.Slug}
		switch h.Type {
		case HistoryUpdate:
			j.Type = "update"
		case HistoryResults:
			j.Type = "results"
		}
		switch h.Status {
		case HistoryDone:
			j.Status = "done"
		case HistoryCurrent:
			j.Status = "current"
		case HistoryIncoming:
			j.Status = "incoming"
		}
		if h.UpdateDate != nil {
			j.Date = ptr(formatDateOf(*h.UpdateDate))
		}
		out[i] = j
	}
	return out
}

// formatDateOf is DateMapper.toFormattedDate(Date).
func formatDateOf(t time.Time) string { return FromTime(t).Format() }

func sectionsJSON(sections []Section) []SectionJSON {
	out := make([]SectionJSON, len(sections))
	for i, s := range sections {
		out[i] = sectionJSON(s)
	}
	return out
}

func sectionJSON(section Section) SectionJSON {
	switch s := section.(type) {
	case SectionTitle:
		return SectionTitleJSON{Title: s.Title, Type: "title"}
	case SectionRichText:
		return SectionRichTextJSON{Description: s.Description, Type: "richText"}
	case SectionImage:
		return SectionImageJSON{URL: s.URL, ContentDescription: s.ContentDescription, Type: "image"}
	case SectionVideo:
		v := SectionVideoJSON{URL: s.URL, VideoWidth: s.Width, VideoHeight: s.Height, Transcription: s.Transcription, Type: "video"}
		if s.AuthorInfo != nil {
			v.AuthorInfo = &VideoAuthorInfoJSON{
				Name: s.AuthorInfo.Name, Message: s.AuthorInfo.Message,
				// DateMapper.toFormattedDate(LocalDate): the start of the day
				Date: LocalDateTime{T: time.Date(s.AuthorInfo.Date.Year, time.Month(s.AuthorInfo.Date.Month), s.AuthorInfo.Date.Day, 0, 0, 0, 0, time.UTC)}.Format(),
			}
		}
		return v
	case SectionFocusNumber:
		return SectionFocusNumberJSON{Title: s.Title, Description: s.Description, Type: "focusNumber"}
	case SectionQuote:
		return SectionQuoteJSON{Description: s.Description, Type: "quote"}
	case SectionAccordion:
		return SectionAccordionJSON{Title: s.Title, Sections: sectionsJSON(s.Sections), Type: "accordion"}
	}
	panic("kotlin.NoWhenBranchMatchedException")
}

// ---------------------------------------------------------------------------
// Feedback on an update
// ---------------------------------------------------------------------------

// InsertFeedbackJSON is InsertFeedbackConsultationUpdateJson (request body).
type InsertFeedbackJSON struct {
	IsPositive bool `json:"isPositive"`
}

// FeedbackResultsBodyJSON is FeedbackConsultationUpdateResultsJson.
type FeedbackResultsBodyJSON struct {
	UserResponse bool                     `json:"userResponse"`
	Stats        *FeedbackUpdateStatsJSON `json:"stats"`
}

// JavaName is the XML root element.
func (FeedbackResultsBodyJSON) JavaName() string { return "FeedbackConsultationUpdateResultsJson" }

// FeedbackUpdateStatsJSON is FeedbackConsultationUpdateStatsJson.
type FeedbackUpdateStatsJSON struct {
	PositiveRatio int `json:"positiveRatio"`
	NegativeRatio int `json:"negativeRatio"`
	ResponseCount int `json:"responseCount"`
}

// ToFeedbackResultsJSON is FeedbackConsultationUpdateJsonMapper.toJson.
func ToFeedbackResultsJSON(r FeedbackResults) FeedbackResultsBodyJSON {
	out := FeedbackResultsBodyJSON{UserResponse: r.UserResponse}
	if r.Stats != nil {
		out.Stats = &FeedbackUpdateStatsJSON{PositiveRatio: r.Stats.PositiveRatio, NegativeRatio: r.Stats.NegativeRatio, ResponseCount: r.Stats.ResponseCount}
	}
	return out
}

// ---------------------------------------------------------------------------
// Consultation preview (GET /consultations)
// ---------------------------------------------------------------------------

// ConsultationPreviewJSON is ConsultationPreviewJson.
type ConsultationPreviewJSON struct {
	Ongoing  []ConsultationOngoingJSON  `json:"ongoing"`
	Finished []ConsultationFinishedJSON `json:"finished"`
	Answered []ConsultationFinishedJSON `json:"answered"`
}

// JavaName is the XML root element.
func (ConsultationPreviewJSON) JavaName() string { return "ConsultationPreviewJson" }

// ConsultationOngoingJSON is ConsultationOngoingJson.
type ConsultationOngoingJSON struct {
	ID             string                        `json:"id"`
	Slug           string                        `json:"slug"`
	Title          string                        `json:"title"`
	CoverURL       string                        `json:"coverUrl"`
	EndDate        string                        `json:"endDate"`
	Thematique     thematique.ThematiqueNoIDJSON `json:"thematique"`
	HighlightLabel *string                       `json:"highlightLabel"`
	Territory      string                        `json:"territory"`
}

// JavaName is the XML class name.
func (ConsultationOngoingJSON) JavaName() string { return "ConsultationOngoingJson" }

// ConsultationFinishedJSON is ConsultationFinishedJson.
type ConsultationFinishedJSON struct {
	ID          string                        `json:"id"`
	Slug        string                        `json:"slug"`
	Title       string                        `json:"title"`
	CoverURL    string                        `json:"coverUrl"`
	Thematique  thematique.ThematiqueNoIDJSON `json:"thematique"`
	Step        int                           `json:"step"`
	UpdateLabel *string                       `json:"updateLabel"`
	UpdateDate  string                        `json:"updateDate"`
	Territory   string                        `json:"territory"`
}

// JavaName is the XML class name.
func (ConsultationFinishedJSON) JavaName() string { return "ConsultationFinishedJson" }

// ToPreviewJSON is ConsultationPreviewJsonMapper.toJson(ongoing, finished, answered).
func ToPreviewJSON(page ConsultationPreviewPage, now LocalDateTime) ConsultationPreviewJSON {
	out := ConsultationPreviewJSON{
		Ongoing:  make([]ConsultationOngoingJSON, len(page.Ongoing)),
		Finished: make([]ConsultationFinishedJSON, len(page.Finished)),
		Answered: make([]ConsultationFinishedJSON, len(page.Answered)),
	}
	for i, d := range page.Ongoing {
		out.Ongoing[i] = ConsultationOngoingJSON{
			ID: d.ID, Slug: d.Slug, Title: d.Title, CoverURL: d.CoverURL, EndDate: d.EndDate.Format(),
			Thematique: thematique.ToNoIDJSON(d.Thematique), HighlightLabel: d.HighlightLabel(now), Territory: d.Territory,
		}
	}
	for i, d := range page.Finished {
		out.Finished[i] = finishedJSON(d, now)
	}
	for i, d := range page.Answered {
		out.Answered[i] = finishedJSON(d, now)
	}
	return out
}

// ToFinishedJSON is ConsultationPreviewJsonMapper.toJson(ConsultationPreviewFinished).
func ToFinishedJSON(d ConsultationPreviewFinished, now LocalDateTime) ConsultationFinishedJSON {
	return finishedJSON(d, now)
}

func finishedJSON(d ConsultationPreviewFinished, now LocalDateTime) ConsultationFinishedJSON {
	step := 1
	switch d.GetStep(now) {
	case CollectingData:
		step = 1
	case PoliticalCommitment:
		step = 2
	case Execution:
		step = 3
	}
	return ConsultationFinishedJSON{
		ID: d.ID, Slug: d.Slug, Title: d.Title, CoverURL: d.CoverURL, Thematique: thematique.ToNoIDJSON(d.Thematique),
		Step: step, UpdateLabel: d.GetUpdateLabel(now), UpdateDate: d.LastUpdateDate.Format(), Territory: d.Territory,
	}
}
