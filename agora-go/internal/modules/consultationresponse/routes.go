package consultationresponse

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	s := Get(a)
	// ReponseConsultationController
	a.Server.POST("/consultations/{consultationId}/responses", s.postResponseConsultation)
	// ConsultationResultsV2Controller
	a.Server.GET("/v2/consultations/{consultationId}/responses", s.getConsultationResults)
	// GetConsultationResponsesWithDemographicInfoController
	a.Server.GET("/admin/consultations/{consultationId}/responses", s.getConsultationResponsesWithDemographicInfo)
}
