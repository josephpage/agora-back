package consultationresponse

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
)

// Coexistence with the Kotlin backend (opt-in, a real Redis):
//
//	S6_IT_REDIS=localhost:6421 S6_IT_REDIS_PASS=gopass go test ./internal/modules/consultationresponse -run Coexistence
func TestCoexistenceKotlinKeysAreDeleted(t *testing.T) {
	addr := os.Getenv("S6_IT_REDIS")
	if addr == "" {
		t.Skip("S6_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S6_IT_REDIS_PASS"), DB: 6})
	defer rdb.Close()
	rdb.FlushDB(ctx)
	defer rdb.FlushDB(ctx)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	for _, coexistence := range []bool{true, false} {
		c := cache.New(rdb, nil, coexistence)
		c.Run(runCtx)
		a := &app.App{Cfg: &config.Config{}, Cache: c, Clock: time.Now}
		a.Log = newApp(false).Log
		s := &Service{a: a}
		rdb.Set(ctx, "consultationResults::c1", `"{}"`, 0)
		rdb.Set(ctx, "consultationResults::c2", `"{}"`, 0)
		rdb.Set(ctx, "consultationResponseRateLimit::ip", "3", 0)
		s.EvictConsultationResults(ctx, "c1")
		n, _ := rdb.Exists(ctx, "consultationResults::c1").Result()
		mustEqual(t, "c1 deleted only in coexistence", n == 0, coexistence)
		n, _ = rdb.Exists(ctx, "consultationResults::c2").Result()
		mustEqual(t, "c2 kept", n, int64(1))
		s.EvictAllConsultationResults(ctx)
		n, _ = rdb.Exists(ctx, "consultationResults::c2").Result()
		mustEqual(t, "c2 deleted by the full eviction only in coexistence", n == 0, coexistence)
		n, _ = rdb.Exists(ctx, "consultationResponseRateLimit::ip").Result()
		mustEqual(t, "the rate limit counters are never touched", n, int64(1))
	}
}

// a second Go instance drops its entry when the first one is evicted (pub/sub)
func TestInvalidationReachesTheOtherInstances(t *testing.T) {
	addr := os.Getenv("S6_IT_REDIS")
	if addr == "" {
		t.Skip("S6_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S6_IT_REDIS_PASS"), DB: 6})
	defer rdb.Close()
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	mk := func() (*app.App, *ConsultationResultsUseCase, *fakeInfo) {
		c := cache.New(rdb, nil, false)
		c.Run(runCtx)
		a := &app.App{Cfg: &config.Config{}, Cache: c, Clock: time.Now, Log: newApp(false).Log}
		uc, fi, _ := newResults(a, someInfo, nil, &fakeResultsSources{})
		return a, uc, fi
	}
	a1, uc1, fi1 := mk()
	_, uc2, fi2 := mk()
	time.Sleep(300 * time.Millisecond) // the subscriptions
	uc1.GetConsultationResults(ctx, "c")
	uc2.GetConsultationResults(ctx, "c")
	uc2.GetConsultationResults(ctx, "c")
	mustEqual(t, "one load per instance", fi1.calls+fi2.calls, 2)
	(&Service{a: a1}).EvictConsultationResults(ctx, "c")
	deadline := time.Now().Add(2 * time.Second)
	var reloaded atomic.Bool
	for time.Now().Before(deadline) && !reloaded.Load() {
		uc2.GetConsultationResults(ctx, "c")
		reloaded.Store(fi2.calls == 2)
		time.Sleep(10 * time.Millisecond)
	}
	mustEqual(t, "the other instance recomputed", fi2.calls, 2)
}
