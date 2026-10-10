package consultationresponse

import (
	"context"
	"crypto/rand"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultation/answered"
	"agora/internal/store"
)

// InsertResult is InsertReponseConsultationRepository.InsertResult.
type InsertResult int

// The InsertResult constants.
const (
	InsertSuccess InsertResult = iota
	InsertFailure
)

// InsertParameters is InsertReponseConsultationRepository.InsertParameters.
type InsertParameters struct {
	ConsultationID  string
	UserID          string
	ParticipationID string
}

// ResponseInserter is InsertReponseConsultationRepository.
type ResponseInserter interface {
	InsertConsultationResponses(ctx context.Context, p InsertParameters, responses []ReponseConsultationInserting) (InsertResult, error)
}

// ---------------------------------------------------------------------------
// InsertReponseConsultationRepositoryImpl + ReponseConsultationMapper.toDto +
// ReponseConsultationDatabaseRepository.saveAll
// ---------------------------------------------------------------------------

// responseRow is a ReponseConsultationDTO.
type responseRow struct {
	id             string
	consultationID string
	questionID     string
	choiceID       *string
	responseText   string
}

// toRows is ReponseConsultationMapper.toDto(consultationId, userId, participationId, domain): one row per
// choice id (a null element gives a row without choice), or ONE row without choice when the
// list of choice ids is null or empty. Every row repeats the text of the answer.
func toRows(consultationID string, r ReponseConsultationInserting, newID func() string) []responseRow {
	if r.ChoiceIDsNil || len(r.ChoiceIDs) == 0 {
		return []responseRow{{id: newID(), consultationID: consultationID, questionID: r.QuestionID, responseText: r.ResponseText}}
	}
	out := make([]responseRow, len(r.ChoiceIDs))
	for i, c := range r.ChoiceIDs {
		out[i] = responseRow{id: newID(), consultationID: consultationID, questionID: r.QuestionID, choiceID: c, responseText: r.ResponseText}
	}
	return out
}

// maxRowsPerStatement keeps a multi-row INSERT under the 65535 parameters of the protocol
// (7 per row).
const maxRowsPerStatement = 9000

// ResponseRepository is InsertReponseConsultationRepositoryImpl.
//
// The never-read `reponseConsultationCache` (Kotlin appended the saved rows to a Redis list
// that grew with every answer and that nothing reads) is not reproduced (decided, class B).
type ResponseRepository struct{ a *app.App }

// InsertConsultationResponses is insertConsultationResponses: the user id and the participation id are
// read with UUID.fromString (the invalid one is a Failure, before anything is written) and
// the rows are saved by ONE multi-row INSERT (saveAll is one transaction in Kotlin: all or
// none). No row at all is a Success without any write.
func (r *ResponseRepository) InsertConsultationResponses(ctx context.Context, p InsertParameters, responses []ReponseConsultationInserting) (InsertResult, error) {
	userUUID, ok := javacompat.ParseUUID(p.UserID)
	if !ok {
		return InsertFailure, nil
	}
	participationUUID, ok := javacompat.ParseUUID(p.ParticipationID)
	if !ok {
		return InsertFailure, nil
	}
	var rows []responseRow
	for _, response := range responses {
		rows = append(rows, toRows(p.ConsultationID, response, randomUUID)...)
	}
	if len(rows) == 0 {
		return InsertSuccess, nil
	}
	// participationDate = Date() (java.util.Date: milliseconds)
	date := store.Millis(r.a.Now())
	insert := func(q store.Querier, chunk []responseRow) error {
		var sb strings.Builder
		sb.WriteString("INSERT INTO reponses_consultation (id, choice_id, consultation_id, participation_date, participation_id, question_id, response_text, user_id) VALUES ")
		args := make([]any, 0, len(chunk)*8)
		for i, row := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := len(args)
			sb.WriteString("($" + strconv.Itoa(n+1) + ",$" + strconv.Itoa(n+2) + ",$" + strconv.Itoa(n+3) + ",$" + strconv.Itoa(n+4) +
				",$" + strconv.Itoa(n+5) + ",$" + strconv.Itoa(n+6) + ",$" + strconv.Itoa(n+7) + ",$" + strconv.Itoa(n+8) + ")")
			args = append(args, row.id, row.choiceID, row.consultationID, date, participationUUID.String(), row.questionID, row.responseText, userUUID.String())
		}
		_, err := q.Exec(ctx, sb.String(), args...)
		return err
	}
	if len(rows) <= maxRowsPerStatement {
		// one statement: atomic by itself, one round trip
		return InsertSuccess, insert(r.a.DB.Pool, rows)
	}
	err := r.a.DB.Tx(ctx, func(tx pgx.Tx) error {
		for start := 0; start < len(rows); start += maxRowsPerStatement {
			end := min(start+maxRowsPerStatement, len(rows))
			if err := insert(tx, rows[start:end]); err != nil {
				return err
			}
		}
		return nil
	})
	return InsertSuccess, err
}

// randomUUID is UUID.randomUUID() (the id Hibernate's merge gets from the mapper): a random version 4
// UUID in canonical lowercase form.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return javacompat.UUIDFromBytes(b).String()
}

// ---------------------------------------------------------------------------
// InsertReponseConsultationUseCase
// ---------------------------------------------------------------------------

