package notification

import "agora/internal/common"

// NotificationJSON is NotificationJson (no @JsonInclude: every field is written).
type NotificationJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Date        string `json:"date"`
}

// JavaName is the XML element name.
func (NotificationJSON) JavaName() string { return "NotificationJson" }

// PaginatedJSON is NotificationPaginatedJson.
type PaginatedJSON struct {
	HasMoreNotifications bool               `json:"hasMoreNotifications"`
	Notifications        []NotificationJSON `json:"notifications"`
}

// JavaName is the XML root element.
func (PaginatedJSON) JavaName() string { return "NotificationPaginatedJson" }

// The category shown in the app for each type (NotificationPaginatedJsonMapper).
const (
	qagNotificationType          = "Questions citoyennes"
	consultationNotificationType = "Consultations"
	reponseSupportType           = "Réponse du support"
)

// ToJSON is NotificationPaginatedJsonMapper.toJson.
func ToJSON(p *Page) PaginatedJSON {
	out := PaginatedJSON{HasMoreNotifications: p.HasMoreNotifications, Notifications: make([]NotificationJSON, len(p.Notifications))}
	for i, n := range p.Notifications {
		var typ string
		switch n.Type {
		case AllReponsesQags, HomeQags, DetailsQag:
			typ = qagNotificationType
		case HomeConsultations, DetailsConsultation:
			typ = consultationNotificationType
		case ReponseSupport:
			typ = reponseSupportType
		}
		out.Notifications[i] = NotificationJSON{Title: n.Title, Description: n.Description, Type: typ, Date: common.FormatDate(n.Date)}
	}
	return out
}
