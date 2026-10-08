package responseqag

import (
	"time"

	"agora/internal/common"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
)

// ---------------------------------------------------------------------------
// domain: IncomingResponsePreview.kt, ResponseQagPreview.kt
// ---------------------------------------------------------------------------

// IncomingResponsePreview is domain/IncomingResponsePreview. The Mondays are the local
// midnight of the LocalDate.
type IncomingResponsePreview struct {
	ID                 string
	Thematique         thematique.Thematique
	Title              string
	SupportCount       int
	DateLundiPrecedent time.Time
	DateLundiSuivant   time.Time
	Order              int
}

// ResponseQagPreview is domain/ResponseQagPreview.
type ResponseQagPreview struct {
	QagID             string
	Thematique        thematique.Thematique
	Title             string
	Author            string
	AuthorPortraitURL string
	ResponseDate      time.Time
	Order             int
}

// ResponseQagPreviewWithoutOrder is domain/ResponseQagPreviewWithoutOrder.
type ResponseQagPreviewWithoutOrder struct {
	QagID             string
	Thematique        thematique.Thematique
	Title             string
	Author            string
	AuthorPortraitURL string
	AuthorFunction    *string
	ResponseDate      time.Time
	ResponseText      *string
	Username          string
}

// ResponseQagPreviewList is the data class of the same name.
type ResponseQagPreviewList struct {
	IncomingResponses []IncomingResponsePreview
	Responses         []ResponseQagPreview
}

// ResponseQagPaginatedList is the data class of the same name.
type ResponseQagPaginatedList struct {
	ResponsesQag  []ResponseQagPreviewWithoutOrder
	MaxPageNumber int
}

// ---------------------------------------------------------------------------
// infrastructure/qagHome/QagPreviewsJson.kt, infrastructure/responseQagPaginated/ResponseQagPaginatedJson.kt
// ---------------------------------------------------------------------------

// QagResponsesJSON is QagResponsesJson (GET /qags/responses).
type QagResponsesJSON struct {
	IncomingResponses []IncomingResponseQagPreviewJSON `json:"incomingResponses"`
	ResponsesList     []ResponseQagPreviewJSON         `json:"responses"`
}

// JavaName is the XML root element.
func (QagResponsesJSON) JavaName() string { return "QagResponsesJson" }

// IncomingResponseQagPreviewJSON is IncomingResponseQagPreviewJson.
type IncomingResponseQagPreviewJSON struct {
	QagID              string                        `json:"qagId"`
	Thematique         thematique.ThematiqueNoIDJSON `json:"thematique"`
	Title              string                        `json:"title"`
	Support            qag.SupportQagJSON            `json:"support"`
	Order              int                           `json:"order"`
	PreviousMondayDate string                        `json:"previousMondayDate"`
	NextMondayDate     string                        `json:"nextMondayDate"`
}

// ResponseQagPreviewJSON is ResponseQagPreviewJson.
type ResponseQagPreviewJSON struct {
	QagID             string                        `json:"qagId"`
	Thematique        thematique.ThematiqueNoIDJSON `json:"thematique"`
	Title             string                        `json:"title"`
	Author            string                        `json:"author"`
	AuthorPortraitURL string                        `json:"authorPortraitUrl"`
	ResponseDate      string                        `json:"responseDate"`
	Order             int                           `json:"order"`
}

// ResponseQagPreviewWithoutOrderJSON is ResponseQagPreviewWithoutOrderJson.
type ResponseQagPreviewWithoutOrderJSON struct {
	QagID             string                        `json:"qagId"`
	Thematique        thematique.ThematiqueNoIDJSON `json:"thematique"`
	Title             string                        `json:"title"`
	Author            string                        `json:"author"`
	AuthorPortraitURL string                        `json:"authorPortraitUrl"`
	AuthorFunction    string                        `json:"authorFunction"`
	ResponseDate      string                        `json:"responseDate"`
	ResponseTexte     string                        `json:"responseTexte"`
	Username          string                        `json:"username"`
}

// ResponseQagPaginatedJSON is ResponseQagPaginatedJson (GET /qags/responses/{pageNumber}).
type ResponseQagPaginatedJSON struct {
	MaxPageNumber int                                  `json:"maxPageNumber"`
	Responses     []ResponseQagPreviewWithoutOrderJSON `json:"responses"`
}

// JavaName is the XML root element.
func (ResponseQagPaginatedJSON) JavaName() string { return "ResponseQagPaginatedJson" }

// ToResponsesJSON is QagHomeJsonMapper.toResponsesJson.
func ToResponsesJSON(l ResponseQagPreviewList) QagResponsesJSON {
	j := QagResponsesJSON{
		IncomingResponses: make([]IncomingResponseQagPreviewJSON, len(l.IncomingResponses)),
		ResponsesList:     make([]ResponseQagPreviewJSON, len(l.Responses)),
	}
	for i, q := range l.IncomingResponses {
		j.IncomingResponses[i] = IncomingResponseQagPreviewJSON{
			QagID:              q.ID,
			Thematique:         thematique.ToNoIDJSON(q.Thematique),
			Title:              q.Title,
			Support:            qag.ToSupportQagJSON(q.SupportCount, true),
			Order:              q.Order,
			PreviousMondayDate: common.FormatDate(q.DateLundiPrecedent),
			NextMondayDate:     common.FormatDate(q.DateLundiSuivant),
		}
	}
	for i, r := range l.Responses {
		j.ResponsesList[i] = ResponseQagPreviewJSON{
			QagID:             r.QagID,
			Thematique:        thematique.ToNoIDJSON(r.Thematique),
			Title:             r.Title,
			Author:            r.Author,
			AuthorPortraitURL: r.AuthorPortraitURL,
			ResponseDate:      common.FormatDate(r.ResponseDate),
			Order:             r.Order,
		}
	}
	return j
}

// ToPaginatedJSON is ResponseQagPaginatedJsonMapper.toJson.
func ToPaginatedJSON(l ResponseQagPaginatedList) ResponseQagPaginatedJSON {
	j := ResponseQagPaginatedJSON{MaxPageNumber: l.MaxPageNumber, Responses: make([]ResponseQagPreviewWithoutOrderJSON, len(l.ResponsesQag))}
	for i, d := range l.ResponsesQag {
		fn, text := "", ""
		if d.AuthorFunction != nil {
			fn = *d.AuthorFunction
		}
		if d.ResponseText != nil {
			text = *d.ResponseText
		}
		j.Responses[i] = ResponseQagPreviewWithoutOrderJSON{
			QagID:             d.QagID,
			Thematique:        thematique.ToNoIDJSON(d.Thematique),
			Title:             d.Title,
			Author:            d.Author,
			AuthorPortraitURL: d.AuthorPortraitURL,
			AuthorFunction:    fn,
			ResponseDate:      common.FormatDate(d.ResponseDate),
			ResponseTexte:     text,
			Username:          d.Username,
		}
	}
	return j
}
