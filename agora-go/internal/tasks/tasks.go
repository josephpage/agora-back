// Package tasks implements the custom commands run by cron.json
// (--run-custom-command=dailyTasks|weeklyTasks|acmeCertificateRenewalTasks).
package tasks

import (
	"context"

	"agora/internal/app"
)

// Handler is CustomCommandHandler.handleTask(arguments).
type Handler func(ctx context.Context, a *app.App, args map[string]string) error

// Handlers maps command names to handlers (AgoraCustomCommandHandler.getHandler).
// Slices fill these in (S10 daily/weekly, S11 ACME).
var Handlers = map[string]Handler{
	"dailyTasks":                  func(ctx context.Context, a *app.App, args map[string]string) error { return Daily(ctx, a) },
	"weeklyTasks":                 func(ctx context.Context, a *app.App, args map[string]string) error { return Weekly(ctx, a, args) },
	"acmeCertificateRenewalTasks": func(ctx context.Context, a *app.App, args map[string]string) error { return AcmeRenewal(ctx, a) },
}

// Daily is DailyTasksHandler.handleTask. TODO(S10).
var Daily = func(ctx context.Context, a *app.App) error { return nil }

// Weekly is WeeklyTasksHandler.handleTask. TODO(S10).
var Weekly = func(ctx context.Context, a *app.App, args map[string]string) error { return nil }

// AcmeRenewal is AcmeCertificateRenewalTasksHandler.handleTask. TODO(S11).
var AcmeRenewal = func(ctx context.Context, a *app.App) error { return nil }
