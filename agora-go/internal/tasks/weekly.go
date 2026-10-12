package tasks

import (
	"context"
	"errors"

	"agora/internal/app"
	"agora/internal/modules/themehebdo"
)

// QagLifecycle is the QaG lifecycle of the moderation slice (S7, module
// internal/modules/moderatus): SelectMostPopularQagUseCase.putMostPopularQagInSelectedStatus,
// ArchiveOldQagUseCase.archiveOldQag and AnonymizeOldQagUseCase.anonymizeOldQag.
// The weekly task only orchestrates them.
type QagLifecycle interface {
	PutMostPopularQagInSelectedStatus(ctx context.Context) error
	ArchiveOldQag(ctx context.Context) error
	AnonymizeOldQag(ctx context.Context) error
}

// qagLifecycleFactory is set by RegisterQagLifecycle.
var qagLifecycleFactory func(a *app.App) QagLifecycle

// RegisterQagLifecycle registers the implementation of QagLifecycle used by the
// weekly task. It must be called during package initialization (an init function
// of the module that implements the lifecycle, or of internal/modules/all, which
// both the web server and the cron command import): the cron process never builds
// the routes. When nothing is registered, the weekly task fails with an ERROR log
// and an error: it never skips the QaG steps silently.
func RegisterQagLifecycle(factory func(a *app.App) QagLifecycle) {
	qagLifecycleFactory = factory
}

// errNoQagLifecycle is the failure of the weekly task when the QaG lifecycle is not registered.
var errNoQagLifecycle = errors.New("weeklyTasks: no QaG lifecycle is registered (tasks.RegisterQagLifecycle); the weekly task cannot run")

// Weekly is WeeklyTasksHandler.handleTask. force_question_selection == "true"
// (the exact string) skips the theme hebdo transition check. The QaG selection
// and archive run only in a transition (or when forced); the anonymization of
// the old QaGs always runs.
func Weekly(ctx context.Context, a *app.App, args map[string]string) error {
	var lifecycle QagLifecycle
	if factory := qagLifecycleFactory; factory != nil {
		lifecycle = factory(a)
	}
	if lifecycle == nil {
		a.Log.Error(errNoQagLifecycle.Error())
		return errNoQagLifecycle
	}
	return runWeekly(ctx, weeklyDeps{
		inTransition: func(ctx context.Context) (bool, error) { return themehebdo.Get(a).IsInTransition(ctx) },
		lifecycle:    lifecycle,
	}, args)
}

// weeklyDeps are the collaborators of the weekly task (faked in the unit tests).
type weeklyDeps struct {
	inTransition func(ctx context.Context) (bool, error) // IsThemeHebdoTransitionUseCase.isInTransition
	lifecycle    QagLifecycle
}

func runWeekly(ctx context.Context, d weeklyDeps, args map[string]string) error {
	ignoreTransition := args["force_question_selection"] == "true"
	transition := ignoreTransition
	if !ignoreTransition {
		inTransition, err := d.inTransition(ctx)
		if err != nil {
			return err
		}
		transition = inTransition
	}
	if transition {
		if err := d.lifecycle.PutMostPopularQagInSelectedStatus(ctx); err != nil {
			return err
		}
		if err := d.lifecycle.ArchiveOldQag(ctx); err != nil {
			return err
		}
	}
	return d.lifecycle.AnonymizeOldQag(ctx)
}
