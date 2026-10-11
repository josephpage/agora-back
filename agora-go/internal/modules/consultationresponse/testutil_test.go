package consultationresponse

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultation/answered"
)

var baseTime = time.Date(2026, 10, 6, 12, 0, 0, 0, time.Local)

func newApp(coexistence bool) *app.App {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &app.App{
		Cfg:   &config.Config{MicroCacheTTL: 0, OpenQuestionMaxTextLength: 400, ConsultationResponseRateLimitPerHour: 20},
		Cache: cache.New(nil, log, coexistence),
		Log:   log,
		Clock: func() time.Time { return baseTime },
	}
}

// ---------------------------------------------------------------------------
// questions
// ---------------------------------------------------------------------------

func choice(q string, i int, id string) consultation.ChoixPossible {
	return consultation.ChoixPossible{ID: id, Label: "Choix " + id, Ordre: i, QuestionID: q}
}

func choices(q string, ids ...string) []consultation.ChoixPossible {
	out := make([]consultation.ChoixPossible, len(ids))
	for i, id := range ids {
		out[i] = choice(q, i, id)
	}
	return out
}

func uniqueQ(id string, ids ...string) consultation.Question {
	return consultation.Question{Kind: consultation.KindUniqueChoice, ID: id, Title: "Q " + id, Order: 1, ChoixPossibleList: choices(id, ids...)}
}

func multiQ(id string, max int, ids ...string) consultation.Question {
	return consultation.Question{Kind: consultation.KindMultipleChoices, ID: id, Title: "Q " + id, Order: 2, MaxChoices: max, ChoixPossibleList: choices(id, ids...)}
}

func condQ(id string, ids ...string) consultation.Question {
	return consultation.Question{Kind: consultation.KindConditional, ID: id, Title: "Q " + id, Order: 3, ChoixPossibleList: choices(id, ids...)}
}

func openQ(id string) consultation.Question {
	return consultation.Question{Kind: consultation.KindOpen, ID: id, Title: "Q " + id, Order: 4}
}

func chapterQ(id string) consultation.Question {
	return consultation.Question{Kind: consultation.KindChapter, ID: id, Title: "Q " + id, Order: 5}
}

type fakeQuestions struct {
	mu    sync.Mutex
	list  []consultation.Question
	calls []string
}

func (f *fakeQuestions) GetConsultationQuestions(_ context.Context, consultationID string) consultation.Questions {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, consultationID)
	return consultation.Questions{QuestionCount: len(f.list), Questions: f.list}
}

func sp(s string) *string { return &s }

func ids(ss ...string) []*string {
	out := make([]*string, len(ss))
	for i := range ss {
		out[i] = &ss[i]
	}
	return out
}

func resp(q string, text string, choiceIDs ...string) ReponseConsultationInserting {
	return ReponseConsultationInserting{QuestionID: q, ChoiceIDs: ids(choiceIDs...), ResponseText: text}
}

// ---------------------------------------------------------------------------
// the collaborators of the insert use case
// ---------------------------------------------------------------------------

type fakeInfo struct {
	info  *consultation.ConsultationInfo
	calls int
}

func (f *fakeInfo) GetConsultation(context.Context, string) *consultation.ConsultationInfo {
	f.calls++
	return f.info
}

type fakeAnswered struct {
	has      bool
	hasCalls int
	inserted []answered.UserAnswered
	err      error
}

func (f *fakeAnswered) HasAnsweredConsultation(context.Context, string, string) (bool, error) {
	f.hasCalls++
	return f.has, f.err
}

func (f *fakeAnswered) InsertUserAnsweredConsultation(_ context.Context, in answered.UserAnswered) (answered.Result, error) {
	f.inserted = append(f.inserted, in)
	return answered.Success, nil
}

type fakeInserter struct {
	calls  int
	params InsertParameters
	got    []ReponseConsultationInserting
	result InsertResult
	err    error
}

func (f *fakeInserter) InsertConsultationResponses(_ context.Context, p InsertParameters, rs []ReponseConsultationInserting) (InsertResult, error) {
	f.calls++
	f.params, f.got = p, rs
	return f.result, f.err
}

func endingAt(t time.Time) *consultation.ConsultationInfo {
	return &consultation.ConsultationInfo{ID: "consultId", Title: "consultTitle", EndDate: consultation.LocalDateTime{T: time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)}}
}

func mustEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}
