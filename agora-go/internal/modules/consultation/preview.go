package consultation

import (
	"context"
	"sort"

	"agora/internal/domain"
	"agora/internal/modules/profile"
)

// PreviewUseCase is ConsultationPreviewUseCase (GET /consultations).
type PreviewUseCase struct {
	info interface {
		GetAnsweredConsultations(ctx context.Context, userID string) ([]ConsultationPreviewFinished, error)
		GetOngoingConsultations(ctx context.Context, territories []domain.Territoire) []ConsultationPreview
		GetOngoingConsultationsWithUnpublished(ctx context.Context, territories []domain.Territoire) []ConsultationPreview
		GetFinishedConsultations(ctx context.Context, territories []domain.Territoire) []ConsultationPreviewFinished
		GetFinishedConsultationsWithUnpublished(ctx context.Context, territories []domain.Territoire) []ConsultationPreviewFinished
	}
	profiles interface {
		GetProfile(ctx context.Context, userID string) (*profile.Profile, error)
	}
}

// GetConsultationPreviewPage is getConsultationPreviewPage. userID is the
// authenticated user (nil when anonymous); canViewUnpublished is
// AuthentificationHelper.canViewUnpublishedConsultations.
func (u *PreviewUseCase) GetConsultationPreviewPage(ctx context.Context, userID *string, canViewUnpublished bool) (ConsultationPreviewPage, error) {
	// Territoire.of(profile) is `emptyList()` (the territories are not used yet), but
	// the profile is still read: an unreadable profile is an error (HTTP 500)
	if userID != nil {
		if _, err := u.profiles.GetProfile(ctx, *userID); err != nil {
			return ConsultationPreviewPage{}, err
		}
	}
	var userTerritoires []domain.Territoire

	answeredConsultations := []ConsultationPreviewFinished{}
	if userID != nil {
		var err error
		answeredConsultations, err = u.info.GetAnsweredConsultations(ctx, *userID)
		if err != nil {
			return ConsultationPreviewPage{}, err
		}
	}

	var ongoing []ConsultationPreview
	var finished []ConsultationPreviewFinished
	if canViewUnpublished {
		ongoing = u.info.GetOngoingConsultationsWithUnpublished(ctx, userTerritoires)
		finished = u.info.GetFinishedConsultationsWithUnpublished(ctx, userTerritoires)
	} else {
		ongoing = u.info.GetOngoingConsultations(ctx, userTerritoires)
		finished = u.info.GetFinishedConsultations(ctx, userTerritoires)
	}

	// removeAnsweredConsultation, then sortedBy { it.endDate } / { it.lastUpdateDate } (stable)
	remaining := make([]ConsultationPreview, 0, len(ongoing))
	for _, o := range ongoing {
		answered := false
		for _, a := range answeredConsultations {
			if a.ID == o.ID {
				answered = true
				break
			}
		}
		if !answered {
			remaining = append(remaining, o)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].EndDate.Before(remaining[j].EndDate) })
	sort.SliceStable(finished, func(i, j int) bool { return finished[i].LastUpdateDate.Before(finished[j].LastUpdateDate) })
	return ConsultationPreviewPage{Ongoing: remaining, Finished: finished, Answered: answeredConsultations}, nil
}
