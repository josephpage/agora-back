package themehebdo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"agora/internal/strapi"
)

// ---- helpers --------------------------------------------------------------------

type fakeRepo struct {
	list  []ThemeHebdo
	err   error
	calls int
}

func (f *fakeRepo) List(context.Context) ([]ThemeHebdo, error) {
	f.calls++
	return f.list, f.err
}

type fakeCurrentCache struct {
	val  *ThemeHebdo
	puts int
}

func (c *fakeCurrentCache) get() (ThemeHebdo, bool) {
	if c.val == nil {
		return ThemeHebdo{}, false
	}
	return *c.val, true
}

func (c *fakeCurrentCache) put(t ThemeHebdo) { c.val = &t; c.puts++ }

func newUseCase(repo themeRepository, cache currentCache, now time.Time) *useCase {
	return &useCase{repo: repo, cache: cache, clock: func() time.Time { return now }, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func datePtr(t time.Time) *time.Time { return &t }

func buildThemeHebdo(debut, fin *time.Time) ThemeHebdo {
	return ThemeHebdo{
		Titre: "Titre", SousTitre: "Sous-titre", Periode: "19-25 mai 2026", Theme: "Éducation",
		AvatarURL: strPtr("https://picsum.photos/40"), Nom: strPtr("Jean Dupont"),
		Fonction: strPtr("Ministre de l'Éducation nationale"), ProchainsThemes: []string{},
		TitreCompteur: "Cloture des votes", DateDebutTheme: debut, DateFinTheme: fin,
	}
}

func with(t ThemeHebdo, f func(*ThemeHebdo)) ThemeHebdo { f(&t); return t }

const defaultAvatar = "https://pub-6c821c1c547c4e3eaa97abd4b0ab8180.r2.dev/logo_agora_f524e2d9bd.png"

// ---- GetThemeHebdoUseCaseTest ----------------------------------------------------

var (
	now       = time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	yesterday = now.Add(-24 * time.Hour)
	tomorrow  = now.Add(24 * time.Hour)
	lastWeek  = now.Add(-7 * 24 * time.Hour)
	nextWeek  = now.Add(7 * 24 * time.Hour)

	expectedMondayDebut = utc(2026, 5, 18, 0, 5)
	expectedSundayFin   = utc(2026, 5, 24, 23, 55)
)

func theme(t *testing.T, list []ThemeHebdo, at time.Time) ThemeHebdo {
	t.Helper()
	got, err := newUseCase(&fakeRepo{list: list}, &fakeCurrentCache{}, at).Theme(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertDefaultTheme(t *testing.T, got ThemeHebdo) {
	t.Helper()
	if !got.DateDebutTheme.Equal(expectedMondayDebut) || !got.DateFinTheme.Equal(expectedSundayFin) {
		t.Fatalf("dates = %v %v", got.DateDebutTheme, got.DateFinTheme)
	}
	if *got.AvatarURL != defaultAvatar || *got.Nom != "Ministre à révéler" || *got.Fonction != "Le ministre qui répondra dépend de votre question gagnante" {
		t.Fatalf("not the default theme: %+v", got)
	}
}

func TestGetThemeHebdo(t *testing.T) {
	t.Run("empty list gives the default theme with default dates", func(t *testing.T) {
		assertDefaultTheme(t, theme(t, nil, now))
	})
	t.Run("one item within the range is returned", func(t *testing.T) {
		got := theme(t, []ThemeHebdo{buildThemeHebdo(&yesterday, &tomorrow)}, now)
		if !got.DateDebutTheme.Equal(yesterday) || !got.DateFinTheme.Equal(tomorrow) {
			t.Fatalf("dates = %v %v", got.DateDebutTheme, got.DateFinTheme)
		}
	})
	t.Run("one item outside the range gives the default theme", func(t *testing.T) {
		assertDefaultTheme(t, theme(t, []ThemeHebdo{with(buildThemeHebdo(&lastWeek, &yesterday), func(x *ThemeHebdo) { x.Titre = "Passé" })}, now))
	})
	t.Run("the matching item among several", func(t *testing.T) {
		list := []ThemeHebdo{
			with(buildThemeHebdo(&lastWeek, &yesterday), func(x *ThemeHebdo) { x.Titre = "Passé" }),
			with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.Titre = "Actuel" }),
			with(buildThemeHebdo(&tomorrow, &nextWeek), func(x *ThemeHebdo) { x.Titre = "Futur" }),
		}
		if got := theme(t, list, now); got.Titre != "Actuel" {
			t.Fatalf("titre = %q", got.Titre)
		}
	})
	t.Run("none matches gives the default theme", func(t *testing.T) {
		list := []ThemeHebdo{buildThemeHebdo(&lastWeek, &yesterday), buildThemeHebdo(&tomorrow, &nextWeek)}
		assertDefaultTheme(t, theme(t, list, now))
	})
	t.Run("the first of several matching items", func(t *testing.T) {
		list := []ThemeHebdo{
			with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.Titre = "Premier" }),
			with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.Titre = "Second" }),
		}
		if got := theme(t, list, now); got.Titre != "Premier" {
			t.Fatalf("titre = %q", got.Titre)
		}
	})
	t.Run("both bounds are inclusive", func(t *testing.T) {
		at := now
		list := []ThemeHebdo{with(buildThemeHebdo(&at, &at), func(x *ThemeHebdo) { x.Titre = "Pile" })}
		if got := theme(t, list, now); got.Titre != "Pile" {
			t.Fatalf("titre = %q", got.Titre)
		}
	})
	for name, th := range map[string]ThemeHebdo{
		"null dateDebutTheme": buildThemeHebdo(nil, &tomorrow),
		"null dateFinTheme":   buildThemeHebdo(&yesterday, nil),
		"both dates null":     buildThemeHebdo(nil, nil),
	} {
		t.Run(name+" is excluded from the range check", func(t *testing.T) {
			assertDefaultTheme(t, theme(t, []ThemeHebdo{th}, now))
		})
	}
	t.Run("original dates are kept when within range", func(t *testing.T) {
		got := theme(t, []ThemeHebdo{buildThemeHebdo(&yesterday, &tomorrow)}, now)
		if !got.DateDebutTheme.Equal(yesterday) || !got.DateFinTheme.Equal(tomorrow) {
			t.Fatalf("dates = %v %v", got.DateDebutTheme, got.DateFinTheme)
		}
	})
}

