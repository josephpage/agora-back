// Package config loads the environment exactly as the Kotlin backend read it
// (same variable names, same defaults, same parsing rules). Kotlin read most
// variables lazily with System.getenv on every call; the environment of a
// process never changes, so reading once at boot is observably identical.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"agora/internal/javacompat"
)

// Config is the typed view of the environment.
type Config struct {
	// HTTP
	Port           string   // AGORA_PORT, else PORT, else 8080
	AllowedOrigins []string // ALLOWED_ORIGINS split on "\n" (kept verbatim, incl. \r and blanks)

	// Database (DATABASE_URL like postgres://user:pass@host:port/db?params)
	DatabaseURL         string
	DatabaseMaxPoolSize int // DATABASE_MAX_POOL_SIZE, default 5

	// Redis (REDIS_URL like redis://user:pass@host:port)
	RedisAddr     string
	RedisUser     string
	RedisPassword string

	// Auth
	JWTSecret         string
	LoginToken        LoginTokenEnv
	RemoteAddressHash RemoteAddressHashEnv

	// Strapi
	CMSAPIURL       string // cms.api.url (CMS_API_URL), default ""
	CMSAuthToken    string // cms.auth.token (CMS_AUTH_TOKEN), default ""
	StrapiSuspended bool   // strapi.suspended (STRAPI_SUSPENDED), default false

	// Business settings
	UniversalLinkURL                        string
	RequiredIOSVersion                      string
	RequiredAndroidVersion                  string
	RequiredWebVersion                      string
	ErrorTextQagDisabled                    *string
	TrendingScoreExponent                   float64
	ThemeHebdoCacheEnabled                  bool
	OpenQuestionMaxTextLength               int
	ConsultationResponseRateLimitPerHour    int
	ConsultationDocumentIDsWithoutDemoAsk   string
	FirebaseCredentialsJSON                 string

	// Go-only operational settings (no Kotlin equivalent, documented in DIVERGENCES.md)
	Coexistence      bool          // AGORA_COEXISTENCE: also evict Kotlin cache keys, cap Go caches
	MicroCacheTTL    time.Duration // AGORA_MICROCACHE_TTL (≤ 5s, decision #2)
	BootstrapSchema  bool          // AGORA_BOOTSTRAP_SCHEMA: create tables if missing (tests/new envs)
	CronDisabled     bool          // AGORA_CRON_DISABLED: custom commands exit immediately (parallel app)
	SentryDSN        string
	SentryEnv        string
	LogLevel         string
	RedisPoolSize    int
	DBAcquireTimeout time.Duration

	lookup func(string) (string, bool)
}

// LoginTokenEnv mirrors the LOGIN_TOKEN_* variables.
type LoginTokenEnv struct {
	EncodeSecret, EncodeTransformation, EncodeAlgorithm string
	DecodeSecret, DecodeTransformation, DecodeAlgorithm string
}

// RemoteAddressHashEnv mirrors the REMOTE_ADDRESS_* variables.
type RemoteAddressHashEnv struct {
	Algorithm, Iterations, KeyLength, Salt string
}

