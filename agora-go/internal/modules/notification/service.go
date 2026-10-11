// Package notification ports the notifications of the application: the paginated list of a user
// (GET /notifications/paginated/{pageNumber}), the administration routes that send a notification
// to one user, to everybody, to a department or to the users of a consultation, and the QaG
// moderation notifications used by the Moderatus slice.
//
// A notification is (1) cut in batches of 300 FCM tokens, scheduled one batch per minute on a
// single-threaded in-memory scheduler (the HTTP request does not wait for the sends), then (2) written
// as one row per user in `notifications`, with set-based statements (never one round trip per user).
package notification

import (
	"context"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// Service gathers the repositories and the use cases of the slice.
type Service struct {
	a        *app.App
	Repo     *Repository
	Sender   *Sender
	Messages Messages
	selector *userSelector
}

// Get returns the App-wide service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "notification", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	return &Service{a: a, Repo: newRepository(a), Sender: newSender(a), selector: &userSelector{a: a}}
}

// dispatch is the common end of the use cases: sendGenericMultiNotification, then
// insertNotifications. Kotlin schedules the FCM batches first, so a failing insert does not cancel them.
func (s *Service) dispatch(ctx context.Context, title, description string, typ TypeNotification, pageArgument *string, aud *audience) (Result, error) {
	aud.tokens.release()
	s.Sender.sendChunks(title, description, aud.tokens, typ, pageArgument)
	if err := s.Repo.insertIDs(ctx, title, description, typ, aud.ids); err != nil {
		return Success, err
	}
	return Success, nil
}

// SendUser is SendUserNotificationUseCase.execute.
func (s *Service) SendUser(ctx context.Context, title, description, userID string) (Result, error) {
	user, err := users.Get(s.a).FindUser(ctx, userID)
	if err != nil {
		return Failure, err
	}
	if user == nil {
		return Failure, &UserIDInconnuError{UserID: userID}
	}
	if javacompat.KotlinIsBlank(user.FCMToken) {
		return Failure, &FcmTokenVideError{}
	}
	s.Sender.SendGenericMultiNotification(title, description, []string{user.FCMToken}, ReponseSupport, nil)
	if err := s.Repo.InsertNotifications(ctx, title, description, ReponseSupport, []string{userID}); err != nil {
		return Success, err
	}
	return Success, nil
}

// SendUsers is SendUsersNotificationUseCase.execute: every user of the table.
func (s *Service) SendUsers(ctx context.Context, title, description string, pageArgument *string, typ TypeNotification) (Result, error) {
	aud, err := s.selector.allUsers(ctx)
	if err != nil {
		return Failure, err
	}
	return s.dispatch(ctx, title, description, typ, pageArgument, aud)
}

// SendDepartement is SendDepartementNotificationUseCase.execute: the users living in the department
// followed by the ones interested in it.
func (s *Service) SendDepartement(ctx context.Context, departement *domain.Department, title, description string, pageArgument *string, typ TypeNotification) (Result, error) {
	aud, err := s.selector.departementUsers(ctx, departement)
	if err != nil {
		return Failure, err
	}
	return s.dispatch(ctx, title, description, typ, pageArgument, aud)
}

// SendConsultationUpdate is SendConsultationNotificationUseCase.sendConsultationUpdateNotification:
// the users who answered the consultation. An unknown consultation is a ConsultationNotFoundError.
func (s *Service) SendConsultationUpdate(ctx context.Context, title, description, consultationID string) (Result, error) {
	if !consultation.Get(s.a).Info.IsConsultationExists(ctx, consultationID) {
		return Failure, &consultation.ConsultationNotFoundError{ConsultationID: consultationID}
	}
	aud, err := s.selector.answeredUsers(ctx, consultationID)
	if err != nil {
		return Failure, err
	}
	return s.dispatch(ctx, title, description, DetailsConsultation, &consultationID, aud)
}

// SendConsultationDeadline is SendConsultationNotificationUseCase.sendConsultationDeadlineNotification:
// one user per FCM token among the users who did not answer.
func (s *Service) SendConsultationDeadline(ctx context.Context, title, description, consultationID string) (Result, error) {
	if !consultation.Get(s.a).Info.IsConsultationExists(ctx, consultationID) {
		return Failure, &consultation.ConsultationNotFoundError{ConsultationID: consultationID}
	}
	aud, err := s.selector.notAnsweredUsers(ctx, consultationID)
	if err != nil {
		return Failure, err
	}
	return s.dispatch(ctx, title, description, DetailsConsultation, &consultationID, aud)
}

// SendNotificationQagRejected is SendQagNotificationUseCase.sendNotificationQagRejected.
func (s *Service) SendNotificationQagRejected(ctx context.Context, qagID string) (Result, error) {
	m := s.Messages.GetQagRejected()
	return s.sendQag(ctx, qagID, m.Title, m.Message)
}

// SendNotificationQagAccepted is SendQagNotificationUseCase.sendNotificationQagAccepted (a message
// drawn at random among the eight accepted texts).
func (s *Service) SendNotificationQagAccepted(ctx context.Context, qagID string) (Result, error) {
	m := s.Messages.RandomAccepted()
	return s.sendQag(ctx, qagID, m.Title, m.Message)
}

// SendNotificationQagAcceptedAfterReject is SendQagNotificationUseCase.sendNotificationQagAcceptedAfterReject.
func (s *Service) SendNotificationQagAcceptedAfterReject(ctx context.Context, qagID string) (Result, error) {
	m := s.Messages.GetQagAcceptedAfterReject()
	return s.sendQag(ctx, qagID, m.Title, m.Message)
}

// sendQag is SendQagNotificationUseCase.sendNotification: to the author of the QaG only.
func (s *Service) sendQag(ctx context.Context, qagID, title, description string) (Result, error) {
	qags := qag.Get(s.a).Info
	info, err := qags.GetQagInfo(ctx, qagID)
	if err != nil {
		return Failure, err
	}
	if info == nil {
		return Failure, &QagIDInconnuError{QagID: qagID}
	}
	// getQagAuthorFcmToken: the QaG is read again, then its author
	authorInfo, err := qags.GetQagInfo(ctx, qagID)
	if err != nil {
		return Failure, err
	}
	if authorInfo == nil {
		return Failure, nil
	}
	userID := authorInfo.UserID
	user, err := users.Get(s.a).FindUser(ctx, userID)
	if err != nil {
		return Failure, err
	}
	if user == nil {
		return Failure, nil
	}
	res := s.Sender.SendGenericMultiNotification(title, description, []string{user.FCMToken}, DetailsQag, &qagID)
	if err := s.Repo.InsertNotifications(ctx, title, description, DetailsQag, []string{userID}); err != nil {
		return res, err
	}
	return res, nil
}

// GetNotificationPaginated is NotificationPaginatedUseCase.getNotificationPaginated.
func (s *Service) GetNotificationPaginated(ctx context.Context, userID string, pageNumber int) (*Page, error) {
	return s.Repo.GetNotificationPaginated(ctx, userID, pageNumber)
}

// DeleteUsersNotifications is NotificationRepository.deleteUsersNotifications (DeleteUsersUseCase).
func (s *Service) DeleteUsersNotifications(ctx context.Context, userIDs []string) error {
	return s.Repo.DeleteUsersNotifications(ctx, userIDs)
}
