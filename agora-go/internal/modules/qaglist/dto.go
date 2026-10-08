package qaglist

import (
	"agora/internal/common"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
)

// ---------------------------------------------------------------------------
// domain (HeaderQag.kt, TrendingCluster.kt)
// ---------------------------------------------------------------------------

// HeaderQag is domain/HeaderQag.
type HeaderQag struct {
	HeaderID string
	Title    string
	Message  string
}

// TrendingCluster is domain/TrendingCluster.
type TrendingCluster struct {
	ID   string
	Mots []string
}

// ---------------------------------------------------------------------------
// infrastructure/qagHome/QagPreviewsJson.kt, infrastructure/qagPaginated/QagPaginatedJson.kt
// ---------------------------------------------------------------------------

// QagPreviewJSON is QagPreviewJson (no @JsonInclude: nulls would be written).
type QagPreviewJSON struct {
	QagID       string                        `json:"qagId"`
	Thematique  thematique.ThematiqueNoIDJSON `json:"thematique"`
	Title       string                        `json:"title"`
	Description string                        `json:"description"`
	Username    string                        `json:"username"`
	Date        string                        `json:"date"`
	Support     qag.SupportQagJSON            `json:"support"`
	IsAuthor    bool                          `json:"isAuthor"`
	CanShare    bool                          `json:"canShare"`
}

// JavaName is the XML root element.
func (QagPreviewJSON) JavaName() string { return "QagPreviewJson" }

// QagPreviewListJSON is QagPreviewListJson (GET /qags/search).
type QagPreviewListJSON struct {
	Results []QagPreviewJSON `json:"results"`
}

// JavaName is the XML root element.
func (QagPreviewListJSON) JavaName() string { return "QagPreviewListJson" }

// HeaderQagJSON is HeaderQagJson.
type HeaderQagJSON struct {
	HeaderID string `json:"headerId"`
	Title    string `json:"title"`
	Message  string `json:"message"`
}

// JavaName is the XML root element.
func (HeaderQagJSON) JavaName() string { return "HeaderQagJson" }

// QagPaginatedJSONV2 is QagPaginatedJsonV2 (@JsonInclude(NON_NULL): a missing header is not written).
type QagPaginatedJSONV2 struct {
	MaxPageNumber int              `json:"maxPageNumber,omitnull"`
	Header        *HeaderQagJSON   `json:"header,omitnull"`
	Qags          []QagPreviewJSON `json:"qags,omitnull"`
}

// JavaName is the XML root element.
func (QagPaginatedJSONV2) JavaName() string { return "QagPaginatedJsonV2" }

// QagCount is the Int body of GET /qags/count (`ResponseEntity<Int>`: a bare JSON
// number, `<Integer>` in XML).
type QagCount int

// JavaName is the XML root element.
func (QagCount) JavaName() string { return "Integer" }

// ToPreviewJSON is QagPaginatedJsonMapper.toJson(QagPreview) / QagHomeJsonMapper.qagToJson.
func ToPreviewJSON(p qag.QagPreview) QagPreviewJSON {
	return QagPreviewJSON{
		QagID:       p.ID,
		Thematique:  thematique.ToNoIDJSON(p.Thematique),
		Title:       p.Title,
		Description: p.Description,
		Username:    p.Username,
		Date:        common.FormatDate(p.Date),
		Support:     qag.ToSupportQagJSON(p.SupportCount, p.IsSupportedByUser),
		IsAuthor:    p.IsAuthor,
		CanShare:    p.CanShare,
	}
}

// ToPreviewListJSON is QagHomeJsonMapper.toJson(List<QagPreview>).
func ToPreviewListJSON(list []qag.QagPreview) QagPreviewListJSON {
	out := make([]QagPreviewJSON, len(list))
	for i, p := range list {
		out[i] = ToPreviewJSON(p)
	}
	return QagPreviewListJSON{Results: out}
}

// ToPaginatedJSON is QagPaginatedJsonMapper.toJson(QagsAndMaxPageCountV2).
func ToPaginatedJSON(r QagsAndMaxPageCountV2) QagPaginatedJSONV2 {
	j := QagPaginatedJSONV2{MaxPageNumber: r.MaxPageCount, Qags: make([]QagPreviewJSON, len(r.Qags))}
	if r.Header != nil {
		j.Header = &HeaderQagJSON{HeaderID: r.Header.HeaderID, Title: r.Header.Title, Message: r.Header.Message}
	}
	for i, p := range r.Qags {
		j.Qags[i] = ToPreviewJSON(p)
	}
	return j
}
