package content

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/httpx"
	"agora/internal/strapi"
)

// fakeStrapi answers the same body (or a body chosen per request) to every request and records the URIs.
type fakeStrapi struct {
	srv  *httptest.Server
	mu   sync.Mutex
	body func(uri string) string
	uris []string
}

func newFakeStrapi(t testing.TB, body func(uri string) string) *fakeStrapi {
	t.Helper()
	f := &fakeStrapi{body: body}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.uris = append(f.uris, strings.TrimPrefix(r.RequestURI, "/api/"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.body(r.RequestURI))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func constBody(s string) func(string) string { return func(string) string { return s } }

func (f *fakeStrapi) lastURI() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uris) == 0 {
		return ""
	}
	return f.uris[len(f.uris)-1]
}

func (f *fakeStrapi) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.uris)
}

// testApp is an App wired to the fake Strapi, without database or Redis, and an
// httpx server with the S9 routes.
type testApp struct {
	*app.App
	strapi *fakeStrapi
	now    time.Time
}

func newTestApp(t testing.TB, body func(uri string) string) *testApp {
	t.Helper()
	f := newFakeStrapi(t, body)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ta := &testApp{strapi: f, now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	ta.App = &app.App{
		Cfg:    &config.Config{MicroCacheTTL: 0},
		Cache:  cache.New(nil, log, false),
		Strapi: strapi.NewClient(strapi.Options{BaseURL: f.srv.URL + "/api/", Token: "token", Timeout: 5 * time.Second, Logger: log}),
		Log:    log,
		Clock:  func() time.Time { return ta.now },
	}
	ta.Server = httpx.NewServer(httpx.Options{Now: ta.App.Clock, Logger: log, ETagPaths: []string{"/thematiques", "/participation_charter"}})
	return ta
}

func (ta *testApp) routes() *testApp {
	Routes(ta.App)
	return ta
}

type reply struct {
	status int
	header http.Header
	body   string
}

func (ta *testApp) do(method, target string, hdr map[string]string) reply {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ta.Server.ServeHTTP(rec, req.WithContext(context.Background()))
	return reply{rec.Code, rec.Header(), rec.Body.String()}
}

func (ta *testApp) get(target string) reply { return ta.do("GET", target, nil) }
