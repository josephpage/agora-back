package consultation

import (
	"crypto/rand"
	"errors"
	"strconv"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func epochDay(d LocalDateTime) int64 {
	s := d.T.Unix()
	days := s / 86400
	if s%86400 < 0 {
		days--
	}
	return days
}

func secondOfDay(d LocalDateTime) int64 {
	return int64(d.T.Hour()*3600 + d.T.Minute()*60 + d.T.Second())
}

// timeOfDay compares the time parts like LocalTime.compareTo.
func timeBefore(a, b LocalDateTime) bool {
	sa, sb := secondOfDay(a), secondOfDay(b)
	if sa != sb {
		return sa < sb
	}
	return a.T.Nanosecond() < b.T.Nanosecond()
}

// daysBetween is ChronoUnit.DAYS.between(a, b) for two LocalDateTime: the
// number of whole days, truncated toward zero.
func daysBetween(a, b LocalDateTime) int64 {
	ad, bd := epochDay(a), epochDay(b)
	if bd > ad && timeBefore(b, a) {
		bd--
	} else if bd < ad && timeBefore(a, b) {
		bd++
	}
	return bd - ad
}

// must unwraps a (value, error) pair: an error is an exception Kotlin does not
// catch (HTTP 500).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// errNotStored marks a load whose result must be returned but not cached.
type notStored[T any] struct{ v T }

func (notStored[T]) Error() string { return "not stored" }

// loadCached is cache.GetOrLoad for a load that decides whether its result may
// be stored (second result): errors and "do not store" results are returned to
// the callers that joined the load but never kept.
func loadCached[T any](a *app.App, name, key string, ttl time.Duration, load func() (T, bool, error)) (T, error) {
	v, err := cache.GetOrLoad(a.Cache, name, key, ttl, func() (T, error) {
		v, store, err := load()
		if err != nil {
			return v, err
		}
		if !store {
			return v, notStored[T]{v}
		}
		return v, nil
	})
	var u notStored[T]
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

// randomUUID is UUID.randomUUID() (also what Hibernate's GenerationType.UUID
// produces): a random version 4 UUID in canonical lowercase form.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return javacompat.UUIDFromBytes(b).String()
}
