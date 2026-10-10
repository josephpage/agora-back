package consultationlist

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/domain"
	"agora/internal/modules/consultation"
	"agora/internal/modules/profile"
)

// Names of the Kotlin caches (ConsultationsFinishedPaginatedListCacheRepositoryImpl and
// ConsultationsAnsweredPaginatedListCacheRepositoryImpl). Both live in the default
// RedisCacheManager: one hour.
const (
	finishedCacheName = "consultationsFinishedPaginated"
	answeredCacheName = "consultationsAnsweredPaginated"
	cacheTTL          = time.Hour

	// preferencesCacheName is the micro-cache of the list behind
	// GET /consultations/finished/{n} without territory (S5-B2): Kotlin has none.
	preferencesCacheName = "consultationsFinishedByPreferences"

	maxFinishedPageListSize = 100
	maxAnsweredPageListSize = 20
)

// ---------------------------------------------------------------------------
// ConsultationsFinishedPaginatedListUseCase
// ---------------------------------------------------------------------------

// FinishedList is ConsultationFinishedPaginatedList. A value is shared between
// requests (it is cached) and must never be modified.
type FinishedList struct {
	Consultations []consultation.ConsultationPreviewFinished
	MaxPageNumber int
}

// AnsweredList is ConsultationAnsweredPaginatedList (shared, never modified).
type AnsweredList struct {
	Consultations []consultation.ConsultationPreviewFinished
	MaxPageNumber int
}

// thematiqueLister is ThematiqueRepository.getThematiqueList.
type thematiqueLister interface {
	List(ctx context.Context) []consultation.Thematique
}

// finishedRepository is ConsultationPreviewFinishedRepository.
type finishedRepository interface {
	GetConsultationFinishedCount(ctx context.Context) int
	GetConsultationFinishedListPage(ctx context.Context, offset, pageSize int, territory domain.Territoire) []consultation.ConsultationWithUpdateInfo
	GetConsultationFinishedList(ctx context.Context, territories []domain.Territoire) []consultation.ConsultationWithUpdateInfo
}

// answeredRepository is ConsultationPreviewAnsweredRepository.
type answeredRepository interface {
	GetConsultationAnsweredCount(ctx context.Context, userID string) (int, error)
	GetConsultationAnsweredList(ctx context.Context, userID string, offset int) ([]consultation.ConsultationWithUpdateInfo, error)
}

// kotlinOffset is `(pageNumber - 1) * pageSize` in Kotlin Int arithmetic: the
// product wraps around on overflow (page 21 474 838 of 100 consultations).
func kotlinOffset(pageNumber, pageSize int) int {
	return int(int32(pageNumber-1) * int32(pageSize))
}

// maxPageNumber is `ceil(count.toDouble() / pageSize.toDouble()).toInt()`.
func maxPageNumber(count, pageSize int) int {
	return int(math.Ceil(float64(count) / float64(pageSize)))
}

// errNoPage marks a page that does not exist (the use case returns null): it is
// returned to the callers that joined the load and never cached.
var errNoPage = errors.New("no such page")

// FinishedUseCase is ConsultationsFinishedPaginatedListUseCase with its cache
// repository (ConsultationsFinishedPaginatedListCacheRepositoryImpl): one entry
// per (territory, page) for an hour, like the Kotlin cache.
type FinishedUseCase struct {
	a      *app.App
	repo   finishedRepository
	themes thematiqueLister
}

// ttl is the Kotlin TTL. While the Kotlin backend still runs, its daily clearing
// of the finished lists (ConsultationCacheClearUseCase) cannot be observed:
// the entries then live at most 5 seconds (CoexistenceTTL).
func (u *FinishedUseCase) ttl() time.Duration { return u.a.Cache.CoexistenceTTL(cacheTTL) }

// GetConsultationFinishedPaginatedList is getConsultationFinishedPaginatedList:
// nil when the page does not exist. An unknown territory is a
// *domain.InvalidTerritoryError (HTTP 400), but only for a page > 0.
func (u *FinishedUseCase) GetConsultationFinishedPaginatedList(ctx context.Context, pageNumber int, inputTerritory string) (*FinishedList, error) {
	if pageNumber <= 0 {
		return nil, nil
	}
	territory, err := domain.TerritoireFrom(inputTerritory)
	if err != nil {
		return nil, err
	}
	key := territory.Value() + "-" + strconv.Itoa(pageNumber)
	page, err := cache.GetOrLoad(u.a.Cache, finishedCacheName, key, u.ttl(), func() (*FinishedList, error) {
		// the shared load must not die with the request that started it
		ctx := context.WithoutCancel(ctx)
		consultationsCount := u.repo.GetConsultationFinishedCount(ctx)
		offset := kotlinOffset(pageNumber, maxFinishedPageListSize)
		if offset > consultationsCount {
			return nil, errNoPage
		}
		thematiques := u.themes.List(ctx)
		infos := u.repo.GetConsultationFinishedListPage(ctx, offset, maxFinishedPageListSize, territory)
		return &FinishedList{
			Consultations: consultation.ToConsultationPreviewFinished(infos, thematiques),
			MaxPageNumber: maxPageNumber(consultationsCount, maxFinishedPageListSize),
		}, nil
	})
	if errors.Is(err, errNoPage) {
		return nil, nil
	}
	return page, err
}

// ---------------------------------------------------------------------------
// ConsultationsByUserPreferencesUseCase
// ---------------------------------------------------------------------------

