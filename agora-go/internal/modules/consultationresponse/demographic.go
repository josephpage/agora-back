package consultationresponse

import (
	"context"

	"agora/internal/app"
	"agora/internal/modules/consultation"
)

// DemographicSource is GetConsultationResponseRepository (the demographic part).
type DemographicSource interface {
	ResponseCounter
	GetParticipantDemographicInfo(ctx context.Context, consultationID string) (*DemographicInfoCount, error)
	// getParticipantDemographicInfoByChoices of every question, in one round trip
	participantDemographicInfoByChoices(ctx context.Context, questionIDs []string) ([]*DemographicInfoCountByChoices, error)
}

// participantDemographicInfoByChoices is getParticipantDemographicInfoByChoices(questionId) for several
// questions: the 8 queries of every question are sent as one batch.
func (r *ResponseQueries) participantDemographicInfoByChoices(ctx context.Context, questionIDs []string) ([]*DemographicInfoCountByChoices, error) {
	if len(questionIDs) == 0 {
		return nil, nil
	}
	lists, err := r.queryByChoices(ctx, questionIDs)
	if err != nil {
		return nil, err
	}
	year := r.a.Now().Year()
	out := make([]*DemographicInfoCountByChoices, len(questionIDs))
	for i := range questionIDs {
		out[i] = toDemographicInfoCountByChoices(year, lists[i])
	}
	return out, nil
}

// GetConsultationResultsWithDemographicRatiosUseCase is usecase/consultationResponse/GetConsultationResultsWithDemographicRatiosUseCase
// (the admin export). Nothing is cached.
type GetConsultationResultsWithDemographicRatiosUseCase struct {
	a         *app.App
	info      InfoSource
	questions QuestionSource
	answered  ParticipantCounter
	responses DemographicSource
}

// GetConsultationResults is getConsultationResults(consultationId): nil for an unknown consultation (HTTP 400).
func (u *GetConsultationResultsWithDemographicRatiosUseCase) GetConsultationResults(ctx context.Context, consultationID string) (*ConsultationResultWithDemographicInfo, error) {
	u.a.Log.Info("📊 Building consultation results for consultationId: " + consultationID + "...")
	info := u.info.GetConsultation(ctx, consultationID)
	if info == nil {
		return nil, nil
	}
	var questionList []consultation.Question
	for _, q := range u.questions.GetConsultationQuestions(ctx, consultationID).Questions {
		switch q.Kind {
		case consultation.KindUniqueChoice, consultation.KindMultipleChoices, consultation.KindConditional:
			questionList = append(questionList, q)
		}
	}
	participantCount, err := u.answered.GetParticipantCount(ctx, consultationID)
	if err != nil {
		return nil, err
	}
	demographicInfo, err := u.responses.GetParticipantDemographicInfo(ctx, consultationID)
	if err != nil {
		return nil, err
	}
	consultationResponseList, err := u.responses.GetConsultationResponsesCount(ctx, consultationID)
	if err != nil {
		return nil, err
	}
	counts := indexCounts(consultationResponseList)

	var questions []consultation.Question
	var questionIDs []string
	for _, q := range questionList {
		if len(q.ChoixPossibleList) > 0 {
			mapped := toQuestionNoResponse(q)
			questions = append(questions, mapped)
			questionIDs = append(questionIDs, mapped.ID)
		}
	}
	byChoices, err := u.responses.participantDemographicInfoByChoices(ctx, questionIDs)
	if err != nil {
		return nil, err
	}
	results := make([]QuestionResultWithDemographicInfo, len(questions))
	for i, question := range questions {
		results[i] = buildDemographicQuestionResults(question, participantCount, counts, byChoices[i], demographicInfo)
	}
	return &ConsultationResultWithDemographicInfo{
		Consultation:     info,
		ParticipantCount: participantCount,
		Results:          results,
		Demographic:      buildDemographicInfo(demographicInfo, participantCount),
	}, nil
}

var emptyChoiceDemographic = emptyDemographicInfoCount()

func buildDemographicQuestionResults(question consultation.Question, participantCount int, counts map[countKey]int32,
	questionDemographicInfo *DemographicInfoCountByChoices, totalDemographicInfo *DemographicInfoCount) QuestionResultWithDemographicInfo {
	responses := make([]ChoiceResultWithDemographicInfo, len(question.ChoixPossibleList))
	for i, choix := range question.ChoixPossibleList {
		choiceCount := int(counts[countKey{question.ID, choix.ID}])
		choiceDemographic, ok := questionDemographicInfo.Choices[choix.ID]
		if !ok {
			choiceDemographic = emptyChoiceDemographic
		}
		responses[i] = ChoiceResultWithDemographicInfo{
			Choix:         choix,
			CountAndRatio: getCountAndRatio(choiceCount, participantCount),
			Demographic:   buildChoicesDemographicInfo(choiceDemographic, totalDemographicInfo, choiceCount),
		}
	}
	return QuestionResultWithDemographicInfo{Question: question, Responses: responses}
}

// buildDemographicInfo is buildDemographicInfo: the ratio of each value over the participants; the
// participants with an unknown value (null key) are the ones who are not counted under another
// value, profile-less participants included.
func buildDemographicInfo(count *DemographicInfoCount, participantCount int) *DemographicInfo {
	var out DemographicInfo
	for dim := range out {
		out[dim] = buildDataMap(count[dim], participantCount)
	}
	return &out
}

func buildDataMap(m *omap[int], participantCount int) *omap[CountAndRatio] {
	out := newOmap[CountAndRatio]()
	for _, key := range m.keys {
		count := m.vals[key]
		if key == noKey {
			count = participantCount - nonNullCount(m)
		}
		out.put(key, getCountAndRatio(count, participantCount))
	}
	return out
}

// nonNullCount is `entries.sumOf { (key, count) -> key?.let { count } ?: 0 }`.
func nonNullCount(m *omap[int]) int {
	sum := int32(0)
	for _, key := range m.keys {
		if key != noKey {
			sum += int32(m.vals[key]) // Int sum
		}
	}
	return int(sum)
}

// buildChoicesDemographicInfo is buildChoicesDemographicInfo: the ratio of a value for a choice is
// over the participants of the WHOLE consultation who have that value (0 when none: the ratio is
// then Infinity, or 0 for a count of 0).
func buildChoicesDemographicInfo(choice, total *DemographicInfoCount, choiceCount int) *DemographicInfo {
	var out DemographicInfo
	for dim := range out {
		out[dim] = buildChoicesDataMap(choice[dim], total[dim], choiceCount)
	}
	return &out
}

func buildChoicesDataMap(choice, total *omap[int], choiceCount int) *omap[CountAndRatio] {
	out := newOmap[CountAndRatio]()
	for _, key := range choice.keys {
		count := choice.vals[key]
		if key == noKey {
			count = choiceCount - nonNullCount(choice)
		}
		allCount := total.vals[key] // ?: 0
		out.put(key, getCountAndRatio(count, allCount))
	}
	return out
}

// getCountAndRatio is getCountAndRatio: `(count.toDouble() / allCount).takeUnless { it.isNaN() } ?: 0.0`.
func getCountAndRatio(count, allCount int) CountAndRatio {
	return CountAndRatio{Count: count, Ratio: ratioOrZero(int32(count), int32(allCount))}
}
