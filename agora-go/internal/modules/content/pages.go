package content

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sync/singleflight"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/strapi"
)

// Strapi single types behind GET /content/* (ContentStrapiRepository). Every
// field of the Kotlin DTOs is a non-null property: a missing / null value makes
// the whole payload undecodable and the request fails with HTTP 500 (the
// single-type client rethrows). @JsonIgnoreProperties("createdAt", ...) only
// names properties that are ignored anyway.

type strapiPoserMaQuestion struct {
	TexteRegles strapi.RichText `json:"texte_regles"`
}

type strapiQuestionsAuGouvernement struct {
	InformationBottomsheet string           `json:"information_bottomsheet"`
	NombreDeQuestions      string           `json:"nombre_de_questions"`
	ProgrammeDuMois        *strapi.RichText `json:"programme_du_mois"`
	CommentCaMarche        *string          `json:"comment_ca_marche"`
}

type strapiReponseAuxQags struct {
	InformationReponseAVenirBottomsheet string `json:"information_reponse_a_venir_bottomsheet"`
}

type strapiSiteVitrineAccueil struct {
	TitreHeader     string          `json:"titre_header"`
	SousTitreHeader string          `json:"sous_titre_header"`
	TitreBody       string          `json:"titre_body"`
	DescriptionBody string          `json:"description_body"`
	TexteImage1     strapi.RichText `json:"texte_image_1"`
	TexteImage2     strapi.RichText `json:"texte_image_2"`
	TexteImage3     strapi.RichText `json:"texte_image_3"`
}

type strapiSiteVitrineConditionGenerales struct {
	ConditionsGeneralesDUtilisation strapi.RichText `json:"conditions_generales_d_utilisation"`
}

type strapiSiteVitrineConsultation struct {
	DonnezVotreAvis strapi.RichText `json:"donnez_votre_avis"`
}

type strapiSiteVitrineDeclarationAccessibilite struct {
	Declaration strapi.RichText `json:"declaration"`
}

type strapiSiteVitrineMentionsLegales struct {
	MentionsLegales strapi.RichText `json:"mentions_legales"`
}

type strapiSiteVitrinePolitiqueConfidentialite struct {
	PolitiqueDeConfidentialite strapi.RichText `json:"politique_de_confidentialite"`
}

type strapiSiteVitrineQuestionAuGouvernement struct {
	Titre        string          `json:"titre"`
	SousTitre    string          `json:"sous_titre"`
	TexteSoutien strapi.RichText `json:"texte_soutien"`
}

// Domain objects of ContentRepositoryImpl.
type pageQuestionAuGouvernementContent struct {
	informationBottomsheet string
	texteTotalQuestions    string
	programmeDuMois        *string
	commentCaMarche        *string
}

type siteVitrineAccueilContent struct {
	titreHeader, sousTitreHeader, titreBody, descriptionBody, texteImage1, texteImage2, texteImage3 string
}

type siteVitrineQuestionAuGouvernementContent struct {
	titre, sousTitre, texteSoutien string
}

// contentCacheName is the Go micro-cache of the Strapi pages (class B, see the
// ledger): Kotlin asks Strapi at every request, Go shares one load for at most
// AGORA_MICROCACHE_TTL (5 s). Only successful, mapped values are kept.
const contentCacheName = "content"

// try runs f turning a panic (a Kotlin exception thrown while mapping) into an
// error: the shared loaders must not panic (a panic inside singleflight with
// waiting callers crashes the process); the handler re-raises the error.
func try[T any](f func() T) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
		}
	}()
	return f(), nil
}

var microGroup singleflight.Group

// microLoad shares one Strapi load between concurrent callers and keeps a
// successful value for AGORA_MICROCACHE_TTL (class B micro-cache). A failure is
// never cached, nor a value for which keep is false (an empty list may only be
// a Strapi outage, which Kotlin retries at every call).
func microLoad[T any](a *app.App, key string, keep func(T) bool, load func() (T, error)) (T, error) {
	if v, ok := a.Cache.Get(contentCacheName, key); ok {
		return v.(T), nil
	}
	v, err, _ := microGroup.Do(key, func() (any, error) {
		if v, ok := a.Cache.Get(contentCacheName, key); ok {
			return v, nil
		}
		val, err := load()
		if err != nil {
			return nil, err
		}
		if keep(val) {
			a.Cache.Put(contentCacheName, key, val, a.Cfg.MicroCacheTTL)
		}
		return val, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

func always[T any](T) bool { return true }

// loadPage is requestSingleType + the ContentRepositoryImpl mapping, with the
// micro-cache. A failure is returned (and never cached).
func loadPage[D, R any](ctx context.Context, a *app.App, model string, mapper func(D) R) (R, error) {
	return microLoad(a, model, always[R], func() (R, error) {
		// the shared load must not die with the request that started it
		dto, err := strapi.Single[D](context.WithoutCancel(ctx), a.Strapi, strapi.NewRequest(model))
		if err != nil {
			var zero R
			return zero, err
		}
		return try(func() R { return mapper(dto) })
	})
}

func pageHandler[D, R, J any](a *app.App, model string, mapper func(D) R, toJSON func(R) J) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		content, err := loadPage(c.Context(), a, model, mapper)
		if err != nil {
			panic(err)
		}
		return cacheControl300(httpx.OK(toJSON(content)))
	}
}

// cacheControl300 is .cacheControl(CacheControl.maxAge(5, TimeUnit.MINUTES).cachePublic()).
func cacheControl300(resp *httpx.Response) *httpx.Response {
	return resp.CacheControl(5*60, true)
}

// qagCount is QagInfoRepository.getQagsCount(null): `SELECT count(*) FROM qags WHERE status = 1`
// (QagInfoDatabaseRepository.getQagsCount); a database failure is an uncaught
// exception (HTTP 500). A variable so that the unit tests need no database.
var qagCount = func(ctx context.Context, a *app.App) int {
	var n int
	if err := a.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM qags WHERE status = 1").Scan(&n); err != nil {
		panic(err)
	}
	return n
}

