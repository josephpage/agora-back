package themehebdo

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"
)

// ThemeHebdo is domain.ThemeHebdo. Dates are java.util.Date values (millisecond
// precision). Treat the slice and the pointed values as read-only: a ThemeHebdo
// may be shared through the cache.
type ThemeHebdo struct {
	Titre           string
	SousTitre       string
	Periode         string
	Theme           string
	AvatarURL       *string
	Nom             *string
	Fonction        *string
	ProchainsThemes []string
	TitreCompteur   string
	DateDebutTheme  *time.Time
	DateFinTheme    *time.Time
	EstThemeLibre   bool
}

func strPtr(s string) *string { return &s }

// Default is ThemeHebdo() (the data class default values).
func Default() ThemeHebdo {
	return ThemeHebdo{
		Titre:           "CETTE SEMAINE",
		SousTitre:       "Posez toutes vos questions sur",
		Periode:         "",
		Theme:           "Semaine libre",
		AvatarURL:       strPtr("https://pub-6c821c1c547c4e3eaa97abd4b0ab8180.r2.dev/logo_agora_f524e2d9bd.png"),
		Nom:             strPtr("Ministre à révéler"),
		Fonction:        strPtr("Le ministre qui répondra dépend de votre question gagnante"),
		ProchainsThemes: []string{},
		TitreCompteur:   "Fin des votes le",
		EstThemeLibre:   true,
	}
}

// themeLibreSousTitre replaces sousTitre when the theme is free.
const themeLibreSousTitre = "Posez vos questions sur n'importe quelle politique publique."

// transitionWindowMs is IsThemeHebdoTransitionUseCase.TRANSITION_WINDOW_MS (6 hours).
const transitionWindowMs int64 = 6 * 3_600_000

// themeRepository is ThemeHebdoRepository.
type themeRepository interface {
	List(ctx context.Context) ([]ThemeHebdo, error)
}

// currentCache is CurrentThemeHebdoCacheRepository ("currentThemeHebdo").
type currentCache interface {
	get() (ThemeHebdo, bool)
	put(ThemeHebdo)
}

// useCase groups GetThemeHebdoUseCase and IsThemeHebdoTransitionUseCase.
type useCase struct {
	repo  themeRepository
	cache currentCache
	clock func() time.Time // Clock
	log   *slog.Logger
	group singleflight.Group
}

// Theme is GetThemeHebdoUseCase.getThemeHebdo().
func (u *useCase) Theme(ctx context.Context) (ThemeHebdo, error) {
	list, err := u.repo.List(ctx)
	if err != nil {
		return ThemeHebdo{}, err
	}
	return buildTheme(list, u.clock()), nil
}

