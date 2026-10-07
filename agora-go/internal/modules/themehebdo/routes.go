package themehebdo

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	a.Server.GET("/theme_hebdo", Get(a).handler)
}
