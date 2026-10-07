package profile

import (
	"context"
	"errors"
	"strings"
	"time"

	"agora/internal/domain"
	"agora/internal/javacompat"
)

// GetProfileUseCase is usecase/profile/GetProfileUseCase: the stored profile
// or an empty one.
func getProfileOrEmpty(ctx context.Context, repo ProfileRepository, userID string) (Profile, error) {
	p, err := repo.GetProfile(ctx, userID)
	if err != nil || p == nil {
		return Profile{}, err
	}
	return *p, nil
}

// insertProfile is InsertProfileUseCase.insertProfile.
func insertProfile(ctx context.Context, repo ProfileRepository, askDates DemographicInfoAskDateRepository, in ProfileInserting) (ProfileEditResult, error) {
	existing, err := repo.GetProfile(ctx, in.UserID)
	if err != nil {
		return ProfileEditFailure, err
	}
	if existing != nil {
		return repo.UpdateProfile(ctx, in)
	}
	if err := askDates.DeleteDate(ctx, in.UserID); err != nil {
		return ProfileEditFailure, err
	}
	return repo.InsertProfile(ctx, in)
}

// ErrInvalidNumberOfDepartments is InvalidNumberOfDepartmentsException
// (@RestControllerAdvice: 400 {"title"}).
var ErrInvalidNumberOfDepartments = errors.New(domain.InvalidNumberOfDepartmentsMessage)

// errNullDepartment is the NullPointerException Kotlin throws for a null
// element of the departments list (HTTP 500).
var errNullDepartment = errors.New("NullPointerException: Parameter specified as non-null is null: departement")

// updateDepartments is UpdateDepartmentsUseCase.execute. The returned
// *domain.InvalidTerritoryError and ErrInvalidNumberOfDepartments are the
// domain exceptions mapped to 400 by the controller advice.
func updateDepartments(ctx context.Context, repo ProfileRepository, userID string, input ProfileDepartmentJSON) error {
	if n := len(input.Departments); n < 0 || n > 2 {
		return ErrInvalidNumberOfDepartments
	}
	departments := make([]*domain.Departement, 0, len(input.Departments))
	for _, name := range input.Departments {
		if name == nil {
			// a null element in a List<String>: Kotlin parameter null check
			return errNullDepartment
		}
		d, err := domain.DepartementFromOrThrow(*name)
		if err != nil {
			return err
		}
		departments = append(departments, d)
	}
	var primary, secondary *domain.Departement
	if len(departments) > 0 {
		primary = departments[0]
	}
	if len(departments) > 1 {
		secondary = departments[1]
	}
	return repo.UpdateDepartments(ctx, userID, primary, secondary)
}

// AnsweredConsultations is UserAnsweredConsultationRepository.getAnsweredConsultationIds.
type AnsweredConsultations interface {
	GetAnsweredConsultationIds(ctx context.Context, userID string) ([]string, error)
}

// AskForDemographicInfoUseCase is usecase/profile/AskForDemographicInfoUseCase.
type AskForDemographicInfoUseCase struct {
	answered    AnsweredConsultations
	profiles    ProfileRepository
	askDates    DemographicInfoAskDateRepository
	excludedIDs string
	now         func() time.Time
}

const (
	minimumConsultationAnsweredRequirement = 1
	daysBeforeAskingDemographicInfo        = 30
)

type askState int

const (
	hasDemographicInfo askState = iota
	hasNotAnsweredEnoughConsultations
	firstTimeAskDemographicInfo
	askDelayFinished
	askDelayUnfinished
)

// AskForDemographicInfo is askForDemographicInfo: should the app ask the user
// for demographic information after the answer to this consultation?
func (u *AskForDemographicInfoUseCase) AskForDemographicInfo(ctx context.Context, userID, consultationID string) (bool, error) {
	for _, part := range strings.Split(u.excludedIDs, ",") {
		t := javacompat.KotlinTrim(part)
		if !javacompat.KotlinIsBlank(t) && t == consultationID {
			return false, nil
		}
	}
	state, err := u.processState(ctx, userID)
	if err != nil {
		return false, err
	}
	switch state {
	case firstTimeAskDemographicInfo, askDelayFinished:
		if err := u.askDates.InsertDate(ctx, userID); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (u *AskForDemographicInfoUseCase) processState(ctx context.Context, userID string) (askState, error) {
	profile, err := u.profiles.GetProfile(ctx, userID)
	if err != nil {
		return 0, err
	}
	if profile != nil && profile.IsCompleted() {
		return hasDemographicInfo, nil
	}
	ids, err := u.answered.GetAnsweredConsultationIds(ctx, userID)
	if err != nil {
		return 0, err
	}
	if len(ids) < minimumConsultationAnsweredRequirement {
		return hasNotAnsweredEnoughConsultations, nil
	}
	askDate, err := u.askDates.GetDate(ctx, userID)
	if err != nil {
		return 0, err
	}
	switch {
	case askDate == nil:
		return firstTimeAskDemographicInfo, nil
	case u.isAskDateDelayFinished(*askDate):
		return askDelayFinished, nil
	}
	return askDelayUnfinished, nil
}

func (u *AskForDemographicInfoUseCase) isAskDateDelayFinished(askDate time.Time) bool {
	return civilDate(u.now()).After(askDate.AddDate(0, 0, daysBeforeAskingDemographicInfo))
}
