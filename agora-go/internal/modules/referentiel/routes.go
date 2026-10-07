package referentiel

import (
	"strings"

	"agora/internal/app"
	"agora/internal/httpx"
)

// Routes registers this module's HTTP routes on a.Server.
func Routes(a *app.App) {
	// ReferentielController.getRegionsEtDepartements
	a.Server.GET("/referentiels/regions-et-departements", func(c *httpx.Ctx) *httpx.Response {
		b, raw := responseBody()
		resp := cacheControl(c, httpx.OK(b), 5*60)
		resp.PrecomputedJSON = raw
		return resp
	})
}

// cacheControl adds Cache-Control unless ?mediaType= makes the content
// negotiation fail: Spring then answers 406 and the entity's headers are not
// written (httpx.negotiate / foundation request F1 in parity/ledger/S0.md).
func cacheControl(c *httpx.Ctx, resp *httpx.Response, maxAgeSeconds int) *httpx.Response {
	if v, ok := c.Param("mediaType"); ok {
		switch strings.ToLower(v) {
		case "", "json", "xml":
		default:
			return resp
		}
	}
	return resp.CacheControl(maxAgeSeconds, true)
}
