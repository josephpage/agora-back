package responseqag

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
	"time"

	"agora/internal/javacompat"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
)

// Collaborators (the Kotlin constructor parameters). *qag.InfoRepository,
// *qag.LowPriorityRepository and thematique.Service implement the first three.

type qagInfoReader interface {
	GetQagsSelectedForResponse(ctx context.Context) ([]qag.QagInfoWithSupportCount, error)
	GetQagsInfo(ctx context.Context, ids []string) ([]qag.QagInfo, error)
}

type lowPriorityReader interface {
	GetLowPriorityQagIDs(ctx context.Context, qagIDs []string) ([]string, error)
}

type thematiqueReader interface {
	ByID(ctx context.Context, id string) *thematique.Thematique
}

// responseRepository is ResponseQagRepository. minDate is java.util.Date milliseconds.
type responseRepository interface {
	// GetResponsesQag is getResponsesQag(qagIds).
	GetResponsesQag(ctx context.Context, qagIDs []string) []qag.ResponseQag
	// GetResponsesQagCount is getResponsesQagCount(minDate).
	GetResponsesQagCount(ctx context.Context, minDate *int64) int
	// GetResponsesQagPage is getResponsesQag(from, pageSize, minDate).
	GetResponsesQagPage(ctx context.Context, from, pageSize int, minDate *int64) []qag.ResponseQag
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewListMapper
// ---------------------------------------------------------------------------

const maxResponseTextLength = 400

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	javaSpacesRe = regexp.MustCompile(`[ \t\n\x0B\f\r]+`)
)

// sanitizeResponseText is ResponseQagPreviewListMapper.sanitizeResponseText: the HTML tags become
// spaces, the (ASCII) white space runs one space, then trim() and the first 400 UTF-16 characters
// followed by "..." when longer.
func sanitizeResponseText(html string) string {
	plain := htmlTagRe.ReplaceAllString(html, " ")
	plain = javaSpacesRe.ReplaceAllString(plain, " ")
	plain = javacompat.KotlinTrim(plain)
	if javacompat.Len16(plain) > maxResponseTextLength {
		return javacompat.Take16(plain, maxResponseTextLength) + "..."
	}
	return plain
}

// toResponseQagPreviewWithoutOrder is ResponseQagPreviewListMapper.toResponseQagPreviewWithoutOrder.
func toResponseQagPreviewWithoutOrder(info qag.QagInfo, response qag.ResponseQag, th thematique.Thematique) ResponseQagPreviewWithoutOrder {
	out := ResponseQagPreviewWithoutOrder{QagID: info.ID, Thematique: th, Title: info.Title, Username: info.Username}
	var text string
	switch {
	case response.Text != nil:
		r := response.Text
		out.Author, out.AuthorPortraitURL, out.AuthorFunction, out.ResponseDate = r.Author, r.AuthorPortraitURL, r.AuthorFunction, r.ResponseDate
		text = r.ResponseText
	case response.Video != nil:
		r := response.Video
		out.Author, out.AuthorPortraitURL, out.AuthorFunction, out.ResponseDate = r.Author, r.AuthorPortraitURL, r.AuthorFunction, r.ResponseDate
		text = r.Transcription
	}
	s := sanitizeResponseText(text)
	out.ResponseText = &s
	return out
}

