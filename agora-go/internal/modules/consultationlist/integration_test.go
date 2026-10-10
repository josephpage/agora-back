package consultationlist

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultation/answered"
	"agora/internal/store"
	"agora/parity/seed"
)

// dbAnswered is the real user_answered_consultation repository for the count and the ids; the Strapi part is
// replaced by one fake consultation per answered id.
type dbAnswered struct{ r *answered.Repository }

func (d dbAnswered) GetConsultationAnsweredCount(ctx context.Context, userID string) (int, error) {
	return d.r.GetConsultationAnsweredCount(ctx, userID)
}

func (d dbAnswered) GetConsultationAnsweredList(ctx context.Context, userID string, offset int) ([]consultation.ConsultationWithUpdateInfo, error) {
	ids, err := d.r.GetAnsweredConsultationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]consultation.ConsultationWithUpdateInfo, len(ids))
	for i, id := range ids {
		out[i] = info(id)
	}
	return out, nil
}

// TestIntegrationEvictionAfterAnInsert runs the eviction path of the answered pages against a real PostgreSQL
// (reseeded!) and a real Redis (opt-in):
//
//	S5_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 S5_IT_REDIS=localhost:6411 S5_IT_REDIS_PASS=gopass \
//	  go test ./internal/modules/consultationlist -run Integration
//
// A user reads their answered consultations, answers (InsertUserAnsweredConsultation), and at once reads the new
// list, on the instance that wrote and on another Go instance; in coexistence mode the Kotlin keys of that user go.
func TestIntegrationEvictionAfterAnInsert(t *testing.T) {
	dsn, addr := os.Getenv("S5_IT_DB"), os.Getenv("S5_IT_REDIS")
	if dsn == "" || addr == "" {
		t.Skip("S5_IT_DB / S5_IT_REDIS not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Seed(ctx, conn, time.Now().UTC().Truncate(time.Second), 1); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	db, err := store.Open(ctx, dsn, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S5_IT_REDIS_PASS")})
	defer rdb.Close()

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	// two Go instances sharing the Redis (pub/sub invalidation)
	mk := func() (*app.App, *Service, *AnsweredUseCase) {
		c := cache.New(rdb, nil, true)
		c.Run(runCtx)
		a := &app.App{Cfg: &config.Config{}, DB: db, Cache: c, Clock: time.Now}
		ans := answered.Get(a)
		s := &Service{a: a}
		ans.OnInserted(s.EvictAnswered) // what build() registers
		return a, s, &AnsweredUseCase{a: a, repo: dbAnswered{ans}, themes: newThemes()}
	}
	a1, s1, uc1 := mk()
	_, _, uc2 := mk()
	time.Sleep(300 * time.Millisecond) // the subscribers are up

	const idle = "00000000-0000-4000-9000-00000000000f"
	const other = "00000000-0000-4000-9000-00000000000e"
	read := func(uc *AnsweredUseCase, user string, page int) *AnsweredList {
		t.Helper()
		l, err := uc.GetConsultationAnsweredPaginatedList(ctx, user, page)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	for _, uc := range []*AnsweredUseCase{uc1, uc2} {
		if l := read(uc, idle, 1); l == nil || len(l.Consultations) != 0 || l.MaxPageNumber != 0 {
			t.Fatalf("a user without answers: %+v", l)
		}
		if l := read(uc, idle, 2); l != nil {
			t.Fatalf("page 2 must not exist: %+v", l)
		}
	}

	// the Kotlin keys of the user and of someone else
	keys := []string{"consultationsAnsweredPaginated" + idle + "::1", "consultationsAnsweredPaginated" + idle + "::2",
		"consultationsAnsweredPaginated" + other + "::1", "consultationsFinishedPaginated::France-1", "consultationsFinishedPaginated::Nord-2"}
	for _, k := range keys {
		if err := rdb.Set(ctx, k, "x", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(k string) bool { n, _ := rdb.Exists(ctx, k).Result(); return n == 1 }

	// the user answers: the repository evicts after the commit
	res, err := answered.Get(a1).InsertUserAnsweredConsultation(ctx, answered.UserAnswered{UserID: idle, ConsultationID: "co0000000000000000000001"})
	if err != nil || res != answered.Success {
		t.Fatalf("%v %v", res, err)
	}
	l := read(uc1, idle, 1)
	if l == nil || len(l.Consultations) != 1 || l.Consultations[0].ID != "co0000000000000000000001" || l.MaxPageNumber != 1 {
		t.Fatalf("the writer instance must see the answer at once: %+v", l)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		l = read(uc2, idle, 1)
		if len(l.Consultations) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the other instance still serves the old page")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if exists(keys[0]) || exists(keys[1]) || !exists(keys[2]) || !exists(keys[3]) {
		t.Error("coexistence: only the Kotlin keys of the user must be deleted by an answer")
	}

	// the daily clearing of the finished pages
	s1.clearFinishedPages(ctx)
	if exists(keys[3]) || exists(keys[4]) || !exists(keys[2]) {
		t.Error("the Kotlin finished pages must be deleted, nothing else")
	}

	// a refused insert (user id that is not a UUID) evicts nothing and fails
	if res, err := answered.Get(a1).InsertUserAnsweredConsultation(ctx, answered.UserAnswered{UserID: "nope", ConsultationID: "c"}); err != nil || res != answered.Failure {
		t.Fatalf("%v %v", res, err)
	}
	if !exists(keys[2]) {
		t.Error("a failed insert must not evict")
	}
	// the caches of data Kotlin can write are capped in coexistence
	if got := (&AnsweredUseCase{a: a1}).ttl(); got != cache.CoexistenceMaxTTL {
		t.Errorf("%v", got)
	}
}
