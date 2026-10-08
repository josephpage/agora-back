package answered

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/config"
	"agora/internal/store"
	"agora/parity/seed"
)

// UserAnsweredConsultationRepositoryImplTest: a user id that is not a UUID never reaches the database
// (a nil database would panic).
func TestInvalidUserIDsDoNotQuery(t *testing.T) {
	r := &Repository{a: &app.App{Clock: time.Now}}
	ctx := context.Background()
	if got, err := r.HasAnsweredConsultation(ctx, "c", "invalid useId UUID"); got || err != nil {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := r.HasAnsweredConsultations(ctx, []string{"a", "b"}, "invalid"); len(got) != 0 || err != nil {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := r.HasAnsweredConsultations(ctx, nil, "00000000-0000-4000-9000-000000000001"); len(got) != 0 || err != nil {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := r.GetAnsweredConsultationIDs(ctx, "invalid"); got == nil || len(got) != 0 || err != nil {
		t.Fatalf("%#v %v", got, err)
	}
	if got, err := r.GetConsultationAnsweredCount(ctx, "invalid"); got != 0 || err != nil {
		t.Fatalf("%v %v", got, err)
	}
	if res, err := r.InsertUserAnsweredConsultation(ctx, UserAnswered{UserID: "invalid", ConsultationID: "c"}); res != Failure || err != nil {
		t.Fatalf("%v %v", res, err)
	}
}

func TestRandomUUID(t *testing.T) {
	a, b := randomUUID(), randomUUID()
	if a == b || len(a) != 36 || a[14] != '4' {
		t.Fatalf("%s %s", a, b)
	}
}

// TestIntegration runs every query against a PostgreSQL database seeded by the parity seed (opt-in):
//
//	S4_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 go test ./internal/modules/consultation/answered -run Integration
//
// It RESEEDS that database.
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("S4_IT_DB")
	if dsn == "" {
		t.Skip("S4_IT_DB not set")
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
	r := &Repository{a: &app.App{Cfg: &config.Config{}, DB: db, Clock: time.Now}}

	const co1, co4 = "co0000000000000000000001", "co0000000000000000000004"
	const regular1 = "00000000-0000-4000-9000-000000000001"
	const idle = "00000000-0000-4000-9000-00000000000f"

	if n, err := r.GetParticipantCount(ctx, co1); err != nil || n != 20 {
		t.Fatalf("participants of consultation 1: %d %v", n, err)
	}
	if n, err := r.GetParticipantCount(ctx, co4); err != nil || n != 10 {
		t.Fatalf("participants of consultation 4 (a duplicate row counts once): %d %v", n, err)
	}
	if n, err := r.GetParticipantCount(ctx, "unknown"); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	if ok, err := r.HasAnsweredConsultation(ctx, co1, regular1); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := r.HasAnsweredConsultation(ctx, "co0000000000000000000002", regular1); err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := r.HasAnsweredConsultation(ctx, co1, idle); err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	ids, err := r.GetAnsweredConsultationIDs(ctx, regular1)
	if err != nil || len(ids) != 3 {
		t.Fatalf("%v %v", ids, err)
	}
	if n, err := r.GetConsultationAnsweredCount(ctx, regular1); err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	m, err := r.HasAnsweredConsultations(ctx, []string{co1, "co0000000000000000000002", co4}, regular1)
	if err != nil || !m[co1] || m["co0000000000000000000002"] || !m[co4] || len(m) != 3 {
		t.Fatalf("%v %v", m, err)
	}
	users, err := r.GetUsersAnsweredConsultation(ctx, co4)
	if err != nil || len(users) != 10 {
		t.Fatalf("%v %v", users, err)
	}
	if res, err := r.InsertUserAnsweredConsultation(ctx, UserAnswered{UserID: idle, ConsultationID: co1}); err != nil || res != Success {
		t.Fatalf("%v %v", res, err)
	}
	if ok, _ := r.HasAnsweredConsultation(ctx, co1, idle); !ok {
		t.Fatal("inserted")
	}
	if n, _ := r.GetParticipantCount(ctx, co1); n != 21 {
		t.Fatalf("%d", n)
	}
}
