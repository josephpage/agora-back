package concertation

import "agora/internal/modules/thematique"

// ConcertationJSON is ConcertationJson (no @JsonInclude: a missing label is written as null).
type ConcertationJSON struct {
	ID           string                        `json:"id"`
	Title        string                        `json:"title"`
	ImageURL     string                        `json:"imageUrl"`
	ExternalLink string                        `json:"externalLink"`
	Thematique   thematique.ThematiqueNoIDJSON `json:"thematique"`
	UpdateLabel  *string                       `json:"updateLabel"`
	UpdateDate   string                        `json:"updateDate"`
	Territory    string                        `json:"territory"`
}

// JavaName is the XML class name of an element.
func (ConcertationJSON) JavaName() string { return "ConcertationJson" }

// ConcertationListJSON is the body of ResponseEntity<List<ConcertationJson>>: Jackson XML
// names the root element after the declared type, "List".
type ConcertationListJSON []ConcertationJSON

// JavaName is the XML root element.
func (ConcertationListJSON) JavaName() string { return "List" }

// toJSON is ConcertationJsonMapper.toConcertationJson.
func toJSON(list []Concertation) ConcertationListJSON {
	out := make(ConcertationListJSON, len(list))
	for i, c := range list {
		out[i] = ConcertationJSON{
			ID:           c.ID,
			Title:        c.Title,
			ImageURL:     c.ImageURL,
			ExternalLink: c.ExternalLink,
			Thematique:   thematique.ToNoIDJSON(c.Thematique),
			UpdateLabel:  c.UpdateLabel,
			UpdateDate:   c.UpdateDate.Format(),
			Territory:    c.Territory,
		}
	}
	return out
}
