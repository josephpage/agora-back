package concertation

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	// ConcertationController
	a.Server.GET("/concertations", Get(a).getConcertations)
}
