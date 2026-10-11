package consultationresponse

import (
	"context"

	"agora/internal/httpx"
)

// postResponseConsultation is ReponseConsultationController.postResponseConsultation.
func (s *Service) postResponseConsultation(c *httpx.Ctx) *httpx.Response {
	consultationID := c.PathVar("consultationId")
	var body ReponsesConsultationJSON
	c.BindBody(&body)
	ipAddressHash := c.IPHash()
	// the answer is written even if the client leaves in the middle of the request
	ctx := context.WithoutCancel(c.Context())
	if s.RateLimit.IsRateLimited(ctx, ipAddressHash) {
		return httpx.Unit(429)
	}
	userID := c.UserID()
	return executeTask(&s.queue, insertResponseTask{userID: userID},
		func() *httpx.Response {
			consultationResponses := toDomain(&body)
			if !s.Control.IsResponseConsultationValid(ctx, consultationID, consultationResponses) {
				return httpx.Unit(400)
			}
			status, err := s.Insert.InsertReponseConsultation(ctx, consultationID, userID, consultationResponses)
			if err != nil {
				panic(err)
			}
			if status == InsertFailure {
				return httpx.Unit(400)
			}
			askDemographicInfo, err := s.profiles.AskForDemographicInfo(ctx, userID, consultationID)
			if err != nil {
				panic(err)
			}
			return httpx.OK(ResponseConsultationResultJSON{AskDemographicInfo: askDemographicInfo})
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// getConsultationResults is ConsultationResultsV2Controller.getConsultationResults.
func (s *Service) getConsultationResults(c *httpx.Ctx) *httpx.Response {
	consultationID := c.PathVar("consultationId")
	results, err := s.Results.GetConsultationResults(c.Context(), consultationID)
	if err != nil {
		panic(err)
	}
	if results == nil {
		return httpx.Unit(400)
	}
	return results.response()
}

// getConsultationResponsesWithDemographicInfo is
// GetConsultationResponsesWithDemographicInfoController.getConsultationResults (admin).
func (s *Service) getConsultationResponsesWithDemographicInfo(c *httpx.Ctx) *httpx.Response {
	consultationID := c.PathVar("consultationId")
	result, err := s.Demographic.GetConsultationResults(c.Context(), consultationID)
	if err != nil {
		panic(err)
	}
	if result == nil {
		return httpx.Unit(400)
	}
	return httpx.Bytes(200, "text/plain;charset=UTF-8", []byte(s.tsv.BuildTsvBody(result)))
}
