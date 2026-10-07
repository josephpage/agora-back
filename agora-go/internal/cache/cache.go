// Package cache implements the Go caching layer:
//
//   - L1: per-process TTL cache holding Go values (no serialization) with
//     singleflight, so one instance computes a key at most once at a time;
//   - invalidation fan-out through Redis pub/sub ("agora:go:inval") so every
//     instance drops evicted keys within milliseconds;
//   - an epoch sentinel ("agora:go:epoch") polled every 2s: a FLUSHDB (admin
//     cache clear, from Go or Kotlin) clears every L1;
//   - helpers for the few keys SHARED with the Kotlin backend, in its exact
//     format ("<cacheName>::<key>", plain JSON scalars): rate limits, signup
//     counters, feature flags;
//   - a coexistence shim deleting the Kotlin cache keys a Go write would have
//     evicted on the Kotlin side.
//
// Staleness rule (plan §cache): a Go entry is never older than the TTL of the
// Kotlin cache it replaces, and every Kotlin eviction event is applied.
package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const (
	invalidationChannel = "agora:go:inval"
	epochKey            = "agora:go:epoch"
	// GoPrefix namespaces every Go-only Redis key.
	GoPrefix = "agora:go:v1:"
)

// Cache is the process-wide cache facade.
type Cache struct {
	rdb         *redis.Client
	log         *slog.Logger
	coexistence bool

	mu      sync.RWMutex
	entries map[string]entry
	group   singleflight.Group
	epoch   string
	now     func() time.Time

	// Invalidation sequencing (guarded by mu). A load that overlapped an
	// invalidation of its key is neither stored nor shared with the callers
	// that arrive after the invalidation: a user always reads their own write.
	seq       uint64               // incremented by every invalidation
	keySeq    map[string]invalMark // L1 key → its last invalidation
	nameSeq   map[string]uint64    // cache name → last name-wide invalidation
	flushSeq  uint64               // last full flush
	lastPrune time.Time
}

type invalMark struct {
	seq uint64
	at  time.Time
}

// invalHorizon bounds how long per-key invalidation marks are kept; a load
// running longer than that is not stored.
const invalHorizon = 2 * time.Minute

type entry struct {
	val     any
	expires time.Time
}

// New creates the cache. rdb may be nil (tests, cron without Redis).
func New(rdb *redis.Client, log *slog.Logger, coexistence bool) *Cache {
	if log == nil {
		log = slog.Default()
	}
	return &Cache{rdb: rdb, log: log, coexistence: coexistence, entries: map[string]entry{}, now: time.Now,
		keySeq: map[string]invalMark{}, nameSeq: map[string]uint64{}}
}

// Redis exposes the client for modules that need raw commands.
func (c *Cache) Redis() *redis.Client { return c.rdb }

// Coexistence reports whether the Kotlin backend may still be serving traffic.
func (c *Cache) Coexistence() bool { return c.coexistence }

// Run starts the invalidation subscriber and the epoch watcher.
func (c *Cache) Run(ctx context.Context) {
	if c.rdb == nil {
		return
	}
	go c.subscribe(ctx)
	go c.watchEpoch(ctx)
}

func (c *Cache) subscribe(ctx context.Context) {
	for ctx.Err() == nil {
		sub := c.rdb.Subscribe(ctx, invalidationChannel)
		ch := sub.Channel()
		for msg := range ch {
			c.applyInvalidation(msg.Payload)
			if nonce, ok := strings.CutPrefix(msg.Payload, "*\x00ack:"); ok {
				// flush acknowledgement awaited by the parity harness
				_ = c.rdb.Set(ctx, invalidationChannel+":ack:"+nonce, "1", 30*time.Second).Err()
			}
		}
		_ = sub.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (c *Cache) watchEpoch(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	c.checkEpoch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.checkEpoch(ctx)
		}
	}
}

