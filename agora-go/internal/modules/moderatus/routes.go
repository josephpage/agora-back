package moderatus

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server. /moderatus/** is public (the login token
// is the "password" parameter), /admin/** needs ADMIN_APIS (httpx security rules).
func Routes(a *app.App) {
	h := &handlers{svc: Get(a)}
	// ModeratusGetQagController, ModeratusLockQagController, ModeratusModerateQagController
	a.Server.GET("/moderatus/new_qags", h.getQagToModerate)
	a.Server.GET("/moderatus/ack_qags", h.lockQag)
	a.Server.GET("/moderatus/result_qag", h.moderateQag)
	// AdminQagController
	a.Server.PUT("/admin/qags/{qagId}/status", h.updateQagStatus)
}
