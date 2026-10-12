// Package all registers every module's routes, in an order approximating
// Spring's handler registration order (only visible in Allow headers).
package all

import (
	"context"

	"agora/internal/app"
	"agora/internal/modules/acme"
	"agora/internal/modules/admin"
	"agora/internal/modules/apidocs"
	"agora/internal/modules/concertation"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultationlist"
	"agora/internal/modules/consultationresponse"
	"agora/internal/modules/content"
	"agora/internal/modules/login"
	"agora/internal/modules/moderatus"
	"agora/internal/modules/notification"
	"agora/internal/modules/profile"
	"agora/internal/modules/qag"
	"agora/internal/modules/qaglist"
	"agora/internal/modules/referentiel"
	"agora/internal/modules/responseqag"
	"agora/internal/modules/thematique"
	"agora/internal/modules/themehebdo"
	"agora/internal/tasks"
)

// Routes registers all HTTP routes.
func Routes(a *app.App) {
	acme.Routes(a)
	admin.Routes(a)
	content.Routes(a)
	concertation.Routes(a)
	consultation.Routes(a)
	consultationlist.Routes(a)
	consultationresponse.Routes(a)
	login.Routes(a)
	moderatus.Routes(a)
	notification.Routes(a)
	profile.Routes(a)
	qag.Routes(a)
	qaglist.Routes(a)
	referentiel.Routes(a)
	responseqag.Routes(a)
	thematique.Routes(a)
	themehebdo.Routes(a)
	apidocs.Routes(a)
}

// qagLifecycle is the weekly-task view of the S7 use cases (WeeklyTasksHandler ignores
// their results, only an error stops the task).
type qagLifecycle struct{ s *moderatus.Service }

func (l qagLifecycle) PutMostPopularQagInSelectedStatus(ctx context.Context) error {
	return l.s.Select.PutMostPopularQagInSelectedStatus(ctx)
}

func (l qagLifecycle) ArchiveOldQag(ctx context.Context) error {
	_, err := l.s.Archive.ArchiveOldQag(ctx)
	return err
}

func (l qagLifecycle) AnonymizeOldQag(ctx context.Context) error {
	_, err := l.s.Anonymize.AnonymizeOldQag(ctx)
	return err
}

// The cron process (--run-custom-command=weeklyTasks) never builds the routes:
// the registration happens when this package is imported (cmd/agora does).
func init() {
	tasks.RegisterQagLifecycle(func(a *app.App) tasks.QagLifecycle { return qagLifecycle{moderatus.Get(a)} })
}
