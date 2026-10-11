package consultationresponse

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"agora/internal/modules/consultation/answered"
)

type insertFixture struct {
	uc       *InsertReponseConsultationUseCase
	info     *fakeInfo
	answered *fakeAnswered
	inserter *fakeInserter
	qs       *fakeQuestions
	evicted  [][2]string
	sanCalls [][2]any
}

// newInsert is InsertReponseConsultationUseCaseTest.mockDate: the clock is fixed at today.
func newInsert(today time.Time, openMax int, info *fakeInfo) *insertFixture {
	f := &insertFixture{info: info, answered: &fakeAnswered{}, inserter: &fakeInserter{result: InsertSuccess}, qs: &fakeQuestions{}}
	f.uc = &InsertReponseConsultationUseCase{
		now: func() time.Time { return today },
		sanitize: func(content string, maxLength int) string {
			f.sanCalls = append(f.sanCalls, [2]any{content, maxLength})
			return "sanitized " + content
		},
		repository: f.inserter, answered: f.answered, questions: f.qs, info: info, openQuestionMaxTextLength: openMax,
		evict: func(_ context.Context, cid, uid string) { f.evicted = append(f.evicted, [2]string{cid, uid}) },
	}
	return f
}

var today = time.Date(2023, time.October, 19, 19, 0, 0, 0, time.Local)

// insertReponseConsultation - when consultation is already finished - should return failure
func TestInsertConsultationAlreadyFinished(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{info: endingAt(time.Date(2023, time.June, 20, 14, 0, 0, 0, time.UTC))})
	got, err := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{resp("q", "")})
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "result", got, InsertFailure)
	mustEqual(t, "info calls", f.info.calls, 1)
	mustEqual(t, "has answered calls", f.answered.hasCalls, 0)
	mustEqual(t, "repository calls", f.inserter.calls, 0)
	mustEqual(t, "question calls", len(f.qs.calls), 0)
	mustEqual(t, "sanitizer calls", len(f.sanCalls), 0)
	mustEqual(t, "nothing evicted", len(f.evicted), 0)
}

// the end date is a strict limit: a consultation that ends right now is still open
func TestInsertConsultationEndDateIsStrict(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{info: endingAt(today)})
	got, _ := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	mustEqual(t, "ends now", got, InsertSuccess)
	f = newInsert(today, 400, &fakeInfo{info: endingAt(today.Add(-time.Nanosecond))})
	got, _ = f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	mustEqual(t, "ended a nanosecond ago", got, InsertFailure)
}

// when has already answered and consultation not finished - should return failure
func TestInsertAlreadyAnswered(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{info: endingAt(time.Date(2024, time.October, 19, 19, 0, 0, 0, time.UTC))})
	f.answered.has = true
	got, _ := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{resp("q", "")})
	mustEqual(t, "result", got, InsertFailure)
	mustEqual(t, "has answered calls", f.answered.hasCalls, 1)
	mustEqual(t, "repository calls", f.inserter.calls, 0)
	mustEqual(t, "question calls", len(f.qs.calls), 0)
	mustEqual(t, "sanitizer calls", len(f.sanCalls), 0)
	mustEqual(t, "nothing evicted", len(f.evicted), 0)
}

// an unknown consultation has no end date: the answer goes on
func TestInsertUnknownConsultationIsNotFinished(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{})
	got, _ := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	mustEqual(t, "result", got, InsertSuccess)
	mustEqual(t, "inserted for the user", len(f.answered.inserted), 1)
}

