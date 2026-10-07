package thematique

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	// ThematiqueController.getThematiqueList; the ETag (ShallowEtagHeaderFilter
	// on /thematiques) is added by httpx.
	a.Server.GET("/thematiques", Get(a).handler)
}