func (c *Cache) checkEpoch(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	v, err := c.rdb.Get(cctx, epochKey).Result()
	if err == redis.Nil {
		v = strconv.FormatInt(time.Now().UnixNano(), 10)
		if ok, err := c.rdb.SetNX(cctx, epochKey, v, 0).Result(); err == nil && !ok {
			v, _ = c.rdb.Get(cctx, epochKey).Result()
		}
	} else if err != nil {
		return
	}
	c.mu.Lock()
	if c.epoch != "" && c.epoch != v {
		c.flushLocked()
		c.log.Info("cache epoch changed (FLUSHDB): L1 cleared")
	}
	c.epoch = v
	c.mu.Unlock()
}

// key layout for L1 entries: "<name>\x00<key>"
func l1Key(name, key string) string { return name + "\x00" + key }

func (c *Cache) applyInvalidation(payload string) {
	name, key, _ := strings.Cut(payload, "\x00")
	c.mu.Lock()
	defer c.mu.Unlock()
	if name == "*" {
		c.flushLocked()
		return
	}
	c.seq++
	if key == "*" {
		prefix := name + "\x00"
		for k := range c.entries {
			if strings.HasPrefix(k, prefix) {
				delete(c.entries, k)
			}
		}
		c.nameSeq[name] = c.seq
		return
	}
	k := l1Key(name, key)
	delete(c.entries, k)
	now := c.now()
	c.keySeq[k] = invalMark{seq: c.seq, at: now}
	if now.Sub(c.lastPrune) > invalHorizon/2 {
		c.lastPrune = now
		for mk, m := range c.keySeq {
			if now.Sub(m.at) > invalHorizon {
				delete(c.keySeq, mk)
			}
		}
	}
}

func (c *Cache) flushLocked() {
	c.seq++
	c.flushSeq = c.seq
	c.entries = map[string]entry{}
	c.keySeq = map[string]invalMark{}
	c.nameSeq = map[string]uint64{}
}

// lastInvalLocked is the sequence number of the latest invalidation that
// covers the L1 key k of cache name.
func (c *Cache) lastInvalLocked(name, k string) uint64 {
	last := c.flushSeq
	if s := c.nameSeq[name]; s > last {
		last = s
	}
	if m, ok := c.keySeq[k]; ok && m.seq > last {
		last = m.seq
	}
	return last
}

// Get returns a fresh L1 value.
func (c *Cache) Get(name, key string) (any, bool) {
	c.mu.RLock()
	e, ok := c.entries[l1Key(name, key)]
	c.mu.RUnlock()
	if !ok || c.now().After(e.expires) {
		return nil, false
	}
	return e.val, true
}

// Put stores a value in L1 for ttl.
func (c *Cache) Put(name, key string, v any, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	c.entries[l1Key(name, key)] = entry{val: v, expires: c.now().Add(ttl)}
	if len(c.entries) > 500_000 {
		c.evictExpiredLocked()
	}
	c.mu.Unlock()
}

func (c *Cache) evictExpiredLocked() {
	now := c.now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) > 500_000 {
		c.entries = map[string]entry{}
	}
}

