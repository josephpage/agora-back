package consultation

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

// TestIntegrationCoexistenceEvictions checks that a Go write deletes the Kotlin cache keys the Kotlin code
// would have replaced, and that the caches of data a Kotlin write can change are capped:
//
//	S4_IT_REDIS=localhost:6421 S4_IT_REDIS_PASS=gopass go test ./internal/modules/consultation -run Coexistence
func TestIntegrationCoexistenceEvictions(t *testing.T) {
	addr := os.Getenv("S4_IT_REDIS")
	if addr == "" {
		t.Skip("S4_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S4_IT_REDIS_PASS")})
	defer rdb.Close()
	a := &app.App{Cfg: &config.Config{}, Cache: cache.New(rdb, nil, true), Clock: time.Now}

	keys := []string{
		"latestConsultationDetailsV2::c1", "consultationDetailsV2::c1/u1", "hasGivenFeedbackConsultationUpdateV2::u1/user-1",
		"hasAnsweredConsultationDetailsV2::c1/user-1", "strapiOngoingConsultations::all", "strapiFinishedConsultations::Nord", "consultationCache::c1",
	}
	for _, k := range keys {
		if err := rdb.Set(ctx, k, "x", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(k string) bool { n, _ := rdb.Exists(ctx, k).Result(); return n == 1 }

	// a feedback: the Kotlin details of the consultation and the user's feedback are dropped
	u := &FeedbackUseCase{a: a}
	u.updateFeedbackStatsCache(ctx, "c1", "u1", &FeedbackStats{PositiveRatio: 50, NegativeRatio: 50, ResponseCount: 2})
	u.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(hasGivenFeedbackCacheName, "u1/user-1"))
	for _, k := range keys[:3] {
		if exists(k) {
			t.Errorf("%s must be deleted", k)
		}
	}
	// an answer to the consultation drops the "has answered" key only
	s := &Service{a: a}
	s.EvictHasAnswered(ctx, "c1", "user-1")
	if exists(keys[3]) || !exists(keys[4]) || !exists(keys[6]) {
		t.Error("EvictHasAnswered")
	}
	// the daily task clears the Kotlin lists
	s.ClearConsultationCaches(ctx)
	if exists(keys[4]) || exists(keys[5]) || !exists(keys[6]) {
		t.Error("ClearConsultationCaches")
	}
	if a.Cache.CoexistenceTTL(detailsCacheTTL) != cache.CoexistenceMaxTTL || a.Cache.CoexistenceTTL(participantCountTTL) != cache.CoexistenceMaxTTL {
		t.Error("the caches of data Kotlin can write are capped while both backends run")
	}
}
