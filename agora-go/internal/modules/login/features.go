package login

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
)

// Feature is domain/AgoraFeature.
type Feature int

// The AgoraFeature constants, in declaration order.
const (
	FeatureSignUp Feature = iota
	FeatureLogin
	FeatureQagSelect
	FeatureFeedbackResponseQag
	FeatureFeedbackConsultationUpdate
	FeatureSuspiciousUserDetection
	FeatureDeleteBannedUsersSupports
	FeatureQagAnonymization
)

// AllFeatures is AgoraFeature.values().
var AllFeatures = []Feature{
	FeatureSignUp, FeatureLogin, FeatureQagSelect, FeatureFeedbackResponseQag,
	FeatureFeedbackConsultationUpdate, FeatureSuspiciousUserDetection,
	FeatureDeleteBannedUsersSupports, FeatureQagAnonymization,
}

// Key is FeatureFlagsRepositoryImpl.toKey: the environment variable name,
// also the Redis cache key suffix.
func (f Feature) Key() string {
	switch f {
	case FeatureSignUp:
		return "IS_SIGNUP_ENABLED"
	case FeatureLogin:
		return "IS_LOGIN_ENABLED"
	case FeatureQagSelect:
		return "IS_QAG_SELECT_ENABLED"
	case FeatureFeedbackResponseQag:
		return "IS_FEEDBACK_ON_RESPONSE_QAG_ENABLED"
	case FeatureFeedbackConsultationUpdate:
		return "IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED"
	case FeatureSuspiciousUserDetection:
		return "IS_SUSPICIOUS_USER_DETECTION_ENABLED"
	case FeatureDeleteBannedUsersSupports:
		return "IS_DELETE_BANNED_USERS_SUPPORTS_ENABLED"
	case FeatureQagAnonymization:
		return "IS_QAG_ANONYMIZATION_ENABLED"
	}
	panic("unknown feature")
}

const featureFlagsCache = "featureFlags"

// FeatureFlags is FeatureFlagsRepositoryImpl + FeatureFlagsUseCase. The flags
// live in the Redis keys shared with Kotlin ("featureFlags::<ENV_NAME>", no
// expiry, plain JSON booleans); the environment value initializes them.
type FeatureFlags struct{ a *app.App }

// IsFeatureEnabled reads the shared Redis flag, falling back to (and
// storing) the environment variable. A Redis failure or an undecodable value
// is an exception on the Kotlin side (HTTP 500).
func (f *FeatureFlags) IsFeatureEnabled(ctx context.Context, feature Feature) (bool, error) {
	key := feature.Key()
	if rdb := f.a.Cache.Redis(); rdb != nil {
		raw, err := rdb.Get(ctx, cache.KotlinKey(featureFlagsCache, key)).Bytes()
		switch {
		case errors.Is(err, redis.Nil):
		case err != nil:
			return false, err
		default:
			isTrue, absent, derr := decodeFlag(raw)
			if derr != nil {
				return false, derr
			}
			if !absent {
				return isTrue, nil
			}
		}
	}
	// System.getenv(featureKey).toBoolean() (null → false)
	value := javacompat.KotlinToBoolean(os.Getenv(key))
	if err := f.a.Cache.SetSharedJSON(ctx, featureFlagsCache, key, value, 0); err != nil {
		return false, err
	}
	return value, nil
}

// decodeFlag reproduces `cache.get(key)?.get()?.let { it == true }` on a value
// written with GenericJackson2JsonRedisSerializer (default typing EVERYTHING):
// natural JSON scalars stand for themselves, other values carry a type id as
// ["class", value]; JSON null (or an empty value) is a cache miss.
func decodeFlag(raw []byte) (isTrue, absent bool, err error) {
	if len(raw) == 0 {
		return false, true, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return false, false, err
	}
	switch t := v.(type) {
	case nil:
		return false, true, nil
	case bool:
		return t, false, nil
	case string, json.Number:
		return false, false, nil
	case []any:
		return decodeTypedFlag(t)
	}
	return false, false, errors.New("SerializationException: missing type id")
}

func decodeTypedFlag(arr []any) (isTrue, absent bool, err error) {
	if len(arr) == 0 {
		return false, false, errors.New("SerializationException: empty array")
	}
	class, ok := arr[0].(string)
	if !ok || len(arr) > 2 {
		return false, false, errors.New("SerializationException: invalid type id")
	}
	if len(arr) == 1 {
		return false, true, nil // typed null
	}
	if class == "java.lang.Boolean" {
		switch t := arr[1].(type) {
		case nil:
			return false, true, nil
		case bool:
			return t, false, nil
		case json.Number:
			i, err := t.Int64()
			if err != nil {
				return false, false, err
			}
			return i != 0, false, nil
		case string:
			switch strings.TrimSpace(t) {
			case "true", "True", "TRUE":
				return true, false, nil
			case "false", "False", "FALSE", "":
				return false, false, nil
			}
		}
		return false, false, errors.New("SerializationException: cannot coerce to Boolean")
	}
	if strings.HasPrefix(class, "java.lang.") || strings.HasPrefix(class, "java.util.") {
		return false, false, nil // some other JDK value: `it == true` is false
	}
	return false, false, errors.New("SerializationException: unknown type id " + class)
}
