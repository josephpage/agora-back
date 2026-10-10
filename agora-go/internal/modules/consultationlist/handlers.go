package consultationlist

import (
	"errors"

	"agora/internal/domain"
	"agora/internal/httpx"
	"agora/internal/javacompat"
)

// must unwraps a (value, error) pair: an error is an exception Kotlin does not
// catch (HTTP 500).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// getConsultationFinishedList is ConsultationFinishedPaginatedController.getConsultationFinishedList
// (GET /consultations/finished/{pageNumber}). The arguments are bound in declaration order: the
// path variable (an Int: HTTP 400 when it is not one), then the territory.
func (s *Service) getConsultationFinishedList(c *httpx.Ctx) *httpx.Response {
	pageNumber := httpx.SpringIntPathVar(c.PathVar("pageNumber"))
	territory := c.OptionalParam("territory")
	ctx := c.Context()

	var consultations *ConsultationPaginatedJSON
	if territory == nil {
		// every finished consultation, one page (maxPageNumber 1)
		list := must(s.Preferences.Execute(ctx, c.UserID()))
		consultations = toJSON(s.a, 1, list)
	} else {
		page, err := s.Finished.GetConsultationFinishedPaginatedList(ctx, pageNumber, *territory)
		var invalid *domain.InvalidTerritoryError
		if errors.As(err, &invalid) {
			panic(&httpx.AdviceError{Status: 400, Title: err.Error()})
		}
		if must(page, err) != nil {
			consultations = toJSON(s.a, page.MaxPageNumber, page.Consultations)
		}
	}
	if consultations == nil {
		return httpx.Empty(404)
	}
	return httpx.OK(consultations)
}

// getConsultationAnsweredList is ConsultationAnsweredPaginatedController.getConsultationAnsweredList
// (GET /consultations/answered/{pageNumber}): the page number is a String read with toIntOrNull.
func (s *Service) getConsultationAnsweredList(c *httpx.Ctx) *httpx.Response {
	pageNumber, ok := javacompat.KotlinToIntOrNull(c.PathVar("pageNumber"))
	if !ok {
		return httpx.Unit(400)
	}
	page := must(s.Answered.GetConsultationAnsweredPaginatedList(c.Context(), c.UserID(), pageNumber))
	if page == nil {
		return httpx.Unit(400)
	}
	return httpx.OK(toJSON(s.a, page.MaxPageNumber, page.Consultations))
}
