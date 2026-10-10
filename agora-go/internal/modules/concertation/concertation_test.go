package concertation

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
	"agora/internal/strapi"
)

var thematiqueDomain = Thematique{ID: "thema-1", Label: "Démocratie", Picto: "\U0001F5F3"}

func ldt(y int, m time.Month, d, h, mi int) consultation.LocalDateTime {
	return consultation.LocalDateTime{T: time.Date(y, m, d, h, mi, 0, 0, time.UTC)}
}

func str(s string) *string { return &s }

// buildDTO is ConcertationMapperTest.buildConcertationDTO.
func buildDTO(mod func(c *strapiConcertation)) *strapiConcertation {
	c := &strapiConcertation{
		DocumentID: "concert-1", Titre: "Concertation test", URLExterne: "https://example.com",
		URLImageDeCouverture: "https://default.jpg", DateDePublication: ldt(2026, 1, 1, 0, 0),
		Thematique: strapiThematique{DocumentID: "thema-1", Label: "Démocratie", Pictogramme: "🗳"},
	}
	if mod != nil {
		mod(c)
	}
	return c
}

// ConcertationMapperTest: toConcertations - thematique matching.
func TestToConcertationsThematiqueMatching(t *testing.T) {
	thematiques := []Thematique{thematiqueDomain}
	t.Run("when thematique matches - should include concertation", func(t *testing.T) {
		got := toConcertations([]*strapiConcertation{buildDTO(nil)}, thematiques)
		if len(got) != 1 || got[0].ID != "concert-1" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("when thematique does not match - should exclude concertation", func(t *testing.T) {
		got := toConcertations([]*strapiConcertation{buildDTO(func(c *strapiConcertation) { c.Thematique.DocumentID = "thema-unknown" })}, thematiques)
		if len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("when multiple concertations with one unmatched thematique - should only return matched ones", func(t *testing.T) {
		got := toConcertations([]*strapiConcertation{
			buildDTO(func(c *strapiConcertation) { c.DocumentID = "concert-1" }),
			buildDTO(func(c *strapiConcertation) { c.DocumentID = "concert-2"; c.Thematique.DocumentID = "thema-unknown" }),
		}, thematiques)
		if len(got) != 1 || got[0].ID != "concert-1" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("first matching thematique wins", func(t *testing.T) {
		got := toConcertations([]*strapiConcertation{buildDTO(nil)}, []Thematique{{ID: "thema-1", Label: "A"}, {ID: "thema-1", Label: "B"}})
		if got[0].Thematique.Label != "A" {
			t.Fatalf("%+v", got)
		}
	})
}

// ConcertationMapperTest: toConcertations - image url.
func TestToConcertationsImageURL(t *testing.T) {
	thematiques := []Thematique{thematiqueDomain}
	cases := []struct {
		name  string
		image *strapi.MediaPicture
		want  string
	}{
		{"when image is null - should use urlImageDeCouverture as fallback", nil, "https://fallback.jpg"},
		{"when image has medium format - should use medium url", &strapi.MediaPicture{
			Formats:                &strapi.MediaPictureFormats{Medium: &strapi.MediaPictureFormatMedium{URL: "https://medium.jpg"}},
			PictureURLNotOptimized: "https://original.jpg"}, "https://medium.jpg"},
		{"when image has no medium format - should use raw picture url", &strapi.MediaPicture{
			Formats: &strapi.MediaPictureFormats{}, PictureURLNotOptimized: "https://original.jpg"}, "https://original.jpg"},
		{"formats null - raw picture url", &strapi.MediaPicture{PictureURLNotOptimized: "https://original.jpg"}, "https://original.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toConcertations([]*strapiConcertation{buildDTO(func(c *strapiConcertation) {
				c.Image = tc.image
				c.URLImageDeCouverture = "https://fallback.jpg"
			})}, thematiques)
			if got[0].ImageURL != tc.want {
				t.Fatalf("%q", got[0].ImageURL)
			}
		})
	}
}

// ConcertationMapperTest: toConcertations - field mapping.
func TestToConcertationsFieldMapping(t *testing.T) {
	thematiques := []Thematique{thematiqueDomain}
	date := ldt(2026, 5, 1, 10, 0)
	got := toConcertations([]*strapiConcertation{buildDTO(func(c *strapiConcertation) {
		c.DocumentID, c.Titre, c.URLExterne, c.FlammeLabel, c.DateDePublication = "concert-abc", "Ma concertation", "https://example.com", str("🔥 Nouveau"), date
	})}, thematiques)
	want := Concertation{ID: "concert-abc", Title: "Ma concertation", ImageURL: "https://default.jpg", ExternalLink: "https://example.com",
		Thematique: thematiqueDomain, UpdateLabel: str("🔥 Nouveau"), UpdateDate: date, Territory: "France"}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("%+v", got)
	}
	got = toConcertations([]*strapiConcertation{buildDTO(nil)}, thematiques)
	if got[0].UpdateLabel != nil {
		t.Fatal("when flammeLabel is null - should map updateLabel as null")
	}
}

