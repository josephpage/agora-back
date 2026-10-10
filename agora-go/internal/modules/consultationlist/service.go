// Package consultationlist ports the paginated consultation lists: the finished
// consultations (GET /consultations/finished/{pageNumber}) and the consultations a
// user answered (GET /consultations/answered/{pageNumber}), with their caches
// (infrastructure/consultationPaginated, usecase/consultationPaginated). The
// concertations (GET /concertations) are in the concertation module.
//
// Caches (parity/ledger/S5.md):
//
//   - "consultationsFinishedPaginated" (Kotlin: Redis, 1 hour, key "<territory>-<page>"):
//     the same keys in L1, 1 hour (5 seconds while the Kotlin backend runs, which clears
//     them every day without Go seeing it). Cleared by ConsultationCacheClearUseCase
//     (Service.ClearConsultationCaches), on every instance and, in coexistence, in Redis.
//   - "consultationsAnsweredPaginated<userId>" (Kotlin: Redis, 1 hour, one cache per user
//     emptied with a KEYS scan): ONE L1 entry per user, evicted by EvictAnswered, which
//     the user_answered_consultation repository calls by itself after every insert:
//     the consultation responses slice has nothing to call.
//
// Other slices:
//
//	s := consultationlist.Get(a)
//	s.EvictAnswered(ctx, userID)           the user's answered pages (automatic after an insert)
//	s.ClearConsultationCaches(ctx)         ConsultationCacheClearUseCase, including the finished pages
package consultationlist

import (
	"context"

	"agora/internal/app"
	"agora/internal/modules/consultation"
	"agora/internal/modules/profile"
	"agora/internal/modules/thematique"
)

// Service gathers the use cases of the slice.
type Service struct {
	a *app.App

	Finished    *FinishedUseCase
	Preferences *PreferencesUseCase
	Answered    *AnsweredUseCase

	consultations *consultation.Service
}

// Get returns the App-wide service. Building it registers its cache eviction with
// the consultation module (the daily clearing, the inserts of answers): every
// process that clears consultation caches (the web server, the daily task) must
// build it first, ClearConsultationCaches below does.
func Get(a *app.App) *Service {
	return app.Singleton(a, "consultationlist", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	c := consultation.Get(a)
	themes := thematique.Get(a)
	s := &Service{
		a:             a,
		consultations: c,
		Finished:      &FinishedUseCase{a: a, repo: c.PreviewFinished, themes: themes},
		Preferences:   &PreferencesUseCase{a: a, repo: c.PreviewFinished, themes: themes, profiles: profile.Get(a)},
		Answered:      &AnsweredUseCase{a: a, repo: c.PreviewAnswered, themes: themes},
	}
	// ConsultationCacheClearUseCase also empties the finished pages
	c.OnClearConsultationCaches(s.clearFinishedPages)
	// InsertReponseConsultationUseCase empties the user's answered pages (before the insert in
	// Kotlin; Go does it after the commit, so a read between the two cannot re-fill a stale page)
	c.Answered.OnInserted(s.EvictAnswered)
	return s
}

// clearFinishedPages is ConsultationFinishedPaginatedListCacheRepository.clearCache:
// every instance drops its pages, and the Kotlin keys go too while both backends run.
func (s *Service) clearFinishedPages(ctx context.Context) {
	s.a.Cache.InvalidateAll(ctx, finishedCacheName)
	s.a.Cache.DeleteKotlinPattern(ctx, finishedCacheName+"::*")
}

// ClearConsultationCaches is ConsultationCacheClearUseCase.clearConsultationCaches
// (the daily task): the details and Strapi lists of the consultation module and the
// finished pages. Prefer it to consultation.Service.ClearConsultationCaches: it
// guarantees that the finished pages are part of it.
func (s *Service) ClearConsultationCaches(ctx context.Context) {
	s.consultations.ClearConsultationCaches(ctx)
}

// EvictAnswered is ConsultationAnsweredPaginatedListCacheRepository.clearCache(userId):
// the answered pages of the user are dropped on every instance (one invalidation,
// no scan) and, while both backends run, in the Kotlin Redis cache of that user.
// The responses slice does not need to call it (answered.Repository.InsertUserAnsweredConsultation
// does); it is for any other path that changes what a user answered.
func (s *Service) EvictAnswered(ctx context.Context, userID string) {
	// the eviction must not be lost with a request that is gone
	ctx = context.WithoutCancel(ctx)
	s.a.Cache.Invalidate(ctx, answeredCacheName, userID)
	s.a.Cache.DeleteKotlinPattern(ctx, answeredCacheName+userID+"::*")
}
