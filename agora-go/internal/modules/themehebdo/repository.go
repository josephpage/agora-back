package themehebdo

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"
)

// listCache is ThemeHebdoCacheRepository's list entry ("themeHebdoCache" /
// "themeHebdoList"), only used when THEME_HEBDO_CACHE_ENABLED=true.
type listCache interface {
	get() ([]ThemeHebdo, bool)
	put([]ThemeHebdo)
}

// repository is ThemeHebdoRepositoryImpl.
type repository struct {
	// cacheEnabled is THEME_HEBDO_CACHE_ENABLED (String?.toBoolean()).
	cacheEnabled bool
	cache        listCache
	// fetch is ThemeHebdoStrapiRepository.getThemeHebdo() (empty on Strapi failure).
	fetch func(ctx context.Context) []*strapiThemeHebdo
	// micro caches the Strapi list for a few seconds when the Kotlin cache is
	// disabled (Kotlin then calls Strapi at every request). Errors are never cached.
	micro listCache

	group singleflight.Group
}

// List is getThemeHebdoList(): mapper errors (a date OffsetDateTime.parse rejects)
// are uncaught exceptions in Kotlin and are returned as errors.
func (r *repository) List(ctx context.Context) ([]ThemeHebdo, error) {
	if !r.cacheEnabled {
		return r.microList(ctx)
	}
	if l, ok := r.cache.get(); ok {
		return l, nil
	}
	v, err, _ := r.group.Do("list", func() (any, error) {
		if l, ok := r.cache.get(); ok {
			return l, nil
		}
		l, err := r.load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		if len(l) > 0 { // getThemeHebdoListAndCacheIt: only a non-empty list is cached
			r.cache.put(l)
		}
		return l, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]ThemeHebdo), nil
}

func (r *repository) microList(ctx context.Context) ([]ThemeHebdo, error) {
	if r.micro == nil {
		return r.load(ctx)
	}
	if l, ok := r.micro.get(); ok {
		return l, nil
	}
	v, err, _ := r.group.Do("micro", func() (any, error) {
		if l, ok := r.micro.get(); ok {
			return l, nil
		}
		l, err := r.load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		if len(l) > 0 { // a failed/empty Strapi answer is retried at every request, like Kotlin
			r.micro.put(l)
		}
		return l, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]ThemeHebdo), nil
}

// load is strapiRepository.getThemeHebdo().let(mapper::toDomain).
func (r *repository) load(ctx context.Context) ([]ThemeHebdo, error) {
	return toDomain(r.fetch(ctx))
}

// ttl of the Kotlin shortTermCacheManager (themeHebdoCache).
const shortTermTTL = 5 * time.Minute
