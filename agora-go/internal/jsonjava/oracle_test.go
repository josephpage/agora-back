package jsonjava_test

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"agora/internal/jsonjava"
	"agora/parity/oracle"
)

type errorResponse struct {
	Title string `json:"title"`
}

func TestOracleStringEscaping(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	inputs := []string{"", "a", "\"quote\"", "back\\slash", "\b\f\n\r\t", "\x00\x01\x1f\x7f", "<script>&amp;</script>",
		"  ", "😀 émoji", "é", "/slash/", " ", "\ufeff", "á", "𝟘𝟙"}
	r := rand.New(rand.NewSource(1))
	alphabet := []rune("ab\"\\/\b\f\n\r\t\x00\x1f\x7f<>&'é€😀 \U0001F680")
	for i := 0; i < 200; i++ {
		var b strings.Builder
		for j := 0; j < r.Intn(10); j++ {
			b.WriteRune(alphabet[r.Intn(len(alphabet))])
		}
		inputs = append(inputs, b.String())
	}
	for _, s := range inputs {
		if !utf8.ValidString(s) {
			continue
		}
		in, _ := json.Marshal(map[string]string{"title": s})
		var want string
		oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": "fr.gouv.agora.infrastructure.common.ErrorResponse", "json": string(in)}, &want)
		got := jsonjava.MarshalString(errorResponse{Title: s})
		if got != want {
			t.Errorf("escape(%q):\n go   %s\n java %s", s, got, want)
		}
	}
}

func TestOracleThematiquesShape(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	type them struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Picto string `json:"picto"`
	}
	type thems struct {
		Thematiques []them `json:"thematiques"`
	}
	v := thems{Thematiques: []them{{"th1", "Écologie 🌱", "🌱"}, {"th2", "Santé", "🏥"}}}
	got := jsonjava.MarshalString(v)
	var want string
	oracle.MustCall(t, "jsonRoundTrip", map[string]any{"className": "fr.gouv.agora.infrastructure.thematique.ThematiquesJson", "json": got}, &want)
	if got != want {
		t.Errorf("thematiques:\n go   %s\n java %s", got, want)
	}
}
