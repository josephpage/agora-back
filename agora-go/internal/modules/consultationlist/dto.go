package consultationlist

import (
	"agora/internal/app"
	"agora/internal/modules/consultation"
)

// ConsultationPaginatedJSON is ConsultationPaginatedJson (no @JsonInclude: nothing is null here).
type ConsultationPaginatedJSON struct {
	MaxPageNumber int                                     `json:"maxPageNumber"`
	Consultations []consultation.ConsultationFinishedJSON `json:"consultations"`
}

// JavaName is the XML root element.
func (ConsultationPaginatedJSON) JavaName() string { return "ConsultationPaginatedJson" }

// toJSON is ConsultationPaginatedJsonMapper.toJson: the step and the update label of every
// consultation depend on the time (ConsultationPreviewJsonMapper.toJson reads the clock for each
// of them), so the cached domain lists are rendered at every request.
func toJSON(a *app.App, maxPageNumber int, list []consultation.ConsultationPreviewFinished) *ConsultationPaginatedJSON {
	out := &ConsultationPaginatedJSON{MaxPageNumber: maxPageNumber, Consultations: make([]consultation.ConsultationFinishedJSON, len(list))}
	for i, d := range list {
		out.Consultations[i] = consultation.ToFinishedJSON(d, consultation.FromTime(a.Now()))
	}
	return out
}
