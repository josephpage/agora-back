package consultation

import "agora/internal/app"

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	s := Get(a)
	// ConsultationDetailsV2Controller
	a.Server.GET("/v2/consultations/{consultationIdOrSlug}", s.getConsultationDetails)
	a.Server.GET("/api/public/consultations/{consultationIdOrSlug}", s.getConsultationDetails)
	// ConsultationDetailsUpdateV2Controller
	a.Server.GET("/v2/consultations/{consultationIdOrSlug}/updates/{consultationUpdateIdOrSlug}", s.getConsultationDetailsUpdate)
	a.Server.GET("/api/public/consultations/{consultationIdOrSlug}/updates/{consultationUpdateIdOrSlug}", s.getConsultationDetailsUpdate)
	// ConsultationPreviewController
	a.Server.GET("/consultations", s.getConsultationPreview)
	// QuestionController
	a.Server.GET("/consultations/{consultationId}/questions", s.getQuestions)
	// FeedbackConsultationUpdateController
	a.Server.POST("/consultations/{consultationId}/updates/{consultationUpdateId}/feedback", s.insertFeedbackConsultationUpdate)
	a.Server.DELETE("/consultations/{consultationId}/updates/{consultationUpdateId}/feedback", s.deleteFeedbackConsultationUpdate)
}
