package notification

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	h := &handlers{a: a, svc: Get(a)}
	// ConsultationNotificationController
	a.Server.GET("/admin/notifyConsultationUpdate/{consultationId}", h.consultationUpdateNotification)
	a.Server.GET("/admin/notifyConsultationDeadline/{consultationId}", h.consultationDeadlineNotification)
	// NotificationPaginatedController
	a.Server.GET("/notifications/paginated/{pageNumber}", h.getNotificationPaginatedList)
	// NotificationUserController
	a.Server.GET("/admin/notifyUser/{userId}", h.notifyUser)
	a.Server.GET("/admin/notifyAllUsers", h.notifyAllUsers)
	a.Server.GET("/admin/notifyUsersInDepartement/{codeDepartement}", h.notifyUsersInDepartement)
}
