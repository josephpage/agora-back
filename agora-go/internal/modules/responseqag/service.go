// Package responseqag ports the Government responses to the QaGs of the app:
// GET /qags/responses (the QaGs selected for a response and the five latest responses) and
// GET /qags/responses/{pageNumber} (the paginated list), with their use cases and mappers.
//
// The Strapi part (ResponseQagRepositoryImpl, ResponseQagStrapiRepository, ResponseQagMapper,
// StrapiResponseQagDTO) is shared with the QaG details: it lives in the qag module
// (qag.Service.Responses, GetResponsesQag / GetAllResponsesQag / GetResponsesTotal).
// Kotlin requests Strapi at every call; the responses are shared by every user, so Go
// keeps a non-empty answer for AGORA_MICROCACHE_TTL (class B, parity/divergences/S3.md).
package responseqag

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
)

const (
	cacheByIDs = "qagResponsesByIDs"
	cacheAll   = "qagResponsesAll"
	cacheTotal = "qagResponsesTotal"
)

// Service gathers the use cases of the slice.
type Service struct {
	a         *app.App
	Previews  *PreviewListUseCase
	Paginated *PaginatedListUseCase
}

// Get returns the App-wide service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "responseqag", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	q := qag.Get(a)
	themes := thematique.Get(a)
	repo := &repository{a: a, strapi: q.Responses}
	return &Service{
		a:         a,
		Previews:  &PreviewListUseCase{qags: q.Info, responses: repo, themes: themes, lowPriority: q.LowPriority},
		Paginated: &PaginatedListUseCase{responses: repo, qags: q.Info, themes: themes, log: a.Log},
	}
}

// strapiResponses is the Strapi side (qag.ResponseRepository).
type strapiResponses interface {
	GetResponsesQag(ctx context.Context, qagIDs []string) []qag.ResponseQag
	GetAllResponsesQag(ctx context.Context) []qag.ResponseQag
	GetResponsesTotal(ctx context.Context) int
}

// repository is ResponseQagRepositoryImpl on top of the Strapi repository.
type repository struct {
	a      *app.App
	strapi strapiResponses
}

func (r *repository) ttl() time.Duration { return r.a.Cfg.MicroCacheTTL }

// shared loads a Strapi answer once at a time and keeps a non-empty one for AGORA_MICROCACHE_TTL
// (an empty answer may be a Strapi outage that Kotlin requests again at every call; a panic of the
// mapper is never kept).
func shared[T any](ctx context.Context, r *repository, name, key string, load func(ctx context.Context) T, keep func(T) bool) T {
	ttl := r.ttl()
	if ttl <= 0 {
		return load(ctx)
	}
	v, err := cache.GetOrLoad(r.a.Cache, name, key, ttl, func() (T, error) {
		v := load(context.WithoutCancel(ctx))
		if !keep(v) {
			return v, notKept[T]{v}
		}
		return v, nil
	})
	var nk notKept[T]
	if errors.As(err, &nk) {
		return nk.v
	}
	if err != nil {
		panic(err)
	}
	return v
}

// notKept carries a result that is returned to the callers of the load but not stored.
type notKept[T any] struct{ v T }

func (notKept[T]) Error() string { return "not kept" }

// GetResponsesQag is getResponsesQag(qagIds).
func (r *repository) GetResponsesQag(ctx context.Context, qagIDs []string) []qag.ResponseQag {
	if len(qagIDs) == 0 {
		return r.strapi.GetResponsesQag(ctx, qagIDs)
	}
	return shared(ctx, r, cacheByIDs, strings.Join(qagIDs, ","),
		func(ctx context.Context) []qag.ResponseQag { return r.strapi.GetResponsesQag(ctx, qagIDs) },
		func(l []qag.ResponseQag) bool { return len(l) > 0 })
}

// all is `strapiRepository.getResponsesQag().let(mapper::toDomain)`: the list is shared, do not modify it.
func (r *repository) all(ctx context.Context) []qag.ResponseQag {
	return shared(ctx, r, cacheAll, "all",
		func(ctx context.Context) []qag.ResponseQag { return r.strapi.GetAllResponsesQag(ctx) },
		func(l []qag.ResponseQag) bool { return len(l) > 0 })
}

// GetResponsesQagCount is getResponsesQagCount(minDate): the number of responses from minDate on, or
// the total reported by Strapi when there is no minDate.
func (r *repository) GetResponsesQagCount(ctx context.Context, minDate *int64) int {
	if minDate == nil {
		return shared(ctx, r, cacheTotal, "total",
			func(ctx context.Context) int { return r.strapi.GetResponsesTotal(ctx) },
			func(n int) bool { return n > 0 })
	}
	n := 0
	for _, resp := range r.all(ctx) {
		if resp.ResponseDateMillis() >= *minDate {
			n++
		}
	}
	return n
}

// GetResponsesQagPage is getResponsesQag(from, pageSize, minDate): the responses from minDate on,
// most recent first, from index `from`. A negative `from` fails like List.subList (HTTP 500).
func (r *repository) GetResponsesQagPage(ctx context.Context, from, pageSize int, minDate *int64) []qag.ResponseQag {
	var list []qag.ResponseQag
	for _, resp := range r.all(ctx) {
		if minDate == nil || resp.ResponseDateMillis() >= *minDate {
			list = append(list, resp)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].ResponseDateMillis() > list[j].ResponseDateMillis() })
	toIndex := len(list)
	if end := int(int32(from) + int32(pageSize)); end < toIndex { // Int arithmetic
		toIndex = end
	}
	if from > toIndex {
		return nil
	}
	if from < 0 {
		panic("java.lang.IndexOutOfBoundsException: fromIndex = " + strconv.Itoa(from))
	}
	return list[from:toIndex]
}
