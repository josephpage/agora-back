package consultationlist

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	s := Get(a)
	// ConsultationAnsweredPaginatedController
	a.Server.GET("/consultations/answered/{pageNumber}", s.getConsultationAnsweredList)
	// ConsultationFinishedPaginatedController
	a.Server.GET("/consultations/finished/{pageNumber}", s.getConsultationFinishedList)
}
