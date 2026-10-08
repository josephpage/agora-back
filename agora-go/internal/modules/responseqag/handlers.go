package responseqag

import (
	"agora/internal/app"
	"agora/internal/httpx"
)

type handlers struct {
	a   *app.App
	svc *Service
}

// must unwraps a (value, error) pair: an error is an exception Kotlin does not
// catch (HTTP 500).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// minDateParam binds `@RequestParam(name = "minDate", required = false) minDateStr: String?` and
// parses it with SimpleDateFormat("yyyy-MM-dd") (non lenient). bad is true when Kotlin answers 400.
func minDateParam(c *httpx.Ctx) (minDate *int64, bad bool) {
	s := c.OptionalParam("minDate")
	if s == nil {
		return nil, false
	}
	ms, ok := parseMinDate(*s)
	if !ok {
		return nil, true
	}
	return &ms, false
}

// getQagResponses is QagHomeController.getQagResponses (GET /qags/responses).
func (h *handlers) getQagResponses(c *httpx.Ctx) *httpx.Response {
	minDate, bad := minDateParam(c)
	if bad {
		return httpx.Unit(400)
	}
	list := must(h.svc.Previews.GetResponseQagPreviewList(c.Context(), minDate))
	return httpx.OK(ToResponsesJSON(list)).CacheControl(5*60, true)
}

// getQagResponsesPaginated is ResponseQagPaginatedController.getQagResponses (GET /qags/responses/{pageNumber}).
func (h *handlers) getQagResponsesPaginated(c *httpx.Ctx) *httpx.Response {
	pageNumber := c.PathVar("pageNumber")
	page, ok := kotlinToInt(pageNumber)
	if !ok {
		return httpx.Unit(400)
	}
	minDate, bad := minDateParam(c)
	if bad {
		return httpx.Unit(400)
	}
	list := must(h.svc.Paginated.GetResponseQagPreviewPaginatedList(c.Context(), page, minDate))
	if list == nil {
		// `ResponseEntity.ok().cacheControl(..).body(null)`: 200, Cache-Control and no body at all
		return httpx.Empty(200).CacheControl(5*60, true)
	}
	return httpx.OK(ToPaginatedJSON(*list)).CacheControl(5*60, true)
}
