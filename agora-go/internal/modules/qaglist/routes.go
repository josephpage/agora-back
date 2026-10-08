package qaglist

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
//
// GET /qags/ask_status (same Kotlin controller) is registered by the qag module.
func Routes(a *app.App) {
	h := &handlers{a: a, svc: Get(a)}
	// QagHomeV2Controller
	a.Server.GET("/v2/qags", h.getQags)
	a.Server.GET("/qags/count", h.getQagCount)
	// QagHomeSearchController
	a.Server.GET("/qags/search", h.getQagSearchPreviews)
}