func TestGeneratePeriodeFromDates(t *testing.T) {
	t.Run("same month: compact format", func(t *testing.T) {
		th := with(buildThemeHebdo(datePtr(utc(2026, 5, 19, 0, 5)), datePtr(utc(2026, 5, 25, 23, 55))), func(x *ThemeHebdo) { x.Periode = "" })
		if got := theme(t, []ThemeHebdo{th}, now); got.Periode != "19-25 MAI" {
			t.Fatalf("periode = %q", got.Periode)
		}
	})
	t.Run("different months: extended format", func(t *testing.T) {
		at := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
		th := with(buildThemeHebdo(datePtr(utc(2026, 5, 26, 0, 5)), datePtr(utc(2026, 6, 1, 23, 55))), func(x *ThemeHebdo) { x.Periode = "" })
		if got := theme(t, []ThemeHebdo{th}, at); got.Periode != "26 MAI - 1 JUIN" {
			t.Fatalf("periode = %q", got.Periode)
		}
	})
	t.Run("an existing periode is kept (upper-cased)", func(t *testing.T) {
		th := with(buildThemeHebdo(datePtr(utc(2026, 5, 19, 0, 5)), datePtr(utc(2026, 5, 25, 23, 55))), func(x *ThemeHebdo) { x.Periode = "Période personnalisée" })
		if got := theme(t, []ThemeHebdo{th}, now); got.Periode != "PÉRIODE PERSONNALISÉE" {
			t.Fatalf("periode = %q", got.Periode)
		}
	})
	t.Run("french month names, accents upper-cased", func(t *testing.T) {
		for m, want := range map[time.Month]string{2: "1-2 FÉVRIER", 8: "1-2 AOÛT", 12: "1-2 DÉCEMBRE"} {
			if got := generatePeriodeFromDates(utc(2026, m, 1, 0, 5), utc(2026, m, 2, 0, 5)); kotlinUppercase(got) != want {
				t.Errorf("month %d: %q", m, got)
			}
		}
	})
	t.Run("only the month is compared, not the year", func(t *testing.T) {
		if got := generatePeriodeFromDates(utc(2026, 10, 6, 0, 5), utc(2027, 10, 12, 0, 5)); got != "6-12 octobre" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestGetThemeHebdoSousTitre(t *testing.T) {
	libre := with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.EstThemeLibre = true })
	if got := theme(t, []ThemeHebdo{libre}, now); got.SousTitre != "Posez vos questions sur n'importe quelle politique publique." {
		t.Fatalf("sousTitre = %q", got.SousTitre)
	}
	if got := theme(t, []ThemeHebdo{buildThemeHebdo(&yesterday, &tomorrow)}, now); got.SousTitre != "Sous-titre" {
		t.Fatalf("sousTitre = %q", got.SousTitre)
	}
}

func TestGetProchainsThemes(t *testing.T) {
	cur := with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.Theme = "Éducation" })
	at := func(days int) *time.Time { return datePtr(now.Add(time.Duration(days) * 24 * time.Hour)) }
	fut := func(name string, from, to int) ThemeHebdo {
		return with(buildThemeHebdo(at(from), at(to)), func(x *ThemeHebdo) { x.Theme = name })
	}
	t.Run("no future theme", func(t *testing.T) {
		if got := theme(t, []ThemeHebdo{cur}, now); len(got.ProchainsThemes) != 0 {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("one future theme", func(t *testing.T) {
		got := theme(t, []ThemeHebdo{cur, fut("Santé", 7, 14)}, now)
		if !reflect.DeepEqual(got.ProchainsThemes, []string{"Santé"}) {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("three future themes in ascending date order", func(t *testing.T) {
		list := []ThemeHebdo{cur, fut("Environnement", 9, 16), fut("Économie", 16, 23), fut("Santé", 2, 9)}
		got := theme(t, list, now)
		if !reflect.DeepEqual(got.ProchainsThemes, []string{"Santé", "Environnement", "Économie"}) {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("more than three: the three closest", func(t *testing.T) {
		list := []ThemeHebdo{cur, fut("Logement", 23, 30), fut("Environnement", 9, 16), fut("Santé", 2, 9), fut("Économie", 16, 23)}
		got := theme(t, list, now)
		if !reflect.DeepEqual(got.ProchainsThemes, []string{"Santé", "Environnement", "Économie"}) {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("default theme: nothing after the default end", func(t *testing.T) {
		if got := theme(t, nil, now); len(got.ProchainsThemes) != 0 {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("a null dateDebutTheme is excluded", func(t *testing.T) {
		list := []ThemeHebdo{cur, with(buildThemeHebdo(nil, at(14)), func(x *ThemeHebdo) { x.Theme = "Santé" })}
		if got := theme(t, list, now); len(got.ProchainsThemes) != 0 {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("themes starting exactly at the current start are not next", func(t *testing.T) {
		list := []ThemeHebdo{cur, with(cur, func(x *ThemeHebdo) { x.Theme = "Même début" })}
		if got := theme(t, list, now); len(got.ProchainsThemes) != 0 {
			t.Fatalf("got %v", got.ProchainsThemes)
		}
	})
	t.Run("the repository list is not modified", func(t *testing.T) {
		list := []ThemeHebdo{cur}
		theme(t, list, now)
		if len(list[0].ProchainsThemes) != 0 || list[0].SousTitre != "Sous-titre" || list[0].Periode != "19-25 mai 2026" {
			t.Fatalf("list modified: %+v", list[0])
		}
	})
}

func TestDefaultDatesFollowTheUTCWeek(t *testing.T) {
	// Sunday 2026-10-11 23:59 UTC is still in the week of Monday 2026-10-05.
	got := theme(t, nil, utc(2026, 10, 11, 23, 59))
	if !got.DateDebutTheme.Equal(utc(2026, 10, 5, 0, 5)) || !got.DateFinTheme.Equal(utc(2026, 10, 11, 23, 55)) {
		t.Fatalf("dates = %v %v", got.DateDebutTheme, got.DateFinTheme)
	}
	// Monday 00:00 UTC starts the next week (although it is already 02:00 in Paris on Sunday night).
	got = theme(t, nil, utc(2026, 10, 12, 0, 0))
	if !got.DateDebutTheme.Equal(utc(2026, 10, 12, 0, 5)) || !got.DateFinTheme.Equal(utc(2026, 10, 18, 23, 55)) {
		t.Fatalf("dates = %v %v", got.DateDebutTheme, got.DateFinTheme)
	}
}

func TestGetCurrentThemeHebdoCache(t *testing.T) {
	t.Run("cache not initialized: computes and stores", func(t *testing.T) {
		repo := &fakeRepo{list: []ThemeHebdo{with(buildThemeHebdo(&yesterday, &tomorrow), func(x *ThemeHebdo) { x.EstThemeLibre = true })}}
		cache := &fakeCurrentCache{}
		got, err := newUseCase(repo, cache, now).Current(context.Background())
		if err != nil || !got.EstThemeLibre {
			t.Fatalf("got %+v err %v", got, err)
		}
		if cache.puts != 1 || !reflect.DeepEqual(*cache.val, got) {
			t.Fatalf("cache = %+v puts %d", cache.val, cache.puts)
		}
	})
	t.Run("cache populated: no repository call", func(t *testing.T) {
		cached := buildThemeHebdo(&yesterday, &tomorrow)
		repo := &fakeRepo{}
		got, err := newUseCase(repo, &fakeCurrentCache{val: &cached}, now).Current(context.Background())
		if err != nil || !reflect.DeepEqual(got, cached) {
			t.Fatalf("got %+v err %v", got, err)
		}
		if repo.calls != 0 {
			t.Fatalf("repository called %d times", repo.calls)
		}
	})
	t.Run("the default theme is cached too", func(t *testing.T) {
		repo := &fakeRepo{}
		cache := &fakeCurrentCache{}
		uc := newUseCase(repo, cache, now)
		uc.Current(context.Background())
		uc.Current(context.Background())
		if repo.calls != 1 || cache.puts != 1 {
			t.Fatalf("repo calls %d puts %d", repo.calls, cache.puts)
		}
	})
	t.Run("errors are not cached", func(t *testing.T) {
		repo := &fakeRepo{err: errBadDate}
		cache := &fakeCurrentCache{}
		if _, err := newUseCase(repo, cache, now).Current(context.Background()); !errors.Is(err, errBadDate) || cache.puts != 0 {
			t.Fatalf("err %v puts %d", err, cache.puts)
		}
	})
}

// ---- IsThemeHebdoTransitionUseCaseTest --------------------------------------------

func TestIsInTransition(t *testing.T) {
	nowT := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	off := func(sec int) *time.Time { return datePtr(nowT.Add(time.Duration(sec) * time.Second)) }
	check := func(t *testing.T, list []ThemeHebdo, want bool) {
		t.Helper()
		repo := &fakeRepo{list: list}
		got, err := newUseCase(repo, &fakeCurrentCache{}, nowT).IsInTransition(context.Background())
		if err != nil || got != want {
			t.Fatalf("got %v (err %v), want %v", got, err, want)
		}
		if repo.calls != 1 {
			t.Fatalf("repository called %d times", repo.calls)
		}
	}
	t.Run("empty list", func(t *testing.T) { check(t, nil, false) })
	t.Run("both dates null", func(t *testing.T) { check(t, []ThemeHebdo{buildThemeHebdo(nil, nil)}, false) })
	t.Run("null debut, fin outside", func(t *testing.T) {
		check(t, []ThemeHebdo{buildThemeHebdo(nil, off(7*24*3600))}, false)
	})
	for _, bound := range []string{"fin", "debut"} {
		mk := func(d *time.Time) ThemeHebdo {
			if bound == "fin" {
				return buildThemeHebdo(nil, d)
			}
			return buildThemeHebdo(d, nil)
		}
		for name, tc := range map[string]struct {
			sec  int
			want bool
		}{
			"exactly now":        {0, true},
			"exactly 6h before":  {6 * 3600, true},
			"exactly 6h after":   {-6 * 3600, true},
			"6h01 before":        {6*3600 + 60, false},
			"6h01 after":         {-6*3600 - 60, false},
			"1 second after 6h":  {-6*3600 - 1, false},
			"1 second before 6h": {6*3600 - 1, true},
		} {
			t.Run(bound+" "+name, func(t *testing.T) { check(t, []ThemeHebdo{mk(off(tc.sec))}, tc.want) })
		}
	}
	t.Run("several themes, none in the window", func(t *testing.T) {
		check(t, []ThemeHebdo{
			buildThemeHebdo(off(-14*24*3600), off(-7*24*3600)),
			buildThemeHebdo(off(7*24*3600), off(14*24*3600)),
		}, false)
	})
	t.Run("several themes, one fin in the window", func(t *testing.T) {
		check(t, []ThemeHebdo{
			buildThemeHebdo(off(-7*24*3600), off(-3600)),
			buildThemeHebdo(off(7*24*3600), off(14*24*3600)),
		}, true)
	})
	t.Run("several themes, one debut in the window", func(t *testing.T) {
		check(t, []ThemeHebdo{
			buildThemeHebdo(off(-14*24*3600), off(-7*24*3600)),
			buildThemeHebdo(off(3600), off(7*24*3600)),
		}, true)
	})
	t.Run("repository errors are returned", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{err: errBadDate}, &fakeCurrentCache{}, nowT).IsInTransition(context.Background())
		if !errors.Is(err, errBadDate) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the outcome is logged", func(t *testing.T) {
		var sb strings.Builder
		uc := newUseCase(&fakeRepo{}, &fakeCurrentCache{}, nowT)
		uc.log = slog.New(slog.NewTextHandler(&sb, nil))
		uc.IsInTransition(context.Background())
		if !strings.Contains(sb.String(), "Aucune transition de thème hebdomadaire détectée") {
			t.Fatalf("log = %q", sb.String())
		}
	})
}

// ---- ThemeHebdoMapperTest ---------------------------------------------------------

func buildStrapi(f func(*strapiThemeHebdo)) []*strapiThemeHebdo {
	d := &strapiThemeHebdo{
		Theme: "Thème libre", Periode: strPtr("19-25 mai 2026"),
		DateDebut: "2026-05-19T00:00:00+02:00", DateFin: "2026-05-25T23:59:00+02:00",
	}
	if f != nil {
		f(d)
	}
	return []*strapiThemeHebdo{d}
}

func mapOne(t *testing.T, f func(*strapiThemeHebdo)) ThemeHebdo {
	t.Helper()
	got, err := toDomain(buildStrapi(f))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v err %v", got, err)
	}
	return got[0]
}

func TestMapperPhoto(t *testing.T) {
	t.Run("null photo gives a null avatarUrl", func(t *testing.T) {
		if got := mapOne(t, nil); got.AvatarURL != nil {
			t.Fatalf("avatarUrl = %v", *got.AvatarURL)
		}
	})
	t.Run("medium format url", func(t *testing.T) {
		got := mapOne(t, func(d *strapiThemeHebdo) {
			d.Photo = &strapi.MediaPicture{
				Formats:                &strapi.MediaPictureFormats{Medium: &strapi.MediaPictureFormatMedium{URL: "https://example.com/medium.jpg"}},
				PictureURLNotOptimized: "https://example.com/original.jpg",
			}
		})
		if *got.AvatarURL != "https://example.com/medium.jpg" {
			t.Fatalf("avatarUrl = %v", *got.AvatarURL)
		}
	})
	t.Run("no medium format: raw url", func(t *testing.T) {
		got := mapOne(t, func(d *strapiThemeHebdo) {
			d.Photo = &strapi.MediaPicture{Formats: &strapi.MediaPictureFormats{}, PictureURLNotOptimized: "https://example.com/original.jpg"}
		})
		if *got.AvatarURL != "https://example.com/original.jpg" {
			t.Fatalf("avatarUrl = %v", *got.AvatarURL)
		}
	})
}

func TestMapperFields(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got := mapOne(t, func(d *strapiThemeHebdo) {
			d.Theme, d.Periode = "Santé", strPtr("19-25 mai 2026")
			d.NomMinistre, d.Fonction = strPtr("Jean Dupont"), strPtr("Ministre de la santé")
		})
		if got.Theme != "Santé" || got.Periode != "19-25 mai 2026" || *got.Nom != "Jean Dupont" || *got.Fonction != "Ministre de la santé" {
			t.Fatalf("got %+v", got)
		}
		if got.DateDebutTheme == nil || got.DateFinTheme == nil {
			t.Fatal("dates missing")
		}
		if got.DateDebutTheme.UnixMilli() != utc(2026, 5, 18, 22, 0).UnixMilli() || got.DateFinTheme.UnixMilli() != utc(2026, 5, 25, 21, 59).UnixMilli() {
			t.Fatalf("dates = %v %v", got.DateDebutTheme.UTC(), got.DateFinTheme.UTC())
		}
		// the data class defaults are kept
		if got.Titre != "CETTE SEMAINE" || got.SousTitre != "Posez toutes vos questions sur" || got.TitreCompteur != "Fin des votes le" {
			t.Fatalf("defaults lost: %+v", got)
		}
	})
	t.Run("null periode gives an empty string", func(t *testing.T) {
		if got := mapOne(t, func(d *strapiThemeHebdo) { d.Periode = nil }); got.Periode != "" {
			t.Fatalf("periode = %q", got.Periode)
		}
	})
	t.Run("null ministre and fonction stay null", func(t *testing.T) {
		got := mapOne(t, nil)
		if got.Nom != nil || got.Fonction != nil {
			t.Fatalf("got %+v", got)
		}
	})
	for _, libre := range []bool{true, false} {
		if got := mapOne(t, func(d *strapiThemeHebdo) { d.EstThemeLibre = libre }); got.EstThemeLibre != libre {
			t.Errorf("est_theme_libre %v → %v", libre, got.EstThemeLibre)
		}
	}
	t.Run("a bad date is an error (500)", func(t *testing.T) {
		for _, bad := range []string{"", "2026-05-19", "2026-05-19T00:00:00", "19/05/2026"} {
			if _, err := toDomain(buildStrapi(func(d *strapiThemeHebdo) { d.DateDebut = bad })); err == nil {
				t.Errorf("date_debut %q accepted", bad)
			}
			if _, err := toDomain(buildStrapi(func(d *strapiThemeHebdo) { d.DateFin = bad })); err == nil {
				t.Errorf("date_fin %q accepted", bad)
			}
		}
	})
	t.Run("a null list element is an error (500)", func(t *testing.T) {
		if _, err := toDomain([]*strapiThemeHebdo{nil}); err == nil {
			t.Fatal("no error")
		}
	})
}

// ---- repository: THEME_HEBDO_CACHE_ENABLED --------------------------------------

type mapCache struct {
	val  []ThemeHebdo
	ok   bool
	puts int
}

func (c *mapCache) get() ([]ThemeHebdo, bool) { return c.val, c.ok }
func (c *mapCache) put(l []ThemeHebdo)        { c.val, c.ok = l, true; c.puts++ }

func TestRepositoryCacheEnabled(t *testing.T) {
	calls := 0
	data := buildStrapi(nil)
	mk := func(enabled bool, micro listCache) (*repository, *mapCache) {
		cache := &mapCache{}
		return &repository{cacheEnabled: enabled, cache: cache, micro: micro, fetch: func(context.Context) []*strapiThemeHebdo { calls++; return data }}, cache
	}
	ctx := context.Background()
	t.Run("disabled: Strapi at every call, nothing cached", func(t *testing.T) {
		calls = 0
		repo, cache := mk(false, nil)
		repo.List(ctx)
		repo.List(ctx)
		if calls != 2 || cache.puts != 0 {
			t.Fatalf("calls %d puts %d", calls, cache.puts)
		}
	})
	t.Run("disabled: the micro cache shares a load", func(t *testing.T) {
		calls = 0
		repo, cache := mk(false, &mapCache{})
		repo.List(ctx)
		repo.List(ctx)
		if calls != 1 || cache.puts != 0 {
			t.Fatalf("calls %d puts %d", calls, cache.puts)
		}
	})
	t.Run("enabled: cached when not empty", func(t *testing.T) {
		calls = 0
		repo, cache := mk(true, nil)
		repo.List(ctx)
		l, _ := repo.List(ctx)
		if calls != 1 || cache.puts != 1 || len(l) != 1 {
			t.Fatalf("calls %d puts %d", calls, cache.puts)
		}
	})
	t.Run("enabled: an empty list is not cached", func(t *testing.T) {
		calls = 0
		saved := data
		data = nil
		defer func() { data = saved }()
		repo, cache := mk(true, nil)
		repo.List(ctx)
		repo.List(ctx)
		if calls != 2 || cache.puts != 0 {
			t.Fatalf("calls %d puts %d", calls, cache.puts)
		}
	})
	t.Run("a mapper error propagates and is not cached", func(t *testing.T) {
		saved := data
		data = buildStrapi(func(d *strapiThemeHebdo) { d.DateFin = "nope" })
		defer func() { data = saved }()
		for _, enabled := range []bool{true, false} {
			repo, cache := mk(enabled, &mapCache{})
			if _, err := repo.List(ctx); err == nil || cache.puts != 0 {
				t.Fatalf("enabled=%v err %v puts %d", enabled, err, cache.puts)
			}
		}
	})
}

// ---- JSON mapper, dates ---------------------------------------------------------

func TestToJSONDates(t *testing.T) {
	th := with(buildThemeHebdo(datePtr(utc(2026, 10, 5, 0, 5)), datePtr(time.UnixMilli(utc(2026, 10, 11, 23, 55).UnixMilli()+120))), func(x *ThemeHebdo) {
		x.AvatarURL, x.Nom, x.Fonction = nil, nil, nil
	})
	got := ToJSON(th)
	if *got.DateDebutTheme != "2026-10-05T02:05:00+02:00" || *got.DateFinTheme != "2026-10-12T01:55:00.12+02:00" {
		t.Fatalf("dates = %s %s", *got.DateDebutTheme, *got.DateFinTheme)
	}
	t.Run("winter offset", func(t *testing.T) {
		if s := formatISOOffsetDateTime(utc(2026, 1, 15, 12, 0).UnixMilli()); s != "2026-01-15T13:00:00+01:00" {
			t.Fatalf("got %s", s)
		}
	})
	t.Run("a null date is a NullPointerException", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		ToJSON(buildThemeHebdo(nil, &tomorrow))
	})
}

func TestParseOffsetDateTimeMillis(t *testing.T) {
	ok := map[string]int64{
		"2026-05-19T00:00:00+02:00":     utc(2026, 5, 18, 22, 0).UnixMilli(),
		"2026-05-19T00:00:00.000Z":      utc(2026, 5, 19, 0, 0).UnixMilli(),
		"2026-05-19T00:00:00.1234567Z":  utc(2026, 5, 19, 0, 0).UnixMilli() + 123,
		"2026-05-19T00:00Z":             utc(2026, 5, 19, 0, 0).UnixMilli(),
		"2026-05-19t00:00:00z":          utc(2026, 5, 19, 0, 0).UnixMilli(),
		"2026-05-19T00:00:00-05:30":     utc(2026, 5, 19, 5, 30).UnixMilli(),
		"2026-05-19T00:00:00+02":        utc(2026, 5, 18, 22, 0).UnixMilli(),
		"2026-05-19T00:00:00+02:00:30":  utc(2026, 5, 18, 21, 59).UnixMilli() + 30_000,
		"2026-05-19T00:00:00.Z":         utc(2026, 5, 19, 0, 0).UnixMilli(),
		"2024-02-29T23:59:59.999+18:00": utc(2024, 2, 29, 5, 59).UnixMilli() + 59_999,
		"1969-12-31T23:59:59.9995Z":     -1,
		"+10000-01-01T00:00:00Z":        253402300800000,
	}
	for in, want := range ok {
		got, err := parseOffsetDateTimeMillis(in)
		if err != nil || got != want {
			t.Errorf("%q = %d (%v), want %d", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "2026-05-19", "2026-05-19T00:00:00", "2026-05-19T00:00:00 +02:00", " 2026-05-19T00:00:00Z",
		"2026-05-19T00:00:00Zx", "2026-05-19T00:00:00+0200", "2026-05-19T00:00:00+2:00", "2026-05-19T24:00:00Z",
		"2026-02-29T00:00:00Z", "2026-13-01T00:00:00Z", "2026-05-19T00:00:60Z", "2026-05-19T00:60:00Z", "2026-5-19T00:00:00Z",
		"26-05-19T00:00:00Z", "12026-05-19T00:00:00Z", "+2026-05-19T00:00:00Z", "-0000-05-19T00:00:00Z",
		"2026-05-19T00:00:00+18:01", "2026-05-19T00:00:00+19:00", "2026-05-19T00:00:00.1234567890Z", "2026-05-19T00:00:0Z",
		"2026-05-19T00:00:00,5Z", "2026-05-19 00:00:00Z", "٢٠٢٦-05-19T00:00:00Z", "2026-05-19T00:00:00+02:",
	} {
		if got, err := parseOffsetDateTimeMillis(in); err == nil {
			t.Errorf("%q accepted (%d)", in, got)
		}
	}
}

func TestKotlinUppercase(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "mai": "MAI", "février": "FÉVRIER", "août": "AOÛT", "Straße": "STRASSE", "ŉ": "ʼN", "ﬁn": "FIN", "ǆ": "Ǆ", "😀 é": "😀 É", "\u0390": "\u0399\u0308\u0301",
	} {
		if got := kotlinUppercase(in); got != want {
			t.Errorf("kotlinUppercase(%q) = %q, want %q", in, got, want)
		}
	}
}
