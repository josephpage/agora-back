package consultation

import (
	"context"
	"sort"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/javacompat"
)

// Kotlin caches of ConsultationInfoRepositoryImpl.
const (
	// consultationCacheName is "consultationCache": the default RedisCacheManager
	// (1 hour), keys = the id, the slug and whatever string was asked for.
	consultationCacheName = "consultationCache"
	consultationCacheTTL  = time.Hour

	// the lists of ConsultationStrapiCacheRepositoryImpl (shortTermCacheManager, 5
	// minutes). The Kotlin read of those entries fails whenever a consultation has
	// questions (Jackson cannot rebuild the polymorphic questions: "missing type id
	// property '__component'"), so Kotlin asks Strapi at every call and only an empty
	// list is ever served from the cache: Go shares a non empty list for
	// AGORA_MICROCACHE_TTL (class B, parity/divergences/S4.md S4-B2).
	ongoingListCacheName  = "strapiOngoingConsultations"
	finishedListCacheName = "strapiFinishedConsultations"
)

// InfoRepository is ConsultationInfoRepositoryImpl.
type InfoRepository struct {
	a        *app.App
	strapi   *StrapiRepository
	answered interface {
		GetAnsweredConsultationIDs(ctx context.Context, userID string) ([]string, error)
	}
	mapper infoMapper
}

// territoryKey is ConsultationStrapiCacheRepositoryImpl.toTerritoryKey.
func territoryKey(territories []domain.Territoire) string {
	values := make([]string, len(territories))
	for i, t := range territories {
		values[i] = t.Value()
	}
	sort.Strings(values)
	if k := strings.Join(values, ","); k != "" {
		return k
	}
	return "all"
}

// sharedList loads a Strapi list through the micro-cache (never stored empty).
func (r *InfoRepository) sharedList(ctx context.Context, name string, territories []domain.Territoire, load func(context.Context) []*strapiConsultation) []*strapiConsultation {
	list, _ := loadCached(r.a, name, territoryKey(territories), r.a.Cfg.MicroCacheTTL, func() ([]*strapiConsultation, bool, error) {
		// the shared load must not die with the request that started it
		l := load(context.WithoutCancel(ctx))
		return l, len(l) > 0, nil
	})
	return list
}

// GetOngoingConsultations is getOngoingConsultations(userTerritoires).
func (r *InfoRepository) GetOngoingConsultations(ctx context.Context, territories []domain.Territoire) []ConsultationPreview {
	today := r.a.Now()
	list := r.sharedList(ctx, ongoingListCacheName, territories, func(ctx context.Context) []*strapiConsultation {
		return r.strapi.GetConsultationsOngoing(ctx, today, territories)
	})
	return r.mapper.toConsultationPreview(list)
}

// GetOngoingConsultationsWithUnpublished is getOngoingConsultationsWithUnpublished.
func (r *InfoRepository) GetOngoingConsultationsWithUnpublished(ctx context.Context, territories []domain.Territoire) []ConsultationPreview {
	today := r.a.Now()
	return r.mapper.toConsultationPreview(r.strapi.GetConsultationsOngoingWithUnpublished(ctx, today, territories))
}

// GetFinishedConsultations is getFinishedConsultations(userTerritoires).
func (r *InfoRepository) GetFinishedConsultations(ctx context.Context, territories []domain.Territoire) []ConsultationPreviewFinished {
	now := r.a.Now()
	list := r.sharedList(ctx, finishedListCacheName, territories, func(ctx context.Context) []*strapiConsultation {
		return r.strapi.GetConsultationsFinished(ctx, now, territories)
	})
	return r.mapper.toDomainFinished(list, FromTime(now))
}

// GetFinishedConsultationsWithUnpublished is getFinishedConsultationsWithUnpublished.
func (r *InfoRepository) GetFinishedConsultationsWithUnpublished(ctx context.Context, territories []domain.Territoire) []ConsultationPreviewFinished {
	now := r.a.Now()
	return r.mapper.toDomainFinished(r.strapi.GetConsultationsFinishedWithUnpublished(ctx, now, territories), FromTime(now))
}

