package qag

import (
	"context"

	"agora/internal/javacompat"
	"agora/internal/strapi"
)

// Exported helpers added for the S3 slice (QaG lists and responses): the list
// variants of ResponseQagRepositoryImpl / ResponseQagStrapiRepository. They reuse
// the Strapi DTOs and the mapper of the single-response repository above.

// GetResponsesQag is ResponseQagRepositoryImpl.getResponsesQag(qagIds): the
// Government responses of the QaGs (ids that are not UUIDs are dropped). The
// Strapi request filters on the canonical UUID strings; with no UUID the request
// builder fails and nothing is requested (an empty list, like Kotlin).
// The mapper's exceptions (a response without content) are panics (HTTP 500).
func (r *ResponseRepository) GetResponsesQag(ctx context.Context, qagIDs []string) []ResponseQag {
	var uuids []string
	for _, id := range qagIDs {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			uuids = append(uuids, u)
		}
	}
	b := strapi.NewRequest("reponse-du-gouvernements").FilterIn("questionId", uuids).Populate(responsePopulate)
	env := strapi.Collection[*strapiResponseQag](ctx, r.a.Strapi, b)
	return responseQagMapper(env.Data, r.a.Now())
}

// GetAllResponsesQag is ResponseQagRepositoryImpl's
// `strapiRepository.getResponsesQag().let(mapper::toDomain)`: every Government
// response, sorted by Strapi on reponseDate desc (the page size is Strapi's cap of 100).
func (r *ResponseRepository) GetAllResponsesQag(ctx context.Context) []ResponseQag {
	b := strapi.NewRequest("reponse-du-gouvernements").SortBy("reponseDate", "desc").Populate(responsePopulate)
	env := strapi.Collection[*strapiResponseQag](ctx, r.a.Strapi, b)
	return responseQagMapper(env.Data, r.a.Now())
}

// GetResponsesTotal is ResponseQagStrapiRepository.getResponsesCount: the
// `meta.pagination.total` of the plain collection request (0 when Strapi fails).
func (r *ResponseRepository) GetResponsesTotal(ctx context.Context) int {
	b := strapi.NewRequest("reponse-du-gouvernements")
	env := strapi.Collection[*strapiResponseQag](ctx, r.a.Strapi, b)
	return env.Meta.Pagination.Total
}

// QagID is ResponseQag.qagId (either variant).
func (r ResponseQag) QagID() string {
	if r.Video != nil {
		return r.Video.QagID
	}
	if r.Text != nil {
		return r.Text.QagID
	}
	return ""
}

// ResponseDateMillis is ResponseQag.responseDate as java.util.Date milliseconds.
func (r ResponseQag) ResponseDateMillis() int64 {
	if r.Video != nil {
		return r.Video.ResponseDate.UnixMilli()
	}
	if r.Text != nil {
		return r.Text.ResponseDate.UnixMilli()
	}
	return 0
}
