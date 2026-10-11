package consultationresponse

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
	"agora/internal/xmljava"
)

type fakeResultsSources struct {
	mu           sync.Mutex
	calls        []string
	participants int
	aggregated   []ConsultationResultAggregated
	counts       []ResponseConsultationCount
	err          error
}

func (f *fakeResultsSources) GetParticipantCount(context.Context, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "participants")
	return f.participants, f.err
}

func (f *fakeResultsSources) GetConsultationResultAggregated(context.Context, string) ([]ConsultationResultAggregated, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "aggregated")
	return f.aggregated, f.err
}

func (f *fakeResultsSources) GetConsultationResponsesCount(context.Context, string) ([]ResponseConsultationCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "counts")
	return f.counts, f.err
}

func newResults(a *app.App, info *consultation.ConsultationInfo, qs []consultation.Question, src *fakeResultsSources) (*ConsultationResultsUseCase, *fakeInfo, *fakeQuestions) {
	fi, fq := &fakeInfo{info: info}, &fakeQuestions{list: qs}
	return &ConsultationResultsUseCase{a: a, info: fi, questions: fq, answered: src, aggregated: src, responses: src}, fi, fq
}

var someInfo = &consultation.ConsultationInfo{ID: "consultationId", Title: "Titre", CoverURL: "https://cover"}

// getConsultationResults - when getConsultation is null - should return null
func TestResultsUnknownConsultation(t *testing.T) {
	src := &fakeResultsSources{}
	uc, fi, fq := newResults(newApp(false), nil, nil, src)
	got, err := uc.BuildConsultationResults(context.Background(), "consultationId")
	if err != nil || got != nil {
		t.Fatalf("got %v %v", got, err)
	}
	mustEqual(t, "info", fi.calls, 1)
	mustEqual(t, "questions", len(fq.calls), 0)
	mustEqual(t, "sources", len(src.calls), 0)
}

