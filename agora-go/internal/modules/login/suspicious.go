package login

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
)

const (
	softBanSignupCount = 10
	signupCountCache   = "signupCount"
	signupCountTTL     = 24 * time.Hour // longTermCacheManager
)

// SignupCountRepository is usecase/suspiciousUser/repository/SignupCountRepository.
type SignupCountRepository interface {
	// GetTodaySignupCount returns nil when the counter is absent or unreadable.
	GetTodaySignupCount(ctx context.Context, ipAddressHash, userAgent string) (*int, error)
	InitTodaySignupCount(ctx context.Context, ipAddressHash, userAgent string, todaySignupCount int) error
}

// redisSignupCount is SignupCountCacheRepository: the Redis key
// "signupCount::<ipHash>/<userAgent>" holds a plain JSON integer, one day.
type redisSignupCount struct{ a *app.App }

var intLiteral = regexp.MustCompile(`^-?[0-9]+$`)

// decodeSignupCount is `get(key)?.get() as? Int`: only a JSON integer fitting
// an Int (or the typed form ["java.lang.Integer", n]) is an Int.
func decodeSignupCount(raw []byte) *int {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		if len(arr) != 2 || arr[0] != "java.lang.Integer" {
			return nil
		}
		v = arr[1]
	}
	n, ok := v.(json.Number)
	if !ok || !intLiteral.MatchString(n.String()) {
		return nil
	}
	i, err := strconv.ParseInt(n.String(), 10, 32)
	if err != nil {
		return nil
	}
	r := int(i)
	return &r
}

func signupCountKey(ipAddressHash, userAgent string) string { return ipAddressHash + "/" + userAgent }

func (r *redisSignupCount) GetTodaySignupCount(ctx context.Context, ipAddressHash, userAgent string) (*int, error) {
	rdb := r.a.Cache.Redis()
	if rdb == nil {
		return nil, nil
	}
	raw, err := rdb.Get(ctx, cache.KotlinKey(signupCountCache, signupCountKey(ipAddressHash, userAgent))).Bytes()
	if errors.Is(err, redis.Nil) || err != nil { // `catch (e: Exception) { null }`
		return nil, nil
	}
	return decodeSignupCount(raw), nil
}

func (r *redisSignupCount) InitTodaySignupCount(ctx context.Context, ipAddressHash, userAgent string, todaySignupCount int) error {
	return r.a.Cache.SetSharedJSON(ctx, signupCountCache, signupCountKey(ipAddressHash, userAgent), todaySignupCount, signupCountTTL)
}

type featureReader interface {
	IsFeatureEnabled(ctx context.Context, feature Feature) (bool, error)
}

// IsSuspiciousUser is IsSuspiciousUserUseCase.
type IsSuspiciousUser struct {
	now       func() time.Time
	flags     featureReader
	counts    SignupCountRepository
	userDatas interface {
		GetSignupHistory(ctx context.Context, ipAddressHash, userAgent string) ([]SignupHistoryCount, error)
	}
}

// IsSuspiciousActivity is isSuspiciousActivity: ten signups (or more) today
// with the same IP hash and user agent.
func (u *IsSuspiciousUser) IsSuspiciousActivity(ctx context.Context, ipAddressHash, userAgent string) (bool, error) {
	enabled, err := u.flags.IsFeatureEnabled(ctx, FeatureSuspiciousUserDetection)
	if err != nil || !enabled {
		return false, err
	}
	count, err := u.counts.GetTodaySignupCount(ctx, ipAddressHash, userAgent)
	if err != nil {
		return false, err
	}
	if count == nil {
		n, err := u.buildTodaySignupCount(ctx, ipAddressHash, userAgent)
		if err != nil {
			return false, err
		}
		count = &n
	}
	return *count >= softBanSignupCount, nil
}

// NotifySignup is notifySignup: increments an existing counter (it never
// creates one).
func (u *IsSuspiciousUser) NotifySignup(ctx context.Context, ipAddressHash, userAgent string) error {
	enabled, err := u.flags.IsFeatureEnabled(ctx, FeatureSuspiciousUserDetection)
	if err != nil || !enabled {
		return err
	}
	count, err := u.counts.GetTodaySignupCount(ctx, ipAddressHash, userAgent)
	if err != nil || count == nil {
		return err
	}
	return u.counts.InitTodaySignupCount(ctx, ipAddressHash, userAgent, *count+1)
}

func (u *IsSuspiciousUser) buildTodaySignupCount(ctx context.Context, ipAddressHash, userAgent string) (int, error) {
	dateNow := civil(u.now())
	history, err := u.userDatas.GetSignupHistory(ctx, ipAddressHash, userAgent)
	if err != nil {
		return 0, err
	}
	var entry *SignupHistoryCount
	for i := range history {
		if history[i].SignupCount >= softBanSignupCount {
			entry = &history[i]
			break
		}
	}
	if entry == nil {
		for i := range history {
			if history[i].Date.Equal(dateNow) {
				entry = &history[i]
				break
			}
		}
	}
	count := 0 // SignupHistoryCount(LocalDate.MIN, 0)
	if entry != nil {
		count = entry.SignupCount
	}
	if err := u.counts.InitTodaySignupCount(ctx, ipAddressHash, userAgent, count); err != nil {
		return 0, err
	}
	return count, nil
}
