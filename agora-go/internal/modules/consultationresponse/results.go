package consultationresponse

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation"
)

// resultsCacheName is the Kotlin cache "consultationResults" (default RedisCacheManager, 1 hour,
// evicted by every answer).
//
// Kotlin stored the results as a JSON String but could never read them back, except for a
// consultation without any question with choices (Jackson cannot rebuild the abstract
// QuestionWithChoices): the results were recomputed and written again at every request, and
// the only entry that was ever served was the "" marker of an unknown consultation (kept for
// one hour). The results are therefore data Kotlin does not cache: Go shares one computation
// for at most AGORA_MICROCACHE_TTL (5 s, decision #2 of DIVERGENCES.md), and the entry is also
// dropped by every answer on every instance (and by DeleteUsers / the aggregation through
// EvictConsultationResults) (class B, S6-B1 and S6-B2).
const resultsCacheName = "consultationResults"

// ParticipantCounter is UserAnsweredConsultationRepository.getParticipantCount.
type ParticipantCounter interface {
	GetParticipantCount(ctx context.Context, consultationID string) (int, error)
}

// AggregatedSource is ConsultationResultAggregatedRepository.getConsultationResultAggregated.
type AggregatedSource interface {
	GetConsultationResultAggregated(ctx context.Context, consultationID string) ([]ConsultationResultAggregated, error)
}

// ResponseCounter is GetConsultationResponseRepository.getConsultationResponsesCount.
type ResponseCounter interface {
	GetConsultationResponsesCount(ctx context.Context, consultationID string) ([]ResponseConsultationCount, error)
}

// ConsultationResultsUseCase is usecase/consultationResults/ConsultationResultsUseCase.
type ConsultationResultsUseCase struct {
	a          *app.App
	info       InfoSource
	questions  QuestionSource
	answered   ParticipantCounter
	aggregated AggregatedSource
	responses  ResponseCounter
	// ttl overrides AGORA_MICROCACHE_TTL (tests)
	ttl time.Duration
}

// GetConsultationResults is getConsultationResults(consultationId): nil for an unknown (or unpublished)
// consultation (HTTP 400). The entry is shared by every request until an answer, another
// instance's answer, or AGORA_MICROCACHE_TTL.
func (u *ConsultationResultsUseCase) GetConsultationResults(ctx context.Context, consultationID string) (*ConsultationResults, error) {
	ttl := u.a.Cfg.MicroCacheTTL
	if u.ttl != 0 {
		ttl = u.ttl
	}
	// a coexisting Kotlin backend can add answers Go never sees
	ttl = u.a.Cache.CoexistenceTTL(ttl)
	return cache.GetOrLoad(u.a.Cache, resultsCacheName, consultationID, ttl, func() (*ConsultationResults, error) {
		// the shared computation must not die with the request that started it
		return u.BuildConsultationResults(context.WithoutCancel(ctx), consultationID)
	})
}

// BuildConsultationResults is buildConsultationResults.
func (u *ConsultationResultsUseCase) BuildConsultationResults(ctx context.Context, consultationID string) (*ConsultationResults, error) {
	info := u.info.GetConsultation(ctx, consultationID)
	if info == nil {
		return nil, nil
	}
	questionList := u.questions.GetConsultationQuestions(ctx, consultationID).Questions
	participantCount, err := u.answered.GetParticipantCount(ctx, consultationID)
	if err != nil {
		return nil, err
	}
	var responseList []ResponseConsultationCount
	if len(questionList) > 0 {
		aggregated, err := u.aggregated.GetConsultationResultAggregated(ctx, consultationID)
		if err != nil {
			return nil, err
		}
		if len(aggregated) > 0 {
			responseList = make([]ResponseConsultationCount, len(aggregated))
			for i, a := range aggregated {
				responseList[i] = ResponseConsultationCount{QuestionID: a.QuestionID, ChoiceID: a.ChoiceID, ResponseCount: a.ResponseCount}
			}
		} else if responseList, err = u.responses.GetConsultationResponsesCount(ctx, consultationID); err != nil {
			return nil, err
		}
	}
	counts := indexCounts(responseList)

	results := &ConsultationResults{Consultation: info, ParticipantCount: participantCount}
	results.ResultsWithChoices = []QuestionResults{}
	results.OpenQuestions = []consultation.Question{}
	for _, q := range questionList {
		switch q.Kind {
		case consultation.KindUniqueChoice, consultation.KindMultipleChoices, consultation.KindConditional:
			if len(q.ChoixPossibleList) > 0 {
				results.ResultsWithChoices = append(results.ResultsWithChoices, buildQuestionResults(toQuestionNoResponse(q), participantCount, counts))
			}
		case consultation.KindOpen:
			results.OpenQuestions = append(results.OpenQuestions, q)
		}
	}
	return results, nil
}

// countKey identifies a (question, choice) pair.
type countKey struct{ question, choice string }

// indexCounts sums the counts per (question, choice): `filter { ... }.sumOf { it.responseCount }`.
func indexCounts(list []ResponseConsultationCount) map[countKey]int32 {
	m := make(map[countKey]int32, len(list))
	for _, c := range list {
		m[countKey{c.QuestionID, c.ChoiceID}] += int32(c.ResponseCount) // Int sum
	}
	return m
}

// buildQuestionResults is buildQuestionResults: the ratio of a choice is its count over the
// participants who did not answer "not applicable" to the question (a division by zero is
// Infinity, 0/0 is 0).
func buildQuestionResults(question consultation.Question, participantCount int, counts map[countKey]int32) QuestionResults {
	var notApplicableCount int32
	if participantCount > 0 {
		notApplicableCount = counts[countKey{question.ID, javacompat.NotApplicableChoiceUUID}]
	}
	participantCountWithoutNotApplicable := int32(participantCount) - notApplicableCount
	// TODO 20 mai 2025 (Kotlin): the seen ratio is disabled until the forms send the questions
	// that were seen but not answered
	const disableSeenRatio = 1.0

	responses := make([]ChoiceResults, len(question.ChoixPossibleList))
	for i, choix := range question.ChoixPossibleList {
		choiceCount := counts[countKey{question.ID, choix.ID}]
		responses[i] = ChoiceResults{Choix: choix, Ratio: ratioOrZero(choiceCount, participantCountWithoutNotApplicable)}
	}
	return QuestionResults{Question: question, Responses: responses, SeenRatio: disableSeenRatio}
}

// ratioOrZero is `(count.toDouble() / allCount).takeUnless { it.isNaN() } ?: 0.0`.
func ratioOrZero(count, allCount int32) float64 {
	r := float64(count) / float64(allCount)
	if r != r {
		return 0.0
	}
	return r
}

// toQuestionNoResponse is QuestionNoResponseMapper.toQuestionNoResponse: a unique or multiple choice
// question gets a last "Pas de réponse" choice (the skipped answers); a conditional question is kept.
func toQuestionNoResponse(q consultation.Question) consultation.Question {
	switch q.Kind {
	case consultation.KindUniqueChoice, consultation.KindMultipleChoices:
		choices := make([]consultation.ChoixPossible, 0, len(q.ChoixPossibleList)+1)
		choices = append(choices, q.ChoixPossibleList...)
		choices = append(choices, consultation.ChoixPossible{
			ID: javacompat.SkipQuestionChoiceUUID, Label: "Pas de réponse", Ordre: len(q.ChoixPossibleList) + 1,
			QuestionID: q.ID, HasOpenTextField: false,
		})
		q.ChoixPossibleList = choices
	}
	return q
}
