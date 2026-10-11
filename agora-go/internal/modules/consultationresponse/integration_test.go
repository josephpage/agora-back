package consultationresponse

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultation/answered"
	"agora/internal/sanitize"
	"agora/internal/store"
	"agora/parity/seed"
)

// itApp reseeds the database named by S6_IT_DB (opt-in, it RESEEDS it!) and returns an App on it:
//
//	S6_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 go test ./internal/modules/consultationresponse -run Integration
func itApp(t *testing.T) *app.App {
	dsn := os.Getenv("S6_IT_DB")
	if dsn == "" {
		t.Skip("S6_IT_DB not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Seed(ctx, conn, time.Now().UTC().Truncate(time.Second), 1); err != nil {
		t.Fatal(err)
	}
	// the scenarios of the harness leave a trigger that normalizes participation ids (parity/scenarios/gen/s6/helpers.py)
	if _, err := conn.Exec(ctx, "DROP TRIGGER IF EXISTS s6_participation ON reponses_consultation"); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	db, err := store.Open(ctx, dsn, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	a := newApp(false)
	a.DB = db
	return a
}

func count(t *testing.T, a *app.App, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := a.DB.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const itUser = "00000000-0000-4000-9000-00000000000f"

func TestIntegrationInsertRows(t *testing.T) {
	a := itApp(t)
	ctx := context.Background()
	repo := &ResponseRepository{a: a}
	cid := "co0000000000000000000002"
	part := "6f1b3a52-5b44-4d1c-9a43-0123456789ac"
	got, err := repo.InsertConsultationResponses(ctx, InsertParameters{ConsultationID: cid, UserID: itUser, ParticipationID: part}, []ReponseConsultationInserting{
		resp("201", "", "2011"),
		resp("202", "", "2021", "2022"),
		resp("203", "texte"),
		{QuestionID: "204", ChoiceIDs: []*string{nil, sp("2041")}, ResponseText: "avec"},
	})
	if err != nil || got != InsertSuccess {
		t.Fatalf("%v %v", got, err)
	}
	mustEqual(t, "rows", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1", cid), 6)
	mustEqual(t, "one participation", count(t, a, "SELECT count(DISTINCT participation_id) FROM reponses_consultation WHERE consultation_id = $1 AND participation_id = $2", cid, part), 1)
	mustEqual(t, "user", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND user_id = $2", cid, itUser), 6)
	mustEqual(t, "null choices", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND choice_id IS NULL", cid), 2)
	mustEqual(t, "texts", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND response_text <> ''", cid), 3)
	mustEqual(t, "recent dates", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND participation_date <= $2", cid, store.Millis(a.Now())), 6)
	mustEqual(t, "distinct ids", count(t, a, "SELECT count(DISTINCT id) FROM reponses_consultation WHERE consultation_id = $1", cid), 6)

	// more rows than a statement holds: one transaction, every row
	var many []string
	for i := 0; i < maxRowsPerStatement+500; i++ {
		many = append(many, fmt.Sprintf("choice-%d", i))
	}
	got, err = repo.InsertConsultationResponses(ctx, InsertParameters{ConsultationID: "co0000000000000000000003", UserID: itUser, ParticipationID: part}, []ReponseConsultationInserting{resp("301", "", many...)})
	if err != nil || got != InsertSuccess {
		t.Fatalf("%v %v", got, err)
	}
	mustEqual(t, "chunked", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = 'co0000000000000000000003'"), len(many))

	// a failure in the middle of the chunks writes nothing (NUL characters are refused by PostgreSQL)
	many[len(many)-1] = "bad\x00choice"
	_, err = repo.InsertConsultationResponses(ctx, InsertParameters{ConsultationID: "co0000000000000000000006", UserID: itUser, ParticipationID: part}, []ReponseConsultationInserting{resp("601", "", many...)})
	if err == nil {
		t.Fatal("expected the NUL error")
	}
	mustEqual(t, "rolled back", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = 'co0000000000000000000006'"), 0)
}

// AggregateConsultationResultUseCaseTest and ConsultationResultAggregatedRepositoryImplTest against the real tables: consultation 7 has
// answers and is not aggregated yet
func TestIntegrationAggregate(t *testing.T) {
	a := itApp(t)
	ctx := context.Background()
	s := &Service{a: a, Responses: &ResponseQueries{a: a}, Aggregated: &AggregatedRepository{a: a}}
	uc := &AggregateConsultationResultUseCase{a: a, responses: *s.Responses, aggregated: s.Aggregated}
	var evicted []string
	uc.afterAggregation = func(_ context.Context, cid string) { evicted = append(evicted, cid) }

	// no answer: nothing is written
	if err := uc.AggregateConsultationResults(ctx, "co9999999999999999999999"); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "nothing aggregated", count(t, a, "SELECT count(*) FROM consultation_results WHERE consultation_id = 'co9999999999999999999999'"), 0)
	mustEqual(t, "no eviction", len(evicted), 0)

	cid := "co0000000000000000000007"
	counts, err := s.Responses.GetConsultationResponsesCount(ctx, cid)
	if err != nil || len(counts) == 0 {
		t.Fatalf("%v %v", counts, err)
	}
	total := 0
	for _, c := range counts {
		total += c.ResponseCount
	}
	withText := count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND LENGTH(response_text) > 0", cid)
	if err := uc.AggregateConsultationResults(ctx, cid); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "aggregated rows", count(t, a, "SELECT count(*) FROM consultation_results WHERE consultation_id = $1", cid), len(counts))
	mustEqual(t, "aggregated sum", count(t, a, "SELECT sum(response_count)::int FROM consultation_results WHERE consultation_id = $1", cid), total)
	mustEqual(t, "only the rows with a text remain", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1", cid), withText)
	mustEqual(t, "anonymized", count(t, a, `SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1
		AND user_id = '00000000-0000-0000-0000-000000000000' AND participation_id = '00000000-0000-0000-0000-000000000000'`, cid), withText)
	mustEqual(t, "evicted", strings.Join(evicted, ","), cid)

	ids, err := s.Aggregated.GetAggregatedResultsConsultationIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		found = found || id == cid
	}
	mustEqual(t, "listed as aggregated", found, true)
	rows, err := s.Aggregated.GetConsultationResultAggregated(ctx, cid)
	if err != nil || len(rows) != len(counts) {
		t.Fatalf("%v %v", rows, err)
	}
	// nothing to insert is a FAILURE without any write
	ok, err := s.Aggregated.insertConsultationResultAggregated(ctx, a.DB.Pool, nil)
	mustEqual(t, "empty insert", ok, false)
	mustEqual(t, "no error", err, nil)
}

func TestIntegrationDeleteUserResponses(t *testing.T) {
	a := itApp(t)
	ctx := context.Background()
	q := &ResponseQueries{a: a}
	before := count(t, a, "SELECT count(*) FROM reponses_consultation")
	mine := count(t, a, "SELECT count(*) FROM reponses_consultation WHERE user_id = '00000000-0000-4000-9000-000000000001'")
	if mine == 0 {
		t.Fatal("seed changed")
	}
	if err := q.DeleteUserConsultationResponses(ctx, []string{"00000000-0000-4000-9000-000000000001", "not-a-uuid"}); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "deleted", count(t, a, "SELECT count(*) FROM reponses_consultation"), before-mine)
	// every id dropped: `IN ()` is a syntax error in Kotlin too
	if err := q.DeleteUserConsultationResponses(ctx, []string{"not-a-uuid"}); err == nil {
		t.Fatal("expected an error for an empty list")
	}
	if err := q.DeleteUserConsultationResponses(ctx, nil); err == nil {
		t.Fatal("expected an error for an empty list")
	}
}

// Many users answer at the same time (a consultation that closes): every answer is written, one multi-row INSERT and one
// user_answered_consultation row each, whatever the interleaving.
func TestIntegrationConcurrentAnswers(t *testing.T) {
	a := itApp(t)
	ctx := context.Background()
	ans := answered.Get(a)
	qs := &fakeQuestions{list: []consultation.Question{uniqueQ("201", "2011", "2012"), multiQ("202", 2, "2021", "2022"), openQ("203")}}
	var evicted atomic.Int32
	uc := &InsertReponseConsultationUseCase{
		now: a.Now, sanitize: sanitize.Sanitize, repository: &ResponseRepository{a: a}, answered: ans, questions: qs, info: &fakeInfo{}, openQuestionMaxTextLength: 400,
		evict: func(context.Context, string, string) { evicted.Add(1) },
	}
	const users = 60
	var wg sync.WaitGroup
	errs := make(chan error, users)
	for i := 0; i < users; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := fmt.Sprintf("00000000-0000-4000-9200-%012d", i)
			res, err := uc.InsertReponseConsultation(ctx, "co0000000000000000000002", user, []ReponseConsultationInserting{resp("201", "", "2011"), resp("203", "texte")})
			if err != nil || res != InsertSuccess {
				errs <- fmt.Errorf("user %d: %v %v", i, res, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	mustEqual(t, "answered rows", count(t, a, "SELECT count(*) FROM user_answered_consultation WHERE consultation_id = 'co0000000000000000000002'"), users)
	// 201, 202 (not applicable), 203
	mustEqual(t, "rows", count(t, a, "SELECT count(*) FROM reponses_consultation WHERE consultation_id = 'co0000000000000000000002'"), users*3)
	mustEqual(t, "participations", count(t, a, "SELECT count(DISTINCT participation_id) FROM reponses_consultation WHERE consultation_id = 'co0000000000000000000002'"), users)
	mustEqual(t, "evictions", int(evicted.Load()), users)
	n, err := ans.GetParticipantCount(ctx, "co0000000000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "participants", n, users)
}

// the eight demographic queries of a consultation in one batch
func TestIntegrationDemographicQueries(t *testing.T) {
	a := itApp(t)
	ctx := context.Background()
	q := &ResponseQueries{a: a}
	d, err := q.GetParticipantDemographicInfo(ctx, "co0000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, k := range d[dimGender].keys {
		total += d[dimGender].vals[k]
	}
	// users_profile rows of the participants of consultation 1 (the participants without a profile are not counted; two profiles that
	// map to the null key replace each other: `associate`)
	profiles := count(t, a, `SELECT count(DISTINCT user_id) FROM users_profile WHERE user_id IN (SELECT DISTINCT user_id FROM reponses_consultation WHERE consultation_id = 'co0000000000000000000001')`)
	if total == 0 || total > profiles {
		t.Fatalf("gender counts %d, profiles %d", total, profiles)
	}
	by, err := q.GetParticipantDemographicInfoByChoices(ctx, "101")
	if err != nil || len(by.Choices) == 0 {
		t.Fatalf("%v %v", by, err)
	}
	list, err := q.participantDemographicInfoByChoices(ctx, []string{"101", "102", "103"})
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %v", list, err)
	}
	mustEqual(t, "same choices", len(list[0].Choices), len(by.Choices))
}
