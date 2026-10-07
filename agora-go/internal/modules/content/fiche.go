package content

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/modules/thematique"
	"agora/internal/strapi"
)

// strapiThematique is ThematiqueStrapiDTO, the thematique embedded in a fiche
// (populate=*): the fiche mapper uses it as is (ThematiqueMapper.toDomain(dto)),
// there is no lookup in the thematique list.
type strapiThematique struct {
	DocumentID  string `json:"documentId"`
	Label       string `json:"label"`
	Pictogramme string `json:"pictogramme"`
}

// strapiFiche is FicheInventaireStrapiDTO.
type strapiFiche struct {
	DocumentID             string              `json:"documentId"`
	EtapeLancement         strapi.RichText     `json:"etape_1_lancement"`
	EtapeAnalyse           strapi.RichText     `json:"etape_2_analyse"`
	EtapeSuivi             strapi.RichText     `json:"etape_3_suivi"`
	Titre                  string              `json:"titre"`
	Debut                  LocalDate           `json:"debut"`
	Fin                    LocalDate           `json:"fin"`
	Porteur                string              `json:"porteur"`
	LienSite               string              `json:"lien_site"`
	ConditionParticipation string              `json:"condition_participation"`
	ModaliteParticipation  string              `json:"modalite_participation"`
	Thematique             strapiThematique    `json:"thematique"`
	Illustration           strapi.MediaPicture `json:"illustration"`
	Etape                  string              `json:"etape"`
	AnneeDeLancement       string              `json:"annee_de_lancement"`
	Type                   string              `json:"type"`
}

// ficheInventaire is domain.FicheInventaire (rich texts already rendered).
type ficheInventaire struct {
	etapeLancement, etapeAnalyse, etapeSuivi string
	titre                                    string
	debut, fin                               LocalDate
	porteur, lienSite                        string
	conditionParticipation                   string
	modaliteParticipation                    string
	thematique                               thematique.Thematique
	illustration                             string
	etape, anneeDeLancement, typ, id         string
}

// toFicheInventaire is FicheInventaireRepositoryImpl.toFicheInventaire. A null
// element of the Strapi list is an NPE (non-null Kotlin parameter): panic.
func toFicheInventaire(f *strapiFiche) ficheInventaire {
	if f == nil {
		panic(errors.New("NullPointerException: Parameter specified as non-null is null: toFicheInventaire, parameter fiche"))
	}
	return ficheInventaire{
		id:                     f.DocumentID,
		etapeLancement:         f.EtapeLancement.ToHTML(),
		etapeAnalyse:           f.EtapeAnalyse.ToHTML(),
		etapeSuivi:             f.EtapeSuivi.ToHTML(),
		titre:                  f.Titre,
		debut:                  f.Debut,
		fin:                    f.Fin,
		porteur:                f.Porteur,
		lienSite:               f.LienSite,
		conditionParticipation: f.ConditionParticipation,
		modaliteParticipation:  f.ModaliteParticipation,
		thematique:             thematique.Thematique{ID: f.Thematique.DocumentID, Label: f.Thematique.Label, Picto: f.Thematique.Pictogramme},
		illustration:           f.Illustration.MediaURL(),
		etape:                  f.Etape,
		anneeDeLancement:       f.AnneeDeLancement,
		typ:                    f.Type,
	}
}

// FicheInventaireJSON is FicheInventaireJson.
type FicheInventaireJSON struct {
	ID                     string                        `json:"id"`
	EtapeLancementHTML     string                        `json:"etapeLancementHtml"`
	EtapeAnalyseHTML       string                        `json:"etapeAnalyseHtml"`
	EtapeSuiviHTML         string                        `json:"etapeSuiviHtml"`
	Titre                  string                        `json:"titre"`
	Debut                  string                        `json:"debut"`
	Fin                    string                        `json:"fin"`
	Porteur                string                        `json:"porteur"`
	LienSite               string                        `json:"lienSite"`
	ConditionParticipation string                        `json:"conditionParticipation"`
	ModaliteParticipation  string                        `json:"modaliteParticipation"`
	Thematique             thematique.ThematiqueNoIDJSON `json:"thematique"`
	IllustrationURL        string                        `json:"illustrationUrl"`
	Etape                  string                        `json:"etape"`
	AnneeDeLancement       string                        `json:"anneeDeLancement"`
	Type                   string                        `json:"type"`
}

// JavaName is the XML root element.
func (FicheInventaireJSON) JavaName() string { return "FicheInventaireJson" }