// GetAnsweredConsultations is getAnsweredConsultations(userId): the
// consultations the user answered (read from the database at every call).
func (r *InfoRepository) GetAnsweredConsultations(ctx context.Context, userID string) ([]ConsultationPreviewFinished, error) {
	if _, ok := javacompat.ToUUIDOrNull(userID); !ok {
		return []ConsultationPreviewFinished{}, nil
	}
	now := r.a.Now()
	ids, err := r.answered.GetAnsweredConsultationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	return r.mapper.toDomainFinished(r.strapi.GetConsultationsByIDs(ctx, ids), FromTime(now)), nil
}

// IsConsultationExists is isConsultationExists(consultationId).
func (r *InfoRepository) IsConsultationExists(ctx context.Context, consultationID string) bool {
	return r.strapi.IsConsultationExists(ctx, consultationID)
}

// GetConsultationsToAggregate is getConsultationsToAggregate: the consultations
// that ended more than 14 days ago.
func (r *InfoRepository) GetConsultationsToAggregate(ctx context.Context) []ConsultationPreview {
	return r.mapper.toConsultationPreview(r.strapi.GetConsultationsEnded14DaysAgo(ctx, r.a.Now()))
}

// putInCacheUnderBothKeys is putInCacheUnderBothKeys: the id and the slug of the
// consultation point to the same entry as the string that was asked for.
func (r *InfoRepository) putInCacheUnderBothKeys(requested string, info *ConsultationInfo) {
	ttl := consultationCacheTTL
	if info.ID != requested {
		r.a.Cache.Put(consultationCacheName, info.ID, info, ttl)
	}
	if info.Slug != requested && info.Slug != info.ID {
		r.a.Cache.Put(consultationCacheName, info.Slug, info, ttl)
	}
}

// cachedInfo reads or builds an entry of "consultationCache"; a consultation
// that is not found is never cached.
func (r *InfoRepository) cachedInfo(ctx context.Context, key string, load func(ctx context.Context) *ConsultationInfo) *ConsultationInfo {
	info, _ := loadCached(r.a, consultationCacheName, key, consultationCacheTTL, func() (*ConsultationInfo, bool, error) {
		// the shared load must not die with the request that started it
		info := load(context.WithoutCancel(ctx))
		return info, info != nil, nil
	})
	return info
}

// GetConsultationByIDOrSlug is getConsultationByIdOrSlug: by slug (published
// only), else by id.
func (r *InfoRepository) GetConsultationByIDOrSlug(ctx context.Context, idOrSlug string) *ConsultationInfo {
	return r.cachedInfo(ctx, idOrSlug, func(ctx context.Context) *ConsultationInfo {
		dto := r.strapi.GetConsultationBySlug(ctx, idOrSlug)
		if dto == nil {
			dto = r.strapi.GetConsultationByID(ctx, idOrSlug)
		}
		if dto == nil {
			return nil
		}
		info := r.mapper.toConsultationInfo(dto)
		r.putInCacheUnderBothKeys(idOrSlug, info)
		return info
	})
}

// GetConsultationByIDOrSlugWithUnpublished is getConsultationByIdOrSlugWithUnpublished.
func (r *InfoRepository) GetConsultationByIDOrSlugWithUnpublished(ctx context.Context, idOrSlug string) *ConsultationInfo {
	return r.cachedInfo(ctx, idOrSlug, func(ctx context.Context) *ConsultationInfo {
		dto := r.strapi.GetConsultationBySlugWithUnpublished(ctx, idOrSlug)
		if dto == nil {
			dto = r.strapi.GetConsultationByIDWithUnpublished(ctx, idOrSlug)
		}
		if dto == nil {
			return nil
		}
		info := r.mapper.toConsultationInfo(dto)
		r.putInCacheUnderBothKeys(idOrSlug, info)
		return info
	})
}

// GetConsultation is getConsultation(consultationId): by id only (cached under
// that key only).
func (r *InfoRepository) GetConsultation(ctx context.Context, consultationID string) *ConsultationInfo {
	return r.cachedInfo(ctx, consultationID, func(ctx context.Context) *ConsultationInfo {
		dto := r.strapi.GetConsultationByID(ctx, consultationID)
		if dto == nil {
			return nil
		}
		return r.mapper.toConsultationInfo(dto)
	})
}
