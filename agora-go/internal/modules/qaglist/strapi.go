package qaglist

import (
	"context"
	"errors"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/modules/content"
	"agora/internal/strapi"
)

// ---------------------------------------------------------------------------
// Header of the QaG tabs: HeaderQagStrapiRepository, HeaderQagRepositoryImpl,
// HeaderQagCacheRepositoryImpl
// ---------------------------------------------------------------------------

const (
	// headerCacheName is the "headersQag" cache of the default cache manager (1 hour).
	headerCacheName = "headersQag"
	headerCacheTTL  = time.Hour

	// Kotlin "shortTermCacheManager" (5 minutes).
	trendingQagCacheName     = "trendingQagCache"
	trendingQagCacheKey      = "trendingQagList"
	trendingClusterCacheName = "trendingClusterCache"
	trendingClusterCacheKey  = "trendingClusterList"
	shortTermTTL             = 5 * time.Minute
)

// headerQagStrapiDTO is HeaderQagStrapiDTO (@JsonIgnoreProperties createdAt, updatedAt,
// publishedAt): a missing or null field anywhere makes the whole Strapi list undecodable
// (empty). datetime_publication is a LocalDateTime Jackson must be able to read.
type headerQagStrapiDTO struct {
	DocumentID          string                `json:"documentId"`
	Titre               string                `json:"titre"`
	Message             string                `json:"message"`
	Type                string                `json:"type"`
	DatetimePublication content.LocalDateTime `json:"datetime_publication"`
}

// headerSource is HeaderQagRepository.getLastHeader.
type headerSource interface {
	LastHeader(ctx context.Context, filterType string) *HeaderQag
}

// strapiHeaders is HeaderQagRepositoryImpl + HeaderQagStrapiRepository.
type strapiHeaders struct{ a *app.App }

// LastHeader is getLastHeader(filterType): the latest published header of the tab
// (a Strapi failure is an empty list, that is no header).
func (s strapiHeaders) LastHeader(ctx context.Context, filterType string) *HeaderQag {
	now := s.a.Now()
	b := strapi.NewRequest("qa-g-headers-onglets").
		WithDateBefore(now, "datetime_publication").
		FilterIn("type", []string{filterType}).
		SortBy("datetime_publication", "desc")
	env := strapi.Collection[*headerQagStrapiDTO](ctx, s.a.Strapi, b)
	if len(env.Data) == 0 {
		return nil
	}
	first := env.Data[0]
	if first == nil {
		// a null element: `.firstOrNull()` returns null and the mapper's `it.documentId` is not reached
		return nil
	}
	return &HeaderQag{HeaderID: first.DocumentID, Title: first.Titre, Message: first.Message}
}

// headerStore is HeaderQagCacheRepository + the repository: the header of a tab is
// requested once and the answer, found or not, is kept one hour (Redis "headersQag"
// in Kotlin; a Strapi failure is kept as "not found" as well).
type headerStore struct {
	a      *app.App
	source headerSource
}

// Header returns the header of the tab, nil when there is none.
func (h headerStore) Header(ctx context.Context, filterType string) (*HeaderQag, error) {
	return cache.GetOrLoad(h.a.Cache, headerCacheName, filterType, headerCacheTTL, func() (*HeaderQag, error) {
		// the shared load must not die with the request that started it
		return h.source.LastHeader(context.WithoutCancel(ctx), filterType), nil
	})
}

// ---------------------------------------------------------------------------
// Trending clusters: TrendingClusterStrapiRepository, TrendingClusterRepositoryImpl,
// TrendingClusterCacheRepository
// ---------------------------------------------------------------------------

// trendingClusterStrapiDTO is TrendingClusterStrapiDTO.
type trendingClusterStrapiDTO struct {
	Titre    string  `json:"titre"`
	Keywords *string `json:"keywords"`
}

// clusterFetcher is TrendingClusterStrapiRepository.getClusters mapped by TrendingClusterRepositoryImpl.
type clusterFetcher interface {
	Fetch(ctx context.Context) []trendingClusterStrapiDTO
}

type strapiClusters struct{ a *app.App }

// Fetch requests "cluster-semaine-libres" (a Strapi failure is an empty list).
func (s strapiClusters) Fetch(ctx context.Context) []trendingClusterStrapiDTO {
	env := strapi.Collection[*trendingClusterStrapiDTO](ctx, s.a.Strapi, strapi.NewRequest("cluster-semaine-libres"))
	out := make([]trendingClusterStrapiDTO, 0, len(env.Data))
	for _, d := range env.Data {
		if d == nil {
			// a null element is kept by Jackson and `dto.keywords` throws a NullPointerException
			panic("java.lang.NullPointerException: null cluster in Strapi data")
		}
		out = append(out, *d)
	}
	return out
}

// mapClusters is the mapNotNull of TrendingClusterRepositoryImpl.getClustersAndCacheIt: the
// keywords are split on commas, trimmed, blanks dropped; a cluster without keyword is ignored.
func mapClusters(dtos []trendingClusterStrapiDTO, onIgnored func(titre string)) []TrendingCluster {
	var out []TrendingCluster
	for _, dto := range dtos {
		var keywords []string
		if dto.Keywords != nil {
			for _, k := range splitComma(*dto.Keywords) {
				k = javacompat.KotlinTrim(k)
				if !javacompat.KotlinIsBlank(k) {
					keywords = append(keywords, k)
				}
			}
		}
		if len(keywords) == 0 {
			if onIgnored != nil {
				onIgnored(dto.Titre)
			}
			continue
		}
		out = append(out, TrendingCluster{ID: dto.Titre, Mots: keywords})
	}
	return out
}

func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// clusterStore is TrendingClusterRepositoryImpl: the clusters are kept five minutes
// when there is at least one (an empty answer is requested again at the next call).
type clusterStore struct {
	a       *app.App
	fetcher clusterFetcher
}

// Clusters is getClusters().
func (c clusterStore) Clusters(ctx context.Context) []TrendingCluster {
	v, err := loadStoring(c.a.Cache, trendingClusterCacheName, trendingClusterCacheKey, shortTermTTL,
		func() ([]TrendingCluster, bool, error) {
			clusters := mapClusters(c.fetcher.Fetch(context.WithoutCancel(ctx)), func(titre string) {
				c.a.Log.Warn("[TrendingCluster] cluster '" + titre + "' ignoré car sa liste de mots clés est vide")
			})
			return clusters, len(clusters) > 0, nil
		})
	if err != nil {
		panic(err)
	}
	return v
}

// uncacheable carries a value that must be returned to the callers but not stored.
type uncacheable[T any] struct{ v T }

func (uncacheable[T]) Error() string { return "uncacheable" }

// loadStoring is cache.GetOrLoad for loads that decide whether their result may be stored
// (second result of load): errors and "do not store" results reach the callers that joined
// the load but are never kept.
func loadStoring[T any](c *cache.Cache, name, key string, ttl time.Duration, load func() (T, bool, error)) (T, error) {
	v, err := cache.GetOrLoad(c, name, key, ttl, func() (T, error) {
		v, store, err := load()
		if err != nil {
			return v, err
		}
		if !store {
			return v, uncacheable[T]{v}
		}
		return v, nil
	})
	var u uncacheable[T]
	if errors.As(err, &u) {
		return u.v, nil
	}
	return v, err
}
