package consultationresponse

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/modules/consultation"
	"agora/internal/store"
)

// AggregatedRepository is ConsultationResultAggregatedRepositoryImpl +
// ConsultationResultAggregatedDatabaseRepository: the consultation_results table, which keeps the
// number of answers per (question, choice) of a finished consultation once its answers are deleted.
type AggregatedRepository struct{ a *app.App }

// GetAggregatedResultsConsultationIDs is getAggregatedResultsConsultationIds.
func (r *AggregatedRepository) GetAggregatedResultsConsultationIDs(ctx context.Context) ([]string, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT DISTINCT consultation_id FROM consultation_results")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != nil {
			out = append(out, *id)
		}
	}
	return out, rows.Err()
}

// GetConsultationResultAggregated is getConsultationResultAggregated(consultationId). A NULL
// consultation, question or choice id is a null in a Kotlin String property: a
// NullPointerException (HTTP 500).
func (r *AggregatedRepository) GetConsultationResultAggregated(ctx context.Context, consultationID string) ([]ConsultationResultAggregated, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT consultation_id, question_id, choice_id, response_count FROM consultation_results WHERE consultation_id = $1", consultationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConsultationResultAggregated{}
	for rows.Next() {
		var c, q, ch *string
		var n int32
		if err := rows.Scan(&c, &q, &ch, &n); err != nil {
			return nil, err
		}
		if c == nil || q == nil || ch == nil {
			panic("java.lang.NullPointerException: consultation_results row with a NULL id")
		}
		out = append(out, ConsultationResultAggregated{ConsultationID: *c, QuestionID: *q, ChoiceID: *ch, ResponseCount: int(n)})
	}
	return out, rows.Err()
}

// insertConsultationResultAggregated is insertConsultationResultAggregated (saveAll of the mapped rows):
// FAILURE (false) without any write when there is nothing to insert.
func (r *AggregatedRepository) insertConsultationResultAggregated(ctx context.Context, q store.Querier, results []ConsultationResultAggregated) (bool, error) {
	if len(results) == 0 {
		return false, nil
	}
	const perStatement = 9000
	for start := 0; start < len(results); start += perStatement {
		chunk := results[start:min(start+perStatement, len(results))]
		var sb strings.Builder
		sb.WriteString("INSERT INTO consultation_results (id, choice_id, consultation_id, question_id, response_count) VALUES ")
		args := make([]any, 0, len(chunk)*5)
		for i, res := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := len(args)
			sb.WriteString("($" + strconv.Itoa(n+1) + ",$" + strconv.Itoa(n+2) + ",$" + strconv.Itoa(n+3) + ",$" + strconv.Itoa(n+4) + ",$" + strconv.Itoa(n+5) + ")")
			args = append(args, randomUUID(), res.ChoiceID, res.ConsultationID, res.QuestionID, int32(res.ResponseCount))
		}
		if _, err := q.Exec(ctx, sb.String(), args...); err != nil {
			return false, err
		}
	}
	return true, nil
}

// deleteConsultationResponses is GetConsultationResponseRepositoryImpl.deleteConsultationResponses: the answers
// without text are deleted, then the user and participation ids of the others are replaced by
// the zero UUID (SQL copied verbatim).
func deleteConsultationResponses(ctx context.Context, q store.Querier, consultationID string) error {
	if _, err := q.Exec(ctx, `DELETE from reponses_consultation
            WHERE consultation_id = $1
            AND (response_text IS NULL OR response_text LIKE '')
            `, consultationID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE reponses_consultation
            SET user_id = '00000000-0000-0000-0000-000000000000', participation_id = '00000000-0000-0000-0000-000000000000'
            WHERE consultation_id = $1
            AND LENGTH(response_text) > 0
            `, consultationID)
	return err
}

// ConsultationSource lists the consultations to aggregate (ConsultationInfoRepository.getConsultationsToAggregate).
type ConsultationSource interface {
	GetConsultationsToAggregate(ctx context.Context) []consultation.ConsultationPreview
}

// AggregateConsultationResultUseCase is usecase/consultationAggregate/AggregateConsultationResultUseCase
// (with AggregateConsultationResultMapper.toInserting, an identity mapping of the counts).
type AggregateConsultationResultUseCase struct {
	a          *app.App
	responses  ResponseQueries
	aggregated *AggregatedRepository
	// afterAggregation runs once a consultation is aggregated (the cached results are dropped)
	afterAggregation func(ctx context.Context, consultationID string)
}

// AggregateConsultationResults is aggregateConsultationResults(consultationId): the number of answers per
// choice is copied to consultation_results, then the answers without text are deleted and the other
// ones anonymized. Kotlin did the three writes in three transactions; here they are one, so a failure
// leaves the consultation to be aggregated again the next day instead of half aggregated (class B).
func (u *AggregateConsultationResultUseCase) AggregateConsultationResults(ctx context.Context, consultationID string) error {
	counts, err := u.responses.GetConsultationResponsesCount(ctx, consultationID)
	if err != nil {
		return err
	}
	u.a.Log.Info("🧮 Aggregate results for consultation : " + strconv.Itoa(len(counts)) + " réponses trouvées pour la consultation " + consultationID)
	if len(counts) == 0 {
		return nil
	}
	inserting := make([]ConsultationResultAggregated, len(counts))
	for i, c := range counts {
		inserting[i] = ConsultationResultAggregated{ConsultationID: consultationID, QuestionID: c.QuestionID, ChoiceID: c.ChoiceID, ResponseCount: c.ResponseCount}
	}
	err = u.a.DB.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := u.aggregated.insertConsultationResultAggregated(ctx, tx, inserting); err != nil {
			return err
		}
		u.a.Log.Info("🧮 Aggregate results for consultation : suppression des réponses pour la consultation " + consultationID)
		return deleteConsultationResponses(ctx, tx, consultationID)
	})
	if err != nil {
		return err
	}
	if u.afterAggregation != nil {
		u.afterAggregation(ctx, consultationID)
	}
	return nil
}

// PickConsultationsToAggregateUseCase is usecase/consultationAggregate/PickConsultationsToAggregateUseCase.
type PickConsultationsToAggregateUseCase struct {
	a          *app.App
	info       ConsultationSource
	aggregated interface {
		GetAggregatedResultsConsultationIDs(ctx context.Context) ([]string, error)
	}
	aggregate interface {
		AggregateConsultationResults(ctx context.Context, consultationID string) error
	}
}

// AggregateConsultations is aggregateConsultations (the daily task): every consultation that ended 14 days
// ago and whose results are not aggregated yet is aggregated. An exception stops the loop.
func (u *PickConsultationsToAggregateUseCase) AggregateConsultations(ctx context.Context) error {
	toAggregate := u.info.GetConsultationsToAggregate(ctx)
	if len(toAggregate) == 0 {
		return nil
	}
	ids, err := u.aggregated.GetAggregatedResultsConsultationIDs(ctx)
	if err != nil {
		return err
	}
	done := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		done[id] = struct{}{}
	}
	for _, c := range toAggregate {
		if _, ok := done[c.ID]; ok {
			continue
		}
		u.a.Log.Info("🧮 Aggregate results for consultation: " + c.Title)
		if err := u.aggregate.AggregateConsultationResults(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}
