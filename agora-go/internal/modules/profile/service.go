package profile

import (
	"context"
	"errors"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/httpx"
	"agora/internal/javacompat"
)

// Service exposes the profile slice to other modules.
type Service struct {
	a        *app.App
	Profiles ProfileRepository
	AskDates DemographicInfoAskDateRepository
	Ask      *AskForDemographicInfoUseCase
}

// pgAnswered is UserAnsweredConsultationRepositoryImpl.getAnsweredConsultationIds
// (the only method of that repository this slice needs).
type pgAnswered struct{ a *app.App }

func (p pgAnswered) GetAnsweredConsultationIds(ctx context.Context, userID string) ([]string, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return []string{}, nil
	}
	rows, err := p.a.DB.Pool.Query(ctx, `SELECT DISTINCT consultation_id FROM user_answered_consultation
            WHERE user_id = $1
        `, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != nil {
			out = append(out, *id)
		}
	}
	return out, rows.Err()
}

// Get returns the App-wide profile service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "profile", func() *Service {
		profiles := &profileRepository{db: &pgProfile{a: a}, cache: l1ProfileCache{a: a}}
		askDates := &askDateRepository{db: &pgAskDate{a: a}, cache: l1AskDateCache{a: a}, now: a.Now}
		return &Service{
			a:        a,
			Profiles: profiles,
			AskDates: askDates,
			Ask: &AskForDemographicInfoUseCase{
				answered:    pgAnswered{a: a},
				profiles:    profiles,
				askDates:    askDates,
				excludedIDs: a.Cfg.ConsultationDocumentIDsWithoutDemoAsk,
				now:         a.Now,
			},
		}
	})
}

// GetProfile is ProfileRepository.getProfile: the stored profile, nil when the
// user has none (or the id is not a UUID).
func (s *Service) GetProfile(ctx context.Context, userID string) (*Profile, error) {
	return s.Profiles.GetProfile(ctx, userID)
}

// GetProfileOrEmpty is GetProfileUseCase.getProfile: an empty profile when
// the user has none.
func (s *Service) GetProfileOrEmpty(ctx context.Context, userID string) (Profile, error) {
	return getProfileOrEmpty(ctx, s.Profiles, userID)
}

// AskForDemographicInfo is AskForDemographicInfoUseCase.askForDemographicInfo.
func (s *Service) AskForDemographicInfo(ctx context.Context, userID, consultationID string) (bool, error) {
	return s.Ask.AskForDemographicInfo(ctx, userID, consultationID)
}

// DeleteUsersProfile is ProfileRepository.deleteUsersProfile (used by DeleteUsersUseCase).
func (s *Service) DeleteUsersProfile(ctx context.Context, userIDs []string) error {
	return s.Profiles.DeleteUsersProfile(ctx, userIDs)
}

// Routes registers GET/POST /profile and POST /profile/departments.
func Routes(a *app.App) {
	s := Get(a)
	a.Server.POST("/profile", s.postProfile)
	a.Server.GET("/profile", s.getProfile)
	a.Server.POST("/profile/departments", s.updateDepartments)
}

// postProfile is ProfileController.postProfile.
func (s *Service) postProfile(c *httpx.Ctx) *httpx.Response {
	var body ProfileJSON
	c.BindBody(&body)
	in := ToDomain(body, c.UserID())
	result, err := insertProfile(c.Context(), s.Profiles, s.AskDates, in)
	if err != nil {
		panic(err)
	}
	if result == ProfileEditSuccess {
		return httpx.String(200, "")
	}
	return httpx.String(400, "")
}

// getProfile is ProfileController.getProfile.
func (s *Service) getProfile(c *httpx.Ctx) *httpx.Response {
	p, err := getProfileOrEmpty(c.Context(), s.Profiles, c.UserID())
	if err != nil {
		panic(err)
	}
	return httpx.OK(ToJSON(p))
}

// updateDepartments is ProfileController.updateDepartments.
func (s *Service) updateDepartments(c *httpx.Ctx) *httpx.Response {
	var body ProfileDepartmentJSON
	c.BindBody(&body)
	err := updateDepartments(c.Context(), s.Profiles, c.UserID(), body)
	var territory *domain.InvalidTerritoryError
	switch {
	case err == nil:
		return httpx.Empty(200)
	case errors.Is(err, ErrInvalidNumberOfDepartments), errors.As(err, &territory):
		panic(&httpx.AdviceError{Status: 400, Title: err.Error()})
	}
	panic(err)
}
