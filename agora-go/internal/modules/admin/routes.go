// Package admin is the administration API (/admin/**, ADMIN_APIS authority, see
// httpx.AgoraRules): the weekly tasks, the Redis FLUSHDB and the user management
// (deletion, publisher upgrade, downgrade) of AdminTasksController and EditUsersController.
package admin

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server, in the order of the
// Kotlin controllers.
func Routes(a *app.App) {
	h := &handlers{a: a}
	// AdminTasksController
	a.Server.POST("/admin/weekly-tasks", h.runWeeklyTasks)
	a.Server.POST("/admin/cache/clear", h.clearCache)
	// EditUsersController
	a.Server.DELETE("/admin/delete_users", h.deleteUsers)
	a.Server.POST("/admin/upgrade_users_to_publisher", h.upgradeUsersToPublisher)
	a.Server.POST("/admin/downgrade_users", h.downgradeUsers)
}
