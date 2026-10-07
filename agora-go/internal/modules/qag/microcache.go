package qag

import (
	"errors"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
)

// uncacheable carries a value that must be returned to the caller but not stored.
type uncacheable[T any] struct{ v T }

func (uncacheable[T]) Error() string { return "uncacheable" }

// microLoad is cache.GetOrLoad for the loads that decide whether their result
// may be stored (second result of load): errors and "do not store" results are
// returned to the callers that joined the load but never kept.
func microLoad[T any](a *app.App, name, key string, ttl time.Duration, load func() (T, bool, error)) (T, error) {
	v, err := cache.GetOrLoad(a.Cache, name, key, ttl, func() (T, error) {
		v, store, err := load()
		if err != nil {
			return v, err
		}
		if !store {
			return v, uncacheable[T]{v}
		}
		return v, nil
	})
	var u uncacheable[T]
	if errors.As(err, &u) {
		return u.v, nil
	}
	return v, err
}

// coexistenceCap is the "cap Go caches" rule of AGORA_COEXISTENCE: while the
// Kotlin backend may still write the data, long-lived Go entries shared with
// Kotlin cache events live at most 5 minutes (their Kotlin evictions cannot be
// observed).
func coexistenceCap(a *app.App, ttl time.Duration) time.Duration {
	if a.Cache.Coexistence() && ttl > 5*time.Minute {
		return 5 * time.Minute
	}
	return ttl
}
