package login

import (
	"os"

	"agora/internal/javacompat"
)

// AppPlatform is domain/AppPlatform.
type AppPlatform int

// The AppPlatform constants.
const (
	PlatformAndroid AppPlatform = iota
	PlatformIOS
	PlatformWeb
)

// AppVersionStatus is usecase/appVersionControl/AppVersionStatus.
type AppVersionStatus int

// The AppVersionStatus constants.
const (
	InvalidApp AppVersionStatus = iota
	UpdateRequired
	Authorized
)

// minimalAppVersionRepository is MinimalAppVersionRepository.
type minimalAppVersionRepository interface {
	GetMinimalAppVersion(platform AppPlatform) int
}

// envMinimalAppVersion is MinimalAppVersionRepositoryImpl.
type envMinimalAppVersion struct{}

// GetMinimalAppVersion reads REQUIRED_<PLATFORM>_VERSION. An unset variable
// is a NullPointerException in Kotlin (null receiver of toIntOrNull); an
// unparsable one means 0.
func (envMinimalAppVersion) GetMinimalAppVersion(platform AppPlatform) int {
	name := map[AppPlatform]string{
		PlatformAndroid: "REQUIRED_ANDROID_VERSION",
		PlatformIOS:     "REQUIRED_IOS_VERSION",
		PlatformWeb:     "REQUIRED_WEB_VERSION",
	}[platform]
	raw, ok := os.LookupEnv(name)
	if !ok {
		panic("NullPointerException: " + name + " is not set")
	}
	if v, ok := javacompat.KotlinToIntOrNull(raw); ok {
		return v
	}
	return 0
}

// AppVersionControl is AppVersionControlUseCase.
type AppVersionControl struct{ repo minimalAppVersionRepository }

func toAppPlatform(platform string) (AppPlatform, bool) {
	switch platform {
	case "android":
		return PlatformAndroid, true
	case "ios":
		return PlatformIOS, true
	case "web":
		return PlatformWeb, true
	}
	return 0, false
}

// GetAppVersionStatus is AppVersionControlUseCase.getAppVersionStatus.
func (u *AppVersionControl) GetAppVersionStatus(platform, versionCode string) AppVersionStatus {
	appPlatform, ok := toAppPlatform(platform)
	if !ok {
		return InvalidApp
	}
	versionCodeInt, ok := javacompat.KotlinToIntOrNull(versionCode)
	if !ok {
		return InvalidApp
	}
	if versionCodeInt >= u.repo.GetMinimalAppVersion(appPlatform) {
		return Authorized
	}
	return UpdateRequired
}
