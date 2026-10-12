package tasks

import (
	"context"

	"agora/internal/app"
	"agora/internal/modules/consultationlist"
	"agora/internal/modules/consultationresponse"
	"agora/internal/modules/login"
)

// Daily is DailyTasksHandler.handleTask, in the Kotlin order: the consultation
// caches are cleared (ConsultationCacheClearUseCase, through the consultation
// lists so that the finished pages are part of it), the consultations that ended
// 14 days ago are aggregated, the users with suspicious activity are flagged, and
// the supports of the banned users are deleted. A failing step stops the task,
// like the Kotlin exception. The TODOs of the Kotlin handler are not implemented
// there either.
func Daily(ctx context.Context, a *app.App) error {
	consultationlist.Get(a).ClearConsultationCaches(ctx)
	if err := consultationresponse.Get(a).AggregateConsultations(ctx); err != nil {
		return err
	}
	if _, err := login.Get(a).FlagSuspiciousUsers(ctx); err != nil {
		return err
	}
	return DeleteSuspiciousSupports(ctx, a)
}
