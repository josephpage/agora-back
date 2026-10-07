package login

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/modules/users"
)

// usersAdapter lets *users.Service satisfy UserRepository (getUserById is
// users.FindUser).
type usersAdapter struct{ *users.Service }

func (u usersAdapter) GetUserByID(ctx context.Context, userID string) (*users.UserInfo, error) {
	return u.FindUser(ctx, userID)
}

// Service gathers the login slice services other modules can use:
// feature flags, app version control, suspicious user detection, error
// messages, the login use case and the users_data repository.
type Service struct {
	a             *app.App
	Flags         *FeatureFlags
	AppVersion    *AppVersionControl
	Suspicious    *IsSuspiciousUser
	ErrorMessages *ErrorMessages
	Login         *LoginUseCase
	UserData      *pgUserData
}

// Get returns the App-wide login services.
func Get(a *app.App) *Service {
	return app.Singleton(a, "login", func() *Service {
		flags := &FeatureFlags{a: a}
		userData := &pgUserData{a: a}
		suspicious := &IsSuspiciousUser{
			now:       a.Now,
			flags:     flags,
			counts:    &redisSignupCount{a: a},
			userDatas: userData,
		}
		return &Service{
			a:             a,
			Flags:         flags,
			AppVersion:    &AppVersionControl{repo: envMinimalAppVersion{}},
			Suspicious:    suspicious,
			ErrorMessages: &ErrorMessages{a: a},
			UserData:      userData,
			Login: &LoginUseCase{
				users:    usersAdapter{users.Get(a)},
				userData: userData,
				notifier: suspicious,
			},
		}
	})
}

// IsFeatureEnabled is FeatureFlagsUseCase.isFeatureEnabled.
func (s *Service) IsFeatureEnabled(ctx context.Context, feature Feature) (bool, error) {
	return s.Flags.IsFeatureEnabled(ctx, feature)
}

// IsSuspiciousActivity is IsSuspiciousUserUseCase.isSuspiciousActivity.
func (s *Service) IsSuspiciousActivity(ctx context.Context, ipAddressHash, userAgent string) (bool, error) {
	return s.Suspicious.IsSuspiciousActivity(ctx, ipAddressHash, userAgent)
}

// GetAppVersionStatus is AppVersionControlUseCase.getAppVersionStatus.
func (s *Service) GetAppVersionStatus(platform, versionCode string) AppVersionStatus {
	return s.AppVersion.GetAppVersionStatus(platform, versionCode)
}

// QagDisabledErrorMessage is ErrorMessagesRepository.getQagDisabledErrorMessage.
func (s *Service) QagDisabledErrorMessage() string { return s.ErrorMessages.QagDisabledErrorMessage() }

// QagErrorMessageOneByWeek is ErrorMessagesRepository.getQagErrorMessageOneByWeek.
func (s *Service) QagErrorMessageOneByWeek() string {
	return s.ErrorMessages.QagErrorMessageOneByWeek()
}

// DeleteUsersData is UserDataRepository.deleteUsersData (used by DeleteUsersUseCase).
func (s *Service) DeleteUsersData(ctx context.Context, userIDs []string) error {
	return s.UserData.DeleteUsersData(ctx, userIDs)
}

// FlagSuspiciousUsers is SuspiciousActivityDetectUseCase.flagSuspiciousUsers
// (daily task): users with at least 3 signups in a day from the same IP hash
// and user agent over the last two weeks are banned. It returns the number of
// flagged users. Kotlin did not evict userCache here (stale ban flags for up
// to an hour); Go does.
func (s *Service) FlagSuspiciousUsers(ctx context.Context) (int, error) {
	enabled, err := s.Flags.IsFeatureEnabled(ctx, FeatureSuspiciousUserDetection)
	if err != nil || !enabled {
		return 0, err
	}
	now := s.a.Now()
	// LocalDate.toDate(): start of that day with the *current* time of day.
	timeOfDay := now.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
	dateNow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := dateNow.Add(timeOfDay)
	start := dateNow.AddDate(0, 0, -14).Add(timeOfDay)
	n, err := s.UserData.FlagUsersWithSuspiciousActivity(ctx, 3, start, end)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		users.Get(s.a).InvalidateAll(ctx)
	}
	return n, nil
}
