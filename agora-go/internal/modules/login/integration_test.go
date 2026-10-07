package login

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/users"
	"agora/internal/store"
)

// TestIntegrationSuspiciousSignups runs the soft-ban logic against a real
// PostgreSQL and Redis (the parity slot of the Go side):
//
//	S1_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 \
//	S1_IT_REDIS=localhost:6421 S1_IT_REDIS_PASS=gopass go test ./internal/modules/login -run Integration
//
// The expected values are the ones observed on the Kotlin reference with the
// same sequence (10 signups, a support call, an 11th signup, another agent).
func TestIntegrationSuspiciousSignups(t *testing.T) {
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
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("S1_IT_REDIS"), Password: os.Getenv("S1_IT_REDIS_PASS")})
	defer rdb.Close()
	a := &app.App{Cfg: &config.Config{}, DB: db, Cache: cache.New(rdb, nil, false), Clock: time.Now}
	svc := Get(a)

	t.Setenv("IS_SUSPICIOUS_USER_DETECTION_ENABLED", "true")
	rdb.Del(ctx, "featureFlags::IS_SUSPICIOUS_USER_DETECTION_ENABLED")
	run := time.Now().UnixNano()
	ip := fmt.Sprintf("it-ip-%d", run)
	ua := fmt.Sprintf("it-ua-%d", run)
	keyOf := func(agent string) string { return "signupCount::" + ip + "/" + agent }
	signup := func(agent string) string {
		u, err := svc.Login.SignUp(ctx, users.SignupRequest{IPAddressHash: ip, UserAgent: agent, FCMToken: "t", Platform: "android", VersionName: "n", VersionCode: "1"})
		if err != nil {
			t.Fatal(err)
		}
		return u.UserID
	}
	var ids []string
	for i := 0; i < 10; i++ {
		ids = append(ids, signup(ua))
	}
	if n, _ := rdb.Exists(ctx, keyOf(ua)).Result(); n != 0 {
		t.Fatal("signups must not create the counter")
	}
	suspicious, err := svc.IsSuspiciousActivity(ctx, ip, ua)
	if err != nil || !suspicious {
		t.Fatalf("10 signups today: suspicious=%v err=%v", suspicious, err)
	}
	if v, _ := rdb.Get(ctx, keyOf(ua)).Result(); v != "10" {
		t.Errorf("counter %q", v)
	}
	if ttl, _ := rdb.TTL(ctx, keyOf(ua)).Result(); ttl < 23*time.Hour || ttl > 24*time.Hour {
		t.Errorf("ttl %v", ttl)
	}
	signup(ua)
	if v, _ := rdb.Get(ctx, keyOf(ua)).Result(); v != "11" {
		t.Errorf("counter after the 11th signup %q", v)
	}
	if s, err := svc.IsSuspiciousActivity(ctx, ip, ua+"-other"); err != nil || s {
		t.Errorf("another user agent: %v %v", s, err)
	}
	if v, _ := rdb.Get(ctx, keyOf(ua+"-other")).Result(); v != "0" {
		t.Errorf("other counter %q", v)
	}
	// the counter is incremented by later signups only once it exists
	other := ua + "-other"
	for i := 0; i < 9; i++ {
		signup(other)
	}
	if v, _ := rdb.Get(ctx, keyOf(other)).Result(); v != "9" {
		t.Errorf("counter %q", v)
	}
	if s, _ := svc.IsSuspiciousActivity(ctx, ip, other); s {
		t.Error("9 is below the threshold")
	}
	signup(other)
	if s, _ := svc.IsSuspiciousActivity(ctx, ip, other); !s {
		t.Error("10 reaches the threshold")
	}
	// history path: drop the counter, the signup history of the day is counted again
	rdb.Del(ctx, keyOf(ua))
	if s, _ := svc.IsSuspiciousActivity(ctx, ip, ua); !s {
		t.Error("the history says 11 signups")
	}
	if v, _ := rdb.Get(ctx, keyOf(ua)).Result(); v != "11" {
		t.Errorf("rebuilt counter %q", v)
	}
	// disabled feature
	rdb.Set(ctx, "featureFlags::IS_SUSPICIOUS_USER_DETECTION_ENABLED", "false", 0)
	if s, _ := svc.IsSuspiciousActivity(ctx, ip, ua); s {
		t.Error("disabled feature never flags")
	}
	rdb.Del(ctx, "featureFlags::IS_SUSPICIOUS_USER_DETECTION_ENABLED")

	// nightly job: 3 signups a day with the same origin are banned
	flagged, err := svc.FlagSuspiciousUsers(ctx)
	if err != nil || flagged < 10 {
		t.Fatalf("flagged %d err %v", flagged, err)
	}
	var banned int
	if err := db.Pool.QueryRow(ctx, "SELECT is_banned FROM agora_users WHERE id = $1", ids[0]).Scan(&banned); err != nil || banned != 1 {
		t.Errorf("banned %d err %v", banned, err)
	}
	if err := svc.DeleteUsersData(ctx, ids); err != nil {
		t.Error(err)
	}
	if _, err := users.Get(a).ChangeAuthorizationLevel(ctx, ids, 8); err != nil {
		t.Error(err)
	}
	if err := users.Get(a).DeleteUsers(ctx, ids); err != nil {
		t.Error(err)
	}
}
