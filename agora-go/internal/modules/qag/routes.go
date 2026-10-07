package qag

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	h := &handlers{a: a, svc: Get(a)}
	// QagDetailsController, DeleteQagController
	a.Server.GET("/qags/{qagId}", h.getQagDetails)
	a.Server.GET("/api/public/qags/{qagId}", h.getPublicQagDetails)
	a.Server.DELETE("/qags/{qagId}", h.deleteQagByID)
	// InsertQagController
	a.Server.POST("/qags", h.insertQag)
	// SupportQagController
	a.Server.POST("/qags/{qagId}/support", h.insertSupportQag)
	a.Server.DELETE("/qags/{qagId}/support", h.deleteSupportQag)
	// FeedbackQagController
	a.Server.POST("/qags/{qagId}/feedback", h.insertFeedbackQag)
	// QagHomeV2Controller.askStatus (its GetQagErrorTextUseCase belongs to this slice)
	a.Server.GET("/qags/ask_status", h.askStatus)
}
