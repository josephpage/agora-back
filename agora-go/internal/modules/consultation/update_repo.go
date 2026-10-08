package consultation

import (
	"context"
	"sort"
	"time"

	"agora/internal/app"
)

// UpdateRepository is ConsultationUpdateV2RepositoryImpl.
type UpdateRepository struct {
	a      *app.App
	strapi *StrapiRepository
	mapper updateMapper
}

// GetUnansweredUsersConsultationUpdateWithUnpublished is
// getUnansweredUsersConsultationUpdateWithUnpublished(consultationId).
func (r *UpdateRepository) GetUnansweredUsersConsultationUpdateWithUnpublished(ctx context.Context, consultationID string) *UpdateInfo {
	c := r.strapi.GetConsultationByIDWithUnpublished(ctx, consultationID)
	if c == nil {
		return nil
	}
	return r.mapper.toDomainUnanswered(c)
}

// GetLatestConsultationUpdate is getLatestConsultationUpdate(consultationId):
// the latest other content, else the sponsor's answer, else the analysis (each
// once published), else the content shown after the answer.
func (r *UpdateRepository) GetLatestConsultationUpdate(ctx context.Context, consultationID string) *UpdateInfo {
	now := FromTime(r.a.Now())
	c := r.strapi.GetConsultationByIDWithUnpublished(ctx, consultationID)
	if c == nil {
		return nil
	}

	var latestOther *strapiContenuAutre
	for _, a := range c.ContenuAutres {
		if !a.DatetimePublication.Before(now) {
			continue
		}
		if latestOther == nil || latestOther.DatetimePublication.Before(a.DatetimePublication) {
			latestOther = a
		}
	}
	switch {
	case latestOther != nil:
		return r.mapper.toDomainContenuAutre(c, latestOther)
	case c.ReponseDuCommanditaire != nil && c.ReponseDuCommanditaire.DatetimePublication.Before(now):
		return r.mapper.toDomainReponseDuCommanditaire(c)
	case c.AnalyseDesReponses != nil && c.AnalyseDesReponses.DatetimePublication.Before(now):
		return r.mapper.toDomainAnalyseDesReponses(c)
	}
	return r.mapper.toDomainAnsweredOrEnded(c, now)
}

// findUpdate resolves an update of a consultation by document id or slug: the
// content before the answer, the one after, the analysis, the sponsor's answer,
// then the other contents (the first match).
func (r *UpdateRepository) findUpdate(c *strapiConsultation, idOrSlug string) *UpdateInfo {
	avant, apres := &c.ContenuAvantReponse, &c.ContenuApresReponseOuTerminee
	analyse, commanditaire := c.AnalyseDesReponses, c.ReponseDuCommanditaire

	foundAvant := avant.DocumentID == idOrSlug || avant.Slug == idOrSlug
	foundApres := apres.DocumentID == idOrSlug || apres.Slug == idOrSlug
	foundAnalyse := analyse != nil && (analyse.DocumentID == idOrSlug || analyse.Slug == idOrSlug)
	foundCommanditaire := commanditaire != nil && (commanditaire.DocumentID == idOrSlug || commanditaire.Slug == idOrSlug)

	switch {
	case foundAvant:
		return r.mapper.toDomainUnanswered(c)
	case foundApres:
		return r.mapper.toDomainAnsweredOrEnded(c, FromTime(r.a.Now()))
	case foundAnalyse:
		return r.mapper.toDomainAnalyseDesReponses(c)
	case foundCommanditaire:
		return r.mapper.toDomainReponseDuCommanditaire(c)
	}
	for _, a := range c.ContenuAutres {
		if a.DocumentID == idOrSlug || a.Slug == idOrSlug {
			return r.mapper.toDomainContenuAutre(c, a)
		}
	}
	return nil
}

