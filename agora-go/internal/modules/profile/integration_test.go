package profile

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/domain"
	"agora/internal/modules/users"
	"agora/internal/store"
)

// TestIntegrationDemographicAsk runs the ask logic and the profile repositories
// against a real PostgreSQL (the parity slot of the Go side):
//
//	S1_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 \
//	S1_IT_REDIS=localhost:6421 S1_IT_REDIS_PASS=gopass go test ./internal/modules/profile -run Integration
//
// The first answer of a user without profile asks for the demographic info and
// stores the date (checked the same way on the Kotlin reference).
func TestIntegrationDemographicAsk(t *testing.T) {
	dbURL := os.Getenv("S1_IT_DB")
	if dbURL == "" {
		t.Skip("S1_IT_DB not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dbURL, 2, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rdb *redis.Client
	if addr := os.Getenv("S1_IT_REDIS"); addr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S1_IT_REDIS_PASS")})
		defer rdb.Close()
	}
	a := &app.App{Cfg: &config.Config{}, DB: db, Cache: cache.New(rdb, nil, false), Clock: time.Now}
	svc := Get(a)
	uid := users.RandomUUID()
	count := func(table string) int {
		var n int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id = $1", uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ask := func() bool {
		got, err := svc.AskForDemographicInfo(ctx, uid, "consultation-1")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM user_answered_consultation WHERE user_id = $1", uid)
		_, _ = db.Pool.Exec(ctx, "DELETE FROM demographic_info_ask_date WHERE user_id = $1", uid)
		_, _ = db.Pool.Exec(ctx, "DELETE FROM users_profile WHERE user_id = $1", uid)
	}()

	if ask() {
		t.Error("no answered consultation: no ask")
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES ($1, 'c1', now(), $2)", users.RandomUUID(), uid); err != nil {
		t.Fatal(err)
	}
	if !ask() || count("demographic_info_ask_date") != 1 {
		t.Error("first time: ask and store the date")
	}
	if ask() {
		t.Error("asked less than 30 days ago (cache)")
	}
	a.Cache.InvalidateAll(ctx, askDateCacheName)
	if ask() {
		t.Error("asked less than 30 days ago (database)")
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE demographic_info_ask_date SET ask_date = now() - interval '31 days' WHERE user_id = $1", uid); err != nil {
		t.Fatal(err)
	}
	a.Cache.InvalidateAll(ctx, askDateCacheName)
	if !ask() || count("demographic_info_ask_date") != 2 {
		t.Error("31 days later: ask again, a second row is stored")
	}
	if ask() {
		t.Error("asked again just now (cache)")
	}
	a.Cache.InvalidateAll(ctx, askDateCacheName)
	if !ask() {
		// Kotlin quirk: LIMIT 1 without ORDER BY returns the oldest row, so the delay looks finished again
		t.Log("the most recent row was returned")
	}

	// profile writes: the insert removes the ask dates, the update keeps the departments
	in := ProfileInserting{UserID: uid, Gender: GenderFeminin, YearOfBirth: ptr(1990), Department: domain.FindDepartmentByCode(ptr("75"))}
	res, err := insertProfile(ctx, svc.Profiles, svc.AskDates, in)
	if err != nil || res != ProfileEditSuccess || count("demographic_info_ask_date") != 0 || count("users_profile") != 1 {
		t.Fatalf("insert: %v %v", res, err)
	}
	if ask() {
		t.Error("completed profile: no ask")
	}
	if err := svc.Profiles.UpdateDepartments(ctx, uid, domain.DepartementFrom("Paris"), domain.DepartementFrom("Nord")); err != nil {
		t.Fatal(err)
	}
	in.Gender = GenderAutre
	if res, err := insertProfile(ctx, svc.Profiles, svc.AskDates, in); err != nil || res != ProfileEditSuccess {
		t.Fatalf("update: %v %v", res, err)
	}
	p, err := svc.GetProfile(ctx, uid)
	if err != nil || p == nil || p.Gender != GenderAutre {
		t.Fatalf("profile %+v %v", p, err)
	}
	// the cached row written by the insert had no departments: Kotlin's stale-cache quirk erases them
	if p.PrimaryDepartment != nil || p.SecondaryDepartment != nil {
		t.Errorf("departments %+v %+v", p.PrimaryDepartment, p.SecondaryDepartment)
	}
	if err := svc.DeleteUsersProfile(ctx, []string{uid, "nope"}); err != nil || count("users_profile") != 0 {
		t.Errorf("delete: %v", err)
	}
}
