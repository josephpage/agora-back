package login

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"agora/internal/modules/users"
)

// ---------------------------------------------------------------------------
// LoginUseCaseTest

type fakeUsers struct {
	calls     *[]string
	existing  *users.UserInfo
	updated   *users.UserInfo
	generated *users.UserInfo
}

func (f fakeUsers) GetUserByID(_ context.Context, id string) (*users.UserInfo, error) {
	*f.calls = append(*f.calls, "users.getUserById "+id)
	return f.existing, nil
}
func (f fakeUsers) UpdateUser(_ context.Context, r users.LoginRequest) (*users.UserInfo, error) {
	*f.calls = append(*f.calls, "users.updateUser "+r.UserID)
	return f.updated, nil
}
func (f fakeUsers) GenerateUser(_ context.Context, r users.SignupRequest) (*users.UserInfo, error) {
	*f.calls = append(*f.calls, "users.generateUser")
	return f.generated, nil
}

type fakeUserData struct {
	calls *[]string
	err   error
}

func (f fakeUserData) AddLoginData(_ context.Context, r users.LoginRequest) error {
	*f.calls = append(*f.calls, "userData.addLogin "+r.UserID)
	return f.err
}
func (f fakeUserData) AddSignupData(_ context.Context, r users.SignupRequest, id string) error {
	*f.calls = append(*f.calls, "userData.addSignup "+id)
	return f.err
}
func (f fakeUserData) GetSignupHistory(context.Context, string, string) ([]SignupHistoryCount, error) {
	*f.calls = append(*f.calls, "userData.getSignupHistory")
	return nil, nil
}

type fakeNotifier struct{ calls *[]string }

func (f fakeNotifier) NotifySignup(_ context.Context, ip, ua string) error {
	*f.calls = append(*f.calls, "suspicious.notifySignup "+ip+" "+ua)
	return nil
}

func TestLoginUseCase(t *testing.T) {
	ctx := context.Background()
	existing := &users.UserInfo{UserID: "userId"}
	updated := &users.UserInfo{UserID: "userId", FCMToken: "x"}
	loginRequest := users.LoginRequest{UserID: "userId", IPAddressHash: "ipAddressHash"}

	build := func(u fakeUsers) (*LoginUseCase, *[]string) {
		calls := &[]string{}
		u.calls = calls
		return &LoginUseCase{users: u, userData: fakeUserData{calls: calls}, notifier: fakeNotifier{calls: calls}}, calls
	}

	t.Run("findUser returns the repository result and nothing else happens", func(t *testing.T) {
		uc, calls := build(fakeUsers{existing: existing})
		got, _ := uc.FindUser(ctx, "userId")
		if got != existing || !reflect.DeepEqual(*calls, []string{"users.getUserById userId"}) {
			t.Errorf("got %v calls %v", got, *calls)
		}
	})
	t.Run("login when the repository does not return a user returns null", func(t *testing.T) {
		uc, calls := build(fakeUsers{})
		got, _ := uc.Login(ctx, loginRequest)
		if got != nil || !reflect.DeepEqual(*calls, []string{"users.getUserById userId"}) {
			t.Errorf("got %v calls %v", got, *calls)
		}
	})
	t.Run("login when the repository returns a user adds the user data then updates the user", func(t *testing.T) {
		uc, calls := build(fakeUsers{existing: existing, updated: updated})
		got, _ := uc.Login(ctx, loginRequest)
		want := []string{"users.getUserById userId", "userData.addLogin userId", "users.updateUser userId"}
		if got != updated || !reflect.DeepEqual(*calls, want) {
			t.Errorf("got %v calls %v", got, *calls)
		}
	})
	t.Run("login stops at the users_data failure (the user is not updated)", func(t *testing.T) {
		calls := &[]string{}
		uc := &LoginUseCase{
			users:    fakeUsers{calls: calls, existing: existing, updated: updated},
			userData: fakeUserData{calls: calls, err: errors.New("value too long")},
			notifier: fakeNotifier{calls: calls},
		}
		if _, err := uc.Login(ctx, loginRequest); err == nil {
			t.Error("expected an error")
		}
		if len(*calls) != 2 {
			t.Errorf("calls %v", *calls)
		}
	})
	t.Run("signUp adds the user data and notifies the suspicious user detection", func(t *testing.T) {
		generated := &users.UserInfo{UserID: "userId"}
		uc, calls := build(fakeUsers{generated: generated})
		got, _ := uc.SignUp(ctx, users.SignupRequest{IPAddressHash: "ipHash", UserAgent: "userAgent"})
		want := []string{"users.generateUser", "userData.addSignup userId", "suspicious.notifySignup ipHash userAgent"}
		if got != generated || !reflect.DeepEqual(*calls, want) {
			t.Errorf("got %v calls %v", got, *calls)
		}
	})
}

