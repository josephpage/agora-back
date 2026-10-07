// Package content ports slice S9 "CMS content & misc": the ten Strapi pages
// (GET /content/*), the welcome page news, the participation charter, the
// fiches inventaire and the application feedback.
package content

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	routePages(a)                                               // ContentController
	a.Server.GET("/welcome_page/last_news", lastNewsHandler(a)) // WelcomePageController
	// ParticipationCharterController; the ETag (ShallowEtagHeaderFilter on
	// /participation_charter) is added by httpx.
	a.Server.GET("/participation_charter", charterHandler(a))
	a.Server.GET("/fiches_inventaire", listHandler(a))             // FicheInventaireController
	a.Server.GET("/fiches_inventaire/{idFiche}", detailHandler(a)) // FicheInventaireController
	a.Server.POST("/feedback", feedbackHandler(a))                 // AppFeedbackController
}