// when getConsultationQuestions is empty - should return object without results (participants only)
func TestResultsNoQuestion(t *testing.T) {
	src := &fakeResultsSources{participants: 23}
	uc, _, _ := newResults(newApp(false), someInfo, nil, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	mustEqual(t, "participants", got.ParticipantCount, 23)
	mustEqual(t, "no results", len(got.ResultsWithChoices), 0)
	mustEqual(t, "no open question", len(got.OpenQuestions), 0)
	if !reflect.DeepEqual(src.calls, []string{"participants"}) {
		t.Fatalf("calls: %v", src.calls)
	}
}

// a chapter, or a question with choices that has no choice: no result, but the counts are read
func TestResultsQuestionsWithoutChoices(t *testing.T) {
	src := &fakeResultsSources{participants: 77}
	uc, _, _ := newResults(newApp(false), someInfo, []consultation.Question{chapterQ("1"), uniqueQ("2")}, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	mustEqual(t, "no results", len(got.ResultsWithChoices), 0)
	mustEqual(t, "no open question", len(got.OpenQuestions), 0)
	if !reflect.DeepEqual(src.calls, []string{"participants", "aggregated", "counts"}) {
		t.Fatalf("calls: %v", src.calls)
	}
}

// the aggregated table wins as soon as it has a row: the raw counts are not read
func TestResultsAggregatedFirst(t *testing.T) {
	src := &fakeResultsSources{participants: 10, aggregated: []ConsultationResultAggregated{
		{QuestionID: "q", ChoiceID: "a", ResponseCount: 4}, {QuestionID: "q", ChoiceID: "a", ResponseCount: 1}, {QuestionID: "q", ChoiceID: "b", ResponseCount: 2}},
		counts: []ResponseConsultationCount{{QuestionID: "q", ChoiceID: "a", ResponseCount: 100}}}
	uc, _, _ := newResults(newApp(false), someInfo, []consultation.Question{uniqueQ("q", "a", "b")}, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	if !reflect.DeepEqual(src.calls, []string{"participants", "aggregated"}) {
		t.Fatalf("calls: %v", src.calls)
	}
	ratios := []float64{got.ResultsWithChoices[0].Responses[0].Ratio, got.ResultsWithChoices[0].Responses[1].Ratio, got.ResultsWithChoices[0].Responses[2].Ratio}
	if !reflect.DeepEqual(ratios, []float64{0.5, 0.2, 0}) {
		t.Fatalf("ratios %v", ratios)
	}
}

// when there is open questions - should return open questions
func TestResultsOpenQuestions(t *testing.T) {
	src := &fakeResultsSources{participants: 8}
	uc, _, _ := newResults(newApp(false), someInfo, []consultation.Question{openQ("1"), openQ("2")}, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	mustEqual(t, "open questions", len(got.OpenQuestions), 2)
	mustEqual(t, "no result with choices", len(got.ResultsWithChoices), 0)
}

type choiceInput struct {
	id            string
	responseCount int
	expectedRatio float64
}

// ConsultationResultsUseCaseTest.getConsultationResultsParameters (the expected seen ratio is always 1.0 since 20 May 2025, the
// ratios of the choices are those of the Kotlin test)
func TestResultsRatios(t *testing.T) {
	type q struct {
		id            string
		choices       []choiceInput
		notApplicable int
	}
	tests := []struct {
		name         string
		participants int
		qs           []q
	}{
		{"1 question with 1 choice & 1 response", 1, []q{{"question1", []choiceInput{{"q1c1", 1, 1.0}}, 0}}},
		{"1 question with 2 choices & some responses", 3, []q{{"question1", []choiceInput{{"q1c1", 2, 0.6666666666666666}, {"q2c2", 1, 0.3333333333333333}}, 0}}},
		{"2 questions with choices & some responses", 10, []q{
			{"question1", []choiceInput{{"q1c1", 8, 0.8}, {"q1c2", 2, 0.2}}, 0}, {"question2", []choiceInput{{"q2c1", 4, 0.4}, {"q2c2", 6, 0.6}}, 0}}},
		{"some questions do not have choices", 10, []q{
			{"question1", []choiceInput{{"q1c1", 8, 0.8}, {"q1c2", 2, 0.2}}, 0}, {"question2", []choiceInput{{"q1c1", 4, 0.4}, {"q2c2", 6, 0.6}}, 0}, {"question3", nil, 0}}},
		{"question with multiple choices", 10, []q{{"question1", []choiceInput{{"q1c1", 10, 1.0}, {"q1c2", 6, 0.6}}, 0}}},
		{"1 question with 1 choice, 1 response and 1 N/A", 2, []q{{"question1", []choiceInput{{"q1c1", 1, 1.0}}, 1}}},
		{"1 question with multiple choices and some N/A", 10, []q{{"question1", []choiceInput{{"q1c1", 8, 1.0}, {"q1c2", 4, 0.5}}, 2}}},
	}
	for _, tt := range tests {
		for _, source := range []string{"counts", "aggregated"} {
			t.Run(tt.name+" / "+source, func(t *testing.T) {
				src := &fakeResultsSources{participants: tt.participants}
				var questions []consultation.Question
				for _, in := range tt.qs {
					var ids []string
					for _, c := range in.choices {
						ids = append(ids, c.id)
						if source == "counts" {
							src.counts = append(src.counts, ResponseConsultationCount{QuestionID: in.id, ChoiceID: c.id, ResponseCount: c.responseCount})
						} else {
							src.aggregated = append(src.aggregated, ConsultationResultAggregated{QuestionID: in.id, ChoiceID: c.id, ResponseCount: c.responseCount})
						}
					}
					if in.notApplicable > 0 {
						na := "11111111-1111-1111-1111-111111111111"
						if source == "counts" {
							src.counts = append(src.counts, ResponseConsultationCount{QuestionID: in.id, ChoiceID: na, ResponseCount: in.notApplicable})
						} else {
							src.aggregated = append(src.aggregated, ConsultationResultAggregated{QuestionID: in.id, ChoiceID: na, ResponseCount: in.notApplicable})
						}
					}
					questions = append(questions, uniqueQ(in.id, ids...))
				}
				uc, _, _ := newResults(newApp(false), someInfo, questions, src)
				got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
				withChoices := got.ResultsWithChoices
				k := 0
				for _, in := range tt.qs {
					if len(in.choices) == 0 {
						continue
					}
					r := withChoices[k]
					k++
					mustEqual(t, in.id+" seen ratio", r.SeenRatio, 1.0)
					// the choices of the question, then "Pas de réponse"
					mustEqual(t, in.id+" choices", len(r.Responses), len(in.choices)+1)
					for i, c := range in.choices {
						mustEqual(t, in.id+"/"+c.id+" ratio", r.Responses[i].Ratio, c.expectedRatio)
					}
					last := r.Responses[len(in.choices)]
					mustEqual(t, "last choice", last.Choix.ID+"/"+last.Choix.Label, "00000000-0000-0000-0000-000000000000/Pas de réponse")
					mustEqual(t, "last choice order", last.Choix.Ordre, len(in.choices)+1)
				}
				mustEqual(t, "questions with choices", k, len(withChoices))
			})
		}
	}
}

// QuestionNoResponseMapper: unique and multiple choices questions get "Pas de réponse", a conditional one does not; the shared question is not modified
func TestToQuestionNoResponse(t *testing.T) {
	u := uniqueQ("q", "a", "b")
	got := toQuestionNoResponse(u)
	mustEqual(t, "unique", len(got.ChoixPossibleList), 3)
	mustEqual(t, "original untouched", len(u.ChoixPossibleList), 2)
	got = toQuestionNoResponse(multiQ("q", 2, "a"))
	mustEqual(t, "multiple max kept", got.MaxChoices, 2)
	mustEqual(t, "multiple", got.ChoixPossibleList[1].QuestionID, "q")
	mustEqual(t, "conditional", len(toQuestionNoResponse(condQ("q", "a")).ChoixPossibleList), 1)
	mustEqual(t, "open", toQuestionNoResponse(openQ("q")).Kind, consultation.KindOpen)
}

// divisions: x / 0 is Infinity, 0 / 0 is 0, negative denominators; (ratio * 100).roundToInt()
func TestResultsExtremeRatios(t *testing.T) {
	src := &fakeResultsSources{participants: 0, counts: []ResponseConsultationCount{{QuestionID: "q", ChoiceID: "a", ResponseCount: 3}}}
	uc, _, _ := newResults(newApp(false), someInfo, []consultation.Question{uniqueQ("q", "a", "b")}, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	mustEqual(t, "infinity", math.IsInf(got.ResultsWithChoices[0].Responses[0].Ratio, 1), true)
	mustEqual(t, "0/0", got.ResultsWithChoices[0].Responses[1].Ratio, 0.0)
	j := ToResultsJSON(got)
	mustEqual(t, "infinity saturates", j.ResultsUniqueChoice[0].Responses[0].Ratio, math.MaxInt32)
	mustEqual(t, "0/0", j.ResultsUniqueChoice[0].Responses[1].Ratio, 0)
	mustEqual(t, "seen ratio", j.ResultsUniqueChoice[0].SeenRatio, 100)

	// more "not applicable" than participants: negative denominator
	na := "11111111-1111-1111-1111-111111111111"
	src = &fakeResultsSources{participants: 2, counts: []ResponseConsultationCount{{QuestionID: "q", ChoiceID: "a", ResponseCount: 3}, {QuestionID: "q", ChoiceID: na, ResponseCount: 5}}}
	uc, _, _ = newResults(newApp(false), someInfo, []consultation.Question{uniqueQ("q", "a")}, src)
	got, _ = uc.BuildConsultationResults(context.Background(), "consultationId")
	mustEqual(t, "negative", got.ResultsWithChoices[0].Responses[0].Ratio, -1.0)
	mustEqual(t, "json", ToResultsJSON(got).ResultsUniqueChoice[0].Responses[0].Ratio, -100)
}

// ConsultationResultJsonMapper: unique and conditional questions first group, multiple choices second, open questions apart; the bytes
func TestResultsJSON(t *testing.T) {
	src := &fakeResultsSources{participants: 2, counts: []ResponseConsultationCount{{QuestionID: "1", ChoiceID: "a", ResponseCount: 1}, {QuestionID: "2", ChoiceID: "x", ResponseCount: 2}}}
	qs := []consultation.Question{uniqueQ("1", "a", "b"), multiQ("2", 2, "x"), condQ("3", "c"), openQ("4"), chapterQ("5")}
	uc, _, _ := newResults(newApp(false), someInfo, qs, src)
	got, _ := uc.BuildConsultationResults(context.Background(), "consultationId")
	j := ToResultsJSON(got)
	want := `{"title":"Titre","coverUrl":"https://cover","participantCount":2,"resultsUniqueChoice":[` +
		`{"questionId":"1","questionTitle":"Q 1","order":1,"seenRatio":100,"responses":[{"choiceId":"a","label":"Choix a","ratio":50},{"choiceId":"b","label":"Choix b","ratio":0},{"choiceId":"00000000-0000-0000-0000-000000000000","label":"Pas de réponse","ratio":0}]},` +
		`{"questionId":"3","questionTitle":"Q 3","order":3,"seenRatio":100,"responses":[{"choiceId":"c","label":"Choix c","ratio":0}]}],` +
		`"resultsMultipleChoice":[{"questionId":"2","questionTitle":"Q 2","order":2,"seenRatio":100,"responses":[{"choiceId":"x","label":"Choix x","ratio":100},{"choiceId":"00000000-0000-0000-0000-000000000000","label":"Pas de réponse","ratio":0}]}],` +
		`"resultsOpen":[{"questionId":"4","questionTitle":"Q 4","order":4}]}`
	mustEqual(t, "json", jsonjava.MarshalString(j), want)
	xml := string(xmljava.Marshal(j))
	mustEqual(t, "xml root", xml[:len("<ConsultationResultsJson>")], "<ConsultationResultsJson>")
	// the empty groups are empty arrays
	empty := ToResultsJSON(&ConsultationResults{Consultation: someInfo})
	mustEqual(t, "empty", jsonjava.MarshalString(empty), `{"title":"Titre","coverUrl":"https://cover","participantCount":0,"resultsUniqueChoice":[],"resultsMultipleChoice":[],"resultsOpen":[]}`)
}

// ---------------------------------------------------------------------------
// the cache
// ---------------------------------------------------------------------------

func TestResultsCacheIsSharedAndEvicted(t *testing.T) {
	a := newApp(false)
	src := &fakeResultsSources{participants: 1, counts: []ResponseConsultationCount{{QuestionID: "q", ChoiceID: "a", ResponseCount: 1}}}
	uc, fi, _ := newResults(a, someInfo, []consultation.Question{uniqueQ("q", "a")}, src)
	ctx := context.Background()
	r1, err := uc.GetConsultationResults(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := uc.GetConsultationResults(ctx, "c1")
	if r1 != r2 {
		t.Fatal("the second read must be the cached value")
	}
	mustEqual(t, "computed once", fi.calls, 1)
	// another consultation has its own entry
	uc.GetConsultationResults(ctx, "c2")
	mustEqual(t, "computed for c2", fi.calls, 2)
	// an answer drops the entry
	a.Cache.Invalidate(ctx, resultsCacheName, "c1")
	r3, _ := uc.GetConsultationResults(ctx, "c1")
	if r3 == r1 {
		t.Fatal("the evicted entry must be recomputed")
	}
	mustEqual(t, "recomputed", fi.calls, 3)
	a.Cache.InvalidateAll(ctx, resultsCacheName)
	uc.GetConsultationResults(ctx, "c1")
	uc.GetConsultationResults(ctx, "c2")
	mustEqual(t, "everything recomputed", fi.calls, 5)
}

// an unknown consultation is cached too (Kotlin keeps the "" marker), until an answer
func TestResultsNotFoundIsCached(t *testing.T) {
	a := newApp(false)
	uc, fi, _ := newResults(a, nil, nil, &fakeResultsSources{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r, err := uc.GetConsultationResults(ctx, "unknown")
		if r != nil || err != nil {
			t.Fatalf("%v %v", r, err)
		}
	}
	mustEqual(t, "looked up once", fi.calls, 1)
	fi.info = someInfo
	a.Cache.Invalidate(ctx, resultsCacheName, "unknown")
	r, _ := uc.GetConsultationResults(ctx, "unknown")
	if r == nil {
		t.Fatal("known after the eviction")
	}
}

// errors are not cached
func TestResultsErrorsAreNotCached(t *testing.T) {
	a := newApp(false)
	src := &fakeResultsSources{err: errors.New("db down"), participants: 1}
	uc, fi, _ := newResults(a, someInfo, []consultation.Question{uniqueQ("q", "a")}, src)
	if _, err := uc.GetConsultationResults(context.Background(), "c"); err == nil {
		t.Fatal("expected an error")
	}
	src.err = nil
	if r, err := uc.GetConsultationResults(context.Background(), "c"); err != nil || r == nil {
		t.Fatalf("%v %v", r, err)
	}
	mustEqual(t, "recomputed", fi.calls, 2)
}

// concurrent readers share one computation
func TestResultsSingleflight(t *testing.T) {
	a := newApp(false)
	src := &fakeResultsSources{participants: 1}
	uc, fi, _ := newResults(a, someInfo, []consultation.Question{openQ("q")}, src)
	var wg sync.WaitGroup
	var nilCount atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if r, _ := uc.GetConsultationResults(context.Background(), "c"); r == nil {
				nilCount.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	mustEqual(t, "none nil", int(nilCount.Load()), 0)
	if fi.calls > 5 { // goroutine scheduling may split the readers around the first store, never 50 computations
		t.Fatalf("computed %d times", fi.calls)
	}
}

// in coexistence mode the entry lives 5 seconds at most (Kotlin answers cannot be observed)
func TestResultsCoexistenceTTL(t *testing.T) {
	a := newApp(true)
	uc, fi, _ := newResults(a, someInfo, nil, &fakeResultsSources{})
	uc.ttl = time.Hour
	if got := a.Cache.CoexistenceTTL(uc.ttl); got != 5*time.Second {
		t.Fatalf("ttl %v", got)
	}
	uc.GetConsultationResults(context.Background(), "c")
	uc.GetConsultationResults(context.Background(), "c")
	mustEqual(t, "cached", fi.calls, 1)
}