// withQagCount is GetContentQuestionsAuGouvernementUseCase: "{}" is replaced (every
// occurrence, literally) by the number of QaGs.
func withQagCount(content pageQuestionAuGouvernementContent, count int) pageQuestionAuGouvernementContent {
	content.texteTotalQuestions = strings.ReplaceAll(content.texteTotalQuestions, "{}", strconv.Itoa(count))
	return content
}

func routePages(a *app.App) {
	s := a.Server

	s.GET("/content/page-questions-au-gouvernement", func(c *httpx.Ctx) *httpx.Response {
		content, err := loadPage(c.Context(), a, "page-questions-au-gouvernement",
			func(d strapiQuestionsAuGouvernement) pageQuestionAuGouvernementContent {
				var programme *string
				if d.ProgrammeDuMois != nil {
					h := d.ProgrammeDuMois.ToHTML()
					programme = &h
				}
				return pageQuestionAuGouvernementContent{d.InformationBottomsheet, d.NombreDeQuestions, programme, d.CommentCaMarche}
			})
		if err != nil {
			panic(err)
		}
		content = withQagCount(content, qagCount(c.Context(), a))
		return cacheControl300(httpx.OK(QuestionsAuGouvernementContentJSON{
			Info:                content.informationBottomsheet,
			TexteTotalQuestions: content.texteTotalQuestions,
			ProgrammeDuMois:     content.programmeDuMois,
			CommentCaMarche:     content.commentCaMarche,
		}))
	})

	s.GET("/content/page-reponses-aux-qags", pageHandler(a, "page-reponse-aux-questions-au-gouvernement",
		func(d strapiReponseAuxQags) string { return d.InformationReponseAVenirBottomsheet },
		func(v string) ReponseAuxQagsJSON { return ReponseAuxQagsJSON{v} }))

	s.GET("/content/page-poser-ma-question", pageHandler(a, "page-poser-ma-question",
		func(d strapiPoserMaQuestion) string { return d.TexteRegles.ToHTML() },
		func(v string) PoserMaQuestionJSON { return PoserMaQuestionJSON{v} }))

	s.GET("/content/page-site-vitrine-accueil", pageHandler(a, "site-vitrine-accueil",
		func(d strapiSiteVitrineAccueil) siteVitrineAccueilContent {
			return siteVitrineAccueilContent{
				d.TitreHeader, d.SousTitreHeader, d.TitreBody, d.DescriptionBody,
				d.TexteImage1.ToHTML(), d.TexteImage2.ToHTML(), d.TexteImage3.ToHTML(),
			}
		},
		func(v siteVitrineAccueilContent) SiteVitrineAccueilJSON {
			return SiteVitrineAccueilJSON{v.titreHeader, v.sousTitreHeader, v.titreBody, v.descriptionBody, v.texteImage1, v.texteImage2, v.texteImage3}
		}))

	s.GET("/content/page-site-vitrine-conditions-generales", pageHandler(a, "site-vitrine-conditions-generales-d-utilisation",
		func(d strapiSiteVitrineConditionGenerales) string { return d.ConditionsGeneralesDUtilisation.ToHTML() },
		func(v string) SiteVitrineConditionGeneralesJSON {
			return SiteVitrineConditionGeneralesJSON{v}
		}))

	s.GET("/content/page-site-vitrine-consultation", pageHandler(a, "site-vitrine-consultation",
		func(d strapiSiteVitrineConsultation) string { return d.DonnezVotreAvis.ToHTML() },
		func(v string) SiteVitrineConsultationJSON { return SiteVitrineConsultationJSON{v} }))

	s.GET("/content/page-site-vitrine-declaration-accessibilite", pageHandler(a, "site-vitrine-declaration-d-accessibilite",
		func(d strapiSiteVitrineDeclarationAccessibilite) string { return d.Declaration.ToHTML() },
		func(v string) SiteVitrineDeclarationAccessibiliteJSON {
			return SiteVitrineDeclarationAccessibiliteJSON{v}
		}))

	s.GET("/content/page-site-vitrine-mentions-legales", pageHandler(a, "site-vitrine-mentions-legale",
		func(d strapiSiteVitrineMentionsLegales) string { return d.MentionsLegales.ToHTML() },
		func(v string) SiteVitrineMentionsLegalesJSON { return SiteVitrineMentionsLegalesJSON{v} }))

	s.GET("/content/page-site-vitrine-politique-confidentialite", pageHandler(a, "site-vitrine-politique-de-confidentialite",
		func(d strapiSiteVitrinePolitiqueConfidentialite) string { return d.PolitiqueDeConfidentialite.ToHTML() },
		func(v string) SiteVitrinePolitiqueConfidentialiteJSON {
			return SiteVitrinePolitiqueConfidentialiteJSON{v}
		}))

	s.GET("/content/page-site-vitrine-question-au-gouvernement", pageHandler(a, "site-vitrine-question-au-gouvernement",
		func(d strapiSiteVitrineQuestionAuGouvernement) siteVitrineQuestionAuGouvernementContent {
			return siteVitrineQuestionAuGouvernementContent{d.Titre, d.SousTitre, d.TexteSoutien.ToHTML()}
		},
		func(v siteVitrineQuestionAuGouvernementContent) SiteVitrineQuestionAuGouvernementJSON {
			return SiteVitrineQuestionAuGouvernementJSON{v.titre, v.sousTitre, v.texteSoutien}
		}))
}
