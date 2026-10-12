package consultationresponse

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
)

// Kotlin cache of ConsultationResponseRateLimitRepositoryImpl: the default
// RedisCacheManager (entries live one hour from their last write), key
// "consultationResponseRateLimit::<ip address hash>", value the Jackson form of an Int:
// the bare number ("5"). The format is shared with the Kotlin backend (both read and
// write it while they run together).
const (
	rateLimitCacheName = "consultationResponseRateLimit"
	rateLimitTTL       = time.Hour
)

// rateLimitScript is CheckConsultationResponseRateLimitUseCase.isRateLimited done in
// one atomic step (Kotlin read the count, compared it, then wrote count + 1: two
// simultaneous requests could both pass). ARGV[1] is the maximum (0 = limiting
// disabled: the counter is still incremented), ARGV[2] the TTL in milliseconds.
//
//   - the stored value is read like `cache.get(key).get() as? Int ?: 0`: only a JSON
//     integer that fits an Int counts, anything else (a string, a Long, a typed
//     value, garbage) is 0 and is overwritten;
//   - limited when max != 0 and count >= max (the counter is left untouched and keeps
//     its TTL);
//   - otherwise `put(key, count + 1)`: the Int wraps around and the TTL restarts, which
//     makes the window a sliding one.
const rateLimitScript = `
local v = redis.call('GET', KEYS[1])
local count = 0
if v and string.match(v, '^%-?%d+$') and not string.match(v, '^%-?0%d') then
  local n = tonumber(v)
  if n >= -2147483648 and n <= 2147483647 then count = n end
end
local max = tonumber(ARGV[1])
if max ~= 0 and count >= max then return 1 end
local nc = count + 1
if nc > 2147483647 then nc = -2147483648 end
redis.call('SET', KEYS[1], string.format('%d', nc), 'PX', ARGV[2])
return 0
`

// RateLimiter is ConsultationResponseRateLimitRepositoryImpl +
// CheckConsultationResponseRateLimitUseCase.
type RateLimiter struct {
	max   int
	store rateStore
}

// rateStore runs the check-and-increment atomically.
type rateStore interface {
	checkAndIncrement(ctx context.Context, key string, max int, ttl time.Duration) (limited bool, err error)
}

// NewRateLimiter builds the limiter of the app (Redis shared with the Kotlin backend).
func newRateLimiter(a *app.App) *RateLimiter {
	var st rateStore
	if rdb := a.Cache.Redis(); rdb != nil {
		st = &redisRateStore{rdb: rdb, script: redis.NewScript(rateLimitScript)}
	} else {
		st = newMemoryRateStore()
	}
	return &RateLimiter{max: a.Cfg.ConsultationResponseRateLimitPerHour, store: st}
}

// IsRateLimited returns true when the IP is rate limited (the request is rejected with
// 429); the counter is incremented when it is not. A Redis failure is the exception
// RedisCache.put throws in Kotlin (HTTP 500).
func (l *RateLimiter) IsRateLimited(ctx context.Context, ipAddressHash string) bool {
	limited, err := l.store.checkAndIncrement(ctx, cache.KotlinKey(rateLimitCacheName, ipAddressHash), l.max, rateLimitTTL)
	if err != nil {
		panic(err)
	}
	return limited
}

type redisRateStore struct {
	rdb    *redis.Client
	script *redis.Script
}

func (s *redisRateStore) checkAndIncrement(ctx context.Context, key string, max int, ttl time.Duration) (bool, error) {
	res, err := s.script.Run(ctx, s.rdb, []string{key}, max, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// memoryRateStore is the same rule over a map (tests, process without Redis).
type memoryRateStore struct {
	mu        sync.Mutex
	counts    map[string]int
	expires   map[string]time.Time
	now       func() time.Time
	lastPrune time.Time
}

func newMemoryRateStore() *memoryRateStore {
	return &memoryRateStore{counts: map[string]int{}, expires: map[string]time.Time{}, now: time.Now}
}

func (s *memoryRateStore) checkAndIncrement(_ context.Context, key string, max int, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now := s.now(); now.Sub(s.lastPrune) > time.Minute {
		// expired windows count as 0 anyway: drop them so the maps stay bounded by the
		// clients of the last window (one entry per IP hash otherwise, for the process life)
		s.lastPrune = now
		for k, exp := range s.expires {
			if !now.Before(exp) {
				delete(s.expires, k)
				delete(s.counts, k)
			}
		}
	}
	count := 0
	if exp, ok := s.expires[key]; ok && s.now().Before(exp) {
		count = s.counts[key]
	}
	if max != 0 && count >= max {
		return true, nil
	}
	nc := int32(count) + 1 // Int arithmetic
	s.counts[key] = int(nc)
	s.expires[key] = s.now().Add(ttl)
	return false, nil
}
