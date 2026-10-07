package themehebdo

import (
	"errors"
	"time"

	"agora/internal/strapi"
)

// ThemeHebdoJSON is ThemeHebdoJson (GET /theme_hebdo body).
type ThemeHebdoJSON struct {
	Titre           string   `json:"titre"`
	SousTitre       string   `json:"sousTitre"`
	Periode         string   `json:"periode"`
	Theme           string   `json:"theme"`
	AvatarURL       *string  `json:"avatarUrl"`
	Nom             *string  `json:"nom"`
	Fonction        *string  `json:"fonction"`
	ProchainsThemes []string `json:"prochainsThemes"`
	TitreCompteur   string   `json:"titreCompteur"`
	DateFinTheme    *string  `json:"dateFinTheme"`
	DateDebutTheme  *string  `json:"dateDebutTheme"`
	EstThemeLibre   bool     `json:"estThemeLibre"`
}

// JavaName is the XML root element.
func (ThemeHebdoJSON) JavaName() string { return "ThemeHebdoJson" }

// ToJSON is ThemeHebdoJsonMapper.toJson: the dates are written as
// ISO_OFFSET_DATE_TIME in Europe/Paris. A null date makes
// DateTimeFormatter.format(null) throw a NullPointerException (→ 500): panic.
func ToJSON(t ThemeHebdo) ThemeHebdoJSON {
	fin := formatDate(t.DateFinTheme)
	debut := formatDate(t.DateDebutTheme)
	return ThemeHebdoJSON{
		Titre:           t.Titre,
		SousTitre:       t.SousTitre,
		Periode:         t.Periode,
		Theme:           t.Theme,
		AvatarURL:       t.AvatarURL,
		Nom:             t.Nom,
		Fonction:        t.Fonction,
		ProchainsThemes: t.ProchainsThemes,
		TitreCompteur:   t.TitreCompteur,
		DateFinTheme:    &fin,
		DateDebutTheme:  &debut,
		EstThemeLibre:   t.EstThemeLibre,
	}
}

func formatDate(d *time.Time) string {
	if d == nil {
		panic("NullPointerException: temporal (DateTimeFormatter.format(null))")
	}
	return formatISOOffsetDateTime(d.UnixMilli())
}

// strapiThemeHebdo is ThemeHebdoStrapiDTO. Required (non-null, no default):
// theme, date_fin, date_debut; est_theme_libre defaults to false.
type strapiThemeHebdo struct {
	Theme         string               `json:"theme"`
	Periode       *string              `json:"periode"`
	Photo         *strapi.MediaPicture `json:"photo"`
	NomMinistre   *string              `json:"nom_ministre"`
	Fonction      *string              `json:"fonction"`
	DateFin       string               `json:"date_fin"`
	DateDebut     string               `json:"date_debut"`
	EstThemeLibre bool                 `json:"est_theme_libre"`
}

// toDomain is ThemeHebdoMapper.toDomain(StrapiDTO<ThemeHebdoStrapiDTO>). A date
// that OffsetDateTime.parse rejects throws in Kotlin (uncaught → 500): error.
func toDomain(data []*strapiThemeHebdo) ([]ThemeHebdo, error) {
	out := make([]ThemeHebdo, 0, len(data))
	for _, item := range data {
		if item == nil { // a JSON null element: NullPointerException on item.periode
			return nil, errNullItem
		}
		t := Default()
		t.Periode = ""
		if item.Periode != nil {
			t.Periode = *item.Periode
		}
		t.Theme = item.Theme
		t.AvatarURL = nil
		if item.Photo != nil {
			u := item.Photo.MediaURL()
			t.AvatarURL = &u
		}
		t.Nom = item.NomMinistre
		t.Fonction = item.Fonction
		debut, err := parseOffsetDateTimeMillis(item.DateDebut)
		if err != nil {
			return nil, err
		}
		fin, err := parseOffsetDateTimeMillis(item.DateFin)
		if err != nil {
			return nil, err
		}
		t.DateDebutTheme = dateFromMillis(debut)
		t.DateFinTheme = dateFromMillis(fin)
		t.EstThemeLibre = item.EstThemeLibre
		out = append(out, t)
	}
	return out, nil
}

// errNullItem is the NullPointerException of a null element of the Strapi list.
var errNullItem = errors.New("NullPointerException: null element in the Strapi theme-hebdos list")

// dateFromMillis builds a java.util.Date.
func dateFromMillis(ms int64) *time.Time {
	t := time.UnixMilli(ms)
	return &t
}
