// Package apidocs serves the springdoc OpenAPI document and Swagger UI exactly
// as captured from the Kotlin reference (springdoc-openapi 2.2.0, swagger-ui
// 5.2.0). The OpenAPI document is a frozen snapshot: the Go routes are
// identical to the Kotlin ones, so the document is unchanged (divergence C-SWAGGER).
package apidocs

import (
	"embed"
	"net/http"
	"path"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/httpx"
)

//go:embed assets/api-docs.json assets/swagger-ui/*
var assets embed.FS

var startTime = time.Now().UTC()

var contentTypes = map[string]string{
	".js":   "text/javascript",
	".css":  "text/css",
	".html": "text/html",
	".png":  "image/png",
	".json": "application/json",
}

// Routes registers the documentation routes.
func Routes(a *app.App) {
	apiDocs, _ := assets.ReadFile("assets/api-docs.json")
	a.Server.GET("/v3/api-docs", func(c *httpx.Ctx) *httpx.Response {
		return httpx.Bytes(200, "application/json", apiDocs)
	})
	a.Server.GET("/v3/api-docs/swagger-config", func(c *httpx.Ctx) *httpx.Response {
		body := `{"configUrl":"/v3/api-docs/swagger-config","oauth2RedirectUrl":"http://` + c.R.Host +
			`/swagger-ui/oauth2-redirect.html","operationsSorter":"alpha","persistAuthorization":true,"tagsSorter":"alpha","url":"/v3/api-docs","validatorUrl":""}`
		return httpx.Bytes(200, "application/json", []byte(body))
	})
	a.Server.GET("/swagger-ui.html", func(c *httpx.Ctx) *httpx.Response {
		return httpx.Empty(302).With("Location", "/swagger-ui/index.html")
	})
	a.Server.GET("/swagger-ui/{file}", func(c *httpx.Ctx) *httpx.Response {
		name := c.PathVar("file")
		if strings.Contains(name, "..") {
			return httpx.Empty(404)
		}
		b, err := assets.ReadFile("assets/swagger-ui/" + name)
		if err != nil {
			panic(&httpx.SpringError{Status: 404})
		}
		ct := contentTypes[path.Ext(name)]
		r := httpx.Bytes(200, ct, b)
		if name == "swagger-initializer.js" {
			// springdoc's transformed resource is served with no-store
			r.With("Cache-Control", "no-store")
		}
		return r.
			With("Last-Modified", startTime.Format(http.TimeFormat)).
			With("Accept-Ranges", "bytes")
	})
}
