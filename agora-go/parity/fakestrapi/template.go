package fakestrapi

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StrapiTimeLayout is the date-time format Strapi uses in its responses.
const StrapiTimeLayout = "2006-01-02T15:04:05.000Z"

// StrapiDateLayout is the date-only format ({{date:now+3d}}).
const StrapiDateLayout = "2006-01-02"

var (
	templateRe = regexp.MustCompile(`^\{\{\s*(date:)?now((?:[+-]\d+[smhdw])*)\s*\}\}$`)
	offsetRe   = regexp.MustCompile(`([+-])(\d+)([smhdw])`)
)

// ResolveTemplate resolves a fixture template string against now.
//
// Supported forms: {{now}}, {{now+3d}}, {{now-2h}}, {{now-30m}}, {{now+10s}},
// {{now+1w}} (several offsets may be chained, e.g. {{now-1d+2h}}) and the
// date-only variant {{date:now+3d}}. matched is false when s is not a
// template at all; err is non-nil when it looks like a template but is not a
// supported one.
func ResolveTemplate(s string, now time.Time) (out string, matched bool, err error) {
	if !isTemplateLike(s) {
		return s, false, nil
	}
	m := templateRe.FindStringSubmatch(s)
	if m == nil {
		return "", true, fmt.Errorf("unsupported template %q", s)
	}
	t := now.UTC()
	for _, off := range offsetRe.FindAllStringSubmatch(m[2], -1) {
		n, convErr := strconv.Atoi(off[2])
		if convErr != nil {
			return "", true, fmt.Errorf("template %q: %v", s, convErr)
		}
		if off[1] == "-" {
			n = -n
		}
		var unit time.Duration
		switch off[3] {
		case "s":
			unit = time.Second
		case "m":
			unit = time.Minute
		case "h":
			unit = time.Hour
		case "d":
			unit = 24 * time.Hour
		case "w":
			unit = 7 * 24 * time.Hour
		}
		t = t.Add(time.Duration(n) * unit)
	}
	if strings.TrimSpace(m[1]) != "" {
		return t.Format(StrapiDateLayout), true, nil
	}
	return t.Format(StrapiTimeLayout), true, nil
}

// applyTemplates resolves templates in every string value of the tree
// (object keys are never templated).
func applyTemplates(v any, now time.Time) (any, error) {
	switch t := v.(type) {
	case string:
		out, _, err := ResolveTemplate(t, now)
		if err != nil {
			return nil, err
		}
		return out, nil
	case []any:
		for i, e := range t {
			r, err := applyTemplates(e, now)
			if err != nil {
				return nil, err
			}
			t[i] = r
		}
		return t, nil
	case *Object:
		for _, k := range t.keys {
			r, err := applyTemplates(t.m[k], now)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			t.m[k] = r
		}
		return t, nil
	}
	return v, nil
}
