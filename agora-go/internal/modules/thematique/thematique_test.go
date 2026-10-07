package thematique

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
)

// ---- ThematiqueMapperTest ------------------------------------------------------

func buildStrapiThematique(documentID, label, pictogramme string) *strapiThematique {
	return &strapiThematique{DocumentID: documentID, Label: label, Pictogramme: pictogramme}
}

func TestMapperToDomainSingleDTO(t *testing.T) {
	t.Run("maps documentId as id", func(t *testing.T) {
		got := toDomain(*buildStrapiThematique("thema-42", "Démocratie", "🗳"))
		if got.ID != "thema-42" {
			t.Fatalf("id = %q", got.ID)
		}
	})
	t.Run("maps label", func(t *testing.T) {
		got := toDomain(*buildStrapiThematique("thema-1", "Démocratie", "🗳"))
		if got.Label != "Démocratie" {
			t.Fatalf("label = %q", got.Label)
		}
	})
	t.Run("maps pictogramme as picto", func(t *testing.T) {
		got := toDomain(*buildStrapiThematique("thema-1", "Démocratie", "🗳"))
		if got.Picto != "🗳" {
			t.Fatalf("picto = %q", got.Picto)
		}
	})
}

func TestMapperToDomainList(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		if got := toDomainList(nil); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("two items keep their order", func(t *testing.T) {
		got := toDomainList([]*strapiThematique{
			buildStrapiThematique("thema-1", "Santé", "🏥"),
			buildStrapiThematique("thema-2", "Éducation", "📚"),
		})
		if len(got) != 2 || got[0].ID != "thema-1" || got[0].Label != "Santé" || got[1].ID != "thema-2" || got[1].Label != "Éducation" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a null element throws like the Kotlin mapper (500)", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		toDomainList([]*strapiThematique{buildStrapiThematique("a", "A", "p"), nil})
	})
	t.Run("one item maps all fields", func(t *testing.T) {
		got := toDomainList([]*strapiThematique{buildStrapiThematique("thema-abc", "Autonomie", "👵")})
		if len(got) != 1 || got[0] != (Thematique{ID: "thema-abc", Label: "Autonomie", Picto: "👵"}) {
			t.Fatalf("got %v", got)
		}
	})
}

// ---- ThematiqueRepositoryImplTest ---------------------------------------------

type fakeCache struct {
	snap       *snapshot
	gets, puts int
}

func (c *fakeCache) get() (*snapshot, bool) {
	c.gets++
	return c.snap, c.snap != nil
}

func (c *fakeCache) put(s *snapshot) {
	c.puts++
	c.snap = s
}

type fakeStrapi struct {
	data  []*strapiThematique
	calls atomic.Int32
}

func (f *fakeStrapi) fetch(context.Context) []*strapiThematique {
	f.calls.Add(1)
	return f.data
}

