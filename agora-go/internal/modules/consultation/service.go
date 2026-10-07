// Package consultation ports the consultation details slice: the consultation
// details (GET /v2/consultations/{id}, GET /v2/consultations/{id}/updates/{updateId}
// and their /api/public twins), the consultation preview (GET /consultations), the
// questions (GET /consultations/{id}/questions) and the feedback on an update
// (POST / DELETE /consultations/{id}/updates/{updateId}/feedback), with the
// Strapi repository and the caches the consultation lists (S5) and the
// consultation responses / results (S6) reuse.
//
// Other slices use the exported Service (see Get):
//
//	s := consultation.Get(a)
//	s.Info.GetConsultation(ctx, id)                       ConsultationInfoRepository.getConsultation (cache "consultationCache")
//	s.Info.GetConsultationByIDOrSlug[WithUnpublished]
//	s.Info.GetOngoingConsultations / GetFinishedConsultations / GetAnsweredConsultations ...
//	s.Questions.GetConsultationQuestions(ctx, id)         QuestionRepository (S6 validates the answers with it)
//	s.Answered.HasAnsweredConsultation / GetParticipantCount / InsertUserAnsweredConsultation ...
//	s.PreviewFinished / s.PreviewAnswered                 the repositories of the paginated lists
//	s.ClearConsultationCaches(ctx)                        ConsultationCacheClearUseCase (daily task)
//	s.EvictHasAnswered(ctx, consultationID, userID)       what InsertReponseConsultationUseCase evicts
//
// See parity/ledger/S4.md for the complete list.
package consultation

import (
	"context"
	"os"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/modules/consultation/answered"
	"agora/internal/modules/login"
	"agora/internal/modules/profile"
)

// Service gathers the S4 repositories and use cases.
type Service struct {
	a *app.App

	Strapi    *StrapiRepository
	Info      *InfoRepository
	Updates   *UpdateRepository
	History   *HistoryRepository
	Answered  *answered.Repository
	Feedbacks *FeedbackRepository
	Questions *QuestionRepository

	PreviewFinished *PreviewFinishedRepository
	PreviewAnswered *PreviewAnsweredRepository

	Details  *DetailsUseCase
	Feedback *FeedbackUseCase
	Preview  *PreviewUseCase

	queue         agoraQueue[feedbackTask]
	universalLink string
	clearHooks    []func(ctx context.Context)
}

// Get returns the App-wide consultation service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "consultation", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	lg := login.Get(a)
	ans := answered.Get(a)
	strapi := newStrapiRepository(a)
	mapper := infoMapper{log: a.Log}
	info := &InfoRepository{a: a, strapi: strapi, answered: ans, mapper: mapper}
	updates := &UpdateRepository{a: a, strapi: strapi}
	history := &HistoryRepository{a: a, strapi: strapi}
	feedbacks := &FeedbackRepository{a: a}
	// UNIVERSAL_LINK_URL is read with System.getenv: a missing variable is the string "null"
	universalLink := "null"
	if v, ok := os.LookupEnv("UNIVERSAL_LINK_URL"); ok {
		universalLink = v
	}
	return &Service{
		a:         a,
		Strapi:    strapi,
		Info:      info,
		Updates:   updates,
		History:   history,
		Answered:  ans,
		Feedbacks: feedbacks,
		Questions: &QuestionRepository{a: a, strapi: strapi},

		PreviewFinished: &PreviewFinishedRepository{a: a, strapi: strapi, mapper: mapper},
		PreviewAnswered: &PreviewAnsweredRepository{a: a, strapi: strapi, answered: ans, mapper: mapper},

		Details:  &DetailsUseCase{a: a, flags: lg.Flags, info: info, updates: updates, answered: ans, feedback: feedbacks, history: history},
		Feedback: &FeedbackUseCase{a: a, flags: lg.Flags, feedback: feedbacks, updates: updates},
		Preview:  &PreviewUseCase{info: info, profiles: profile.Get(a)},

		universalLink: universalLink,
	}
}

// OnClearConsultationCaches registers a function run by ClearConsultationCaches
// (the consultation lists of S5 register the clearing of their paginated cache,
// which ConsultationCacheClearUseCase also calls).
func (s *Service) OnClearConsultationCaches(f func(ctx context.Context)) {
	s.clearHooks = append(s.clearHooks, f)
}

// ClearConsultationCaches is ConsultationCacheClearUseCase.clearConsultationCaches
// (the daily task): the cached latest details and the Strapi lists, on every
// instance, and the Kotlin keys while the two backends run together.
func (s *Service) ClearConsultationCaches(ctx context.Context) {
	s.a.Cache.InvalidateAll(ctx, latestDetailsCacheName)
	s.a.Cache.InvalidateAll(ctx, ongoingListCacheName)
	s.a.Cache.InvalidateAll(ctx, finishedListCacheName)
	s.a.Cache.DeleteKotlinPattern(ctx, latestDetailsCacheName+"::*")
	s.a.Cache.DeleteKotlinPattern(ctx, ongoingListCacheName+"::*")
	s.a.Cache.DeleteKotlinPattern(ctx, finishedListCacheName+"::*")
	for _, f := range s.clearHooks {
		f(ctx)
	}
}

// EvictHasAnswered is ConsultationDetailsV2CacheRepository.evictHasAnsweredConsultation,
// called by InsertReponseConsultationUseCase. Go does not cache "has answered"
// (it is one indexed query per request): only the Kotlin key is dropped, while a
// Kotlin instance may still be running (Kotlin never reads that cache either).
func (s *Service) EvictHasAnswered(ctx context.Context, consultationID, userID string) {
	s.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey("hasAnsweredConsultationDetailsV2", consultationID+"/"+userID))
}