// ---------------------------------------------------------------------------
// AppVersionControlUseCaseTest

type fakeMinimal map[AppPlatform]int

func (f fakeMinimal) GetMinimalAppVersion(p AppPlatform) int { return f[p] }

func TestAppVersionControl(t *testing.T) {
	uc := &AppVersionControl{repo: fakeMinimal{PlatformAndroid: 7, PlatformIOS: 8, PlatformWeb: 9}}
	for _, c := range []struct {
		name, platform, code string
		want                 AppVersionStatus
	}{
		{"unknown platform", "quantum-computer", "42", InvalidApp},
		{"platform is case sensitive", "Android", "42", InvalidApp},
		{"versionCode is not an Int", "web", "3.14", InvalidApp},
		{"versionCode overflows", "web", "2147483648", InvalidApp},
		{"required version lower than versionCode", "android", "56", Authorized},
		{"required version higher than versionCode", "ios", "2", UpdateRequired},
		{"required version equals versionCode", "web", "9", Authorized},
		{"negative version", "web", "-9", UpdateRequired},
		{"explicit plus sign", "web", "+9", Authorized},
	} {
		if got := uc.GetAppVersionStatus(c.platform, c.code); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// IsSuspiciousUserUseCaseTest

type fakeFlags struct {
	enabled bool
	calls   *[]string
}

func (f fakeFlags) IsFeatureEnabled(_ context.Context, feature Feature) (bool, error) {
	*f.calls = append(*f.calls, "flags "+feature.Key())
	return f.enabled, nil
}

type fakeCounts struct {
	value *int
	calls *[]string
}

func (f *fakeCounts) GetTodaySignupCount(context.Context, string, string) (*int, error) {
	*f.calls = append(*f.calls, "counts.get")
	return f.value, nil
}
func (f *fakeCounts) InitTodaySignupCount(_ context.Context, _, _ string, n int) error {
	*f.calls = append(*f.calls, "counts.init "+itoa(int64(n)))
	return nil
}

type fakeHistory struct {
	history []SignupHistoryCount
	calls   *[]string
}

func (f fakeHistory) GetSignupHistory(context.Context, string, string) ([]SignupHistoryCount, error) {
	*f.calls = append(*f.calls, "history.get")
	return f.history, nil
}

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestIsSuspiciousUser(t *testing.T) {
	ctx := context.Background()
	n := func(i int) *int { return &i }
	build := func(enabled bool, count *int, history []SignupHistoryCount, now time.Time) (*IsSuspiciousUser, *[]string) {
		calls := &[]string{}
		return &IsSuspiciousUser{
			now:       func() time.Time { return now },
			flags:     fakeFlags{enabled: enabled, calls: calls},
			counts:    &fakeCounts{value: count, calls: calls},
			userDatas: fakeHistory{history: history, calls: calls},
		}, calls
	}
	jan1 := time.Date(2024, 1, 1, 12, 30, 59, 0, time.Local)
	flag := "flags IS_SUSPICIOUS_USER_DETECTION_ENABLED"

	cases := []struct {
		name    string
		enabled bool
		count   *int
		history []SignupHistoryCount
		now     time.Time
		want    bool
		calls   []string
	}{
		{"feature disabled", false, nil, nil, jan1, false, []string{flag}},
		{"count lower than 10", true, n(8), nil, jan1, false, []string{flag, "counts.get"}},
		{"count greater or equal to 10", true, n(10), nil, jan1, true, []string{flag, "counts.get"}},
		{"no count and no history: init to 0", true, nil, nil, jan1, false, []string{flag, "counts.get", "history.get", "counts.init 0"}},
		{"no count and a day with 14 signups: init to 14", true, nil, []SignupHistoryCount{{Date: time.Time{}, SignupCount: 14}}, jan1, true, []string{flag, "counts.get", "history.get", "counts.init 14"}},
		{"no count, today's entry has 8", true, nil, []SignupHistoryCount{{Date: date(2024, 1, 15), SignupCount: 8}}, time.Date(2024, 1, 15, 12, 30, 59, 0, time.Local), false, []string{flag, "counts.get", "history.get", "counts.init 8"}},
		{"no count, an old day over the threshold wins over today's entry", true, nil, []SignupHistoryCount{{Date: date(2024, 1, 15), SignupCount: 8}, {Date: date(2023, 5, 5), SignupCount: 11}}, time.Date(2024, 1, 15, 1, 0, 0, 0, time.Local), true, []string{flag, "counts.get", "history.get", "counts.init 11"}},
		{"no count, only another day under the threshold", true, nil, []SignupHistoryCount{{Date: date(2024, 1, 14), SignupCount: 9}}, time.Date(2024, 1, 15, 1, 0, 0, 0, time.Local), false, []string{flag, "counts.get", "history.get", "counts.init 0"}},
	}
	for _, c := range cases {
		uc, calls := build(c.enabled, c.count, c.history, c.now)
		got, err := uc.IsSuspiciousActivity(ctx, "ipHash", "userAgent")
		if err != nil || got != c.want || !reflect.DeepEqual(*calls, c.calls) {
			t.Errorf("%s: got %v err %v calls %v", c.name, got, err, *calls)
		}
	}

	notify := []struct {
		name    string
		enabled bool
		count   *int
		calls   []string
	}{
		{"notifySignup with the feature disabled does nothing", false, nil, []string{flag}},
		{"notifySignup without a counter does nothing", true, nil, []string{flag, "counts.get"}},
		{"notifySignup increments an existing counter", true, n(19), []string{flag, "counts.get", "counts.init 20"}},
	}
	for _, c := range notify {
		uc, calls := build(c.enabled, c.count, nil, jan1)
		if err := uc.NotifySignup(ctx, "ipHash", "userAgent"); err != nil || !reflect.DeepEqual(*calls, c.calls) {
			t.Errorf("%s: err %v calls %v", c.name, err, *calls)
		}
	}
}

// ---------------------------------------------------------------------------
// Shared Redis values

func TestDecodeFlag(t *testing.T) {
	for _, c := range []struct {
		raw          string
		isTrue, miss bool
		fails        bool
	}{
		{"true", true, false, false},
		{"false", false, false, false},
		{" true", true, false, false},
		{"true ", true, false, false},
		{`"true"`, false, false, false},
		{`"false"`, false, false, false},
		{"1", false, false, false},
		{"1.5", false, false, false},
		{"-0", false, false, false},
		{"1e3", false, false, false},
		{`""`, false, false, false},
		{"null", false, true, false},
		{"", false, true, false},
		{`["java.lang.Boolean", true]`, true, false, false},
		{`["java.lang.Boolean", false]`, false, false, false},
		{`["java.lang.Boolean", "true"]`, true, false, false},
		{`["java.lang.Boolean", 1]`, true, false, false},
		{`["java.lang.Boolean"]`, false, true, false},
		{`["java.lang.Long", 1]`, false, false, false},
		{`["java.lang.Integer", 1]`, false, false, false},
		{`["java.lang.String","true"]`, false, false, false},
		{`["java.util.ArrayList",[true]]`, false, false, false},
		{"[true]", false, false, true},
		{"[]", false, false, true},
		{"{}", false, false, true},
		{`{"a":1}`, false, false, true},
		{`["x.Y", true]`, false, false, true},
		{"[true, true]", false, false, true},
		{"[1, true]", false, false, true},
		{"garbage", false, false, true},
		{"TRUE", false, false, true},
		{"True", false, false, true},
		{"nan", false, false, true},
		{"NaN", false, false, true},
		{" ", false, false, true},
	} {
		isTrue, absent, err := decodeFlag([]byte(c.raw))
		if (err != nil) != c.fails || (err == nil && (isTrue != c.isTrue || absent != c.miss)) {
			t.Errorf("%q: true=%v absent=%v err=%v", c.raw, isTrue, absent, err)
		}
	}
}

func TestDecodeSignupCount(t *testing.T) {
	for raw, want := range map[string]int{"0": 0, "10": 10, "-3": -3, "2147483647": 2147483647, `["java.lang.Integer", 5]`: 5} {
		if got := decodeSignupCount([]byte(raw)); got == nil || *got != want {
			t.Errorf("%q: %v", raw, got)
		}
	}
	for _, raw := range []string{"", "x", "2147483648", "5.0", "1e2", `"5"`, "true", "null", `["java.lang.Long", 5]`, "[5]", "{}"} {
		if got := decodeSignupCount([]byte(raw)); got != nil {
			t.Errorf("%q: %v", raw, *got)
		}
	}
}

func TestFeatureKeys(t *testing.T) {
	want := []string{"IS_SIGNUP_ENABLED", "IS_LOGIN_ENABLED", "IS_QAG_SELECT_ENABLED", "IS_FEEDBACK_ON_RESPONSE_QAG_ENABLED",
		"IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED", "IS_SUSPICIOUS_USER_DETECTION_ENABLED",
		"IS_DELETE_BANNED_USERS_SUPPORTS_ENABLED", "IS_QAG_ANONYMIZATION_ENABLED"}
	for i, f := range AllFeatures {
		if f.Key() != want[i] {
			t.Errorf("feature %d: %s", i, f.Key())
		}
	}
}
