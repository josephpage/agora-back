// Package consultationresponse ports the consultation responses and results slice (S6): the
// submission of the answers of a user to a consultation
// (POST /consultations/{consultationId}/responses), the results of a consultation
// (GET /v2/consultations/{consultationId}/responses), the admin export with the demographic
// information of the participants (GET /admin/consultations/{consultationId}/responses, TSV) and
// the aggregation of the results of the finished consultations (daily task).
//
// Other slices use the exported Service (see Get):
//
//	s := consultationresponse.Get(a)
//	s.AggregateConsultations(ctx)                       PickConsultationsToAggregateUseCase.aggregateConsultations (daily task)
//	s.AggregateConsultationResults(ctx, consultationID) AggregateConsultationResultUseCase
//	s.DeleteUserConsultationResponses(ctx, userIDs)     GetConsultationResponseRepository.deleteUserConsultationResponses (DeleteUsersUseCase)
//	s.EvictConsultationResults / EvictAllConsultationResults   drop the cached results (after anything that changes them)
//
// See parity/ledger/S6.md for the complete list.
package consultationresponse

import (
	"context"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/modules/consultation"
	"agora/internal/modules/profile"
	"agora/internal/sanitize"
)

// Service gathers the S6 repositories and use cases.
type Service struct {
	a *app.App

	RateLimit   *RateLimiter
	Responses   *ResponseQueries
	Aggregated  *AggregatedRepository
	Inserter    *ResponseRepository
	Control     *ControlResponseConsultationUseCase
	Insert      *InsertReponseConsultationUseCase
	Results     *ConsultationResultsUseCase
	Demographic *GetConsultationResultsWithDemographicRatiosUseCase
	Aggregate   *AggregateConsultationResultUseCase
	Pick        *PickConsultationsToAggregateUseCase

	profiles *profile.Service
	tsv      tsvMapper
	queue    agoraQueue[insertResponseTask]
}

// Get returns the App-wide consultation responses service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "consultationresponse", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	cs := consultation.Get(a)
	s := &Service{
		a:          a,
		RateLimit:  newRateLimiter(a),
		Responses:  &ResponseQueries{a: a},
		Aggregated: &AggregatedRepository{a: a},
		Inserter:   &ResponseRepository{a: a},
		profiles:   profile.Get(a),
		tsv:        tsvMapper{nf: processNumberFormat()},
	}
	s.Control = newControlResponseConsultationUseCase(cs.Questions, a.Cfg.OpenQuestionMaxTextLength)
	s.Insert = &InsertReponseConsultationUseCase{
		now:                       a.Now,
		sanitize:                  sanitize.Sanitize,
		repository:                s.Inserter,
		answered:                  cs.Answered,
		questions:                 cs.Questions,
		info:                      cs.Info,
		openQuestionMaxTextLength: a.Cfg.OpenQuestionMaxTextLength,
		evict: func(ctx context.Context, consultationID, userID string) {
			cs.EvictHasAnswered(ctx, consultationID, userID)
			s.EvictConsultationResults(ctx, consultationID)
		},
	}
	s.Results = &ConsultationResultsUseCase{
		a: a, info: cs.Info, questions: cs.Questions, answered: cs.Answered, aggregated: s.Aggregated, responses: s.Responses,
	}
	s.Demographic = &GetConsultationResultsWithDemographicRatiosUseCase{
		a: a, info: cs.Info, questions: cs.Questions, answered: cs.Answered, responses: s.Responses,
	}
	s.Aggregate = &AggregateConsultationResultUseCase{a: a, responses: *s.Responses, aggregated: s.Aggregated}
	s.Pick = &PickConsultationsToAggregateUseCase{a: a, info: cs.Info, aggregated: s.Aggregated, aggregate: s.Aggregate}
	return s
}

// EvictConsultationResults drops the cached results of a consultation on every instance
// (ConsultationResultsCacheRepository.evictConsultationResultsCache), and the Kotlin entry while the
// Kotlin backend is still running.
func (s *Service) EvictConsultationResults(ctx context.Context, consultationID string) {
	s.a.Cache.Invalidate(ctx, resultsCacheName, consultationID)
	s.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(resultsCacheName, consultationID))
}

// EvictAllConsultationResults drops every cached result: to call after something that changes the
// results of several consultations at once (the deletion of users).
func (s *Service) EvictAllConsultationResults(ctx context.Context) {
	s.a.Cache.InvalidateAll(ctx, resultsCacheName)
	s.a.Cache.DeleteKotlinPattern(ctx, resultsCacheName+"::*")
}

// AggregateConsultations is PickConsultationsToAggregateUseCase.aggregateConsultations.
func (s *Service) AggregateConsultations(ctx context.Context) error {
	return s.Pick.AggregateConsultations(ctx)
}

// AggregateConsultationResults is AggregateConsultationResultUseCase.aggregateConsultationResults.
func (s *Service) AggregateConsultationResults(ctx context.Context, consultationID string) error {
	return s.Aggregate.AggregateConsultationResults(ctx, consultationID)
}

// DeleteUserConsultationResponses is GetConsultationResponseRepository.deleteUserConsultationResponses
// (DeleteUsersUseCase): the answers of the users are deleted and the cached results dropped.
func (s *Service) DeleteUserConsultationResponses(ctx context.Context, userIDs []string) error {
	err := s.Responses.DeleteUserConsultationResponses(ctx, userIDs)
	s.EvictAllConsultationResults(ctx)
	return err
}
