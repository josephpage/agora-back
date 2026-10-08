package responseqag

import (
	"agora/internal/app"
	"agora/internal/javacompat"
)

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	h := &handlers{a: a, svc: Get(a)}
	// QagHomeController
	a.Server.GET("/qags/responses", h.getQagResponses)
	// ResponseQagPaginatedController
	a.Server.GET("/qags/responses/{pageNumber}", h.getQagResponsesPaginated)
}

// kotlinToInt is String.toIntOrNull().
func kotlinToInt(s string) (int, bool) { return javacompat.KotlinToIntOrNull(s) }
