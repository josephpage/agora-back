// Package tasks implements the custom commands run by cron.json
// (--run-custom-command=dailyTasks|weeklyTasks|acmeCertificateRenewalTasks),
// the start-up feature flag log (FeatureFlagsLogger) and the custom command
// parsing of AgoraCustomCommandHelper. Parsing lives in command.go.
package tasks

import (
	"context"

	"agora/internal/app"
)

// Handler is CustomCommandHandler.handleTask(arguments).
type Handler func(ctx context.Context, a *app.App, args map[string]string) error

// Handlers maps command names to handlers (AgoraCustomCommandHandler.getHandler).
var Handlers = map[string]Handler{
	"dailyTasks":                  func(ctx context.Context, a *app.App, args map[string]string) error { return Daily(ctx, a) },
	"weeklyTasks":                 func(ctx context.Context, a *app.App, args map[string]string) error { return Weekly(ctx, a, args) },
	"acmeCertificateRenewalTasks": func(ctx context.Context, a *app.App, args map[string]string) error { return AcmeRenewal(ctx, a) },
}

// AcmeRenewal is AcmeCertificateRenewalTasksHandler.handleTask. TODO(S11).
var AcmeRenewal = func(ctx context.Context, a *app.App) error { return nil }
