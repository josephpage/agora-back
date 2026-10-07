package qag

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
)

// TestIntegrationCoexistenceEvictions checks that a Go write deletes the Kotlin
// cache keys the Kotlin code would have evicted or replaced:
//
//	S2_IT_REDIS=localhost:6421 S2_IT_REDIS_PASS=gopass go test ./internal/modules/qag -run Coexistence
func TestIntegrationCoexistenceEvictions(t *testing.T) {
	addr := os.Getenv("S2_IT_REDIS")
	if addr == "" {
		t.Skip("S2_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S2_IT_REDIS_PASS")})
	defer rdb.Close()
	a := &app.App{Cfg: &config.Config{}, Cache: cache.New(rdb, nil, true), Clock: time.Now}

	keys := []string{"feedbackResults::qag-1", "userFeedbackQags::user-1/qag-1"}
	for _, k := range keys {
		if err := rdb.Set(ctx, k, "x", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	(l1FeedbackResults{a}).evict(ctx, "qag-1")
	if n, _ := rdb.Exists(ctx, keys[0]).Result(); n != 0 {
		t.Fatal("feedbackResults::qag-1 must be deleted")
	}
	if n, _ := rdb.Exists(ctx, keys[1]).Result(); n != 1 {
		t.Fatal("userFeedbackQags::user-1/qag-1 must not be touched by the results eviction")
	}
	(l1UserFeedback{a}).set(ctx, "user-1", "qag-1", true)
	if n, _ := rdb.Exists(ctx, keys[1]).Result(); n != 0 {
		t.Fatal("userFeedbackQags::user-1/qag-1 must be deleted")
	}
	v, ok := a.Cache.Get(userFeedbackCache, "user-1/qag-1")
	if !ok || v.(userFeedbackEntry).answer == nil || !*v.(userFeedbackEntry).answer {
		t.Fatalf("the new answer is kept locally: %v", v)
	}
	if coexistenceCap(a, time.Hour) != cache.CoexistenceMaxTTL {
		t.Fatal("no Go entry shared with Kotlin cache events outlives 5 minutes in coexistence mode")
	}
}