// InfoSource is ConsultationInfoRepository.getConsultation (by id, published consultations).
type InfoSource interface {
	GetConsultation(ctx context.Context, consultationID string) *consultation.ConsultationInfo
}

// AnsweredStore is the part of UserAnsweredConsultationRepository the use case needs.
type AnsweredStore interface {
	HasAnsweredConsultation(ctx context.Context, consultationID, userID string) (bool, error)
	InsertUserAnsweredConsultation(ctx context.Context, in answered.UserAnswered) (answered.Result, error)
}

// otherQuestionMaxLength is InsertReponseConsultationUseCase.OTHER_QUESTION_MAX_LENGTH.
const otherQuestionMaxLength = 200

// InsertReponseConsultationUseCase is usecase/consultationResponse/InsertReponseConsultationUseCase.
type InsertReponseConsultationUseCase struct {
	now                       func() time.Time
	sanitize                  func(content string, maxLength int) string
	repository                ResponseInserter
	answered                  AnsweredStore
	questions                 QuestionSource
	info                      InfoSource
	openQuestionMaxTextLength int
	// evict runs once the answer is written (or refused): what Kotlin evicts BEFORE its insert
	// (the answers of the user, "has answered", the cached results of the consultation).
	evict func(ctx context.Context, consultationID, userID string)
}

// InsertReponseConsultation is insertReponseConsultation. A consultation whose end date is passed, or that
// the user already answered, is a Failure (HTTP 400); an unknown consultation is not (it has no end date).
func (u *InsertReponseConsultationUseCase) InsertReponseConsultation(ctx context.Context, consultationID, userID string, consultationResponses []ReponseConsultationInserting) (InsertResult, error) {
	if info := u.info.GetConsultation(ctx, consultationID); info != nil && info.EndDate.Before(consultation.FromTime(u.now())) {
		return InsertFailure, nil // this consultation is already finished
	}
	has, err := u.answered.HasAnsweredConsultation(ctx, consultationID, userID)
	if err != nil {
		return InsertFailure, err
	}
	if has {
		return InsertFailure, nil // user has already answered this consultation
	}

	questionList := u.questions.GetConsultationQuestions(ctx, consultationID).Questions
	filled := addMissingResponseIfQuestionWithChoices(consultationResponses, questionList)

	// The cached results, "has answered" and the answered lists describe the state BEFORE the
	// answer: they are dropped once the rows are written (Kotlin dropped them before, so a
	// read in between could cache the old state again).
	defer u.evict(ctx, consultationID, userID)

	result, err := u.repository.InsertConsultationResponses(ctx, InsertParameters{
		ConsultationID: consultationID, UserID: userID, ParticipationID: randomUUID(),
	}, u.sanitizeConsultationResponse(filled, questionList))
	if err != nil {
		return InsertFailure, err
	}
	if result == InsertSuccess {
		// the answers of the user are evicted by the repository hook (consultationlist)
		if _, err := u.answered.InsertUserAnsweredConsultation(ctx, answered.UserAnswered{UserID: userID, ConsultationID: consultationID}); err != nil {
			return InsertFailure, err
		}
	}
	return result, nil
}

// addMissingResponseIfQuestionWithChoices: for each question of the consultation (in its order) the
// answer of the user, else a "not applicable" answer when the question has choices; the other
// questions without an answer are skipped, answers to unknown questions are dropped.
func addMissingResponseIfQuestionWithChoices(responses []ReponseConsultationInserting, questions []consultation.Question) []ReponseConsultationInserting {
	byQuestion := make(map[string]int, len(responses))
	for i, r := range responses {
		if _, ok := byQuestion[r.QuestionID]; !ok {
			byQuestion[r.QuestionID] = i // find returns the first answer
		}
	}
	out := make([]ReponseConsultationInserting, 0, len(questions))
	for _, q := range questions {
		if i, ok := byQuestion[q.ID]; ok {
			out = append(out, responses[i])
			continue
		}
		switch q.Kind {
		case consultation.KindUniqueChoice, consultation.KindMultipleChoices, consultation.KindConditional:
			notApplicable := javacompat.NotApplicableChoiceUUID
			out = append(out, ReponseConsultationInserting{QuestionID: q.ID, ChoiceIDs: []*string{&notApplicable}, ResponseText: ""})
		}
	}
	return out
}

// sanitizeConsultationResponse sanitizes the text of the answers: the open questions keep
// OPEN_QUESTION_MAX_TEXT_LENGTH characters (UTF-16 units), the other questions 200.
func (u *InsertReponseConsultationUseCase) sanitizeConsultationResponse(responses []ReponseConsultationInserting, questions []consultation.Question) []ReponseConsultationInserting {
	kinds := make(map[string]consultation.QuestionKind, len(questions))
	for _, q := range questions {
		if _, ok := kinds[q.ID]; !ok {
			kinds[q.ID] = q.Kind
		}
	}
	out := make([]ReponseConsultationInserting, len(responses))
	for i, r := range responses {
		maxLength := otherQuestionMaxLength
		if kind, ok := kinds[r.QuestionID]; ok && kind == consultation.KindOpen {
			maxLength = u.openQuestionMaxTextLength
		}
		out[i] = r
		if r.ResponseText != "" {
			out[i].ResponseText = u.sanitize(r.ResponseText, maxLength)
		}
	}
	return out
}
