package consultation

import (
	"context"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/strapi"
)

// ConsultationStrapiRepository: the URIs sent to Strapi are byte for byte the
// Kotlin ones (the parity harness compares the request log of the fake Strapi).

var sectionTypes = []string{
	"consultation-section.section-titre",
	"consultation-section.section-texte-riche",
	"consultation-section.section-citation",
	"consultation-section.section-image",
	"consultation-section.section-video",
	"consultation-section.section-chiffre",
	"consultation-section.section-accordeon",
}

var contenuWithSections = []string{
	"consultation_avant_reponse",
	"consultation_apres_reponse_ou_terminee",
	"consultation_contenu_autres",
	"consultation_contenu_analyse_des_reponse",
	"contenu_reponse_du_commanditaires",
}

var commonMediaPopulate = []string{
	"[image_de_couverture][fields][0]=url",
	"[image_de_couverture][fields][1]=formats",
	"[image_page_de_contenu][fields][0]=url",
	"[image_page_de_contenu][fields][1]=formats",
}

// listPopulate is LIST_POPULATE (list queries: sections are not populated).
var listPopulate = func() string {
	items := []string{
		"[thematique]=*",
		"[questions][populate]=*",
		"[consultation_contenu_a_venir]=*",
	}
	for _, c := range contenuWithSections {
		if c != "consultation_contenu_analyse_des_reponse" {
			items = append(items, "["+c+"][populate]=*")
		}
	}
	items = append(items, "[consultation_contenu_analyse_des_reponse][populate][pdf_analyse][fields][0]=url")
	items = append(items, commonMediaPopulate...)
	return strings.Join(items, "&populate")
}()

// detailPopulate is DETAIL_POPULATE (detail queries: sections through the fragment API).
var detailPopulate = func() string {
	items := []string{
		"[thematique]=*",
		"[questions][populate]=*",
		"[consultation_contenu_a_venir]=*",
	}
	for _, c := range contenuWithSections {
		for _, t := range sectionTypes {
			items = append(items, "["+c+"][populate][sections][on]["+t+"][populate]=*")
		}
	}
	items = append(items, "[consultation_contenu_analyse_des_reponse][populate][pdf_analyse][fields][0]=url")
	items = append(items, commonMediaPopulate...)
	return strings.Join(items, "&populate")
}()

// byIDTTL is ConsultationStrapiCacheByIdRepositoryImpl.TTL.
const byIDTTL = 5 * time.Minute

// StrapiRepository is ConsultationStrapiRepository (with its
// ConsultationStrapiCacheByIdRepositoryImpl). Lists are the decoded Strapi data
// (empty on any Strapi failure, like the Kotlin client).
type StrapiRepository struct {
	a *app.App

	// ConsultationStrapiCacheByIdRepositoryImpl: two in-JVM maps with a 5 minute
	// TTL, never flushed by the Redis flush / cache clear. They are kept in this
	// process (not in the shared L1 cache) for the same reason.
	mu        sync.Mutex
	byID      map[string]byIDEntry
	byIDUnpub map[string]byIDEntry
	loads     singleflight.Group
	now       func() time.Time
}

type byIDEntry struct {
	dto      *strapiConsultation
	cachedAt time.Time
}

func newStrapiRepository(a *app.App) *StrapiRepository {
	return &StrapiRepository{a: a, byID: map[string]byIDEntry{}, byIDUnpub: map[string]byIDEntry{}, now: time.Now}
}

func (r *StrapiRepository) collection(ctx context.Context, b *strapi.RequestBuilder) strapi.Envelope[*strapiConsultation] {
	return strapi.Collection[*strapiConsultation](ctx, r.a.Strapi, b)
}

func territoryFilter(b *strapi.RequestBuilder, territories []domain.Territoire) *strapi.RequestBuilder {
	if len(territories) > 0 {
		values := make([]string, len(territories))
		for i, t := range territories {
			values[i] = t.Value()
		}
		b.FilterIn("territoire", values)
	}
	return b
}

// GetConsultationsOngoing is getConsultationsOngoing(date, territories).
func (r *StrapiRepository) GetConsultationsOngoing(ctx context.Context, date time.Time, territories []domain.Territoire) []*strapiConsultation {
	b := strapi.NewRequest("consultations").
		WithDateBefore(date, "datetime_de_debut").
		WithDateAfter(date, "datetime_de_fin").
		Populate(listPopulate)
	return r.collection(ctx, territoryFilter(b, territories)).Data
}

// GetConsultationsOngoingWithUnpublished is getConsultationsOngoingWithUnpublished.
func (r *StrapiRepository) GetConsultationsOngoingWithUnpublished(ctx context.Context, date time.Time, territories []domain.Territoire) []*strapiConsultation {
	b := strapi.NewRequest("consultations").
		WithDateBefore(date, "datetime_de_debut").
		WithDateAfter(date, "datetime_de_fin").
		WithUnpublished().
		Populate(listPopulate)
	return r.collection(ctx, territoryFilter(b, territories)).Data
}

// GetConsultationsFinished is getConsultationsFinished(date, territories).
func (r *StrapiRepository) GetConsultationsFinished(ctx context.Context, date time.Time, territories []domain.Territoire) []*strapiConsultation {
	b := strapi.NewRequest("consultations").
		WithDateBefore(date, "datetime_de_fin").
		Populate(listPopulate)
	return r.collection(ctx, territoryFilter(b, territories)).Data
}

// GetConsultationsFinishedWithUnpublished is getConsultationsFinishedWithUnpublished.
func (r *StrapiRepository) GetConsultationsFinishedWithUnpublished(ctx context.Context, date time.Time, territories []domain.Territoire) []*strapiConsultation {
	b := strapi.NewRequest("consultations").
		WithDateBefore(date, "datetime_de_fin").
		WithUnpublished().
		Populate(listPopulate)
	return r.collection(ctx, territoryFilter(b, territories)).Data
}

