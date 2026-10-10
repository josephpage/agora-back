package consultationresponse

import (
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
)

// ---------------------------------------------------------------------------
// infrastructure/consultationResponse/ReponsesConsultationJson.kt
// ---------------------------------------------------------------------------

// ReponsesConsultationJSON is ReponsesConsultationJson (the request body). A JSON null
// element of `responses` stays null in Kotlin (the Kotlin module does not check the
// elements of a list) and fails later with a NullPointerException (HTTP 500).
type ReponsesConsultationJSON struct {
	ConsultationID string                     `json:"consultationId"`
	Responses      []*ReponseConsultationJSON `json:"responses"`
}

// ReponseConsultationJSON is ReponseConsultationJson. The elements of choiceIds may be null.
type ReponseConsultationJSON struct {
	QuestionID   string    `json:"questionId"`
	ChoiceIDs    []*string `json:"choiceIds"`
	ResponseText *string   `json:"responseText"`
}

// ResponseConsultationResultJSON is ResponseConsultationResultJson.
type ResponseConsultationResultJSON struct {
	AskDemographicInfo bool `json:"askDemographicInfo"`
}

// JavaName is the XML root element.
func (ResponseConsultationResultJSON) JavaName() string { return "ResponseConsultationResultJson" }

// toDomain is ReponseConsultationJsonMapper.toDomain: a null element of `responses` is a
// NullPointerException.
func toDomain(body *ReponsesConsultationJSON) []ReponseConsultationInserting {
	out := make([]ReponseConsultationInserting, len(body.Responses))
	for i, r := range body.Responses {
		if r == nil {
			panic("java.lang.NullPointerException: ReponseConsultationJsonMapper.toDomain: null element of responses")
		}
		text := ""
		if r.ResponseText != nil {
			text = *r.ResponseText
		}
		out[i] = ReponseConsultationInserting{QuestionID: r.QuestionID, ChoiceIDs: r.ChoiceIDs, ResponseText: text}
	}
	return out
}

// ---------------------------------------------------------------------------
// infrastructure/consultationResults/ConsultationResultsJson.kt, ConsultationResultJsonMapper.kt
// ---------------------------------------------------------------------------

// ConsultationResultsJSON is ConsultationResultsJson.
type ConsultationResultsJSON struct {
	Title                 string                    `json:"title"`
	CoverURL              string                    `json:"coverUrl"`
	ParticipantCount      int                       `json:"participantCount"`
	ResultsUniqueChoice   []QuestionResultsJSON     `json:"resultsUniqueChoice"`
	ResultsMultipleChoice []QuestionResultsJSON     `json:"resultsMultipleChoice"`
	ResultsOpen           []QuestionOpenResultsJSON `json:"resultsOpen"`
}

// JavaName is the XML root element.
func (ConsultationResultsJSON) JavaName() string { return "ConsultationResultsJson" }

// QuestionResultsJSON is QuestionResultsJson.
type QuestionResultsJSON struct {
	QuestionID    string              `json:"questionId"`
	QuestionTitle string              `json:"questionTitle"`
	Order         int                 `json:"order"`
	SeenRatio     int                 `json:"seenRatio"`
	Responses     []ChoiceResultsJSON `json:"responses"`
}

// QuestionOpenResultsJSON is QuestionOpenResultsJson.
type QuestionOpenResultsJSON struct {
	QuestionID    string `json:"questionId"`
	QuestionTitle string `json:"questionTitle"`
	Order         int    `json:"order"`
}

// ChoiceResultsJSON is ChoiceResultsJson.
type ChoiceResultsJSON struct {
	ChoiceID string `json:"choiceId"`
	Label    string `json:"label"`
	Ratio    int    `json:"ratio"`
}

// ToResultsJSON is ConsultationResultJsonMapper.toJson. The ratios are
// `(ratio * 100).roundToInt()` (Math.round, saturating at the Int bounds; NaN cannot
// happen: the use case turns it into 0.0).
func ToResultsJSON(domain *ConsultationResults) *ConsultationResultsJSON {
	out := &ConsultationResultsJSON{
		Title:            domain.Consultation.Title,
		CoverURL:         domain.Consultation.CoverURL,
		ParticipantCount: domain.ParticipantCount,
	}
	out.ResultsUniqueChoice = []QuestionResultsJSON{}
	out.ResultsMultipleChoice = []QuestionResultsJSON{}
	for _, r := range domain.ResultsWithChoices {
		switch r.Question.Kind {
		case consultation.KindUniqueChoice, consultation.KindConditional:
			out.ResultsUniqueChoice = append(out.ResultsUniqueChoice, toQuestionResultsJSON(r))
		case consultation.KindMultipleChoices:
			out.ResultsMultipleChoice = append(out.ResultsMultipleChoice, toQuestionResultsJSON(r))
		}
	}
	out.ResultsOpen = make([]QuestionOpenResultsJSON, len(domain.OpenQuestions))
	for i, q := range domain.OpenQuestions {
		out.ResultsOpen[i] = QuestionOpenResultsJSON{QuestionID: q.ID, QuestionTitle: q.Title, Order: q.Order}
	}
	return out
}

func toQuestionResultsJSON(r QuestionResults) QuestionResultsJSON {
	responses := make([]ChoiceResultsJSON, len(r.Responses))
	for i, c := range r.Responses {
		responses[i] = ChoiceResultsJSON{ChoiceID: c.Choix.ID, Label: c.Choix.Label, Ratio: javacompat.KotlinRoundToInt(c.Ratio * 100)}
	}
	return QuestionResultsJSON{
		QuestionID: r.Question.ID, QuestionTitle: r.Question.Title, Order: r.Question.Order,
		SeenRatio: javacompat.KotlinRoundToInt(r.SeenRatio * 100), Responses: responses,
	}
}

// response is ResponseEntity.ok().body(mapper.toJson(results)): the JSON of a cached value is
// encoded once and shared by every request (the XML is encoded per request).
func (r *ConsultationResults) response() *httpx.Response {
	r.once.Do(func() {
		r.dto = ToResultsJSON(r)
		r.json = jsonjava.Marshal(r.dto)
	})
	resp := httpx.OK(r.dto)
	resp.PrecomputedJSON = r.json
	return resp
}