func newRepo(c *fakeCache, s *fakeStrapi) *repository {
	return &repository{cache: c, fetch: s.fetch, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestRepositoryGetThematiqueList(t *testing.T) {
	ctx := context.Background()
	t.Run("cached list is returned without calling Strapi", func(t *testing.T) {
		cache := &fakeCache{snap: newSnapshot([]Thematique{{ID: "a", Label: "A", Picto: "p"}})}
		s := &fakeStrapi{}
		got := newRepo(cache, s).snapshot(ctx).list
		if len(got) != 1 || got[0].ID != "a" {
			t.Fatalf("got %v", got)
		}
		if s.calls.Load() != 0 || cache.puts != 0 {
			t.Fatalf("strapi calls=%d puts=%d", s.calls.Load(), cache.puts)
		}
	})
	t.Run("not initialized and Strapi returns nothing: not cached, empty list", func(t *testing.T) {
		cache := &fakeCache{}
		s := &fakeStrapi{}
		got := newRepo(cache, s).snapshot(ctx).list
		if len(got) != 0 {
			t.Fatalf("got %v", got)
		}
		if cache.puts != 0 || s.calls.Load() != 1 {
			t.Fatalf("puts=%d strapi calls=%d", cache.puts, s.calls.Load())
		}
	})
	t.Run("not initialized and Strapi returns something: cached then returned", func(t *testing.T) {
		cache := &fakeCache{}
		s := &fakeStrapi{data: []*strapiThematique{buildStrapiThematique("th1", "Santé", "🏥")}}
		repo := newRepo(cache, s)
		got := repo.snapshot(ctx).list
		if len(got) != 1 || got[0].ID != "th1" {
			t.Fatalf("got %v", got)
		}
		if cache.puts != 1 || s.calls.Load() != 1 {
			t.Fatalf("puts=%d strapi calls=%d", cache.puts, s.calls.Load())
		}
		// the second call is served from the cache
		repo.snapshot(ctx)
		if s.calls.Load() != 1 {
			t.Fatalf("Strapi called again: %d", s.calls.Load())
		}
	})
	t.Run("empty Strapi answers are retried at every call", func(t *testing.T) {
		cache := &fakeCache{}
		s := &fakeStrapi{}
		repo := newRepo(cache, s)
		repo.snapshot(ctx)
		repo.snapshot(ctx)
		if s.calls.Load() != 2 {
			t.Fatalf("strapi calls=%d", s.calls.Load())
		}
	})
}

func TestRepositoryGetThematique(t *testing.T) {
	ctx := context.Background()
	t.Run("cached list with a matching thematique", func(t *testing.T) {
		want := Thematique{ID: "0f2c2c0e-0000-4000-8000-000000000001", Label: "L", Picto: "P"}
		cache := &fakeCache{snap: newSnapshot([]Thematique{want})}
		s := &fakeStrapi{}
		got := newRepo(cache, s).byID(ctx, want.ID)
		if got == nil || *got != want {
			t.Fatalf("got %v", got)
		}
		if s.calls.Load() != 0 {
			t.Fatal("Strapi must not be called")
		}
	})
	t.Run("cached list without the thematique returns nil", func(t *testing.T) {
		cache := &fakeCache{snap: newSnapshot([]Thematique{{ID: "00000000-0000-0000-0000-000000000000"}})}
		s := &fakeStrapi{}
		if got := newRepo(cache, s).byID(ctx, "other"); got != nil {
			t.Fatalf("got %v", got)
		}
		if s.calls.Load() != 0 {
			t.Fatal("Strapi must not be called")
		}
	})
	t.Run("not initialized, Strapi has the thematique: cached then returned", func(t *testing.T) {
		cache := &fakeCache{}
		s := &fakeStrapi{data: []*strapiThematique{buildStrapiThematique("th9", "Lbl", "p")}}
		got := newRepo(cache, s).byID(ctx, "th9")
		if got == nil || got.ID != "th9" || got.Label != "Lbl" {
			t.Fatalf("got %v", got)
		}
		if cache.puts != 1 {
			t.Fatalf("puts=%d", cache.puts)
		}
	})
	t.Run("not initialized, Strapi lacks the thematique: cached, nil", func(t *testing.T) {
		cache := &fakeCache{}
		s := &fakeStrapi{data: []*strapiThematique{buildStrapiThematique("th9", "Lbl", "p")}}
		if got := newRepo(cache, s).byID(ctx, "unknown"); got != nil {
			t.Fatalf("got %v", got)
		}
		if cache.puts != 1 {
			t.Fatalf("puts=%d", cache.puts)
		}
	})
	t.Run("first thematique wins on duplicated ids", func(t *testing.T) {
		cache := &fakeCache{snap: newSnapshot([]Thematique{{ID: "d", Label: "first"}, {ID: "x"}, {ID: "d", Label: "second"}})}
		got := newRepo(cache, &fakeStrapi{}).byID(ctx, "d")
		if got == nil || got.Label != "first" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("the not-found error is logged", func(t *testing.T) {
		var sb strings.Builder
		repo := newRepo(&fakeCache{snap: newSnapshot(nil)}, &fakeStrapi{})
		repo.log = slog.New(slog.NewTextHandler(&sb, nil))
		repo.byID(ctx, "zz")
		if !strings.Contains(sb.String(), "Thematique id 'zz' non trouvée") || !strings.Contains(sb.String(), "level=ERROR") {
			t.Fatalf("log = %q", sb.String())
		}
	})
}

// ---- ListThematiqueUseCase ------------------------------------------------------

func TestSortThematiques(t *testing.T) {
	autre := Thematique{ID: idThematiqueAutre, Label: "Autre"}
	in := []Thematique{
		{ID: "1", Label: "Santé"},
		autre,
		{ID: "2", Label: "Environnement"},
		{ID: "3", Label: "Zoologie"},
		{ID: "4", Label: "Éducation"},       // 'É' (U+00C9) sorts after ASCII letters
		{ID: "5", Label: "environnement"},   // lower case after upper case
		{ID: "6", Label: "Environnement"},   // equal label: stable (after id 2)
		{ID: "7", Label: "😀 supplementary"}, // lead surrogate D83D < U+FF21
		{ID: "8", Label: "ＡＢ fullwidth"},    // U+FF21 > surrogates in UTF-16 order
	}
	got := sortThematiques(in)
	var ids []string
	for _, g := range got {
		ids = append(ids, g.ID)
	}
	if joined := strings.Join(ids, ","); joined != "2,6,1,3,5,4,7,8,"+idThematiqueAutre {
		t.Fatalf("order = %s", joined)
	}
	// the input is not modified
	if in[1] != autre {
		t.Fatal("input modified")
	}
}

func TestCompareUTF16(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"a", "a", 0}, {"a", "b", -1}, {"b", "a", 1},
		{"ab", "a", 1}, {"a", "ab", -1}, {"", "a", -1},
		{"\U0001F600", "Ａ", -1}, // U+1F600 = D83D DE00 < U+FF21 in UTF-16 (UTF-8 order says the opposite)
		{"Ａ", "\U0001F600", 1},
		{"\U0001F600", "\U0001F601", -1},
		{"\U00010000", "\U0001F600", -1},
		{"", "\U00010000", 1},
	}
	for _, c := range cases {
		if got := compareUTF16(c.a, c.b); got != c.want {
			t.Errorf("compareUTF16(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestJSONBodies(t *testing.T) {
	list := []Thematique{{ID: "a", Label: "L", Picto: "P"}}
	if got := ToListJSON(list); len(got.Thematiques) != 1 || got.Thematiques[0] != (ThematiqueJSON{ID: "a", Label: "L", Picto: "P"}) {
		t.Fatalf("got %v", got)
	}
	if got := ToNoIDJSON(list[0]); got != (ThematiqueNoIDJSON{Label: "L", Picto: "P"}) {
		t.Fatalf("got %v", got)
	}
	if got := ToListJSON(nil); got.Thematiques == nil || len(got.Thematiques) != 0 {
		t.Fatalf("empty list must be [] not null: %#v", got)
	}
}
