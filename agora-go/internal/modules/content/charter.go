package content

import (
	"context"
	"errors"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/strapi"
)

// strapiCharter is ParticipationCharterStrapiDTO.
type strapiCharter struct {
	Charte        strapi.RichText `json:"charte"`
	CharteSummary strapi.RichText `json:"charte_preview"`
	DatetimeDebut LocalDateTime   `json:"datetime_debut"`
}

// participationCharter is usecase.participationCharter.ParticipationCharter (HTML).
type participationCharter struct{ text, preview string }

const charterCacheKey = "charte-participations"

// latestCharter is ParticipationCharterRepositoryImpl.getLatestParticipationCharter:
// the first element of the Strapi list (version started before now, newest first).
// An empty list (also a Strapi failure) is a NoSuchElementException (HTTP 500).
//
// Kotlin asks Strapi at every request; Go shares one load for AGORA_MICROCACHE_TTL
// (class B micro-cache; the "datetime_debut < now" filter is therefore evaluated
// up to 5 s late).
func latestCharter(ctx context.Context, a *app.App) (participationCharter, error) {
	return microLoad(a, charterCacheKey, always[participationCharter], func() (participationCharter, error) {
		now := a.Now()
		env := strapi.Collection[*strapiCharter](context.WithoutCancel(ctx), a.Strapi,
			strapi.NewRequest("charte-participations").
				WithDateBefore(now, "datetime_debut").
				SortBy("datetime_debut", "desc"))
		return try(func() participationCharter {
			if len(env.Data) == 0 {
				panic(errors.New("NoSuchElementException: List is empty."))
			}
			first := env.Data[0]
			if first == nil {
				panic(errors.New("NullPointerException: ParticipationCharterStrapiDTO is null"))
			}
			return participationCharter{first.Charte.ToHTML(), first.CharteSummary.ToHTML()}
		})
	})
}

// charterHandler is ParticipationCharterController.getParticipationCharterText;
// the ETag is added by httpx (ShallowEtagHeaderFilter on /participation_charter).
func charterHandler(a *app.App) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		charter, err := latestCharter(c.Context(), a)
		if err != nil {
			panic(err)
		}
		return httpx.OK(ParticipationCharterJSON{
			ExtraText:   "<body>" + charter.text + "</body>",
			PreviewText: "<body>" + charter.preview + "</body>",
		})
	}
}
