package admin

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/cache"
)

// TestIntegrationClearCacheFlushesRedis is opt-in: S10_IT_REDIS=localhost:6421
// S10_IT_REDIS_PASS=... go test ./internal/modules/admin -run Integration.
// It FLUSHDB the Redis database of S10_IT_REDIS (a dedicated test instance, like the
// parity Redis). Checked: the Kotlin keys go, the Go entries of this instance are
// dropped, and a second Go instance (pub/sub) drops its entries too.
func TestIntegrationClearCacheFlushesRedis(t *testing.T) {
	addr := os.Getenv("S10_IT_REDIS")
	if addr == "" {
		t.Skip("S10_IT_REDIS not set")
	}
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("S10_IT_REDIS_PASS")})
	defer rdb.Close()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	writer := cache.New(rdb, logger, true)
	writer.Run(ctx)
	other := cache.New(rdb, logger, true)
	other.Run(ctx)
	time.Sleep(100 * time.Millisecond) // let the subscribers connect

	if err := rdb.Set(ctx, cache.KotlinKey("userCache", "s10-it-user"), "1", 0).Err(); err != nil {
		t.Fatal(err)
	}
	writer.Put("users", "s10-it-user", "cached", time.Minute)
	other.Put("users", "s10-it-user", "cached", time.Minute)

	if err := clearAllCaches(ctx, writer.FlushDB); err != nil {
		t.Fatal(err)
	}
	if n := rdb.Exists(ctx, cache.KotlinKey("userCache", "s10-it-user")).Val(); n != 0 {
		t.Errorf("the Kotlin key survived FLUSHDB")
	}
	if _, ok := writer.Get("users", "s10-it-user"); ok {
		t.Errorf("the writing instance kept its L1 entry")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := other.Get("users", "s10-it-user"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the other instance kept its L1 entry after the flush")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