// GetConsultationUpdateBySlugOrIDWithUnpublished is getConsultationUpdateBySlugOrIdWithUnpublished.
func (r *UpdateRepository) GetConsultationUpdateBySlugOrIDWithUnpublished(ctx context.Context, consultationID, idOrSlug string) *UpdateInfo {
	c := r.strapi.GetConsultationByIDWithUnpublished(ctx, consultationID)
	if c == nil {
		return nil
	}
	return r.findUpdate(c, idOrSlug)
}

// GetConsultationUpdateBySlugOrID is getConsultationUpdateBySlugOrId.
func (r *UpdateRepository) GetConsultationUpdateBySlugOrID(ctx context.Context, consultationID, idOrSlug string) *UpdateInfo {
	c := r.strapi.GetConsultationByID(ctx, consultationID)
	if c == nil {
		return nil
	}
	return r.findUpdate(c, idOrSlug)
}

// GetConsultationUpdate is getConsultationUpdate(consultationId, updateId): by
// document id only.
func (r *UpdateRepository) GetConsultationUpdate(ctx context.Context, consultationID, updateID string) *UpdateInfo {
	c := r.strapi.GetConsultationByID(ctx, consultationID)
	if c == nil {
		return nil
	}
	// `val contenuAutre = consultation.consultationContenuAutres.firstOrNull { ... }` is
	// evaluated first: a null element before the match throws
	var contenuAutre *strapiContenuAutre
	for _, a := range c.ContenuAutres {
		if a.DocumentID == updateID {
			contenuAutre = a
			break
		}
	}
	switch {
	case c.ContenuAvantReponse.DocumentID == updateID:
		return r.mapper.toDomainUnanswered(c)
	case c.ContenuApresReponseOuTerminee.DocumentID == updateID:
		return r.mapper.toDomainAnsweredOrEnded(c, FromTime(r.a.Now()))
	case c.AnalyseDesReponses != nil && c.AnalyseDesReponses.DocumentID == updateID:
		return r.mapper.toDomainAnalyseDesReponses(c)
	case c.ReponseDuCommanditaire != nil && c.ReponseDuCommanditaire.DocumentID == updateID:
		return r.mapper.toDomainReponseDuCommanditaire(c)
	case contenuAutre != nil:
		return r.mapper.toDomainContenuAutre(c, contenuAutre)
	}
	return nil
}

// ---------------------------------------------------------------------------
// History (ConsultationUpdateHistoryMapper, ConsultationUpdateHistoryRepositoryImpl)
// ---------------------------------------------------------------------------

// Kotlin cache of the update history: shortTermCacheManager (5 minutes), key = consultation id.
const (
	historyCacheName = "consultationUpdateHistory"
	historyCacheTTL  = 5 * time.Minute
)

// HistoryRepository is ConsultationUpdateHistoryRepositoryImpl.
type HistoryRepository struct {
	a      *app.App
	strapi *StrapiRepository
}

// GetConsultationUpdateHistory is getConsultationUpdateHistory(consultationId):
// empty (and not cached) when the consultation is unknown.
func (r *HistoryRepository) GetConsultationUpdateHistory(ctx context.Context, consultationID string) ([]UpdateHistory, error) {
	return loadCached(r.a, historyCacheName, consultationID, historyCacheTTL, func() ([]UpdateHistory, bool, error) {
		c := r.strapi.GetConsultationByIDWithUnpublished(context.WithoutCancel(ctx), consultationID)
		if c == nil {
			return []UpdateHistory{}, false, nil
		}
		return historyOf(c, r.a.Now), true, nil
	})
}