// profileReader is ProfileRepository.getProfile.
type profileReader interface {
	GetProfile(ctx context.Context, userID string) (*profile.Profile, error)
}

// PreferencesUseCase is ConsultationsByUserPreferencesUseCase: the finished
// consultations for the territories of the user's profile. Territoire.of(profile)
// is `emptyList()` (the territories are not used yet), so the list is the same for
// everybody: it is shared for AGORA_MICROCACHE_TTL. The profile is still read.
type PreferencesUseCase struct {
	a        *app.App
	repo     finishedRepository
	themes   thematiqueLister
	profiles profileReader
}

// Execute is execute(): the list of every finished consultation. The returned
// slice is shared and must not be modified.
func (u *PreferencesUseCase) Execute(ctx context.Context, userID string) ([]consultation.ConsultationPreviewFinished, error) {
	// an unreadable profile is an exception (HTTP 500)
	if _, err := u.profiles.GetProfile(ctx, userID); err != nil {
		return nil, err
	}
	return loadShared(ctx, u.a, preferencesCacheName, "all", func(ctx context.Context) ([]consultation.ConsultationPreviewFinished, bool) {
		var territoires []domain.Territoire // Territoire.of(userProfile) = emptyList()
		thematiques := u.themes.List(ctx)
		list := consultation.ToConsultationPreviewFinished(u.repo.GetConsultationFinishedList(ctx, territoires), thematiques)
		return list, len(list) > 0
	}), nil
}

// loadShared is the micro-cache of shared Strapi data: one load at a time, kept
// for AGORA_MICROCACHE_TTL when the loader says it is worth keeping (never an
// empty list, which may be a Strapi failure).
func loadShared[T any](ctx context.Context, a *app.App, name, key string, load func(ctx context.Context) (T, bool)) T {
	v, err := cache.GetOrLoad(a.Cache, name, key, a.Cfg.MicroCacheTTL, func() (T, error) {
		// the shared load must not die with the request that started it
		v, store := load(context.WithoutCancel(ctx))
		if !store {
			return v, notStored[T]{v}
		}
		return v, nil
	})
	var u notStored[T]
	if errors.As(err, &u) {
		return u.v
	}
	return v
}

// notStored carries a result that must be returned but not cached.
type notStored[T any] struct{ v T }

func (notStored[T]) Error() string { return "not stored" }

// ---------------------------------------------------------------------------
// ConsultationsAnsweredPaginatedListUseCase
// ---------------------------------------------------------------------------

// AnsweredUseCase is ConsultationsAnsweredPaginatedListUseCase with its cache
// repository (ConsultationsAnsweredPaginatedListCacheRepositoryImpl).
//
// Every page of a user shows the same consultations: the Kotlin repository ignores
// the offset and returns all of the user's answered consultations, so the pages
// only differ by whether they exist (offset <= count). The cache therefore keeps
// ONE entry per user (key = the user id, 1 hour, evicted at once by EvictAnswered)
// instead of one Redis key per page and a KEYS scan to clear them. The entry holds
// the count (always read first, as Kotlin does) and, filled by the first page that
// exists, the list. A page that does not exist never reaches Strapi.
type AnsweredUseCase struct {
	a      *app.App
	repo   answeredRepository
	themes thematiqueLister
}

func (u *AnsweredUseCase) ttl() time.Duration { return u.a.Cache.CoexistenceTTL(cacheTTL) }

// answeredEntry is what the cache holds for one user.
type answeredEntry struct {
	count int // getConsultationAnsweredCount, when the entry was created

	mu     sync.Mutex
	loaded bool
	list   *AnsweredList
}

// GetConsultationAnsweredPaginatedList is getConsultationAnsweredPaginatedList:
// nil when the page does not exist.
func (u *AnsweredUseCase) GetConsultationAnsweredPaginatedList(ctx context.Context, userID string, pageNumber int) (*AnsweredList, error) {
	if pageNumber <= 0 {
		return nil, nil
	}
	e, err := cache.GetOrLoad(u.a.Cache, answeredCacheName, userID, u.ttl(), func() (*answeredEntry, error) {
		// the shared load must not die with the request that started it
		count, err := u.repo.GetConsultationAnsweredCount(context.WithoutCancel(ctx), userID)
		if err != nil {
			return nil, err
		}
		return &answeredEntry{count: count}, nil
	})
	if err != nil {
		return nil, err
	}
	offset := kotlinOffset(pageNumber, maxAnsweredPageListSize)
	if offset > e.count {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.loaded {
		list, err := u.buildList(ctx, userID, offset, e.count)
		if err != nil {
			return nil, err
		}
		e.list, e.loaded = list, true
	}
	return e.list, nil
}

func (u *AnsweredUseCase) buildList(ctx context.Context, userID string, offset, count int) (*AnsweredList, error) {
	var infos []consultation.ConsultationWithUpdateInfo
	if count != 0 { // no answer: nothing to read, as the Kotlin repository finds no consultation id
		var err error
		if infos, err = u.repo.GetConsultationAnsweredList(ctx, userID, offset); err != nil {
			return nil, err
		}
	}
	thematiques := u.themes.List(ctx)
	return &AnsweredList{
		Consultations: consultation.ToConsultationPreviewFinished(infos, thematiques),
		MaxPageNumber: maxPageNumber(count, maxAnsweredPageListSize),
	}, nil
}