// when answer open question - should sanitize with open_question_max_text_length then return result from insert repository
func TestInsertOpenQuestionIsSanitizedWithOpenMaxLength(t *testing.T) {
	ongoing := &fakeInfo{info: endingAt(time.Date(2024, time.October, 19, 19, 0, 0, 0, time.UTC))}
	for _, openMax := range []int{400, 2000} {
		f := newInsert(today, openMax, ongoing)
		f.qs.list = append(f.qs.list, openQ("question1"))
		got, err := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{{QuestionID: "question1", ChoiceIDsNil: true, ResponseText: "salut"}})
		if err != nil {
			t.Fatal(err)
		}
		mustEqual(t, "result", got, InsertSuccess)
		if !reflect.DeepEqual(f.sanCalls, [][2]any{{"salut", openMax}}) {
			t.Fatalf("sanitizer calls: %v", f.sanCalls)
		}
		want := []ReponseConsultationInserting{{QuestionID: "question1", ChoiceIDsNil: true, ResponseText: "sanitized salut"}}
		if !reflect.DeepEqual(f.inserter.got, want) {
			t.Fatalf("inserted: %+v", f.inserter.got)
		}
		if !reflect.DeepEqual(f.answered.inserted, []answered.UserAnswered{{UserID: "userId", ConsultationID: "consultId"}}) {
			t.Fatalf("answered: %+v", f.answered.inserted)
		}
		if !reflect.DeepEqual(f.evicted, [][2]string{{"consultId", "userId"}}) {
			t.Fatalf("evicted: %v", f.evicted)
		}
		mustEqual(t, "participation id is a UUID", len(f.inserter.params.ParticipationID), 36)
		mustEqual(t, "params", f.inserter.params.ConsultationID+"/"+f.inserter.params.UserID, "consultId/userId")
		mustEqual(t, "question reads", len(f.qs.calls), 1)
	}
}

// when answer unique question with choice other - should sanitize with other_text_max_length
func TestInsertOtherTextIsSanitizedWith200(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{info: endingAt(time.Date(2024, time.October, 19, 19, 0, 0, 0, time.UTC))})
	f.qs.list = append(f.qs.list, uniqueQ("question1", "11111111-2222-0000-9999-5555555555555"))
	got, _ := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{resp("question1", "autre choix", "11111111-2222-0000-9999-5555555555555")})
	mustEqual(t, "result", got, InsertSuccess)
	if !reflect.DeepEqual(f.sanCalls, [][2]any{{"autre choix", 200}}) {
		t.Fatalf("sanitizer calls: %v", f.sanCalls)
	}
	mustEqual(t, "text", f.inserter.got[0].ResponseText, "sanitized autre choix")
}

// when has missing response on questions with condition - should return result from insert repository with added responses
func TestInsertAddsNotApplicableAnswers(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{info: endingAt(time.Date(2024, time.October, 19, 19, 0, 0, 0, time.UTC))})
	f.qs.list = append(f.qs.list, uniqueQ("question1", "a"), multiQ("question2", 2, "a"), condQ("question3", "a"), openQ("question4"), chapterQ("question5"))
	got, _ := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	mustEqual(t, "result", got, InsertSuccess)
	mustEqual(t, "no sanitizer call", len(f.sanCalls), 0)
	// one "not applicable" answer per question with choices, in the order of the questions; nothing for the open question and the chapter
	var got3 []string
	for _, r := range f.inserter.got {
		if len(r.ChoiceIDs) != 1 || *r.ChoiceIDs[0] != "11111111-1111-1111-1111-111111111111" || r.ResponseText != "" {
			t.Fatalf("not a not applicable answer: %+v", r)
		}
		got3 = append(got3, r.QuestionID)
	}
	if !reflect.DeepEqual(got3, []string{"question1", "question2", "question3"}) {
		t.Fatalf("questions: %v", got3)
	}
}

// the answers follow the order of the questions; answers to unknown questions are dropped; the answer is kept as is (a chapter too)
func TestInsertOrderAndFilling(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{})
	f.qs.list = append(f.qs.list, uniqueQ("1", "a"), openQ("2"), chapterQ("3"), multiQ("4", 2, "a", "b"))
	f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{
		resp("4", "", "a"), resp("3", "", "x"), resp("2", "t"), resp("99", "", "z")})
	var order []string
	for _, r := range f.inserter.got {
		order = append(order, r.QuestionID)
	}
	if !reflect.DeepEqual(order, []string{"1", "2", "3", "4"}) {
		t.Fatalf("order: %v", order)
	}
	mustEqual(t, "open text sanitized with 400", f.sanCalls[0] == [2]any{"t", 400}, true)
}

// the sanitizer is not called for an empty text
func TestInsertEmptyTextIsNotSanitized(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{})
	f.qs.list = append(f.qs.list, openQ("1"))
	f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", []ReponseConsultationInserting{resp("1", "")})
	mustEqual(t, "sanitizer calls", len(f.sanCalls), 0)
}

