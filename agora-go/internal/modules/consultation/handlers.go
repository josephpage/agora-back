package consultation

import (
	"errors"

	"agora/internal/httpx"
)

// Raise panics with what DefaultControllerAdvice / Spring make of an exception:
// ConsultationNotFoundException and ConsultationUpdateNotFoundException are 404
// {"title"} responses, anything else is an HTTP 500.
func Raise(err error) {
	var notFound *ConsultationNotFoundError
	var updateNotFound *ConsultationUpdateNotFoundError
	switch {
	case errors.As(err, &notFound):
		panic(&httpx.AdviceError{Status: 404, Title: "Veuillez renseigner un id de consultation existant."})
	case errors.As(err, &updateNotFound):
		panic(&httpx.AdviceError{Status: 404, Title: "Veuillez renseigner un id de contenu de consultation existant."})
	}
	panic(err)
}

func raising[T any](v T, err error) T {
	if err != nil {
		Raise(err)
	}
	return v
}

// getConsultationDetails is ConsultationDetailsV2Controller.getConsultationDetails.
func (s *Service) getConsultationDetails(c *httpx.Ctx) *httpx.Response {
	idOrSlug := c.PathVar("consultationIdOrSlug")
	details := raising(s.Details.GetConsultation(c.Context(), idOrSlug, c.OptionalUserID()))
	return httpx.OK(ToDetailsJSON(details, s.universalLink))
}

// getConsultationDetailsUpdate is ConsultationDetailsUpdateV2Controller.getConsultationDetailsUpdate.
func (s *Service) getConsultationDetailsUpdate(c *httpx.Ctx) *httpx.Response {
	idOrSlug := c.PathVar("consultationIdOrSlug")
	updateIDOrSlug := c.PathVar("consultationUpdateIdOrSlug")
	details := raising(s.Details.GetConsultationUpdate(c.Context(), idOrSlug, updateIDOrSlug, c.OptionalUserID()))
	return httpx.OK(ToDetailsJSON(details, s.universalLink))
}

// getConsultationPreview is ConsultationPreviewController.getConsultationPreviewOngoingList.
func (s *Service) getConsultationPreview(c *httpx.Ctx) *httpx.Response {
	userID := c.OptionalUserID()
	page := raising(s.Preview.GetConsultationPreviewPage(c.Context(), userID, c.CanViewUnpublishedConsultations()))
	return httpx.OK(ToPreviewJSON(page, FromTime(s.a.Now())))
}

// getQuestions is QuestionController.getQuestions.
func (s *Service) getQuestions(c *httpx.Ctx) *httpx.Response {
	consultationID := c.PathVar("consultationId")
	questions := s.Questions.GetConsultationQuestions(c.Context(), consultationID)
	return httpx.OK(ToQuestionsJSON(questions)).CacheControl(5*60, true)
}

// insertFeedbackConsultationUpdate is FeedbackConsultationUpdateController.insertFeedbackConsultationUpdate.
func (s *Service) insertFeedbackConsultationUpdate(c *httpx.Ctx) *httpx.Response {
	consultationID := c.PathVar("consultationId")
	consultationUpdateID := c.PathVar("consultationUpdateId")
	var body InsertFeedbackJSON
	c.BindBody(&body)
	userID := c.UserID()
	ctx := c.Context()
	return executeTask(&s.queue, feedbackTask{userID: userID},
		func() *httpx.Response {
			result := raising(s.Feedback.InsertFeedback(ctx, FeedbackInserting{
				UserID: userID, ConsultationID: consultationID, ConsultationUpdateID: consultationUpdateID, IsPositive: body.IsPositive,
			}))
			if !result.Success {
				return httpx.Unit(400)
			}
			return httpx.OK(ToFeedbackResultsJSON(result.Results))
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// deleteFeedbackConsultationUpdate is FeedbackConsultationUpdateController.deleteFeedbackConsultationUpdate
// (deprecated: nothing is deleted).
func (s *Service) deleteFeedbackConsultationUpdate(c *httpx.Ctx) *httpx.Response {
	_ = c.PathVar("consultationId")
	_ = c.PathVar("consultationUpdateId")
	return httpx.Unit(200)
}