// toIncomingResponsePreview is ResponseQagPreviewListMapper.toIncomingResponsePreview: the Monday
// of the week the QaG was posted (itself when it is a Monday) and the next Monday, in the process zone.
func toIncomingResponsePreview(q qag.QagInfoWithSupportCount, order int, th thematique.Thematique) IncomingResponsePreview {
	d := q.Date.In(time.Local)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
	back := (int(day.Weekday()) + 6) % 7 // days since the previous Monday (0 on a Monday)
	ahead := 7 - back                    // next(MONDAY) is strictly after
	return IncomingResponsePreview{
		ID:                 q.ID,
		Thematique:         th,
		Title:              q.Title,
		SupportCount:       q.SupportCount,
		DateLundiPrecedent: day.AddDate(0, 0, -back),
		DateLundiSuivant:   day.AddDate(0, 0, ahead),
		Order:              order,
	}
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewOrderMapper
// ---------------------------------------------------------------------------

type qagWithResponse struct {
	qag      qag.QagInfoWithSupportCount
	response qag.ResponseQag
}

type orderedQag struct {
	qag   qag.QagInfoWithSupportCount
	order int
}

type orderedQagWithResponse struct {
	qagWithResponse
	order int
}

type orderResult struct {
	incoming  []orderedQag
	responses []orderedQagWithResponse
}

// buildOrderResult is ResponseQagPreviewOrderMapper.buildOrderResult: the QaGs without response
// then the ones with a response, stably sorted by low priority (false first) then by date descending
// (the post date of a QaG without response, the response date otherwise); the order of an item is its
// index in that list.
func buildOrderResult(lowPriorityIDs []string, incoming []qag.QagInfoWithSupportCount, responses []qagWithResponse) orderResult {
	low := make(map[string]bool, len(lowPriorityIDs))
	for _, id := range lowPriorityIDs {
		low[id] = true
	}
	type item struct {
		date     int64
		low      bool
		incoming *qag.QagInfoWithSupportCount
		response *qagWithResponse
	}
	items := make([]item, 0, len(incoming)+len(responses))
	for i := range incoming {
		items = append(items, item{date: incoming[i].Date.UnixMilli(), low: low[incoming[i].ID], incoming: &incoming[i]})
	}
	for i := range responses {
		items = append(items, item{date: responses[i].response.ResponseDateMillis(), low: low[responses[i].qag.ID], response: &responses[i]})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].low != items[j].low {
			return !items[i].low
		}
		return items[i].date > items[j].date
	})
	var out orderResult
	for index, it := range items {
		if it.incoming != nil {
			out.incoming = append(out.incoming, orderedQag{qag: *it.incoming, order: index})
		} else {
			out.responses = append(out.responses, orderedQagWithResponse{qagWithResponse: *it.response, order: index})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewListUseCase
// ---------------------------------------------------------------------------

// PreviewListUseCase is ResponseQagPreviewListUseCase.
type PreviewListUseCase struct {
	qags        qagInfoReader
	responses   responseRepository
	themes      thematiqueReader
	lowPriority lowPriorityReader
}

// GetResponseQagPreviewList is getResponseQagPreviewList(minDate): the QaGs selected for a response,
// those with their response (the five most recent) and those still waiting for it. A response older
// than minDate is dropped before the split, so its QaG is listed as waiting.
func (u *PreviewListUseCase) GetResponseQagPreviewList(ctx context.Context, minDate *int64) (ResponseQagPreviewList, error) {
	selected, err := u.qags.GetQagsSelectedForResponse(ctx)
	if err != nil {
		return ResponseQagPreviewList{}, err
	}
	ids := make([]string, len(selected))
	for i, q := range selected {
		ids[i] = q.ID
	}
	all := u.responses.GetResponsesQag(ctx, ids)
	var responses []qag.ResponseQag
	for _, r := range all {
		if minDate == nil || r.ResponseDateMillis() >= *minDate {
			responses = append(responses, r)
		}
	}

	var withResponse []qagWithResponse
	var without []qag.QagInfoWithSupportCount
	for _, q := range selected {
		var found *qag.ResponseQag
		for i := range responses {
			if responses[i].QagID() == q.ID {
				found = &responses[i]
				break
			}
		}
		if found != nil {
			withResponse = append(withResponse, qagWithResponse{qag: q, response: *found})
		} else {
			without = append(without, q)
		}
	}
	if len(selected) == 0 {
		return ResponseQagPreviewList{IncomingResponses: []IncomingResponsePreview{}, Responses: []ResponseQagPreview{}}, nil
	}
	sort.SliceStable(withResponse, func(i, j int) bool {
		return withResponse[i].response.ResponseDateMillis() > withResponse[j].response.ResponseDateMillis()
	})
	if len(withResponse) > 5 {
		withResponse = withResponse[:5]
	}

	lowPriority, err := u.lowPriority.GetLowPriorityQagIDs(ctx, ids)
	if err != nil {
		return ResponseQagPreviewList{}, err
	}
	ordered := buildOrderResult(lowPriority, without, withResponse)

	out := ResponseQagPreviewList{IncomingResponses: []IncomingResponsePreview{}, Responses: []ResponseQagPreview{}}
	for _, o := range ordered.incoming {
		if th := u.themes.ByID(ctx, o.qag.ThematiqueID); th != nil {
			out.IncomingResponses = append(out.IncomingResponses, toIncomingResponsePreview(o.qag, o.order, *th))
		}
	}
	for _, o := range ordered.responses {
		if th := u.themes.ByID(ctx, o.qag.ThematiqueID); th != nil {
			out.Responses = append(out.Responses, toResponseQagPreview(o, *th))
		}
	}
	return out, nil
}

// toResponseQagPreview is ResponseQagPreviewListMapper.toResponseQagPreview.
func toResponseQagPreview(o orderedQagWithResponse, th thematique.Thematique) ResponseQagPreview {
	p := ResponseQagPreview{QagID: o.qag.ID, Thematique: th, Title: o.qag.Title, Order: o.order}
	switch {
	case o.response.Text != nil:
		p.Author, p.AuthorPortraitURL, p.ResponseDate = o.response.Text.Author, o.response.Text.AuthorPortraitURL, o.response.Text.ResponseDate
	case o.response.Video != nil:
		p.Author, p.AuthorPortraitURL, p.ResponseDate = o.response.Video.Author, o.response.Video.AuthorPortraitURL, o.response.Video.ResponseDate
	}
	return p
}

// ---------------------------------------------------------------------------
// GetResponseQagPreviewPaginatedListUseCase
// ---------------------------------------------------------------------------

const responsePageSize = 5

// PaginatedListUseCase is GetResponseQagPreviewPaginatedListUseCase.
type PaginatedListUseCase struct {
	responses responseRepository
	qags      qagInfoReader
	themes    thematiqueReader
	log       *slog.Logger
}

// GetResponseQagPreviewPaginatedList is getResponseQagPreviewPaginatedList(pageNumber, minDate); nil is
// Kotlin's null (HTTP 200 with an empty body). The offset is Int arithmetic: a huge page number wraps
// around, and a negative offset fails (IndexOutOfBoundsException, HTTP 500).
func (u *PaginatedListUseCase) GetResponseQagPreviewPaginatedList(ctx context.Context, pageNumber int, minDate *int64) (*ResponseQagPaginatedList, error) {
	if pageNumber <= 0 {
		return nil, nil
	}
	count := u.responses.GetResponsesQagCount(ctx, minDate)
	offset := int(int32(pageNumber-1) * responsePageSize)
	if offset > count {
		return nil, nil
	}
	page := u.responses.GetResponsesQagPage(ctx, offset, responsePageSize, minDate)
	previews, err := u.toResponseQagPreview(ctx, page)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(previews, func(i, j int) bool {
		return previews[i].ResponseDate.UnixMilli() > previews[j].ResponseDate.UnixMilli()
	})
	return &ResponseQagPaginatedList{ResponsesQag: previews, MaxPageNumber: (count + responsePageSize - 1) / responsePageSize}, nil
}

func (u *PaginatedListUseCase) toResponseQagPreview(ctx context.Context, responses []qag.ResponseQag) ([]ResponseQagPreviewWithoutOrder, error) {
	ids := make([]string, len(responses))
	for i, r := range responses {
		ids[i] = r.QagID()
	}
	infos, err := u.qags.GetQagsInfo(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*qag.QagInfo, len(infos))
	for i := range infos {
		if _, dup := byID[infos[i].ID]; !dup {
			byID[infos[i].ID] = &infos[i]
		}
	}
	out := make([]ResponseQagPreviewWithoutOrder, 0, len(responses))
	for _, r := range responses {
		info := byID[r.QagID()]
		if info == nil {
			u.log.Error("toResponseQagPreview - la réponse à la question '" + r.QagID() + "' n'a pas été trouvée")
			continue
		}
		th := u.themes.ByID(ctx, info.ThematiqueID)
		if th == nil {
			u.log.Error("toResponseQagPreview - la thématique '" + info.ThematiqueID + "' n'a pas été trouvée pour la QaG " + info.ID)
			continue
		}
		out = append(out, toResponseQagPreviewWithoutOrder(*info, r, *th))
	}
	return out, nil
}