// a failure of the repository (invalid ids) is a failure: the user is not marked as having answered; what the answer drops is still dropped
func TestInsertRepositoryFailure(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{})
	f.inserter.result = InsertFailure
	got, err := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "result", got, InsertFailure)
	mustEqual(t, "user not marked", len(f.answered.inserted), 0)
	mustEqual(t, "evicted", len(f.evicted), 1)
}

// an exception of the repository (database) is propagated (HTTP 500); the caches are dropped anyway
func TestInsertRepositoryError(t *testing.T) {
	f := newInsert(today, 400, &fakeInfo{})
	f.inserter.err = errors.New("boom")
	_, err := f.uc.InsertReponseConsultation(context.Background(), "consultId", "userId", nil)
	if err == nil {
		t.Fatal("expected the error")
	}
	mustEqual(t, "user not marked", len(f.answered.inserted), 0)
	mustEqual(t, "evicted", len(f.evicted), 1)
}

// InsertReponseConsultationRepositoryImplTest: invalid ids and the empty list never reach the database
func TestRepositoryInvalidIDs(t *testing.T) {
	r := &ResponseRepository{} // no database: nothing may be written
	p := InsertParameters{ConsultationID: "c", UserID: "6f1b3a52-5b44-4d1c-9a43-0123456789ab", ParticipationID: "6f1b3a52-5b44-4d1c-9a43-0123456789ac"}
	one := []ReponseConsultationInserting{resp("q", "", "a")}
	bad := p
	bad.UserID = "invalid userID"
	got, err := r.InsertConsultationResponses(context.Background(), bad, one)
	mustEqual(t, "invalid user id", got, InsertFailure)
	mustEqual(t, "no error", err, nil)
	bad = p
	bad.ParticipationID = "invalid participationId"
	got, err = r.InsertConsultationResponses(context.Background(), bad, one)
	mustEqual(t, "invalid participation id", got, InsertFailure)
	mustEqual(t, "no error", err, nil)
	got, err = r.InsertConsultationResponses(context.Background(), p, nil)
	mustEqual(t, "no answer", got, InsertSuccess)
	mustEqual(t, "no error", err, nil)
}

// ReponseConsultationMapper.toDto
func TestToRows(t *testing.T) {
	var n atomic.Int32
	newID := func() string { return string(rune('a' + n.Add(1) - 1)) }
	rows := toRows("c", resp("q", "texte", "x", "y"), newID)
	mustEqual(t, "one row per choice", len(rows), 2)
	mustEqual(t, "choice 1", *rows[0].choiceID, "x")
	mustEqual(t, "choice 2", *rows[1].choiceID, "y")
	mustEqual(t, "text repeated", rows[0].responseText+"/"+rows[1].responseText, "texte/texte")
	mustEqual(t, "ids", rows[0].id+rows[1].id, "ab")

	rows = toRows("c", resp("q", "texte"), newID)
	mustEqual(t, "no choice: one row", len(rows), 1)
	if rows[0].choiceID != nil {
		t.Fatal("the choice id must be null")
	}
	rows = toRows("c", ReponseConsultationInserting{QuestionID: "q", ChoiceIDsNil: true}, newID)
	mustEqual(t, "null list: one row", len(rows), 1)
	rows = toRows("c", ReponseConsultationInserting{QuestionID: "q", ChoiceIDs: []*string{nil, sp("a")}}, newID)
	mustEqual(t, "a null element is a row without choice", len(rows), 2)
	if rows[0].choiceID != nil || *rows[1].choiceID != "a" {
		t.Fatal("rows of [null, a]")
	}
	mustEqual(t, "UUID", len(randomUUID()), 36)
	mustEqual(t, "version 4", randomUUID()[14], byte('4'))
}

// ReponseConsultationJsonMapper: a null text is "", a null element of the list is a NullPointerException
func TestToDomainJSON(t *testing.T) {
	body := &ReponsesConsultationJSON{Responses: []*ReponseConsultationJSON{{QuestionID: "q", ChoiceIDs: ids("a"), ResponseText: nil}, {QuestionID: "r", ChoiceIDs: ids(), ResponseText: sp("t")}}}
	got := toDomain(body)
	mustEqual(t, "null text", got[0].ResponseText, "")
	mustEqual(t, "text", got[1].ResponseText, "t")
	defer func() {
		if recover() == nil {
			t.Fatal("expected a NullPointerException")
		}
	}()
	toDomain(&ReponsesConsultationJSON{Responses: []*ReponseConsultationJSON{nil}})
}
