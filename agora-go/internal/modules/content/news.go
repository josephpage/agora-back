package content

import (
	"context"
	"errors"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/strapi"
)

// strapiNews is NewsStrapiDTO.
type strapiNews struct {
	Message                 strapi.RichText `json:"message"`
	ShortMessage            string          `json:"short_message"`
	CallToAction            string          `json:"call_to_action"`
	DateDeDebut             LocalDateTime   `json:"date_de_debut"`
	PageRouteMobile         string          `json:"page_route_mobile_enum"`
	PageRouteArgumentMobile *string         `json:"page_route_argument_mobile"`
}

// news is domain.News.
type news struct {
	description      string
	shortDescription string
	callToActionText string
	routeName        string
	routeArgument    *string
	beginDate        LocalDateTime
}

const newsCacheKey = "welcome-page-news"

// newsList is NewsRepository.getNews(): the Strapi collection ("welcome-page-news",
// sorted by date_de_debut desc), mapped. A Strapi failure gives an empty list; a
// null element of `data` makes the mapper throw (HTTP 500).
//
// Kotlin asks Strapi at every request; Go shares one load for AGORA_MICROCACHE_TTL
// (class B micro-cache, never for an empty answer or a mapper failure).
func newsList(ctx context.Context, a *app.App) ([]news, error) {
	return microLoad(a, newsCacheKey, func(l []news) bool { return len(l) > 0 }, func() ([]news, error) {
		env := strapi.Collection[*strapiNews](context.WithoutCancel(ctx), a.Strapi,
			strapi.NewRequest("welcome-page-news").SortBy("date_de_debut", "desc"))
		return try(func() []news {
			out := make([]news, len(env.Data))
			for i, n := range env.Data {
				if n == nil {
					panic(errors.New("NullPointerException: NewsStrapiDTO element is null"))
				}
				out[i] = news{
					description:      n.Message.ToHTML(),
					shortDescription: n.ShortMessage,
					callToActionText: n.CallToAction,
					routeName:        n.PageRouteMobile,
					routeArgument:    n.PageRouteArgumentMobile,
					beginDate:        n.DateDeDebut,
				}
			}
			return out
		})
	})
}

// lastNews is GetLastNewsUseCase.execute(): the news with the greatest beginDate
// among those started (beginDate <= now); on a tie the first one of the list.
func lastNews(list []news, now LocalDateTime) *NewsJSON {
	var best *news
	for i := range list {
		n := &list[i]
		if n.beginDate.Compare(now) > 0 {
			continue
		}
		if best == nil || best.beginDate.Compare(n.beginDate) < 0 { // maxByOrNull keeps the first maximum
			best = n
		}
	}
	if best == nil {
		return nil
	}
	return &NewsJSON{
		Description:      best.description,
		ShortDescription: best.shortDescription,
		CallToActionText: best.callToActionText,
		RouteName:        best.routeName,
		RouteArgument:    best.routeArgument,
	}
}

// lastNewsHandler is WelcomePageController.getLastNews.
func lastNewsHandler(a *app.App) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		list, err := newsList(c.Context(), a)
		if err != nil {
			panic(err)
		}
		last := lastNews(list, localDateTimeOf(a.Now()))
		if last == nil {
			return httpx.Empty(404) // ResponseEntity.notFound().build()
		}
		return httpx.OK(*last)
	}
}
