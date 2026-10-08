package acme

import (
	"context"
	"strings"

	"agora/internal/app"
	"agora/internal/tasks"
)

func init() {
	// --run-custom-command=acmeCertificateRenewalTasks
	tasks.AcmeRenewal = func(ctx context.Context, a *app.App) error {
		svc, err := Get(a)
		if err != nil {
			return err
		}
		return svc.RunRenewalTask(ctx)
	}
}

// StubModeEnabled is `@ConditionalOnProperty(name = ["ACME_STUB_MODE"], havingValue = "true")`:
// the exact value "true" ignoring the case.
func StubModeEnabled(a *app.App) bool {
	v, ok := a.Cfg.Lookup("ACME_STUB_MODE")
	return ok && strings.EqualFold(v, "true")
}

// Routes registers this module's HTTP routes on a.Server. /.well-known/acme-challenge/**
// and /stub/** are public, /admin/** needs ADMIN_APIS (httpx security rules).
func Routes(a *app.App) {
	svc, err := Get(a)
	if err != nil {
		// the Kotlin application fails to start with an invalid AcmeConfig
		panic("ACME configuration: " + err.Error())
	}
	a.Server.GET("/.well-known/acme-challenge/{token}", svc.challengeHandler)
	a.Server.POST("/admin/acme/renew-certificate", svc.renewHandler)
	a.Server.POST("/admin/acme/upload-certificate", svc.uploadHandler)
	a.Server.GET("/admin/acme/cloudflare/zone-info", svc.zoneInfoHandler)
	a.Server.GET("/admin/acme/server/directory", svc.directoryHandler)
	if StubModeEnabled(a) {
		svc.registerStubRoutes()
	}
}