// GetOrLoad returns the cached value or loads it once (singleflight).
// Errors are not cached. A load that overlapped an invalidation of its key is
// returned to the callers that joined it before the invalidation, but it is
// not stored, and callers arriving after the invalidation start a new load.
func GetOrLoad[T any](c *Cache, name, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	if v, ok := c.Get(name, key); ok {
		return v.(T), nil
	}
	k := l1Key(name, key)
	c.mu.RLock()
	gen := c.lastInvalLocked(name, k)
	c.mu.RUnlock()
	v, err, _ := c.group.Do(k+"\x00"+strconv.FormatUint(gen, 10), func() (any, error) {
		if v, ok := c.Get(name, key); ok {
			return v, nil
		}
		c.mu.RLock()
		start, startAt := c.seq, c.now()
		c.mu.RUnlock()
		val, err := load()
		if err != nil {
			return nil, err
		}
		c.putIfNotInvalidated(name, k, val, ttl, start, startAt)
		return val, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

func (c *Cache) putIfNotInvalidated(name, k string, v any, ttl time.Duration, start uint64, startAt time.Time) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.lastInvalLocked(name, k) > start || now.Sub(startAt) > invalHorizon/2 {
		return
	}
	c.entries[k] = entry{val: v, expires: now.Add(ttl)}
	if len(c.entries) > 500_000 {
		c.evictExpiredLocked()
	}
}

// Invalidate drops a key locally and on every other instance.
func (c *Cache) Invalidate(ctx context.Context, name, key string) {
	c.applyInvalidation(name + "\x00" + key)
	if c.rdb != nil {
		if err := c.rdb.Publish(ctx, invalidationChannel, name+"\x00"+key).Err(); err != nil {
			c.log.Warn("cache invalidation publish failed", "err", err)
		}
	}
}

// InvalidateAll drops every key of a cache name everywhere.
func (c *Cache) InvalidateAll(ctx context.Context, name string) { c.Invalidate(ctx, name, "*") }

// FlushLocal clears this instance's L1 (used after FLUSHDB, before epoch poll).
func (c *Cache) FlushLocal() {
	c.mu.Lock()
	c.flushLocked()
	c.mu.Unlock()
}

// FlushEverywhere clears every L1 (publishes "*").
func (c *Cache) FlushEverywhere(ctx context.Context) {
	c.FlushLocal()
	if c.rdb != nil {
		_ = c.rdb.Publish(ctx, invalidationChannel, "*\x00").Err()
	}
}

// ---------------------------------------------------------------------------
// Keys shared with the Kotlin backend (Spring RedisCache, "<cache>::<key>").
// Values are plain JSON scalars (Jackson default typing leaves natural types
// unwrapped), so both backends read and write the same bytes.
// ---------------------------------------------------------------------------

// KotlinKey builds a Spring RedisCache key.
func KotlinKey(cacheName, key string) string { return cacheName + "::" + key }

// GetSharedJSON reads a shared key and decodes its JSON value.
func (c *Cache) GetSharedJSON(ctx context.Context, cacheName, key string, out any) (bool, error) {
	if c.rdb == nil {
		return false, nil
	}
	b, err := c.rdb.Get(ctx, KotlinKey(cacheName, key)).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return false, err
	}
	return true, nil
}

// SetSharedJSON writes a shared key (ttl 0 = no expiration, like the
// "eternal" cache manager). Like RedisCache.put, every write resets the TTL.
func (c *Cache) SetSharedJSON(ctx context.Context, cacheName, key string, v any, ttl time.Duration) error {
	if c.rdb == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, KotlinKey(cacheName, key), b, ttl).Err()
}

// DeleteKotlinKeys removes Kotlin cache entries (coexistence shim). No-op
// when coexistence is off.
func (c *Cache) DeleteKotlinKeys(ctx context.Context, keys ...string) {
	if !c.coexistence || c.rdb == nil || len(keys) == 0 {
		return
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.log.Warn("coexistence shim DEL failed", "err", err)
	}
}

// DeleteKotlinPattern removes Kotlin cache entries matching a pattern with
// SCAN (never KEYS). No-op when coexistence is off.
func (c *Cache) DeleteKotlinPattern(ctx context.Context, pattern string) {
	if !c.coexistence || c.rdb == nil {
		return
	}
	iter := c.rdb.Scan(ctx, 0, pattern, 500).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) == 500 {
			_ = c.rdb.Del(ctx, batch...).Err()
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		_ = c.rdb.Del(ctx, batch...).Err()
	}
}

// FlushDB reproduces POST /admin/cache/clear (RedisConnection.flushDb).
func (c *Cache) FlushDB(ctx context.Context) error {
	if c.rdb == nil {
		c.FlushLocal()
		return nil
	}
	if err := c.rdb.FlushDB(ctx).Err(); err != nil {
		return err
	}
	c.FlushEverywhere(ctx)
	return nil
}
