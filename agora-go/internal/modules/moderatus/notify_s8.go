package moderatus

import (
	"context"

	"agora/internal/modules/notification"
)

// s8Notifier plugs the notifications slice (S8, SendQagNotificationUseCase) into the
// moderation: the notification row, the FCM batches (through the single-worker scheduler of
// S8) and the random accepted message all come from one implementation.
type s8Notifier struct{ n *notification.Service }

func toNotificationResult(r notification.Result) NotificationResult {
	if r == notification.Success {
		return NotificationSuccess
	}
	return NotificationFailure
}

func (s s8Notifier) SendNotificationQagRejected(ctx context.Context, qagID string) (NotificationResult, error) {
	r, err := s.n.SendNotificationQagRejected(ctx, qagID)
	return toNotificationResult(r), err
}

func (s s8Notifier) SendNotificationQagAccepted(ctx context.Context, qagID string) (NotificationResult, error) {
	r, err := s.n.SendNotificationQagAccepted(ctx, qagID)
	return toNotificationResult(r), err
}

func (s s8Notifier) SendNotificationQagAcceptedAfterReject(ctx context.Context, qagID string) (NotificationResult, error) {
	r, err := s.n.SendNotificationQagAcceptedAfterReject(ctx, qagID)
	return toNotificationResult(r), err
}