// FicheInventaireListJSON is the body of ResponseEntity<List<FicheInventaireJson>>:
// Jackson XML names the root element after the declared type, "List".
type FicheInventaireListJSON []FicheInventaireJSON

// JavaName is the XML root element.
func (FicheInventaireListJSON) JavaName() string { return "List" }

// toFicheInventaireJSON is FicheInventaireJsonMapper.toFicheInventaireJson.
func toFicheInventaireJSON(f ficheInventaire) FicheInventaireJSON {
	return FicheInventaireJSON{
		ID:                     f.id,
		EtapeLancementHTML:     f.etapeLancement,
		EtapeAnalyseHTML:       f.etapeAnalyse,
		EtapeSuiviHTML:         f.etapeSuivi,
		Titre:                  f.titre,
		Debut:                  f.debut.formatStartOfDay(),
		Fin:                    f.fin.formatStartOfDay(),
		Porteur:                f.porteur,
		LienSite:               f.lienSite,
		ConditionParticipation: f.conditionParticipation,
		ModaliteParticipation:  f.modaliteParticipation,
		Thematique:             thematique.ToNoIDJSON(f.thematique),
		IllustrationURL:        f.illustration,
		Etape:                  f.etape,
		AnneeDeLancement:       f.anneeDeLancement,
		Type:                   f.typ,
	}
}

// ficheFilters is FicheInventaireFilters (the derived properties).
type ficheFilters struct {
	titre, thematique, anneeDeLancement                  *string
	etape, conditionParticipation, modaliteParticipation []string // nil = null
}

// newFicheFilters is the constructor of FicheInventaireFilters: the derived properties
// (blank strings and empty lists are null, list elements trimmed).
func newFicheFilters(titre, thematique *string, etape, conditionParticipation, modaliteParticipation []string, anneeDeLancement *string) ficheFilters {
	return ficheFilters{
		titre:                  nonBlank(titre),
		thematique:             nonBlank(thematique),
		etape:                  trimNonEmpty(etape),
		conditionParticipation: trimNonEmpty(conditionParticipation),
		modaliteParticipation:  trimNonEmpty(modaliteParticipation),
		anneeDeLancement:       nonBlank(anneeDeLancement),
	}
}

func nonBlank(p *string) *string {
	if p == nil || javacompat.KotlinIsBlank(*p) {
		return nil
	}
	return p
}

// trimNonEmpty is list?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() }.
func trimNonEmpty(l []string) []string {
	if l == nil {
		return nil
	}
	var out []string
	for _, s := range l {
		if t := javacompat.KotlinTrim(s); t != "" {
			out = append(out, t)
		}
	}
	return out // nil when empty
}

// cacheKey is FicheInventaireRepositoryImpl.toCacheKey. A null filter is written
// "null": a literal "null" value shares the key of an absent filter.
func (f ficheFilters) cacheKey() string {
	str := func(p *string) string {
		if p == nil {
			return "null"
		}
		return *p
	}
	list := func(l []string) string {
		if l == nil {
			return "null"
		}
		s := append([]string(nil), l...)
		sort.SliceStable(s, func(i, j int) bool { return compareUTF16(s[i], s[j]) < 0 })
		return strings.Join(s, ",")
	}
	return strings.Join([]string{
		"titre=" + str(f.titre),
		"thematique=" + str(f.thematique),
		"etape=" + list(f.etape),
		"condition=" + list(f.conditionParticipation),
		"modalite=" + list(f.modaliteParticipation),
		"annee=" + str(f.anneeDeLancement),
	}, "|")
}

// strapiRequest is FicheInventaireStrapiRepository.getFichesInventaire's builder.
func (f ficheFilters) strapiRequest() *strapi.RequestBuilder {
	b := strapi.NewRequest("fiche-inventaires")
	if f.titre != nil {
		b.Contains("titre", f.titre)
	}
	if f.thematique != nil {
		b.FilterInPath([]string{"thematique", "documentId"}, []string{*f.thematique})
	}
	if f.etape != nil {
		b.FilterIn("etape", f.etape)
	}
	if f.conditionParticipation != nil {
		b.FilterIn("condition_participation", f.conditionParticipation)
	}
	if f.modaliteParticipation != nil {
		b.FilterIn("modalite_participation", f.modaliteParticipation)
	}
	if f.anneeDeLancement != nil {
		b.FilterIn("annee_de_lancement", []string{*f.anneeDeLancement})
	}
	b.SortBy("debut", "desc")
	return b
}