// A JSON null element of the Strapi list fails in the Kotlin mapper (HTTP 500).
func TestToConcertationsNullElementPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	toConcertations([]*strapiConcertation{nil}, []Thematique{thematiqueDomain})
}

// GetConcertationsUseCase: sortedByDescending { updateDate } is stable.
func TestSortByUpdateDateDescending(t *testing.T) {
	list := []Concertation{
		{ID: "a", UpdateDate: ldt(2026, 1, 1, 0, 0)},
		{ID: "b", UpdateDate: ldt(2026, 3, 1, 0, 0)},
		{ID: "c", UpdateDate: ldt(2026, 2, 1, 0, 0)},
		{ID: "d", UpdateDate: ldt(2026, 3, 1, 0, 0)},
		{ID: "e", UpdateDate: ldt(2026, 1, 1, 0, 0)},
	}
	sortByUpdateDateDescending(list)
	var ids string
	for _, c := range list {
		ids += c.ID
	}
	if ids != "bdcae" {
		t.Fatalf("%s", ids)
	}
}

func TestStrapiConcertationDecoding(t *testing.T) {
	decode := func(data string) ([]*strapiConcertation, error) {
		var env strapi.Envelope[*strapiConcertation]
		err := jsonjava.Unmarshal([]byte(`{"data":[`+data+`],"meta":{"pagination":{"page":1,"pageSize":100,"pageCount":1,"total":1}}}`), &env)
		return env.Data, err
	}
	ok := `{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05.000Z","thematique":{"documentId":"x","label":"l","pictogramme":"p","id":1},"flamme_label":null,"image":null,"createdAt":"c","extra":1}`
	list, err := decode(ok)
	if err != nil || len(list) != 1 || list[0].DateDePublication.Format() != "2026-01-02 03:04:05" || list[0].FlammeLabel != nil || list[0].Image != nil {
		t.Fatalf("%+v %v", list, err)
	}
	// nullable properties may be missing
	if _, err := decode(`{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05","thematique":{"documentId":"x","label":"l","pictogramme":"p"}}`); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"missing titre":     `{"documentId":"d","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05","thematique":{"documentId":"x","label":"l","pictogramme":"p"}}`,
		"null thematique":   `{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05","thematique":null}`,
		"thematique label":  `{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05","thematique":{"documentId":"x","pictogramme":"p"}}`,
		"bad date":          `{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"nope","thematique":{"documentId":"x","label":"l","pictogramme":"p"}}`,
		"null image_url":    `{"documentId":"d","titre":"t","url":"u","image_url":null,"datetime_publication":"2026-01-02T03:04:05","thematique":{"documentId":"x","label":"l","pictogramme":"p"}}`,
		"image without url": `{"documentId":"d","titre":"t","url":"u","image_url":"i","datetime_publication":"2026-01-02T03:04:05","thematique":{"documentId":"x","label":"l","pictogramme":"p"},"image":{"formats":null}}`,
	} {
		if _, err := decode(bad); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

func TestToJSON(t *testing.T) {
	list := []Concertation{
		{ID: "c1", Title: "T", ImageURL: "i", ExternalLink: "l", Thematique: thematiqueDomain, UpdateLabel: str("Nouveau"), UpdateDate: ldt(2026, 5, 1, 10, 0), Territory: "France"},
		{ID: "c2", Title: "U", ImageURL: "j", ExternalLink: "m", Thematique: thematiqueDomain, UpdateDate: ldt(2026, 5, 2, 0, 0), Territory: "France"},
	}
	got := jsonjava.MarshalString(toJSON(list))
	want := `[{"id":"c1","title":"T","imageUrl":"i","externalLink":"l","thematique":{"label":"Démocratie","picto":"PICTO"},"updateLabel":"Nouveau","updateDate":"2026-05-01 10:00:00","territory":"France"},` +
		`{"id":"c2","title":"U","imageUrl":"j","externalLink":"m","thematique":{"label":"Démocratie","picto":"PICTO"},"updateLabel":null,"updateDate":"2026-05-02 00:00:00","territory":"France"}]`
	want = strings.ReplaceAll(want, "PICTO", "\\"+"uD83D"+"\\"+"uDDF3") // Jackson escapes the supplementary characters
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := jsonjava.MarshalString(toJSON(nil)); got != "[]" {
		t.Fatalf("%s", got)
	}
	if (ConcertationListJSON{}).JavaName() != "List" || (ConcertationJSON{}).JavaName() != "ConcertationJson" {
		t.Fatal("java names")
	}
}

type fakeThemes struct{ list []Thematique }

func (f fakeThemes) List(context.Context) []Thematique { return f.list }

func newService(ttl time.Duration, themes []Thematique, fetch func() []*strapiConcertation, calls *atomic.Int32) *Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &app.App{Cfg: &config.Config{MicroCacheTTL: ttl}, Cache: cache.New(nil, log, false), Log: log, Clock: time.Now}
	return &Service{a: a, themes: fakeThemes{themes}, fetch: func(context.Context) []*strapiConcertation {
		calls.Add(1)
		return fetch()
	}}
}

