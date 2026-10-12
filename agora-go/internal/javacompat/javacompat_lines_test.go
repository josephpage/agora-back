package javacompat

import (
	"slices"
	"testing"
)

func TestKotlinLines(t *testing.T) {
	for in, want := range map[string][]string{
		"":              {""},
		"a":             {"a"},
		"a\n":           {"a", ""},
		"a\r\nb":        {"a", "b"},
		"a\rb\r":        {"a", "b", ""},
		"a\n\rb":        {"a", "", "b"},
		"a\r\r\nb\n\nc": {"a", "", "b", "", "c"},
	} {
		if got := KotlinLines(in); !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
