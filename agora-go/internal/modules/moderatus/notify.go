package moderatus

import (
	"context"

	"agora/internal/fcm"
	"agora/internal/javacompat"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// NotificationResult is usecase/notification/repository/NotificationResult.
type NotificationResult int

// The NotificationResult constants.
const (
	NotificationSuccess NotificationResult = iota
	NotificationFailure
)

// QagNotifier is the part of SendQagNotificationUseCase the moderation uses: the
// notification of the author of a QaG. That use case belongs to the notifications
// slice (S8): Get plugs it in (s8Notifier). The port below (qagNotifier, same messages,
// same notifications row, same FCM message) is only used by the unit tests of this
// package and is kept equal to S8 by the oracle tests of both slices.
type QagNotifier interface {
	SendNotificationQagRejected(ctx context.Context, qagID string) (NotificationResult, error)
	SendNotificationQagAccepted(ctx context.Context, qagID string) (NotificationResult, error)
	SendNotificationQagAcceptedAfterReject(ctx context.Context, qagID string) (NotificationResult, error)
}

// NotificationMessage is NotificationMessageDTO.
type NotificationMessage struct{ Title, Message string }

// The messages of NotificationMessageRepositoryImpl (the accepted ones keep the order of the
// Kotlin setOf: one is drawn at random by Random.Default).
const (
	qagRejectedTitle   = "Question hors Charte."
	qagRejectedMessage = "Malheureusement, votre question a du être modérée."

	qagAcceptedAfterRejectTitle   = "Bonne nouvelle !"
	qagAcceptedAfterRejectMessage = "Votre question a été ré-évaluée et correspond à la charte."
)

// QagAcceptedNotifications is QAG_ACCEPTED_NOTIFICATIONS.
var QagAcceptedNotifications = []NotificationMessage{
	{"Et si vous partagiez votre question ?", "Proposez à vos amis de la soutenir pour avoir plus de chance d’obtenir une réponse !"},
	{"✅ Question publiée", "Votre question est désormais proposée à tous les utilisateurs sur Agora."},
	{"🙏 Merci pour votre question !", "Partagez-la à vos amis pour la faire monter dans le classement."},
	{"👋 Votre question est en ligne", "Faites-la connaître pour obtenir une réponse."},
	{"🗓 Votre question sur Agora", "Elle est publiée. Vous avez jusqu’à lundi pour la faire gagner !"},
	{"🥳 Votre question est en ligne", "Multipliez vos chances de la faire gagner en la partageant."},
	{"✅ Question publiée !", "La réponse pourrait intéresser vos proches ? Partagez-leur pour augmenter vos chances."},
	{"🥁 Question publiée avec succès", "Sera-t-elle en tête lundi à 10h ? Proposez à vos amis de la soutenir."},
}

// typeDetailsQag is TypeNotification.DETAILS_QAG: its name goes in the FCM data, its ordinal
// (the entity has no @Enumerated) in the notifications.type column.
const (
	typeDetailsQagName    = "DETAILS_QAG"
	typeDetailsQagOrdinal = "2"
)

// QagIDInconnuError is QagIdInconnuException (the QaG vanished between two reads).
type QagIDInconnuError struct{ QagID string }

func (e *QagIDInconnuError) Error() string { return "QagIdInconnuException: " + e.QagID }

type notifQagReader interface {
	GetQagInfo(ctx context.Context, qagID string) (*qag.QagInfo, error)
}

type notifUserReader interface {
	FindUser(ctx context.Context, userID string) (*users.UserInfo, error)
}

// fcmSender is the part of fcm.Sender used here.
type fcmSender interface {
	SendEachForMulticast(ctx context.Context, m fcm.Message) (fcm.BatchResponse, error)
}

// notificationInserter is NotificationRepositoryImpl.insertNotifications.
type notificationInserter interface {
	InsertNotification(ctx context.Context, title, description, typeOrdinal, userID string) error
}

// qagNotifier is SendQagNotificationUseCase with NotificationMessageRepositoryImpl,
// NotificationSendingRepositoryImpl and NotificationRepositoryImpl behind it.
type qagNotifier struct {
	qags    notifQagReader
	users   notifUserReader
	sender  func() fcmSender // nil sender = FirebaseApp not initialised
	notifs  notificationInserter
	pick    func(n int) int // Set.random()
	runSend func(func())    // NotificationSendingRepositoryImpl schedules the batch on a pool thread
	logf    func(format string, args ...any)
}

func (n *qagNotifier) SendNotificationQagRejected(ctx context.Context, qagID string) (NotificationResult, error) {
	return n.send(ctx, qagID, qagRejectedTitle, qagRejectedMessage)
}

func (n *qagNotifier) SendNotificationQagAccepted(ctx context.Context, qagID string) (NotificationResult, error) {
	m := QagAcceptedNotifications[n.pick(len(QagAcceptedNotifications))]
	return n.send(ctx, qagID, m.Title, m.Message)
}

func (n *qagNotifier) SendNotificationQagAcceptedAfterReject(ctx context.Context, qagID string) (NotificationResult, error) {
	return n.send(ctx, qagID, qagAcceptedAfterRejectTitle, qagAcceptedAfterRejectMessage)
}

// send is SendQagNotificationUseCase.sendNotification. The QaG is read once (Kotlin reads it
// twice, the second read only fetches the author).
func (n *qagNotifier) send(ctx context.Context, qagID, title, description string) (NotificationResult, error) {
	info, err := n.qags.GetQagInfo(ctx, qagID)
	if err != nil {
		return NotificationFailure, err
	}
	if info == nil {
		return NotificationFailure, &QagIDInconnuError{QagID: qagID}
	}
	userID := info.UserID
	user, err := n.users.FindUser(ctx, userID)
	if err != nil {
		return NotificationFailure, err
	}
	if user == nil {
		// fcmToken == null
		return NotificationFailure, nil
	}

	result := n.sendGenericMultiNotification(title, description, []string{user.FCMToken}, qagID)

	if err := n.notifs.InsertNotification(ctx, title, description, typeDetailsQagOrdinal, userID); err != nil {
		return NotificationFailure, err
	}
	return result, nil
}

// sendGenericMultiNotification is NotificationSendingRepositoryImpl.sendGenericMultiNotification
// for one token list of DETAILS_QAG: blank tokens dropped, duplicates dropped, chunks of 300, one
// FCM multicast per chunk (the chunk n is scheduled n minutes later), always SUCCESS once scheduled.
func (n *qagNotifier) sendGenericMultiNotification(title, description string, tokens []string, pageArgument string) NotificationResult {
	n.logf("📩 Sending multi-notification: %s", title)
	seen := map[string]bool{}
	var distinct []string
	for _, t := range tokens {
		if javacompat.KotlinIsBlank(t) || seen[t] {
			continue
		}
		seen[t] = true
		distinct = append(distinct, t)
	}
	const maxSimultaneousNotifications = 300
	for i := 0; i < len(distinct); i += maxSimultaneousNotifications {
		chunk := distinct[i:min(i+maxSimultaneousNotifications, len(distinct))]
		msg := fcm.Message{
			Tokens: chunk,
			Title:  title,
			Body:   description,
			Data:   map[string]string{"type": typeDetailsQagName, "pageArgument": pageArgument},
		}
		batchIndex, batchCount := i/maxSimultaneousNotifications, (len(distinct)+maxSimultaneousNotifications-1)/maxSimultaneousNotifications
		n.runSend(func() { n.sendBatch(msg, batchIndex, batchCount) })
	}
	return NotificationSuccess
}

// sendBatch is SendNotificationTask.run.
func (n *qagNotifier) sendBatch(msg fcm.Message, batchIndex, batchCount int) {
	s := n.sender()
	if s == nil {
		n.logf("⚠️ Send multi-notification error on batch n°%d/%d: %v", batchIndex+1, batchCount, fcm.ErrNotInitialized)
		return
	}
	resp, err := s.SendEachForMulticast(context.Background(), msg)
	if err != nil {
		n.logf("⚠️ Send multi-notification error on batch n°%d/%d %v", batchIndex+1, batchCount, err)
		return
	}
	n.logf("📩 Send multi-notification n°%d/%d results: %d successes, %d failures", batchIndex+1, batchCount, resp.SuccessCount, resp.FailureCount)
}