// Load reads the process environment.
func Load() (*Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads configuration through a lookup function (tests).
func LoadFrom(lookup func(string) (string, bool)) (*Config, error) {
	get := func(k string) string { v, _ := lookup(k); return v }
	c := &Config{lookup: lookup}

	if v, ok := lookup("AGORA_PORT"); ok {
		c.Port = v
	} else if v, ok := lookup("PORT"); ok {
		c.Port = v
	} else {
		c.Port = "8080"
	}

	// CrossOriginConfig: System.getenv("ALLOWED_ORIGINS").split("\n") — NPE if unset.
	origins, ok := lookup("ALLOWED_ORIGINS")
	if !ok {
		return nil, fmt.Errorf("ALLOWED_ORIGINS is not set (the Kotlin app failed to start too)")
	}
	c.AllowedOrigins = strings.Split(origins, "\n")

	c.DatabaseURL = get("DATABASE_URL")
	// Kotlin: System.getenv("DATABASE_MAX_POOL_SIZE").toIntOrNull() ?: 5
	c.DatabaseMaxPoolSize = 5
	if v, ok := javacompat.KotlinToIntOrNull(get("DATABASE_MAX_POOL_SIZE")); ok {
		c.DatabaseMaxPoolSize = v
	}

	if ru := get("REDIS_URL"); ru != "" {
		u, err := url.Parse(ru)
		if err == nil {
			c.RedisAddr = u.Host
			if u.User != nil {
				c.RedisUser = u.User.Username()
				c.RedisPassword, _ = u.User.Password()
			}
			if c.RedisUser == "" {
				c.RedisUser = "default"
			}
		}
	}

	c.JWTSecret = get("JWT_SECRET")
	c.LoginToken = LoginTokenEnv{
		EncodeSecret:         get("LOGIN_TOKEN_ENCODE_SECRET"),
		EncodeTransformation: get("LOGIN_TOKEN_ENCODE_TRANSFORMATION"),
		EncodeAlgorithm:      get("LOGIN_TOKEN_ENCODE_ALGORITHM"),
		DecodeSecret:         get("LOGIN_TOKEN_DECODE_SECRET"),
		DecodeTransformation: get("LOGIN_TOKEN_DECODE_TRANSFORMATION"),
		DecodeAlgorithm:      get("LOGIN_TOKEN_DECODE_ALGORITHM"),
	}
	c.RemoteAddressHash = RemoteAddressHashEnv{
		Algorithm:  get("REMOTE_ADDRESS_SECRET_KEY_ALGORITHM"),
		Iterations: get("REMOTE_ADDRESS_HASH_ITERATIONS"),
		KeyLength:  get("REMOTE_ADDRESS_HASH_KEY_LENGTH"),
		Salt:       get("REMOTE_ADDRESS_HASH_SALT"),
	}

	c.CMSAPIURL = get("CMS_API_URL")
	c.CMSAuthToken = get("CMS_AUTH_TOKEN")
	if v, ok := lookup("STRAPI_SUSPENDED"); ok {
		b, err := springBoolean(v)
		if err != nil {
			return nil, fmt.Errorf("STRAPI_SUSPENDED: %w", err)
		}
		c.StrapiSuspended = b
	}

	c.UniversalLinkURL = get("UNIVERSAL_LINK_URL")
	c.RequiredIOSVersion = get("REQUIRED_IOS_VERSION")
	c.RequiredAndroidVersion = get("REQUIRED_ANDROID_VERSION")
	c.RequiredWebVersion = get("REQUIRED_WEB_VERSION")
	if v, ok := lookup("ERROR_TEXT_QAG_DISABLED"); ok {
		c.ErrorTextQagDisabled = &v
	}
	// QagPaginatedV2UseCase: System.getenv("TRENDING_SCORE_EXPONENT")?.toDoubleOrNull() ?: 1.5
	c.TrendingScoreExponent = 1.5
	if v, ok := lookup("TRENDING_SCORE_EXPONENT"); ok {
		if f, ok := kotlinToDoubleOrNull(v); ok {
			c.TrendingScoreExponent = f
		}
	}
	c.ThemeHebdoCacheEnabled = javacompat.KotlinToBoolean(get("THEME_HEBDO_CACHE_ENABLED"))

	// @Value("${OPEN_QUESTION_MAX_TEXT_LENGTH:400}") Int — invalid value fails the boot.
	c.OpenQuestionMaxTextLength = 400
	if v, ok := lookup("OPEN_QUESTION_MAX_TEXT_LENGTH"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("OPEN_QUESTION_MAX_TEXT_LENGTH: %w", err)
		}
		c.OpenQuestionMaxTextLength = n
	}
	// ConsultationResponseRateLimitRepositoryImpl: toIntOrNull() ?: 20
	c.ConsultationResponseRateLimitPerHour = 20
	if v, ok := javacompat.KotlinToIntOrNull(get("CONSULTATION_RESPONSE_RATE_LIMIT_PER_HOUR")); ok {
		c.ConsultationResponseRateLimitPerHour = v
	}
	c.ConsultationDocumentIDsWithoutDemoAsk = get("CONSULTATION_DOCUMENT_IDS_WITHOUT_DEMOGRAPHIC_ASK")
	c.FirebaseCredentialsJSON = get("FIREBASE_CREDENTIALS_JSON")

	c.Coexistence = javacompat.KotlinToBoolean(get("AGORA_COEXISTENCE"))
	c.MicroCacheTTL = 5 * time.Second
	if v := get("AGORA_MICROCACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 && d <= 5*time.Second {
			c.MicroCacheTTL = d
		}
	}
	c.BootstrapSchema = javacompat.KotlinToBoolean(get("AGORA_BOOTSTRAP_SCHEMA"))
	c.CronDisabled = javacompat.KotlinToBoolean(get("AGORA_CRON_DISABLED"))
	c.SentryDSN = get("SENTRY_DSN")
	c.SentryEnv = get("SENTRY_ENVIRONMENT")
	c.LogLevel = get("LOG_LEVEL")
	c.RedisPoolSize = 50
	if v, ok := javacompat.KotlinToIntOrNull(get("AGORA_REDIS_POOL_SIZE")); ok && v > 0 {
		c.RedisPoolSize = v
	}
	c.DBAcquireTimeout = 30 * time.Second // Hikari connectionTimeout default
	return c, nil
}

// Getenv returns a raw environment variable ("" when unset), for the rare
// call sites that read ad-hoc variables (feature flags, ACME…).
func (c *Config) Getenv(name string) string { v, _ := c.lookup(name); return v }

// Lookup returns a raw environment variable and whether it is set.
func (c *Config) Lookup(name string) (string, bool) { return c.lookup(name) }

// springBoolean reproduces Spring's StringToBooleanConverter.
func springBoolean(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	case "":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean value %q", v)
}

// kotlinToDoubleOrNull reproduces String.toDoubleOrNull() for common inputs
// (Java Double.parseDouble grammar, via a screening regex in Kotlin).
func kotlinToDoubleOrNull(s string) (float64, bool) {
	t := javacompat.JavaStringTrim(s)
	if t != s || t == "" {
		// Kotlin screens with a regex that does not allow surrounding spaces
		if javacompat.JavaStringTrim(s) == "" {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(t, "d"), "D"), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