// GetConsultationsFinishedByTerritories is getConsultationsFinishedByTerritories.
func (r *StrapiRepository) GetConsultationsFinishedByTerritories(ctx context.Context, date time.Time, territories []domain.Territoire) []*strapiConsultation {
	return r.GetConsultationsFinished(ctx, date, territories)
}

// GetConsultationsByIDs is getConsultationsByIds: no request for an empty list.
func (r *StrapiRepository) GetConsultationsByIDs(ctx context.Context, ids []string) []*strapiConsultation {
	if len(ids) == 0 {
		return []*strapiConsultation{}
	}
	b := strapi.NewRequest("consultations").GetByIDs(ids).Populate(listPopulate)
	return r.collection(ctx, b).Data
}

// first is `data.firstOrNull()`: a JSON null element is "not found" too.
func first(list []*strapiConsultation) *strapiConsultation {
	if len(list) == 0 {
		return nil
	}
	return list[0]
}

// GetConsultationBySlug is getConsultationBySlug (published consultations only).
func (r *StrapiRepository) GetConsultationBySlug(ctx context.Context, slug string) *strapiConsultation {
	b := strapi.NewRequest("consultations").FilterIn("slug", []string{slug}).Populate(detailPopulate)
	return first(r.collection(ctx, b).Data)
}

// GetConsultationBySlugWithUnpublished is getConsultationBySlugWithUnpublished.
func (r *StrapiRepository) GetConsultationBySlugWithUnpublished(ctx context.Context, slug string) *strapiConsultation {
	b := strapi.NewRequest("consultations").FilterIn("slug", []string{slug}).WithUnpublished().Populate(detailPopulate)
	return first(r.collection(ctx, b).Data)
}

// fetchByID is the request shared by getConsultationById and
// getConsultationByIdWithUnpublished (both ask for the drafts too).
func (r *StrapiRepository) fetchByID(ctx context.Context, id string) *strapiConsultation {
	b := strapi.NewRequest("consultations").GetByIDs([]string{id}).WithUnpublished().Populate(detailPopulate)
	return first(r.collection(ctx, b).Data)
}

func (r *StrapiRepository) cached(m map[string]byIDEntry, id string) *strapiConsultation {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := m[id]
	if !ok {
		return nil
	}
	// Duration.between(cachedAt, now).seconds >= TTL.seconds
	if int64(r.now().Sub(e.cachedAt)/time.Second) >= int64(byIDTTL/time.Second) {
		delete(m, id)
		return nil
	}
	return e.dto
}

func (r *StrapiRepository) put(m map[string]byIDEntry, id string, dto *strapiConsultation) {
	r.mu.Lock()
	m[id] = byIDEntry{dto: dto, cachedAt: r.now()}
	r.mu.Unlock()
}

// loadByID is the body of getConsultationById[WithUnpublished]: the cached DTO
// (a cached null is a miss), else Strapi, then the result is cached. Concurrent
// misses of one id share one Strapi request.
func (r *StrapiRepository) loadByID(ctx context.Context, m map[string]byIDEntry, kind, id string) *strapiConsultation {
	if dto := r.cached(m, id); dto != nil {
		return dto
	}
	v, _, _ := r.loads.Do(kind+"\x00"+id, func() (any, error) {
		if dto := r.cached(m, id); dto != nil {
			return dto, nil
		}
		// the shared load must not die with the request that started it
		dto := r.fetchByID(context.WithoutCancel(ctx), id)
		r.put(m, id, dto)
		return dto, nil
	})
	return v.(*strapiConsultation)
}

// GetConsultationByID is getConsultationById (5 minute in-process cache).
func (r *StrapiRepository) GetConsultationByID(ctx context.Context, id string) *strapiConsultation {
	return r.loadByID(ctx, r.byID, "byId", id)
}

// GetConsultationByIDWithUnpublished is getConsultationByIdWithUnpublished (5 minute in-process cache).
func (r *StrapiRepository) GetConsultationByIDWithUnpublished(ctx context.Context, id string) *strapiConsultation {
	return r.loadByID(ctx, r.byIDUnpub, "byIdUnpublished", id)
}

// EvictConsultationByID is ConsultationStrapiCacheByIdRepository.evictConsultationById
// (nothing calls it in the Kotlin code; kept for the slices that will).
func (r *StrapiRepository) EvictConsultationByID(id string) {
	r.mu.Lock()
	delete(r.byID, id)
	delete(r.byIDUnpub, id)
	r.mu.Unlock()
}

// GetConsultationsEnded14DaysAgo is getConsultationsEnded14DaysAgo.
func (r *StrapiRepository) GetConsultationsEnded14DaysAgo(ctx context.Context, today time.Time) []*strapiConsultation {
	b := strapi.NewRequest("consultations").
		WithDateBefore(FromTime(today).PlusDays(-14).ToDate(), "datetime_de_fin").
		Populate(listPopulate)
	return r.collection(ctx, b).Data
}

// IsConsultationExists is isConsultationExists: exactly one match (drafts included).
func (r *StrapiRepository) IsConsultationExists(ctx context.Context, id string) bool {
	b := strapi.NewRequest("consultations").GetByIDs([]string{id}).WithUnpublished()
	return r.collection(ctx, b).Meta.Pagination.Total == 1
}

// CountFinishedConsultations is countFinishedConsultations.
func (r *StrapiRepository) CountFinishedConsultations(ctx context.Context, date time.Time) int {
	b := strapi.NewRequest("consultations").WithDateBefore(date, "datetime_de_fin")
	return r.collection(ctx, b).Meta.Pagination.Total
}