const (
	fichesCacheName = "fichesInventaireCache"
	// fichesCacheTTL is the shortTermCacheManager entry TTL (5 minutes).
	fichesCacheTTL = 5 * time.Minute
)

// getAll is FicheInventaireRepositoryImpl.getAll: the 5 minutes cache keyed by
// toCacheKey (empty lists included: a Strapi failure is cached too), else Strapi
// then the mapping (an exception is not cached).
func getAllFiches(ctx context.Context, a *app.App, f ficheFilters) ([]ficheInventaire, error) {
	return cache.GetOrLoad(a.Cache, fichesCacheName, f.cacheKey(), fichesCacheTTL, func() ([]ficheInventaire, error) {
		env := strapi.Collection[*strapiFiche](context.WithoutCancel(ctx), a.Strapi, f.strapiRequest())
		return try(func() []ficheInventaire {
			out := make([]ficheInventaire, len(env.Data))
			for i, d := range env.Data {
				out[i] = toFicheInventaire(d)
			}
			return out
		})
	})
}

// getFiche is FicheInventaireRepositoryImpl.get: not cached; nil when Strapi has
// no (first) element (a Strapi failure included).
func getFiche(ctx context.Context, a *app.App, id string) (*ficheInventaire, error) {
	env := strapi.Collection[*strapiFiche](ctx, a.Strapi, strapi.NewRequest("fiche-inventaires").GetByIDs([]string{id}))
	if len(env.Data) == 0 || env.Data[0] == nil { // firstOrNull()
		return nil, nil
	}
	f, err := try(func() ficheInventaire { return toFicheInventaire(env.Data[0]) })
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// listHandler is FicheInventaireController.getFichesInventaireList.
func listHandler(a *app.App) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		filters := newFicheFilters(c.OptionalParam("titre"), c.OptionalParam("thematique"), c.ParamList("etape"),
			c.ParamList("conditionParticipation"), c.ParamList("modaliteParticipation"), c.OptionalParam("anneeDeLancement"))
		fiches, err := getAllFiches(c.Context(), a, filters)
		if err != nil {
			panic(err)
		}
		out := make(FicheInventaireListJSON, len(fiches))
		for i, f := range fiches {
			out[i] = toFicheInventaireJSON(f)
		}
		return httpx.OK(out)
	}
}

// detailHandler is FicheInventaireController.getFichesInventaire (FicheInventaireNotFound → 404).
func detailHandler(a *app.App) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		fiche, err := getFiche(c.Context(), a, c.PathVar("idFiche"))
		if err != nil {
			panic(err)
		}
		if fiche == nil {
			return notFoundAdvice(c, "Veuillez renseigner un id de fiche inventaire existant.")
		}
		return httpx.OK(toFicheInventaireJSON(*fiche))
	}
}

// mediaTypeUnacceptable tells whether the ?mediaType= parameter makes the content
// negotiation fail (neither json nor xml nor empty, case insensitive).
func mediaTypeUnacceptable(c *httpx.Ctx) bool {
	v, ok := c.Param("mediaType")
	if !ok {
		return false
	}
	switch strings.ToLower(v) {
	case "", "json", "xml":
		return false
	}
	return true
}

// notFoundAdvice is a DefaultControllerAdvice handler (@ResponseStatus(NOT_FOUND),
// body ErrorResponse(title)). When the ?mediaType= parameter makes the content
// negotiation fail, Spring cannot write the body: the status chosen by the
// handler (>= 400) is kept and the body is empty (observed on the reference),
// whereas httpx answers 406 (foundation finding "status >= 400 and
// ?mediaType=foo" in parity/ledger/S9.md).
func notFoundAdvice(c *httpx.Ctx, title string) *httpx.Response {
	if mediaTypeUnacceptable(c) {
		return httpx.Empty(404)
	}
	panic(&httpx.AdviceError{Status: 404, Title: title})
}

// utf16Lead is the first UTF-16 code unit of r.
func utf16Lead(r rune) rune {
	if r >= 0x10000 {
		return 0xD800 + ((r - 0x10000) >> 10)
	}
	return r
}

// compareUTF16 is java.lang.String.compareTo (lexicographic on UTF-16 code
// units; Kotlin's List<String>.sorted()). Only the sign is meaningful.
func compareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			la, lb := utf16Lead(ra), utf16Lead(rb)
			switch {
			case la < lb:
				return -1
			case la > lb:
				return 1
			case ra < rb:
				return -1
			default:
				return 1
			}
		}
		a, b = a[sa:], b[sb:]
	}
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return -1
	}
	return 1
}