// Current is GetThemeHebdoUseCase.getCurrentThemeHebdo(): the theme cached for
// 5 minutes, computed (and cached, even when it is the default theme) on a miss.
func (u *useCase) Current(ctx context.Context) (ThemeHebdo, error) {
	if t, ok := u.cache.get(); ok {
		return t, nil
	}
	v, err, _ := u.group.Do("current", func() (any, error) {
		if t, ok := u.cache.get(); ok {
			return t, nil
		}
		t, err := u.Theme(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		u.cache.put(t)
		return t, nil
	})
	if err != nil {
		return ThemeHebdo{}, err
	}
	return v.(ThemeHebdo), nil
}

// IsInTransition is IsThemeHebdoTransitionUseCase.isInTransition(): true when
// now is within 6 hours of the start or the end of any theme.
func (u *useCase) IsInTransition(ctx context.Context) (bool, error) {
	nowMs := u.clock().UnixMilli()
	themes, err := u.repo.List(ctx)
	if err != nil {
		return false, err
	}
	isTransition := false
	for _, t := range themes {
		if withinWindow(nowMs, t.DateDebutTheme) || withinWindow(nowMs, t.DateFinTheme) {
			isTransition = true
			break
		}
	}
	if isTransition {
		u.log.Info("🗓️ Transition de thème hebdomadaire détectée (fenêtre de 6h)")
	} else {
		u.log.Info("🗓️ Aucune transition de thème hebdomadaire détectée")
	}
	return isTransition, nil
}

// withinWindow is isWithinWindow: Math.abs(now.time - boundary.time) <= 6h
// (Java's Math.abs wraps on Long.MIN_VALUE, like this one).
func withinWindow(nowMs int64, boundary *time.Time) bool {
	if boundary == nil {
		return false
	}
	diff := nowMs - boundary.UnixMilli()
	if diff < 0 {
		diff = -diff
	}
	return diff <= transitionWindowMs
}

// buildTheme is the body of getThemeHebdo() once the list is loaded.
func buildTheme(list []ThemeHebdo, now time.Time) ThemeHebdo {
	nowMs := now.UnixMilli()
	current := Default()
	for _, t := range list {
		if isCurrentDateInRange(nowMs, t) {
			current = t
			break
		}
	}
	result := withPeriode(withDefaultDates(current, now))
	result.ProchainsThemes = prochainsThemes(list, result)
	if result.EstThemeLibre {
		result.SousTitre = themeLibreSousTitre
	}
	result.Periode = kotlinUppercase(result.Periode)
	return result
}

// isCurrentDateInRange: !now.before(debut) && !now.after(fin); false when a bound is null.
func isCurrentDateInRange(nowMs int64, t ThemeHebdo) bool {
	if t.DateDebutTheme == nil || t.DateFinTheme == nil {
		return false
	}
	return nowMs >= t.DateDebutTheme.UnixMilli() && nowMs <= t.DateFinTheme.UnixMilli()
}

// withDefaultDates fills the missing dates with this week's Monday 00:05 UTC and
// Sunday 23:55 UTC (ISO week of the UTC date).
func withDefaultDates(t ThemeHebdo, now time.Time) ThemeHebdo {
	today := now.UTC()
	sinceMonday := (int(today.Weekday()) + 6) % 7 // DayOfWeek.MONDAY = 0
	monday := time.Date(today.Year(), today.Month(), today.Day()-sinceMonday, 0, 5, 0, 0, time.UTC)
	sunday := time.Date(today.Year(), today.Month(), today.Day()-sinceMonday+6, 23, 55, 0, 0, time.UTC)
	if t.DateDebutTheme == nil {
		d := monday
		t.DateDebutTheme = &d
	}
	if t.DateFinTheme == nil {
		d := sunday
		t.DateFinTheme = &d
	}
	return t
}

// withPeriode generates the periode from the dates when it is empty.
func withPeriode(t ThemeHebdo) ThemeHebdo {
	if t.Periode != "" {
		return t
	}
	if t.DateDebutTheme == nil || t.DateFinTheme == nil {
		return t
	}
	t.Periode = generatePeriodeFromDates(*t.DateDebutTheme, *t.DateFinTheme)
	return t
}

// prochainsThemes: the (at most) 3 themes starting after the current one, by start date.
func prochainsThemes(all []ThemeHebdo, current ThemeHebdo) []string {
	if current.DateDebutTheme == nil {
		return []string{}
	}
	currentMs := current.DateDebutTheme.UnixMilli()
	var next []ThemeHebdo
	for _, t := range all {
		if t.DateDebutTheme != nil && t.DateDebutTheme.UnixMilli() > currentMs {
			next = append(next, t)
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].DateDebutTheme.UnixMilli() < next[j].DateDebutTheme.UnixMilli() })
	if len(next) > 3 {
		next = next[:3]
	}
	out := make([]string, len(next))
	for i, t := range next {
		out[i] = t.Theme
	}
	return out
}

// frenchMonths is Month.getDisplayName(TextStyle.FULL, Locale.FRENCH).
var frenchMonths = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

// generatePeriodeFromDates is "19-25 mai" (same month: only the month is
// compared, not the year) or "26 mai - 1 juin", from the UTC dates.
func generatePeriodeFromDates(debut, fin time.Time) string {
	d, f := debut.UTC(), fin.UTC()
	if d.Month() == f.Month() {
		return strconv.Itoa(d.Day()) + "-" + strconv.Itoa(f.Day()) + " " + frenchMonths[d.Month()-1]
	}
	return strconv.Itoa(d.Day()) + " " + frenchMonths[d.Month()-1] + " - " + strconv.Itoa(f.Day()) + " " + frenchMonths[f.Month()-1]
}
