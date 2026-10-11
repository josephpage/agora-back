package acme

import (
	"fmt"
	"strings"
	"unicode"

	"agora/internal/config"
	"agora/internal/javacompat"
)

// Config is config/AcmeConfig.kt: every field is a Spring @Value with a
// default, read from the environment (the Kotlin bean is built at startup: an
// invalid boolean or port makes the application fail to start, which Go
// reproduces by returning an error from LoadConfig).
type Config struct {
	Enabled                      bool   // ACME_ENABLED:false
	ServerURL                    string // ACME_SERVER_URL
	Domain                       string // ACME_DOMAIN
	Sans                         string // ACME_SANS (comma separated)
	EabKid                       string // ACME_EAB_KID
	EabHmacKey                   string // ACME_EAB_HMAC_KEY
	EncryptionKey                string // ACME_ENCRYPTION_KEY
	CloudflareZoneID             string // CLOUDFLARE_ZONE_ID
	CloudflareAPIToken           string // CLOUDFLARE_API_TOKEN
	ServerInteractionEnabled     bool   // ACME_SERVER_INTERACTION_ENABLED:false
	CloudflareInteractionEnabled bool   // ACME_CLOUDFLARE_INTERACTION_ENABLED:false
	CloudflareBaseURL            string // CLOUDFLARE_BASE_URL:https://api.cloudflare.com/client/v4
	StubMode                     bool   // ACME_STUB_MODE:false (parsed, never read by the Kotlin code)
	CronEnabled                  bool   // ACME_CRON_ENABLED:false
	Port                         int    // server.port:8080 (SERVER_PORT), used by the stub to build its URLs
}

// DefaultCloudflareBaseURL is the default of CLOUDFLARE_BASE_URL.
const DefaultCloudflareBaseURL = "https://api.cloudflare.com/client/v4"

// LoadConfig reads the AcmeConfig @Value properties.
func LoadConfig(cfg *config.Config) (*Config, error) {
	str := func(name, def string) string {
		if v, ok := cfg.Lookup(name); ok {
			return v
		}
		return def
	}
	c := &Config{
		ServerURL:          str("ACME_SERVER_URL", ""),
		Domain:             str("ACME_DOMAIN", ""),
		Sans:               str("ACME_SANS", ""),
		EabKid:             str("ACME_EAB_KID", ""),
		EabHmacKey:         str("ACME_EAB_HMAC_KEY", ""),
		EncryptionKey:      str("ACME_ENCRYPTION_KEY", ""),
		CloudflareZoneID:   str("CLOUDFLARE_ZONE_ID", ""),
		CloudflareAPIToken: str("CLOUDFLARE_API_TOKEN", ""),
		CloudflareBaseURL:  str("CLOUDFLARE_BASE_URL", DefaultCloudflareBaseURL),
	}
	var err error
	for _, b := range []struct {
		name string
		dst  *bool
	}{
		{"ACME_ENABLED", &c.Enabled},
		{"ACME_SERVER_INTERACTION_ENABLED", &c.ServerInteractionEnabled},
		{"ACME_CLOUDFLARE_INTERACTION_ENABLED", &c.CloudflareInteractionEnabled},
		{"ACME_STUB_MODE", &c.StubMode},
		{"ACME_CRON_ENABLED", &c.CronEnabled},
	} {
		if *b.dst, err = springBool(cfg, b.name); err != nil {
			return nil, err
		}
	}
	c.Port = 8080
	if v, ok := cfg.Lookup("SERVER_PORT"); ok {
		if c.Port, err = springInt(v); err != nil {
			return nil, fmt.Errorf("SERVER_PORT: %w", err)
		}
	}
	return c, nil
}

// AllDomains is AcmeConfig.allDomains: the main domain (CN of the
// certificate) followed by the non blank, trimmed ACME_SANS entries.
func (c *Config) AllDomains() []string {
	out := []string{c.Domain}
	for _, s := range strings.Split(c.Sans, ",") {
		t := javacompat.KotlinTrim(s)
		if !javacompat.KotlinIsBlank(t) {
			out = append(out, t)
		}
	}
	return out
}

// springBool reproduces `@Value("${NAME:false}") val x: Boolean`: Spring's
// StringToBooleanConverter on a primitive (an empty value converts to null,
// which cannot be assigned: the context fails to start).
func springBool(cfg *config.Config, name string) (bool, error) {
	v, ok := cfg.Lookup(name)
	if !ok {
		return false, nil
	}
	switch strings.ToLower(javacompat.JavaStringTrim(v)) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s: invalid boolean value '%s'", name, v)
}

// springInt reproduces `@Value("${server.port:8080}") val port: Int`
// (NumberUtils.parseNumber: all whitespace removed, "0x"/"#" hexadecimal).
func springInt(v string) (int, error) {
	if v == "" {
		return 0, fmt.Errorf("a null value cannot be assigned to a primitive type")
	}
	t := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, v)
	neg := strings.HasPrefix(t, "-")
	body := strings.TrimPrefix(t, "-")
	digits, isHex := "", false
	switch {
	case strings.HasPrefix(body, "0x"), strings.HasPrefix(body, "0X"):
		digits, isHex = body[2:], true
	case strings.HasPrefix(body, "#"):
		digits, isHex = body[1:], true
	}
	if isHex {
		if digits == "" {
			return 0, fmt.Errorf("invalid number %q", v)
		}
		var n int64
		for _, r := range digits {
			d := javacompat.JavaCharacterDigit(r, 16)
			if d < 0 {
				return 0, fmt.Errorf("invalid number %q", v)
			}
			if n = n*16 + int64(d); n > 1<<31 {
				return 0, fmt.Errorf("invalid number %q", v)
			}
		}
		if neg {
			n = -n
		}
		if n > 1<<31-1 || n < -(1<<31) {
			return 0, fmt.Errorf("invalid number %q", v)
		}
		return int(n), nil
	}
	n, ok := javacompat.KotlinToIntOrNull(t)
	if !ok {
		return 0, fmt.Errorf("invalid number %q", v)
	}
	return n, nil
}
