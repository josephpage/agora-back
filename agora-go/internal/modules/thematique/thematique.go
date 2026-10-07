// Package thematique ports the "thématiques" referential (GET /thematiques):
// ThematiqueController, ListThematiqueUseCase, ThematiqueRepositoryImpl,
// ThematiqueCacheRepository, ThematiqueStrapiRepository and the mappers.
//
// Other slices read thematiques through the exported Service (see Get):
//
//	t := thematique.Get(a).ByID(ctx, qag.ThematiqueID) // ThematiqueRepository.getThematique(id)
//	all := thematique.Get(a).List(ctx)                 // ThematiqueRepository.getThematiqueList()
//
// Kotlin cache: "thematiqueCache" (default RedisCacheManager, 1 hour), key
// "thematiqueList", filled only when the Strapi list is not empty. Go keeps the
// same rule in L1 (1 hour, FLUSHDB-aware), so a Strapi outage is retried at
// every call exactly like in Kotlin.
package thematique

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/jsonjava"
	"agora/internal/strapi"
)

const (
	cacheName = "thematique"
	cacheKey  = "thematiqueList"
	// cacheTTL is the default cache manager's entryTtl (Duration.ofHours(1)).
	cacheTTL = time.Hour
)

// Thematique is domain.Thematique.
type Thematique struct {
	ID    string
	Label string
	Picto string
}

// snapshot is one Strapi load, with the derived views computed once.
type snapshot struct {
	list   []Thematique // ThematiqueRepository.getThematiqueList() order (Strapi order)
	byID   map[string]int
	sorted []Thematique // ListThematiqueUseCase.getThematiqueList() order

	body    ThematiquesJSON // GET /thematiques body
	bodyRaw []byte          // its Jackson JSON
}

func newSnapshot(list []Thematique) *snapshot {
	s := &snapshot{list: list, byID: make(map[string]int, len(list))}
	for i, t := range list {
		// find { it.id == id } returns the FIRST match
		if _, dup := s.byID[t.ID]; !dup {
			s.byID[t.ID] = i
		}
	}
	s.sorted = sortThematiques(list)
	s.body = ToListJSON(s.sorted)
	s.bodyRaw = jsonjava.Marshal(s.body)
	return s
}

// listCache is ThematiqueCacheRepository.
type listCache interface {
	get() (*snapshot, bool)
	put(*snapshot)
}

// repository is ThematiqueRepositoryImpl.
type repository struct {
	cache listCache
	// fetch is ThematiqueStrapiRepository.getThematiques(): the decoded Strapi
	// data (empty on any Strapi failure).
	fetch func(ctx context.Context) []*strapiThematique
	log   *slog.Logger
	group singleflight.Group
}

// snapshot is getThematiqueList(): cache, else Strapi (cached when not empty).
func (r *repository) snapshot(ctx context.Context) *snapshot {
	if s, ok := r.cache.get(); ok {
		return s
	}
	v, _, _ := r.group.Do("list", func() (any, error) {
		if s, ok := r.cache.get(); ok {
			return s, nil
		}
		// the shared load must not die with the request that started it
		s := newSnapshot(toDomainList(r.fetch(context.WithoutCancel(ctx))))
		if len(s.list) > 0 {
			r.cache.put(s)
		}
		return s, nil
	})
	return v.(*snapshot)
}

// byID is getThematique(id): the first thematique with that id, nil (logged)
// when there is none.
func (r *repository) byID(ctx context.Context, id string) *Thematique {
	s := r.snapshot(ctx)
	i, ok := s.byID[id]
	if !ok {
		r.log.Error("Thematique id '" + id + "' non trouvée")
		return nil
	}
	t := s.list[i]
	return &t
}

// l1Cache keeps the snapshot in the process-wide cache.
type l1Cache struct{ a *app.App }

func (c l1Cache) get() (*snapshot, bool) {
	v, ok := c.a.Cache.Get(cacheName, cacheKey)
	if !ok {
		return nil, false
	}
	return v.(*snapshot), true
}

func (c l1Cache) put(s *snapshot) { c.a.Cache.Put(cacheName, cacheKey, s, cacheTTL) }

// Service gives access to the thematiques (ThematiqueRepository +
// ListThematiqueUseCase). Safe for concurrent use. The slices it returns are
// copies the caller may modify.
type Service struct {
	repo *repository
}

// Get returns the App-wide thematique service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "thematique", func() *Service {
		return &Service{repo: &repository{
			cache: l1Cache{a},
			fetch: func(ctx context.Context) []*strapiThematique {
				return strapi.Collection[*strapiThematique](ctx, a.Strapi, strapi.NewRequest("thematiques")).Data
			},
			log: a.Log,
		}}
	})
}

// List is ThematiqueRepository.getThematiqueList(): the thematiques in Strapi
// order (empty when Strapi fails). Used by the consultation / concertation /
// fiche inventaire mappers that look thematiques up by id.
func (s *Service) List(ctx context.Context) []Thematique {
	return append([]Thematique(nil), s.repo.snapshot(ctx).list...)
}

// Sorted is ListThematiqueUseCase.getThematiqueList(): by label, the "autre"
// thematique last. This is the order of GET /thematiques.
func (s *Service) Sorted(ctx context.Context) []Thematique {
	return append([]Thematique(nil), s.repo.snapshot(ctx).sorted...)
}

// ByID is ThematiqueRepository.getThematique(id): nil when unknown (an error is
// logged like in Kotlin). Unlike Kotlin (one list scan, and one Strapi call when
// the cache is empty, per call) the lookup is a map access on the cached list;
// the result is identical: the first thematique of the list with that id.
func (s *Service) ByID(ctx context.Context, id string) *Thematique {
	return s.repo.byID(ctx, id)
}

// handler is ThematiqueController.getThematiqueList.
func (s *Service) handler(c *httpx.Ctx) *httpx.Response {
	snap := s.repo.snapshot(c.Context())
	resp := cacheControl(c, httpx.OK(snap.body), 5*60)
	resp.PrecomputedJSON = snap.bodyRaw
	return resp
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
