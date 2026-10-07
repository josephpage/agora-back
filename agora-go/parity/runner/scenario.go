// Package runner is the differential parity harness: it replays scenarios
// against the Kotlin reference and the Go rewrite, and compares status codes,
// headers, bodies (with bijective binding of generated values) and the
// resulting database state.
package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Scenario is one YAML file under parity/scenarios.
type Scenario struct {
	Name  string   `yaml:"name"`
	Tags  []string `yaml:"tags"`
	Setup Setup    `yaml:"setup"`
	Steps []Step   `yaml:"steps"`
	// DBDiff: "step" (after each step), "end" (default) or "none".
	DBDiff string `yaml:"dbdiff"`
	// Skip documents why a scenario is disabled.
	Skip string `yaml:"skip"`
	file string
}

// Setup prepares both environments before the scenario.
type Setup struct {
	// Reseed: reseed both databases (default true).
	Reseed *bool `yaml:"reseed"`
	// Strapi faults / overrides applied to both fakes.
	StrapiFaults    []map[string]any `yaml:"strapiFaults"`
	StrapiOverrides []map[string]any `yaml:"strapiOverrides"`
	// Redis: extra keys to set on both Redis instances (shared-format keys).
	Redis map[string]string `yaml:"redis"`
	// SQL statements executed on both databases after seeding.
	SQL []string `yaml:"sql"`
}

// Step is one HTTP request executed on both sides.
type Step struct {
	ID          string            `yaml:"id"`
	Method      string            `yaml:"method"`
	Path        string            `yaml:"path"` // may contain a raw query string
	Query       map[string]string `yaml:"query"`
	Headers     map[string]string `yaml:"headers"` // a "b64:" value is sent as raw bytes
	As          string            `yaml:"as"`      // seeded user id (or {{var}}) to authenticate with a minted JWT
	Bearer      string            `yaml:"bearer"`  // explicit token (template)
	Body        any               `yaml:"body"`    // JSON body (object/array/scalar)
	BodyRaw     *string           `yaml:"bodyRaw"`
	BodyB64     *string           `yaml:"bodyB64"` // raw body bytes (invalid UTF-8, UTF-16...), base64
	ContentType string            `yaml:"contentType"`
	Capture     map[string]string `yaml:"capture"` // var → JSON path ($.a.b[0])
	Compare     Compare           `yaml:"compare"`
	Repeat      int               `yaml:"repeat"`   // run N times sequentially
	Parallel    int               `yaml:"parallel"` // run N copies concurrently (compare status multiset)
	SleepMs     int               `yaml:"sleepMs"`  // wait before the step
	Admin       *AdminAction      `yaml:"admin"`    // non-HTTP action (run a cron task on both sides)
}

// AdminAction runs a custom command on both sides instead of an HTTP call.
type AdminAction struct {
	Command string            `yaml:"command"` // dailyTasks | weeklyTasks
	Args    map[string]string `yaml:"args"`
}

// Compare tunes the comparison of a step.
type Compare struct {
	// Body: "exact" (default: byte equality after binding), "json" (semantic),
	// "status" (status only), "none".
	Body string `yaml:"body"`
	// Unordered lists JSON paths of arrays compared as multisets.
	Unordered []string `yaml:"unordered"`
	// Ignore lists JSON paths whose values are not compared.
	Ignore []string `yaml:"ignore"`
	// Headers to compare in addition to the default set.
	Headers []string `yaml:"headers"`
}

var templateRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.:-]+)\s*\}\}`)

// LoadScenarios loads every *.yaml under dir.
func LoadScenarios(dir string) ([]*Scenario, error) {
	var out []*Scenario
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		for {
			var s Scenario
			if err := dec.Decode(&s); err != nil {
				if err.Error() == "EOF" {
					break
				}
				return fmt.Errorf("%s: %w", p, err)
			}
			if s.Name == "" {
				continue
			}
			s.file = p
			out = append(out, &s)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].file < out[j].file })
	return out, err
}

// HasTag reports whether the scenario carries one of the tags.
func (s *Scenario) HasTag(tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	for _, t := range tags {
		for _, st := range s.Tags {
			if t == st {
				return true
			}
		}
	}
	return false
}

// expand replaces {{var}} using vars; unknown variables are left untouched.
func expand(s string, vars map[string]string) string {
	return templateRe.ReplaceAllStringFunc(s, func(m string) string {
		name := templateRe.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// expandAny walks a YAML-decoded value and expands strings.
func expandAny(v any, vars map[string]string) any {
	switch t := v.(type) {
	case string:
		e := expand(t, vars)
		return e
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = expandAny(e, vars)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = expandAny(e, vars)
		}
		return out
	}
	return v
}
