package qaglist

import (
	"strings"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/javacompat"
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

// getQags is QagHomeV2Controller.getQagDetails (GET /v2/qags). The arguments are bound
// in declaration order (pageNumber, thematiqueId, filterType), then the user.
func (h *handlers) getQags(c *httpx.Ctx) *httpx.Response {
	pageNumber := c.RequiredParam("pageNumber")
	thematiqueID := c.OptionalParam("thematiqueId")
	filterType := c.RequiredParam("filterType")
	userID := c.UserID()

	var usedThematiqueID *string
	if thematiqueID != nil && !javacompat.KotlinIsBlank(*thematiqueID) {
		usedThematiqueID = thematiqueID
	}
	usedFilterType := ""
	if !javacompat.KotlinIsBlank(filterType) {
		usedFilterType = filterType
	}

	page, ok := javacompat.KotlinToIntOrNull(pageNumber)
	if !ok {
		return httpx.Unit(400)
	}

	ctx := c.Context()
	var result *QagsAndMaxPageCountV2
	switch usedFilterType {
	case filterTop:
		result = must(h.svc.Paginated.GetPopularQagPaginated(ctx, userID, page, usedThematiqueID))
	case filterLatest:
		result = must(h.svc.Paginated.GetLatestQagPaginated(ctx, userID, page, usedThematiqueID))
	case filterSupporting:
		result = must(h.svc.Paginated.GetSupportedQagPaginated(ctx, userID, page, usedThematiqueID))
	case filterTrending:
		result = must(h.svc.Paginated.GetTrendingQag(ctx, userID))
	}
	if result == nil {
		return httpx.Unit(400)
	}
	return httpx.OK(ToPaginatedJSON(*result)).CacheControl(5*60, false)
}

// maxCharacterSize is QagHomeSearchController.MAX_CHARACTER_SIZE.
const maxCharacterSize = 75

// filterKeywords is the keywords expression of QagHomeSearchController: blank = null,
// take(75), replaceDiacritics, then every character outside [A-Za-z0-9 ] removed.
func filterKeywords(keywords *string) *string {
	if keywords == nil || javacompat.KotlinIsBlank(*keywords) {
		return nil
	}
	s := javacompat.ReplaceDiacritics(javacompat.Take16(*keywords, maxCharacterSize))
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	return &out
}

// getQagSearchPreviews is QagHomeSearchController.getQagSearchPreviews (GET /qags/search).
func (h *handlers) getQagSearchPreviews(c *httpx.Ctx) *httpx.Response {
	keywords := c.OptionalParam("keywords")
	userID := c.UserID()
	filtered := filterKeywords(keywords)
	if filtered == nil || javacompat.KotlinIsBlank(*filtered) || len(*filtered) < 3 {
		return httpx.Unit(404)
	}
	var words []string
	for _, w := range strings.Split(*filtered, " ") {
		if !javacompat.KotlinIsBlank(w) {
			words = append(words, w)
		}
	}
	previews := must(h.svc.Search(c.Context(), userID, words))
	return httpx.OK(ToPreviewListJSON(previews))
}

// getQagCount is QagHomeV2Controller.getQagCount (GET /qags/count).
func (h *handlers) getQagCount(c *httpx.Ctx) *httpx.Response {
	return httpx.OK(QagCount(must(h.svc.Count(c.Context()))))
}
