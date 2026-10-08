package responseqag

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

var th = thematique.Thematique{ID: "thematiqueId", Label: "Santé", Picto: "🏥"}

func textResponse(qagID string, date time.Time, text string) qag.ResponseQag {
	return qag.ResponseQag{Text: &qag.ResponseQagText{
		Author: "author", AuthorPortraitURL: "portraitUrl", ResponseDate: date, FeedbackQuestion: "feedbackQuestion",
		QagID: qagID, ResponseLabel: "label", ResponseText: text,
	}}
}

func videoResponse(qagID string, date time.Time, transcription string) qag.ResponseQag {
	return qag.ResponseQag{Video: &qag.ResponseQagVideo{
		Author: "author", AuthorPortraitURL: "portraitUrl", ResponseDate: date, FeedbackQuestion: "feedbackQuestion",
		QagID: qagID, AuthorDescription: "description", VideoURL: "videoUrl", VideoTitle: "videoTitle",
		VideoWidth: 1280, VideoHeight: 720, Transcription: transcription,
	}}
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewListMapperTest
// ---------------------------------------------------------------------------

var qagInfo = qag.QagInfo{
	ID: "qagId", ThematiqueID: "thematiqueId", Title: "title", Description: "description", Date: time.UnixMilli(0),
	Status: qag.StatusSelectedForResponse, Username: "username", UserID: "userId",
}

func TestToResponseQagPreviewWithoutOrder(t *testing.T) {
	date := time.UnixMilli(1000)
	fn := "Ministre de l'économie"
	t.Run("text keeps the function and the text", func(t *testing.T) {
		r := textResponse("qagId", date, "Le texte de la réponse")
		r.Text.AuthorFunction = &fn
		got := toResponseQagPreviewWithoutOrder(qagInfo, r, th)
		want := ResponseQagPreviewWithoutOrder{
			QagID: "qagId", Thematique: th, Title: "title", Author: "author", AuthorPortraitURL: "portraitUrl",
			AuthorFunction: &fn, ResponseDate: date, ResponseText: ptr("Le texte de la réponse"), Username: "username",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})
	long := func(c string, n int) string { return strings.Repeat(c, n) }
	for _, tc := range []struct {
		name, text, want string
	}{
		{"html tags are stripped", "<p>Le texte de la <strong>réponse</strong></p>", "Le texte de la réponse"},
		{"paragraphs are separated by a space", "<p>premier paragraphe</p><p>deuxième paragraphe</p>", "premier paragraphe deuxième paragraphe"},
		{"shorter than 400 characters", long("a", 100), long("a", 100)},
		{"exactly 400 characters", long("a", 400), long("a", 400)},
		{"longer than 400 characters", long("a", 450), long("a", 400) + "..."},
		{"html then truncate", "<p>" + long("b", 450) + "</p>", long("b", 400) + "..."},
		{"white space runs", "a \t\n\r\f\v  b", "a b"},
		{"unicode spaces are only trimmed at the ends", " a  b ", "a  b"},
		{"a tag spanning lines", "x<a\nhref='y'>z</a>w", "x z w"},
		{"empty", "", ""},
	} {
		t.Run("text: "+tc.name, func(t *testing.T) {
			got := toResponseQagPreviewWithoutOrder(qagInfo, textResponse("qagId", date, tc.text), th)
			if got.ResponseText == nil || *got.ResponseText != tc.want {
				t.Fatalf("got %q want %q", *got.ResponseText, tc.want)
			}
		})
	}
	t.Run("video uses the transcription", func(t *testing.T) {
		got := toResponseQagPreviewWithoutOrder(qagInfo, videoResponse("qagId", date, "La transcription de la vidéo"), th)
		if *got.ResponseText != "La transcription de la vidéo" {
			t.Fatalf("got %q", *got.ResponseText)
		}
	})
	t.Run("video transcription longer than 400 characters", func(t *testing.T) {
		got := toResponseQagPreviewWithoutOrder(qagInfo, videoResponse("qagId", date, long("c", 450)), th)
		if *got.ResponseText != long("c", 400)+"..." {
			t.Fatalf("got %q", *got.ResponseText)
		}
	})
	t.Run("the length is counted in UTF-16 units", func(t *testing.T) {
		// 200 emojis are 400 units: kept; 201 are cut after 200 emojis
		got := sanitizeResponseText(long("😀", 200))
		if got != long("😀", 200) {
			t.Fatal("200 emojis (400 units) must be kept")
		}
		got = sanitizeResponseText(long("😀", 201))
		if got != long("😀", 200)+"..." {
			t.Fatalf("got %q", got)
		}
	})
}

func TestToIncomingResponsePreview(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }
	for _, tc := range []struct {
		name       string
		date       time.Time
		prev, next time.Time
	}{
		{"a thursday: the previous and the next Monday", day(2024, 6, 6), day(2024, 6, 3), day(2024, 6, 10)},
		{"a monday: itself and the next Monday", day(2024, 6, 10), day(2024, 6, 10), day(2024, 6, 17)},
		{"a sunday", time.Date(2024, 6, 9, 23, 59, 59, 0, time.Local), day(2024, 6, 3), day(2024, 6, 10)},
		{"a tuesday around a month end", day(2024, 4, 30), day(2024, 4, 29), day(2024, 5, 6)},
		{"over a year end", day(2024, 12, 31), day(2024, 12, 30), day(2025, 1, 6)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := qag.QagInfoWithSupportCount{ID: "id", ThematiqueID: "thematiqueId", Title: "title", Date: tc.date, Status: qag.StatusSelectedForResponse}
			got := toIncomingResponsePreview(q, 0, th)
			want := IncomingResponsePreview{ID: "id", Thematique: th, Title: "title", DateLundiPrecedent: tc.prev, DateLundiSuivant: tc.next}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v want %+v", got, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewOrderMapperTest
// ---------------------------------------------------------------------------

type orderInput struct {
	qagID    string
	date     time.Time
	expected int
}

func jan(d int) time.Time { return time.Date(2024, 1, d, 0, 0, 0, 0, time.Local) }

func TestBuildOrderResult(t *testing.T) {
	for _, tc := range []struct {
		name      string
		low       []string
		incoming  []orderInput
		responses []orderInput
	}{
		{"one incoming response", nil, []orderInput{{"qagId", jan(1), 0}}, nil},
		{"incoming responses by date desc", nil, []orderInput{{"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}, nil},
		{"one response", nil, nil, []orderInput{{"qagId", jan(1), 0}}},
		{"responses by date desc", nil, nil, []orderInput{{"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}},
		{"only low priority, one incoming response", []string{"qagId"}, []orderInput{{"qagId", jan(1), 0}}, nil},
		{"only low priority, incoming responses", []string{"qagId0", "qagId1"}, []orderInput{{"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}, nil},
		{"only low priority, one response", []string{"qagId"}, nil, []orderInput{{"qagId", jan(1), 0}}},
		{"only low priority, responses", []string{"qagId0", "qagId1"}, nil, []orderInput{{"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}},
		{"an incoming response and a response by date desc", nil, []orderInput{{"qagId1", jan(1), 1}}, []orderInput{{"qagId0", jan(2), 0}}},
		{"some low priority, incoming responses", []string{"qagId2"},
			[]orderInput{{"qagId2", jan(10), 2}, {"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}, nil},
		{"some low priority, responses", []string{"qagId2"}, nil,
			[]orderInput{{"qagId2", jan(10), 2}, {"qagId1", jan(1), 1}, {"qagId0", jan(2), 0}}},
		{"some low priority, incoming responses and responses", []string{"qagId2", "qagId3"},
			[]orderInput{{"qagId2", jan(10), 2}, {"qagId1", jan(1), 1}},
			[]orderInput{{"qagId0", jan(2), 0}, {"qagId3", jan(2), 3}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var incoming []qag.QagInfoWithSupportCount
			for _, in := range tc.incoming {
				incoming = append(incoming, qag.QagInfoWithSupportCount{ID: in.qagID, Date: in.date})
			}
			var responses []qagWithResponse
			for _, in := range tc.responses {
				responses = append(responses, qagWithResponse{qag: qag.QagInfoWithSupportCount{ID: in.qagID}, response: videoResponse(in.qagID, in.date, "")})
			}
			got := buildOrderResult(tc.low, incoming, responses)
			orders := map[string]int{}
			for _, o := range got.incoming {
				orders["i:"+o.qag.ID] = o.order
			}
			for _, o := range got.responses {
				orders["r:"+o.qag.ID] = o.order
			}
			if len(got.incoming) != len(tc.incoming) || len(got.responses) != len(tc.responses) {
				t.Fatalf("got %d incoming / %d responses", len(got.incoming), len(got.responses))
			}
			for _, in := range tc.incoming {
				if orders["i:"+in.qagID] != in.expected {
					t.Errorf("incoming %s: order %d want %d", in.qagID, orders["i:"+in.qagID], in.expected)
				}
			}
			for _, in := range tc.responses {
				if orders["r:"+in.qagID] != in.expected {
					t.Errorf("response %s: order %d want %d", in.qagID, orders["r:"+in.qagID], in.expected)
				}
			}
			// each list keeps the order of the sort
			for i := 1; i < len(got.incoming); i++ {
				if got.incoming[i-1].order > got.incoming[i].order {
					t.Error("incoming responses are not in order")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ResponseQagPreviewListUseCaseTest
// ---------------------------------------------------------------------------

type fakeQags struct {
	selected   []qag.QagInfoWithSupportCount
	infos      []qag.QagInfo
	infosCalls [][]string
}

func (f *fakeQags) GetQagsSelectedForResponse(context.Context) ([]qag.QagInfoWithSupportCount, error) {
	return f.selected, nil
}

func (f *fakeQags) GetQagsInfo(_ context.Context, ids []string) ([]qag.QagInfo, error) {
	f.infosCalls = append(f.infosCalls, ids)
	return f.infos, nil
}

type fakeLow struct {
	ids   []string
	calls [][]string
}

func (f *fakeLow) GetLowPriorityQagIDs(_ context.Context, ids []string) ([]string, error) {
	f.calls = append(f.calls, ids)
	return f.ids, nil
}

type fakeThemes map[string]thematique.Thematique

func (f fakeThemes) ByID(_ context.Context, id string) *thematique.Thematique {
	if t, ok := f[id]; ok {
		return &t
	}
	return nil
}

type fakeResponses struct {
	byIDs      []qag.ResponseQag
	byIDsCalls [][]string
	count      int
	countCalls []*int64
	page       []qag.ResponseQag
	pageCalls  [][3]int64
}

func (f *fakeResponses) GetResponsesQag(_ context.Context, ids []string) []qag.ResponseQag {
	f.byIDsCalls = append(f.byIDsCalls, ids)
	return f.byIDs
}

func (f *fakeResponses) GetResponsesQagCount(_ context.Context, minDate *int64) int {
	f.countCalls = append(f.countCalls, minDate)
	return f.count
}

func (f *fakeResponses) GetResponsesQagPage(_ context.Context, from, pageSize int, minDate *int64) []qag.ResponseQag {
	var md int64 = -1
	if minDate != nil {
		md = *minDate
	}
	f.pageCalls = append(f.pageCalls, [3]int64{int64(from), int64(pageSize), md})
	return f.page
}

func TestPreviewListMinDateFiltering(t *testing.T) {
	selected := []qag.QagInfoWithSupportCount{{ID: "qagId", ThematiqueID: "thematiqueId", Title: "title", Date: time.UnixMilli(0), Status: qag.StatusSelectedForResponse}}
	for _, tc := range []struct {
		name         string
		minDate      *int64
		responseDate int64
		wantResponse bool
	}{
		{"no minDate: no filtering", nil, 500, true},
		{"response before minDate is dropped", ptr(int64(1000)), 500, false},
		{"response after minDate is kept", ptr(int64(1000)), 2000, true},
		{"response exactly on minDate is kept", ptr(int64(1000)), 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &fakeResponses{byIDs: []qag.ResponseQag{textResponse("qagId", time.UnixMilli(tc.responseDate), "x")}}
			uc := &PreviewListUseCase{qags: &fakeQags{selected: selected}, responses: resp, themes: fakeThemes{"thematiqueId": th}, lowPriority: &fakeLow{}}
			got, err := uc.GetResponseQagPreviewList(ctx, tc.minDate)
			if err != nil {
				t.Fatal(err)
			}
			if (len(got.Responses) == 1) != tc.wantResponse || (len(got.IncomingResponses) == 1) == tc.wantResponse {
				t.Fatalf("responses=%d incoming=%d, want a response: %v", len(got.Responses), len(got.IncomingResponses), tc.wantResponse)
			}
			if !reflect.DeepEqual(resp.byIDsCalls, [][]string{{"qagId"}}) {
				t.Fatalf("responses requested for %v", resp.byIDsCalls)
			}
		})
	}
	t.Run("no QaG selected for a response", func(t *testing.T) {
		low := &fakeLow{}
		uc := &PreviewListUseCase{qags: &fakeQags{}, responses: &fakeResponses{}, themes: fakeThemes{}, lowPriority: low}
		got, err := uc.GetResponseQagPreviewList(ctx, ptr(int64(1000)))
		if err != nil || len(got.Responses) != 0 || len(got.IncomingResponses) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if len(low.calls) != 0 {
			t.Fatal("the low priority QaGs are not read when nothing is selected")
		}
	})
}

func TestPreviewListKeepsFiveAndOrders(t *testing.T) {
	var selected []qag.QagInfoWithSupportCount
	var responses []qag.ResponseQag
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		selected = append(selected, qag.QagInfoWithSupportCount{ID: id, ThematiqueID: "thematiqueId", Title: id, Date: jan(i + 1)})
		if i < 7 { // the last one has no response
			responses = append(responses, textResponse(id, jan(10+i), "x"))
		}
	}
	uc := &PreviewListUseCase{qags: &fakeQags{selected: selected}, responses: &fakeResponses{byIDs: responses}, themes: fakeThemes{"thematiqueId": th}, lowPriority: &fakeLow{ids: []string{"g"}}}
	got, err := uc.GetResponseQagPreviewList(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// five most recent responses: g f e d c, but g is low priority and goes last; the incoming one (h) is older than all the responses
	var ids []string
	for _, r := range got.Responses {
		ids = append(ids, r.QagID)
	}
	if !reflect.DeepEqual(ids, []string{"f", "e", "d", "c", "g"}) {
		t.Fatalf("responses %v", ids)
	}
	if len(got.IncomingResponses) != 1 || got.IncomingResponses[0].ID != "h" || got.IncomingResponses[0].Order != 4 {
		t.Fatalf("incoming %+v", got.IncomingResponses)
	}
}

func TestPreviewListSkipsUnknownThematiques(t *testing.T) {
	selected := []qag.QagInfoWithSupportCount{
		{ID: "a", ThematiqueID: "unknown", Date: jan(1)},
		{ID: "b", ThematiqueID: "thematiqueId", Date: jan(2)},
	}
	responses := []qag.ResponseQag{textResponse("a", jan(3), "x")}
	uc := &PreviewListUseCase{qags: &fakeQags{selected: selected}, responses: &fakeResponses{byIDs: responses}, themes: fakeThemes{"thematiqueId": th}, lowPriority: &fakeLow{}}
	got, _ := uc.GetResponseQagPreviewList(ctx, nil)
	if len(got.Responses) != 0 || len(got.IncomingResponses) != 1 || got.IncomingResponses[0].ID != "b" || got.IncomingResponses[0].Order != 1 {
		t.Fatalf("got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// GetResponseQagPreviewPaginatedListUseCaseTest
// ---------------------------------------------------------------------------

func newPaginated(resp *fakeResponses, qags *fakeQags, themes fakeThemes) *PaginatedListUseCase {
	return &PaginatedListUseCase{responses: resp, qags: qags, themes: themes, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestPaginatedPageNumberLowerOrEqualToZero(t *testing.T) {
	resp, qags := &fakeResponses{}, &fakeQags{}
	got, err := newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 0, nil)
	if got != nil || err != nil || len(resp.countCalls) != 0 || len(qags.infosCalls) != 0 {
		t.Fatalf("got %v %v, count calls %d", got, err, len(resp.countCalls))
	}
	got, _ = newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, -3, nil)
	if got != nil {
		t.Fatal("negative page number must give null")
	}
}

func TestPaginatedPageNumberHigherThanMax(t *testing.T) {
	resp, qags := &fakeResponses{count: 3}, &fakeQags{}
	got, _ := newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 2, nil)
	if got != nil || len(resp.pageCalls) != 0 || len(qags.infosCalls) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestPaginatedOffsetEqualToCountIsNotBeyond(t *testing.T) {
	resp, qags := &fakeResponses{count: 5}, &fakeQags{}
	got, _ := newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 2, nil)
	if got == nil || len(got.ResponsesQag) != 0 || got.MaxPageNumber != 1 {
		t.Fatalf("got %+v: the page starting at offset == count is empty but exists", got)
	}
}

func TestPaginatedResponsesWithoutQagOrThematique(t *testing.T) {
	resp := &fakeResponses{count: 1, page: []qag.ResponseQag{videoResponse("qagId", jan(1), "")}}
	qags := &fakeQags{}
	got, _ := newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 1, nil)
	if got == nil || len(got.ResponsesQag) != 0 || got.MaxPageNumber != 1 {
		t.Fatalf("response without QaG: %+v", got)
	}
	if !reflect.DeepEqual(qags.infosCalls, [][]string{{"qagId"}}) {
		t.Fatalf("infos %v", qags.infosCalls)
	}

	resp = &fakeResponses{count: 11, page: []qag.ResponseQag{textResponse("qagId", jan(1), "")}}
	qags = &fakeQags{infos: []qag.QagInfo{{ID: "qagId", ThematiqueID: "thematiqueId"}}}
	got, _ = newPaginated(resp, qags, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 2, nil)
	if got == nil || len(got.ResponsesQag) != 0 || got.MaxPageNumber != 3 {
		t.Fatalf("response without thematique: %+v", got)
	}
	if !reflect.DeepEqual(resp.pageCalls, [][3]int64{{5, 5, -1}}) {
		t.Fatalf("page calls %v", resp.pageCalls)
	}
}

func TestPaginatedMapsAndSortsByDateDesc(t *testing.T) {
	resp := &fakeResponses{count: 11, page: []qag.ResponseQag{
		textResponse("a", jan(1), "<p>un</p>"), videoResponse("b", jan(5), "deux"), textResponse("c", jan(3), "trois"),
	}}
	qags := &fakeQags{infos: []qag.QagInfo{
		{ID: "a", ThematiqueID: "thematiqueId", Title: "A", Username: "ua"},
		{ID: "b", ThematiqueID: "thematiqueId", Title: "B", Username: "ub"},
		{ID: "c", ThematiqueID: "thematiqueId", Title: "C", Username: "uc"},
	}}
	got, _ := newPaginated(resp, qags, fakeThemes{"thematiqueId": th}).GetResponseQagPreviewPaginatedList(ctx, 3, nil)
	if got == nil || got.MaxPageNumber != 3 {
		t.Fatalf("got %+v", got)
	}
	var order []string
	for _, r := range got.ResponsesQag {
		order = append(order, r.QagID+":"+*r.ResponseText+":"+r.Username)
	}
	if !reflect.DeepEqual(order, []string{"b:deux:ub", "c:trois:uc", "a:un:ua"}) {
		t.Fatalf("order %v", order)
	}
	if !reflect.DeepEqual(resp.pageCalls, [][3]int64{{10, 5, -1}}) {
		t.Fatalf("page calls %v", resp.pageCalls)
	}
}

func TestPaginatedWithMinDate(t *testing.T) {
	minDate := ptr(int64(1_000_000))
	resp := &fakeResponses{count: 1, page: []qag.ResponseQag{videoResponse("qagId", jan(1), "")}}
	got, _ := newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 1, minDate)
	if got == nil || len(got.ResponsesQag) != 0 || got.MaxPageNumber != 1 {
		t.Fatalf("got %+v", got)
	}
	if len(resp.countCalls) != 1 || *resp.countCalls[0] != 1_000_000 || !reflect.DeepEqual(resp.pageCalls, [][3]int64{{0, 5, 1_000_000}}) {
		t.Fatalf("minDate not passed on: %v %v", resp.countCalls, resp.pageCalls)
	}
	// 6 responses after the filter: two pages
	resp = &fakeResponses{count: 6}
	got, _ = newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 1, minDate)
	if got == nil || got.MaxPageNumber != 2 {
		t.Fatalf("got %+v", got)
	}
	resp = &fakeResponses{count: 3}
	if got, _ = newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 2, minDate); got != nil {
		t.Fatal("beyond the filtered count must give null")
	}
}

func TestPaginatedOffsetOverflowsLikeKotlinInt(t *testing.T) {
	// (858993461 - 1) * 5 = 4294967300 wraps to 4: still inside a list of 10 responses
	resp := &fakeResponses{count: 10}
	got, _ := newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 858993461, nil)
	if got == nil || !reflect.DeepEqual(resp.pageCalls, [][3]int64{{4, 5, -1}}) {
		t.Fatalf("got %v, calls %v", got, resp.pageCalls)
	}
	// 858993460: 4294967295 wraps to -1 (a negative offset is not beyond the end)
	resp = &fakeResponses{count: 10}
	got, _ = newPaginated(resp, &fakeQags{}, fakeThemes{}).GetResponseQagPreviewPaginatedList(ctx, 858993460, nil)
	if got == nil || !reflect.DeepEqual(resp.pageCalls, [][3]int64{{-1, 5, -1}}) {
		t.Fatalf("got %v, calls %v", got, resp.pageCalls)
	}
}

// ---------------------------------------------------------------------------
// ResponseQagPaginatedJsonMapperTest
// ---------------------------------------------------------------------------

func TestPaginatedJSON(t *testing.T) {
	date := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	l := ResponseQagPaginatedList{MaxPageNumber: 5, ResponsesQag: []ResponseQagPreviewWithoutOrder{
		{QagID: "qag-1", Thematique: th, Title: "Titre de la QAG", Author: "Auteur", AuthorPortraitURL: "https://portrait.url", ResponseDate: date, ResponseText: ptr("Voici la réponse"), Username: "utilisateur"},
		{QagID: "qag-2", Thematique: th, ResponseDate: date, ResponseText: nil, AuthorFunction: ptr("fn")},
	}}
	j := ToPaginatedJSON(l)
	if j.MaxPageNumber != 5 || len(j.Responses) != 2 {
		t.Fatalf("%+v", j)
	}
	first := j.Responses[0]
	if first.ResponseTexte != "Voici la réponse" || first.AuthorFunction != "" || first.ResponseDate != "2025-01-01 00:00:00" ||
		first.QagID != "qag-1" || first.Thematique.Label != "Santé" || first.Username != "utilisateur" {
		t.Fatalf("%+v", first)
	}
	if second := j.Responses[1]; second.ResponseTexte != "" || second.AuthorFunction != "fn" {
		t.Fatalf("%+v", second)
	}
	if empty := ToPaginatedJSON(ResponseQagPaginatedList{MaxPageNumber: 0}); empty.Responses == nil {
		t.Fatal("an empty list is written [] not null")
	}
}

// ---------------------------------------------------------------------------
// ResponseQagRepositoryImplTest
// ---------------------------------------------------------------------------

type fakeStrapi struct {
	all   []qag.ResponseQag
	total int
	calls int
}

func (f *fakeStrapi) GetResponsesQag(context.Context, []string) []qag.ResponseQag { f.calls++; return f.all }
func (f *fakeStrapi) GetAllResponsesQag(context.Context) []qag.ResponseQag         { f.calls++; return f.all }
func (f *fakeStrapi) GetResponsesTotal(context.Context) int                         { f.calls++; return f.total }

func testRepo(ttl time.Duration, s *fakeStrapi) *repository {
	return &repository{a: &app.App{Cfg: &config.Config{MicroCacheTTL: ttl}, Cache: cache.New(nil, nil, false)}, strapi: s}
}

func TestRepositoryPage(t *testing.T) {
	three := []qag.ResponseQag{textResponse("a", jan(3), ""), textResponse("b", jan(2), ""), textResponse("c", jan(1), "")}
	for _, tc := range []struct {
		name  string
		all   []qag.ResponseQag
		from  int
		pages int
		want  int
	}{
		{"more responses than from", three, 2, 20, 1},
		{"from equal to the number of responses", three[:1], 1, 20, 0},
		{"from beyond the number of responses", three[:1], 20, 20, 0},
		{"from zero", three, 0, 5, 3},
		{"a page cut in the middle", three, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := testRepo(0, &fakeStrapi{all: tc.all}).GetResponsesQagPage(ctx, tc.from, tc.pages, nil)
			if len(got) != tc.want {
				t.Fatalf("got %d want %d", len(got), tc.want)
			}
		})
	}
	t.Run("sorted by date desc and filtered by minDate", func(t *testing.T) {
		all := []qag.ResponseQag{textResponse("old", jan(1), ""), textResponse("new", jan(9), ""), textResponse("mid", jan(5), "")}
		got := testRepo(0, &fakeStrapi{all: all}).GetResponsesQagPage(ctx, 0, 5, ptr(jan(2).UnixMilli()))
		if len(got) != 2 || got[0].QagID() != "new" || got[1].QagID() != "mid" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a negative offset fails like List.subList", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a panic (HTTP 500)")
			}
		}()
		testRepo(0, &fakeStrapi{all: three}).GetResponsesQagPage(ctx, -1, 5, nil)
	})
	t.Run("from + pageSize overflows like Int", func(t *testing.T) {
		if got := testRepo(0, &fakeStrapi{all: three}).GetResponsesQagPage(ctx, 2147483646, 5, nil); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
}

func TestRepositoryCountWithMinDate(t *testing.T) {
	all := []qag.ResponseQag{textResponse("a", jan(1), ""), textResponse("b", jan(5), ""), textResponse("c", jan(9), "")}
	s := &fakeStrapi{all: all, total: 42}
	r := testRepo(0, s)
	if n := r.GetResponsesQagCount(ctx, nil); n != 42 {
		t.Fatalf("without minDate the Strapi total is used: %d", n)
	}
	if n := r.GetResponsesQagCount(ctx, ptr(jan(5).UnixMilli())); n != 2 {
		t.Fatalf("with minDate the mapped responses are counted: %d", n)
	}
}

func TestRepositoryMicroCache(t *testing.T) {
	s := &fakeStrapi{all: []qag.ResponseQag{textResponse("a", jan(1), "")}, total: 1}
	r := testRepo(time.Minute, s)
	for i := 0; i < 3; i++ {
		r.GetResponsesQagPage(ctx, 0, 5, nil)
		r.GetResponsesQagCount(ctx, nil)
		r.GetResponsesQag(ctx, []string{"a"})
	}
	if s.calls != 3 {
		t.Fatalf("each Strapi request must be made once while the answer is kept, got %d", s.calls)
	}
	// an empty answer may be an outage: it is never kept
	empty := &fakeStrapi{}
	r = testRepo(time.Minute, empty)
	for i := 0; i < 3; i++ {
		r.GetResponsesQagPage(ctx, 0, 5, nil)
		r.GetResponsesQagCount(ctx, nil)
	}
	if empty.calls != 6 {
		t.Fatalf("empty answers must be requested again, got %d calls", empty.calls)
	}
}
