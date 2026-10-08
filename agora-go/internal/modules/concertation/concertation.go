// Package concertation ports GET /concertations: ConcertationController,
// GetConcertationsUseCase, ConcertationRepository, ConcertationStrapiRepository,
// ConcertationMapper and the JSON mapper.
//
// Kotlin asks Strapi at every request (a thematique list that is cached for an hour,
// and the concertations); the response is public for 5 minutes. Go shares the sorted
// list between requests for AGORA_MICROCACHE_TTL (S5-B1, parity/divergences/S5.md).
package concertation

import (
	"context"
	"errors"
	"sort"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/httpx"
	"agora/internal/modules/consultation"
	"agora/internal/modules/thematique"
	"agora/internal/strapi"
)

const cacheName = "concertations"

// Thematique is domain.Thematique.
type Thematique = thematique.Thematique

// Concertation is domain.Concertation.
type Concertation struct {
	ID           string
	Title        string
	ImageURL     string
	ExternalLink string
	Thematique   Thematique
	UpdateLabel  *string
	UpdateDate   consultation.LocalDateTime
	Territory    string // "toutes les concertations sont nationales"
}

// nationalTerritory is Territoire.Pays.FRANCE.value.
const nationalTerritory = "France"

// strapiThematique is ThematiqueStrapiDTO (a missing/null field makes the whole
// Strapi list undecodable, i.e. empty, like in Kotlin).
type strapiThematique struct {
	DocumentID  string `json:"documentId"`
	Label       string `json:"label"`
	Pictogramme string `json:"pictogramme"`
}

// strapiConcertation is ConcertationStrapiDTO (@JsonIgnoreProperties createdAt, updatedAt).
type strapiConcertation struct {
	DocumentID           string                     `json:"documentId"`
	Titre                string                     `json:"titre"`
	URLExterne           string                     `json:"url"`
	URLImageDeCouverture string                     `json:"image_url"`
	DateDePublication    consultation.LocalDateTime `json:"datetime_publication"`
	Thematique           strapiThematique           `json:"thematique"`
	FlammeLabel          *string                    `json:"flamme_label"`
	Image                *strapi.MediaPicture       `json:"image"`
}

// getURLImageCouverture is getUrlImageCouverture: image?.mediaUrl() ?: urlImageDeCouverture.
func (c *strapiConcertation) getURLImageCouverture() string {
	if c.Image != nil {
		return c.Image.MediaURL()
	}
	return c.URLImageDeCouverture
}

// toConcertations is ConcertationMapper.toConcertations: the concertations whose
// thematique is unknown are dropped. A JSON null element of the Strapi list reaches the
// lambda as a null (type erasure) and fails on its first access: an exception (HTTP 500).
func toConcertations(data []*strapiConcertation, thematiques []Thematique) []Concertation {
	out := make([]Concertation, 0, len(data))
	for _, c := range data {
		if c == nil {
			panic("NullPointerException: ConcertationMapper.toConcertations: null element")
		}
		var found *Thematique
		for i := range thematiques {
			if thematiques[i].ID == c.Thematique.DocumentID {
				found = &thematiques[i]
				break
			}
		}
		if found == nil {
			continue
		}
		out = append(out, Concertation{
			ID:           c.DocumentID,
			Title:        c.Titre,
			ImageURL:     c.getURLImageCouverture(),
			ExternalLink: c.URLExterne,
			Thematique:   *found,
			UpdateLabel:  c.FlammeLabel,
			UpdateDate:   c.DateDePublication,
			Territory:    nationalTerritory,
		})
	}
	return out
}

// sortByUpdateDateDescending is GetConcertationsUseCase's sortedByDescending { it.updateDate }
// (stable: equal dates keep the Strapi order).
func sortByUpdateDateDescending(list []Concertation) {
	sort.SliceStable(list, func(i, j int) bool { return list[j].UpdateDate.Before(list[i].UpdateDate) })
}

// thematiqueLister is ThematiqueRepository.getThematiqueList.
type thematiqueLister interface {
	List(ctx context.Context) []Thematique
}

// Service is GetConcertationsUseCase and what it uses.
type Service struct {
	a *app.App

	themes thematiqueLister
	// fetch is ConcertationStrapiRepository.getAll (errors and unreadable payloads: empty).
	fetch func(ctx context.Context) []*strapiConcertation
}

// Get returns the App-wide service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "concertation", func() *Service {
		return &Service{
			a:      a,
			themes: thematique.Get(a),
			fetch: func(ctx context.Context) []*strapiConcertation {
				return strapi.Collection[*strapiConcertation](ctx, a.Strapi, strapi.NewRequest("concertations")).Data
			},
		}
	})
}

// getAll is ConcertationRepository.getAll then GetConcertationsUseCase.execute: the thematiques
// are read first, then the concertations; the result is sorted by update date, latest first.
func (s *Service) getAll(ctx context.Context) []Concertation {
	thematiques := s.themes.List(ctx)
	list := toConcertations(s.fetch(ctx), thematiques)
	sortByUpdateDateDescending(list)
	return list
}

// Execute is GetConcertationsUseCase.execute. The list is shared between requests for
// AGORA_MICROCACHE_TTL (never an empty list: it may come from a Strapi or thematique
// failure) and must not be modified.
func (s *Service) Execute(ctx context.Context) []Concertation {
	list, err := cache.GetOrLoad(s.a.Cache, cacheName, "all", s.a.Cfg.MicroCacheTTL, func() ([]Concertation, error) {
		// the shared load must not die with the request that started it
		list := s.getAll(context.WithoutCancel(ctx))
		if len(list) == 0 {
			return list, errEmpty
		}
		return list, nil
	})
	if errors.Is(err, errEmpty) {
		return []Concertation{}
	}
	return list
}

var errEmpty = errors.New("empty list is not cached")

// getConcertations is ConcertationController.getConcertations.
func (s *Service) getConcertations(c *httpx.Ctx) *httpx.Response {
	return httpx.OK(toJSON(s.Execute(c.Context()))).CacheControl(5*60, true)
}
