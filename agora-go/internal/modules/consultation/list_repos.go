package consultation

import (
	"context"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation/answered"
)

// The repositories of the paginated consultation lists (S5): these classes are
// in the "consultation" Kotlin package (ConsultationPreviewFinishedRepositoryImpl,
// ConsultationPreviewAnsweredRepositoryImpl) and implement S5 interfaces.

// PreviewFinishedRepository is ConsultationPreviewFinishedRepositoryImpl.
type PreviewFinishedRepository struct {
	a      *app.App
	strapi *StrapiRepository
	mapper infoMapper
}

// GetConsultationFinishedCount is getConsultationFinishedCount: the total of the
// Strapi query of the finished consultations.
func (r *PreviewFinishedRepository) GetConsultationFinishedCount(ctx context.Context) int {
	return r.strapi.CountFinishedConsultations(ctx, r.a.Now())
}

// GetConsultationFinishedList is getConsultationFinishedList(territories).
func (r *PreviewFinishedRepository) GetConsultationFinishedList(ctx context.Context, territories []domain.Territoire) []ConsultationWithUpdateInfo {
	now := r.a.Now()
	return r.mapper.toConsultationsWithUpdateInfo(r.strapi.GetConsultationsFinishedByTerritories(ctx, now, territories), FromTime(now))
}

// GetConsultationFinishedListPage is getConsultationFinishedList(offset, pageSize,
// territory): offset and page size are ignored (the caller paginates).
func (r *PreviewFinishedRepository) GetConsultationFinishedListPage(ctx context.Context, offset, pageSize int, territory domain.Territoire) []ConsultationWithUpdateInfo {
	now := r.a.Now()
	return r.mapper.toConsultationsWithUpdateInfo(r.strapi.GetConsultationsFinished(ctx, now, []domain.Territoire{territory}), FromTime(now))
}

// PreviewAnsweredRepository is ConsultationPreviewAnsweredRepositoryImpl.
type PreviewAnsweredRepository struct {
	a        *app.App
	strapi   *StrapiRepository
	answered *answered.Repository
	mapper   infoMapper
}

// GetConsultationAnsweredCount is getConsultationAnsweredCount(userId): 0 when
// the user id is not a UUID.
func (r *PreviewAnsweredRepository) GetConsultationAnsweredCount(ctx context.Context, userID string) (int, error) {
	return r.answered.GetConsultationAnsweredCount(ctx, userID)
}

// GetConsultationAnsweredList is getConsultationAnsweredList(userId, offset): all
// the answered consultations (the offset is ignored); empty when the user id is
// not a UUID.
func (r *PreviewAnsweredRepository) GetConsultationAnsweredList(ctx context.Context, userID string, offset int) ([]ConsultationWithUpdateInfo, error) {
	if _, ok := javacompat.ToUUIDOrNull(userID); !ok {
		return []ConsultationWithUpdateInfo{}, nil
	}
	now := r.a.Now()
	ids, err := r.answered.GetAnsweredConsultationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	return r.mapper.toConsultationsWithUpdateInfo(r.strapi.GetConsultationsByIDs(ctx, ids), FromTime(now)), nil
}

// ToConsultationPreviewFinished is ConsultationPreviewFinishedMapper: the
// consultations whose thematique is unknown are dropped.
func ToConsultationPreviewFinished(infos []ConsultationWithUpdateInfo, thematiques []Thematique) []ConsultationPreviewFinished {
	out := make([]ConsultationPreviewFinished, 0, len(infos))
	for _, info := range infos {
		var found *Thematique
		for i := range thematiques {
			if thematiques[i].ID == info.ThematiqueID {
				found = &thematiques[i]
				break
			}
		}
		if found == nil {
			continue
		}
		out = append(out, ConsultationPreviewFinished{
			ID: info.ID, Slug: info.Slug, Title: info.Title, CoverURL: info.CoverURL, Thematique: *found,
			UpdateLabel: info.UpdateLabel, EndDate: info.EndDate, LastUpdateDate: info.UpdateDate, Territory: info.Territory,
		})
	}
	return out
}

// ToConsultationPreviewOngoing is ConsultationPreviewOngoingMapper.
func ToConsultationPreviewOngoing(info *ConsultationInfo, thematique Thematique) ConsultationPreview {
	return ConsultationPreview{
		ID: info.ID, Slug: info.Slug, Title: info.Title, CoverURL: info.CoverURL, Thematique: thematique,
		EndDate: info.EndDate, Territory: info.Territory,
	}
}