// historyOf is ConsultationUpdateHistoryMapper.toDomain: newest first, the
// latest published content is CURRENT, the future ones are left out.
func historyOf(c *strapiConsultation, nowFn func() time.Time) []UpdateHistory {
	avant := &c.ContenuAvantReponse
	apres := &c.ContenuApresReponseOuTerminee
	analyse := c.AnalyseDesReponses
	commanditaire := c.ReponseDuCommanditaire

	now := FromTime(nowFn())
	var autres []*strapiContenuAutre
	for _, a := range c.ContenuAutres {
		if a.DatetimePublication.Before(now) {
			autres = append(autres, a)
		}
	}
	sort.SliceStable(autres, func(i, j int) bool { return autres[j].DatetimePublication.Before(autres[i].DatetimePublication) })

	var dernierContenuID string
	switch {
	case len(autres) > 0:
		dernierContenuID = autres[0].DocumentID
	case commanditaire != nil:
		dernierContenuID = commanditaire.DocumentID
	case analyse != nil:
		dernierContenuID = analyse.DocumentID
	default:
		dernierContenuID = apres.DocumentID
	}
	status := func(id string) HistoryStatus {
		if dernierContenuID == id {
			return HistoryCurrent
		}
		return HistoryDone
	}
	date := func(d LocalDateTime) *time.Time {
		t := d.ToDate().Truncate(time.Millisecond) // Date.from(instant) keeps milliseconds
		return &t
	}

	historiqueAvant := UpdateHistory{
		Type: HistoryUpdate, ConsultationUpdateID: ptr(avant.DocumentID), Status: HistoryDone, Title: avant.HistoriqueTitre,
		Slug: ptr(avant.Slug), UpdateDate: date(c.DateDeDebut), ActionText: ptr(avant.HistoriqueCallToAction),
	}
	historiqueApres := UpdateHistory{
		Type: HistoryResults, ConsultationUpdateID: ptr(apres.DocumentID), Status: status(apres.DocumentID), Title: apres.HistoriqueTitre,
		Slug: ptr(apres.Slug), UpdateDate: date(c.DateDeFin), ActionText: ptr(apres.HistoriqueCallToAction),
	}

	var result []UpdateHistory
	if c.ContenuAVenir != nil {
		// the "incoming" entry has no date: it is never filtered out
		result = append(result, UpdateHistory{
			Type: HistoryUpdate, ConsultationUpdateID: nil, Status: HistoryIncoming, Title: c.ContenuAVenir.TitreHistorique,
		})
	}
	if commanditaire != nil {
		result = append(result, UpdateHistory{
			Type: HistoryUpdate, ConsultationUpdateID: ptr(commanditaire.DocumentID), Status: status(commanditaire.DocumentID),
			Title: commanditaire.HistoriqueTitre, Slug: ptr(commanditaire.Slug), UpdateDate: date(commanditaire.DatetimePublication),
			ActionText: ptr(commanditaire.HistoriqueCallToAction),
		})
	}
	if analyse != nil {
		result = append(result, UpdateHistory{
			Type: HistoryUpdate, ConsultationUpdateID: ptr(analyse.DocumentID), Status: status(analyse.DocumentID),
			Title: analyse.HistoriqueTitre, Slug: ptr(analyse.Slug), UpdateDate: date(analyse.DatetimePublication),
			ActionText: ptr(analyse.HistoriqueCallToAction),
		})
	}
	result = append(result, historiqueApres, historiqueAvant)
	for _, a := range autres {
		result = append(result, UpdateHistory{
			Type: HistoryUpdate, ConsultationUpdateID: ptr(a.DocumentID), Status: status(a.DocumentID),
			Title: a.HistoriqueTitre, Slug: ptr(a.Slug), UpdateDate: date(a.DatetimePublication),
			ActionText: ptr(a.HistoriqueCallToAction),
		})
	}

	// `.filter { it.updateDate?.toLocalDateTime()?.isBefore(LocalDateTime.now(clock)) ?: true }`
	// (the clock is read again for every element)
	out := make([]UpdateHistory, 0, len(result))
	for _, h := range result {
		if h.UpdateDate == nil || FromTime(*h.UpdateDate).Before(FromTime(nowFn())) {
			out = append(out, h)
		}
	}
	return out
}
