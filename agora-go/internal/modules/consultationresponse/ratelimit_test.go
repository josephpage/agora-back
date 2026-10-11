package consultationresponse

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/cache"
)

func memLimiter(max int) (*RateLimiter, *memoryRateStore) {
	st := newMemoryRateStore()
	return &RateLimiter{max: max, store: st}, st
}

// ConsultationResponseRateLimitRepositoryImplTest + CheckConsultationResponseRateLimitUseCaseTest
func TestRateLimitBelowEqualsAndAboveTheLimit(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		count int
		want  bool
	}{{"5", 5, false}, {"19", 19, false}, {"20", 20, true}, {"99", 99, true}, {"none", 0, false}} {
		t.Run(tt.name, func(t *testing.T) {
			l, st := memLimiter(20)
			// the key of the store is the Kotlin key
			st.counts[cache.KotlinKey(rateLimitCacheName, "ipHash123")] = tt.count
			st.expires[cache.KotlinKey(rateLimitCacheName, "ipHash123")] = time.Now().Add(time.Hour)
			mustEqual(t, "limited", l.IsRateLimited(ctx, "ipHash123"), tt.want)
		})
	}
}

func TestRateLimitIncrementsOnlyWhenNotLimited(t *testing.T) {
	ctx := context.Background()
	l, st := memLimiter(3)
	key := cache.KotlinKey(rateLimitCacheName, "ip")
	for i := 1; i <= 3; i++ {
		mustEqual(t, "not limited", l.IsRateLimited(ctx, "ip"), false)
		mustEqual(t, "count", st.counts[key], i)
	}
	for i := 0; i < 3; i++ {
		mustEqual(t, "limited", l.IsRateLimited(ctx, "ip"), true)
		mustEqual(t, "count unchanged", st.counts[key], 3)
	}
	mustEqual(t, "other address", l.IsRateLimited(ctx, "other"), false)
}

func TestRateLimit21stIsRefused(t *testing.T) {
	ctx := context.Background()
	l, _ := memLimiter(20)
	for i := 1; i <= 20; i++ {
		mustEqual(t, "request", l.IsRateLimited(ctx, "ip"), false)
	}
	mustEqual(t, "21st", l.IsRateLimited(ctx, "ip"), true)
}

// 0 disables the limit but the counter still counts; a negative maximum refuses everything
func TestRateLimitZeroAndNegative(t *testing.T) {
	ctx := context.Background()
	l, st := memLimiter(0)
	for i := 0; i < 50; i++ {
		mustEqual(t, "disabled", l.IsRateLimited(ctx, "ip"), false)
	}
	mustEqual(t, "still counting", st.counts[cache.KotlinKey(rateLimitCacheName, "ip")], 50)
	l, _ = memLimiter(-1)
	mustEqual(t, "negative", l.IsRateLimited(ctx, "ip"), true)
}

func TestRateLimitWindowSlides(t *testing.T) {
	ctx := context.Background()
	l, st := memLimiter(2)
	now := time.Now()
	st.now = func() time.Time { return now }
	l.IsRateLimited(ctx, "ip")
	now = now.Add(50 * time.Minute)
	l.IsRateLimited(ctx, "ip") // the write restarts the hour
	now = now.Add(50 * time.Minute)
	mustEqual(t, "still limited: 100 minutes after the first request, 50 after the second", l.IsRateLimited(ctx, "ip"), true)
	now = now.Add(11 * time.Minute)
	mustEqual(t, "an hour after the last counted request", l.IsRateLimited(ctx, "ip"), false)
}

func TestRateLimitConcurrent(t *testing.T) {
	ctx := context.Background()
	l, _ := memLimiter(20)
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !l.IsRateLimited(ctx, "ip") {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	mustEqual(t, "exactly the limit passes", int(passed.Load()), 20)
}

// The Lua script against a real Redis (opt-in): S6_IT_REDIS=localhost:6421 S6_IT_REDIS_PASS=gopass go test ./internal/modules/consultationresponse -run Redis
func TestRateLimitRedisScript(t *testing.T) {
	addr := os.Getenv("S6_IT_REDIS")
	if addr == "" {
		t.Skip("S6_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S6_IT_REDIS_PASS"), DB: 5})
	defer rdb.Close()
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	defer rdb.FlushDB(ctx)
	st := &redisRateStore{rdb: rdb, script: redis.NewScript(rateLimitScript)}
	l := &RateLimiter{max: 20, store: st}
	key := cache.KotlinKey(rateLimitCacheName, "iphash")

	// the value is the bare number, the TTL one hour, restarted by every counted request
	mustEqual(t, "first", l.IsRateLimited(ctx, "iphash"), false)
	v, _ := rdb.Get(ctx, key).Result()
	mustEqual(t, "value", v, "1")
	rdb.PExpire(ctx, key, 10*time.Second)
	l.IsRateLimited(ctx, "iphash")
	ttl, _ := rdb.PTTL(ctx, key).Result()
	if ttl < 59*time.Minute || ttl > time.Hour {
		t.Fatalf("ttl %v", ttl)
	}
	// 20 requests passed? no: 2 so far; 18 more, then the 21st is refused and leaves the counter and the TTL alone
	for i := 0; i < 18; i++ {
		mustEqual(t, "request", l.IsRateLimited(ctx, "iphash"), false)
	}
	rdb.PExpire(ctx, key, 10*time.Second)
	mustEqual(t, "21st", l.IsRateLimited(ctx, "iphash"), true)
	v, _ = rdb.Get(ctx, key).Result()
	mustEqual(t, "value after refusal", v, "20")
	ttl, _ = rdb.PTTL(ctx, key).Result()
	if ttl > 10*time.Second {
		t.Fatalf("a refusal restarted the TTL: %v", ttl)
	}

	// values written by hand (Jackson Integer or not)
	cases := []struct {
		value   string
		limited bool
		after   string
	}{
		{"19", false, "20"}, {"20", true, "20"}, {"21", true, "21"}, {"0", false, "1"}, {"-1", false, "0"}, {"-0", false, "1"}, {"garbage", false, "1"}, {`"20"`, false, "1"},
		{"20.0", false, "1"}, {"99999999999", false, "1"}, {"2147483647", true, "2147483647"}, {"-2147483648", false, "-2147483647"}, {"", false, "1"}, {"00025", false, "1"},
		{"+5", false, "1"}, {"1e1", false, "1"}, {`["java.lang.Integer",5]`, false, "1"},
	}
	for _, c := range cases {
		rdb.Set(ctx, key, c.value, 0)
		mustEqual(t, "limited for "+c.value, l.IsRateLimited(ctx, "iphash"), c.limited)
		v, _ = rdb.Get(ctx, key).Result()
		mustEqual(t, "value after "+c.value, v, c.after)
	}
	// the Int wraps around
	zero := &RateLimiter{max: 0, store: st}
	rdb.Set(ctx, key, "2147483647", 0)
	mustEqual(t, "disabled", zero.IsRateLimited(ctx, "iphash"), false)
	v, _ = rdb.Get(ctx, key).Result()
	mustEqual(t, "wrapped", v, "-2147483648")

	// atomic: 300 simultaneous requests, exactly 20 pass
	rdb.Del(ctx, key)
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 300; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !l.IsRateLimited(ctx, "iphash") {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	mustEqual(t, "passed", int(passed.Load()), 20)
}
