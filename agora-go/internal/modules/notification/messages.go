package notification

import "math/rand/v2"

// MessageDTO is infrastructure/notification/dto/NotificationMessageDTO.
type MessageDTO struct {
	Title   string
	Message string
}

// Messages is NotificationMessageRepositoryImpl: the fixed texts of the QaG notifications. The strings are
// the Kotlin ones, character for character (non-breaking spaces and typographic apostrophes included).
type Messages struct{}

// qagAcceptedNotifications is QAG_ACCEPTED_NOTIFICATIONS (a Set, in declaration order).
var qagAcceptedNotifications = []MessageDTO{
	{Title: "Et si vous partagiez votre question\u00a0?", Message: "Proposez \u00e0 vos amis de la soutenir pour avoir plus de chance d\u2019obtenir une r\u00e9ponse\u00a0!"},
	{Title: "\u2705 Question publi\u00e9e", Message: "Votre question est d\u00e9sormais propos\u00e9e \u00e0 tous les utilisateurs sur Agora."},
	{Title: "\U0001f64f Merci pour votre question\u00a0!", Message: "Partagez-la \u00e0 vos amis pour la faire monter dans le classement."},
	{Title: "\U0001f44b Votre question est en ligne", Message: "Faites-la conna\u00eetre pour obtenir une r\u00e9ponse."},
	{Title: "\U0001f5d3 Votre question sur Agora", Message: "Elle est publi\u00e9e. Vous avez jusqu\u2019\u00e0 lundi pour la faire gagner\u00a0!"},
	{Title: "\U0001f973 Votre question est en ligne", Message: "Multipliez vos chances de la faire gagner en la partageant."},
	{Title: "\u2705 Question publi\u00e9e\u00a0!", Message: "La r\u00e9ponse pourrait int\u00e9resser vos proches\u00a0? Partagez-leur pour augmenter vos chances."},
	{Title: "\U0001f941 Question publi\u00e9e avec succ\u00e8s", Message: "Sera-t-elle en t\u00eate lundi \u00e0 10h\u00a0? Proposez \u00e0 vos amis de la soutenir."},
}

// GetQagRejected is getQagRejected.
func (Messages) GetQagRejected() MessageDTO {
	return MessageDTO{Title: "Question hors Charte.", Message: "Malheureusement, votre question a du \u00eatre mod\u00e9r\u00e9e."}
}

// FindAllByStatusAccepted is findAllByStatusAccepted.
func (Messages) FindAllByStatusAccepted() []MessageDTO { return qagAcceptedNotifications }

// RandomAccepted is findAllByStatusAccepted().random(): a uniformly drawn element.
func (m Messages) RandomAccepted() MessageDTO {
	all := m.FindAllByStatusAccepted()
	return all[rand.IntN(len(all))]
}

// GetQagAcceptedAfterReject is getQagAcceptedAfterReject.
func (Messages) GetQagAcceptedAfterReject() MessageDTO {
	return MessageDTO{Title: "Bonne nouvelle\u00a0!", Message: "Votre question a \u00e9t\u00e9 r\u00e9-\u00e9valu\u00e9e et correspond \u00e0 la charte."}
}
