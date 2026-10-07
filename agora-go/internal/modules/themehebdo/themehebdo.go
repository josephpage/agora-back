// Package themehebdo ports the weekly theme (GET /theme_hebdo):
// ThemeHebdoController, GetThemeHebdoUseCase, IsThemeHebdoTransitionUseCase,
// ThemeHebdoRepositoryImpl, ThemeHebdoCacheRepository, ThemeHebdoStrapiRepository
// and the mappers.
//
// Other slices (trending QaGs, weekly tasks) use the exported Service:
//
//	th, err := themehebdo.Get(a).Current(ctx)       // getCurrentThemeHebdo() (5 min cache)
//	ok, err := themehebdo.Get(a).IsInTransition(ctx) // IsThemeHebdoTransitionUseCase.isInTransition()
//
// The errors correspond to exceptions Kotlin does not catch (a Strapi date that
// OffsetDateTime.parse rejects): an HTTP handler must panic(err) (→ 500), a task
// must fail like the Kotlin one.
//
// Caches (Kotlin "themeHebdoCache", shortTermCacheManager = 5 minutes):
//   - the current theme ("currentThemeHebdo") is always cached 5 minutes;
//   - the Strapi list ("themeHebdoList", non-empty lists only) is cached 5 minutes
//     when THEME_HEBDO_CACHE_ENABLED=true; otherwise Kotlin calls Strapi at every
//     request and Go shares one load for AGORA_MICROCACHE_TTL (≤ 5 s).
package themehebdo

import (
	"context"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/strapi"
)

const (
	cacheName    = "themehebdo"
	listKey      = "themeHebdoList"
	currentKey   = "currentThemeHebdo"
	microListKey = "micro:" + listKey
)

// Service is the weekly theme service. Safe for concurrent use.
type Service struct {
	a    *app.App
	repo *repository
	uc   *useCase
}

type l1ListCache struct {
	a   *app.App
	key string
	ttl func() time.Duration
}

func (c l1ListCache) get() ([]ThemeHebdo, bool) {
	v, ok := c.a.Cache.Get(cacheName, c.key)
	if !ok {
		return nil, false
	}
	return v.([]ThemeHebdo), true
}

func (c l1ListCache) put(l []ThemeHebdo) { c.a.Cache.Put(cacheName, c.key, l, c.ttl()) }

type l1CurrentCache struct{ a *app.App }

func (c l1CurrentCache) get() (ThemeHebdo, bool) {
	v, ok := c.a.Cache.Get(cacheName, currentKey)
	if !ok {
		return ThemeHebdo{}, false
	}
	return v.(ThemeHebdo), true
}

func (c l1CurrentCache) put(t ThemeHebdo) { c.a.Cache.Put(cacheName, currentKey, t, shortTermTTL) }

// Get returns the App-wide weekly theme service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "themehebdo", func() *Service {
		repo := &repository{
			cacheEnabled: a.Cfg.ThemeHebdoCacheEnabled,
			cache:        l1ListCache{a, listKey, func() time.Duration { return shortTermTTL }},
			fetch: func(ctx context.Context) []*strapiThemeHebdo {
				return strapi.Collection[*strapiThemeHebdo](ctx, a.Strapi, strapi.NewRequest("theme-hebdos")).Data
			},
			micro: l1ListCache{a, microListKey, func() time.Duration { return a.Cfg.MicroCacheTTL }},
		}
		return &Service{
			a:    a,
			repo: repo,
			uc:   &useCase{repo: repo, cache: l1CurrentCache{a}, clock: a.Now, log: a.Log},
		}
	})
}

// List is ThemeHebdoRepository.getThemeHebdoList(): every weekly theme, in
// Strapi order. The returned slice is shared: do not modify it.
func (s *Service) List(ctx context.Context) ([]ThemeHebdo, error) { return s.repo.List(ctx) }

// Theme is GetThemeHebdoUseCase.getThemeHebdo(): the theme of the current week
// (the default "semaine libre" theme when none is in range), with its upcoming
// themes. Not cached (see Current).
func (s *Service) Theme(ctx context.Context) (ThemeHebdo, error) { return s.uc.Theme(ctx) }

// Current is GetThemeHebdoUseCase.getCurrentThemeHebdo(): Theme, cached 5 minutes.
func (s *Service) Current(ctx context.Context) (ThemeHebdo, error) { return s.uc.Current(ctx) }

// IsInTransition is IsThemeHebdoTransitionUseCase.isInTransition().
func (s *Service) IsInTransition(ctx context.Context) (bool, error) { return s.uc.IsInTransition(ctx) }

// handler is ThemeHebdoController.getThemeHebdo.
func (s *Service) handler(c *httpx.Ctx) *httpx.Response {
	theme, err := s.Theme(c.Context())
	if err != nil {
		panic(err)
	}
	return cacheControl(c, httpx.OK(ToJSON(theme)), 10)
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