// The micro-cache: shared for the TTL, never an empty list, none when the TTL is 0.
func TestExecuteMicroCache(t *testing.T) {
	ctx := context.Background()
	data := []*strapiConcertation{buildDTO(nil)}
	var calls atomic.Int32
	s := newService(time.Minute, []Thematique{thematiqueDomain}, func() []*strapiConcertation { return data }, &calls)
	for i := 0; i < 3; i++ {
		if got := s.Execute(ctx); len(got) != 1 {
			t.Fatalf("%+v", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("strapi called %d times, want 1 (shared)", calls.Load())
	}

	calls.Store(0)
	data = nil
	s = newService(time.Minute, []Thematique{thematiqueDomain}, func() []*strapiConcertation { return data }, &calls)
	for i := 0; i < 3; i++ {
		if got := s.Execute(ctx); got == nil || len(got) != 0 {
			t.Fatalf("%#v", got)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("an empty list must not be kept: %d calls", calls.Load())
	}

	// no thematique (a thematique outage): everything is dropped, nothing is kept
	calls.Store(0)
	s = newService(time.Minute, nil, func() []*strapiConcertation { return []*strapiConcertation{buildDTO(nil)} }, &calls)
	s.Execute(ctx)
	s.Execute(ctx)
	if calls.Load() != 2 {
		t.Fatalf("%d calls", calls.Load())
	}

	calls.Store(0)
	s = newService(0, []Thematique{thematiqueDomain}, func() []*strapiConcertation { return []*strapiConcertation{buildDTO(nil)} }, &calls)
	s.Execute(ctx)
	s.Execute(ctx)
	if calls.Load() != 2 {
		t.Fatalf("TTL 0: %d calls", calls.Load())
	}
}
