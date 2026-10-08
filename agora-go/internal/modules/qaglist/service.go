// Package qaglist ports the QaG lists of the app home: GET /v2/qags (top, latest,
// supporting and trending tabs), GET /qags/search and GET /qags/count, with the
// QaG tab headers (Strapi "qa-g-headers-onglets") and the free-theme trending clusters
// (Strapi "cluster-semaine-libres").
//
// Performance design (shared data may be cached for at most AGORA_MICROCACHE_TTL):
//   - a page of the top / latest tabs and the count of accepted QaGs are shared by every
//     user: they go through a micro-cache with one load at a time (singleflight);
//   - what depends on the user (the QaGs the user supports, the QaGs the user wrote,
//     the whole "supporting" tab) is read at every request, so the user always sees
//     the effect of their own support / unsupport at once;
//   - the trending candidates keep Kotlin's "trendingQagCache" (5 minutes), the headers
//     "headersQag" (1 hour) and the clusters "trendingClusterCache" (5 minutes, non-empty only).
package qaglist

import (
	"context"
	"strconv"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
	"agora/internal/modules/themehebdo"
)

const (
	pageCacheName  = "qagListPage"
	countCacheName = "qagListCount"
)

// Service gathers the use cases of the slice.
type Service struct {
	a         *app.App
	Paginated *PaginatedUseCase
	Search    func(ctx context.Context, userID string, keywords []string) ([]qag.QagPreview, error)
	Count     func(ctx context.Context) (int, error)
}

// Get returns the App-wide service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "qaglist", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	q := qag.Get(a)
	themes := thematique.Get(a)
	headers := headerStore{a: a, source: strapiHeaders{a: a}}
	pages := &microPages{a: a, info: q.Info}
	uc := &PaginatedUseCase{
		supported:  q.Info,
		shared:     pages,
		themes:     themes,
		headers:    headers,
		trending:   &trendingCache{a: a, info: q.Info},
		supports:   supportAdapter{q},
		themeHebdo: themehebdo.Get(a),
		clusters:   clusterStore{a: a, fetcher: strapiClusters{a: a}},
		exponent:   a.Cfg.TrendingScoreExponent,
		now:        a.Now,
	}
	// GET /qags/count is getQagsCount(null): the same shared count as the lists
	count := func(ctx context.Context) (int, error) { return pages.Count(ctx, nil) }
	return &Service{a: a, Paginated: uc, Search: q.GetQagByKeywords, Count: count}
}

// supportAdapter is SupportQagUseCase.
type supportAdapter struct{ q *qag.Service }

func (s supportAdapter) GetUserSupportedQagIDs(ctx context.Context, userID string) ([]string, error) {
	return s.q.GetUserSupportedQagIDs(ctx, userID)
}

func (s supportAdapter) GetSupportedQagCount(ctx context.Context, userID string, thematiqueID *string) (int, error) {
	return s.q.GetSupportedQagCount(ctx, userID, thematiqueID)
}

// ---------------------------------------------------------------------------
// Micro-cache of the shared pages
// ---------------------------------------------------------------------------

type sharedInfo interface {
	GetQagsCount(ctx context.Context, thematiqueID *string) (int, error)
	GetPopularQagsPaginatedV2(ctx context.Context, offset int, thematiqueID *string) ([]qag.QagInfoWithSupportCount, error)
	GetLatestQagsPaginatedV2(ctx context.Context, offset int, thematiqueID *string) ([]qag.QagInfoWithSupportCount, error)
}

// microPages shares the count of accepted QaGs and the pages of the top / latest tabs
// between the users for AGORA_MICROCACHE_TTL (class B, see parity/divergences/S3.md): a
// support of another user shows up in the counts and the order within that delay. The
// per-user overlay is never cached. Errors are never kept.
type microPages struct {
	a    *app.App
	info sharedInfo
}

func (m *microPages) ttl() time.Duration { return m.a.Cache.CoexistenceTTL(m.a.Cfg.MicroCacheTTL) }

func thematiqueKey(thematiqueID *string) string {
	if thematiqueID == nil {
		return "-"
	}
	return "+" + *thematiqueID
}

// Count is getQagsCount(thematiqueId).
func (m *microPages) Count(ctx context.Context, thematiqueID *string) (int, error) {
	ttl := m.ttl()
	if ttl <= 0 {
		return m.info.GetQagsCount(ctx, thematiqueID)
	}
	return cache.GetOrLoad(m.a.Cache, countCacheName, thematiqueKey(thematiqueID), ttl, func() (int, error) {
		// the shared load must not die with the request that started it
		return m.info.GetQagsCount(context.WithoutCancel(ctx), thematiqueID)
	})
}

// Page is getPopularQagsPaginatedV2 / getLatestQagsPaginatedV2. The returned slice is
// shared: it must not be modified.
func (m *microPages) Page(ctx context.Context, filter string, offset int, thematiqueID *string) ([]qag.QagInfoWithSupportCount, error) {
	load := func(ctx context.Context) ([]qag.QagInfoWithSupportCount, error) {
		if filter == filterTop {
			return m.info.GetPopularQagsPaginatedV2(ctx, offset, thematiqueID)
		}
		return m.info.GetLatestQagsPaginatedV2(ctx, offset, thematiqueID)
	}
	ttl := m.ttl()
	if ttl <= 0 {
		return load(ctx)
	}
	key := filter + "\x00" + strconv.Itoa(offset) + "\x00" + thematiqueKey(thematiqueID)
	return cache.GetOrLoad(m.a.Cache, pageCacheName, key, ttl, func() ([]qag.QagInfoWithSupportCount, error) {
		return load(context.WithoutCancel(ctx))
	})
}

// ---------------------------------------------------------------------------
// TrendingQagCacheRepository
// ---------------------------------------------------------------------------

type trendingInfo interface {
	GetTrendingQagsV3(ctx context.Context) ([]qag.QagInfoWithSupportCount, error)
}

// trendingCache keeps the trending candidates five minutes (Kotlin "trendingQagCache",
// an empty list is kept as well). In coexistence mode the entry lives at most 5 seconds
// (the support counts it holds can be changed by the Kotlin backend).
type trendingCache struct {
	a    *app.App
	info trendingInfo
}

// Candidates is the cache lookup, else getTrendingQagsV3() (cached).
func (t *trendingCache) Candidates(ctx context.Context) ([]qag.QagInfoWithSupportCount, error) {
	return cache.GetOrLoad(t.a.Cache, trendingQagCacheName, trendingQagCacheKey, t.a.Cache.CoexistenceTTL(shortTermTTL),
		func() ([]qag.QagInfoWithSupportCount, error) {
			return t.info.GetTrendingQagsV3(context.WithoutCancel(ctx))
		})
}
