package qaglist

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/qag"
	"agora/internal/modules/thematique"
	"agora/internal/modules/themehebdo"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

// Monday 2024-01-08 at 12:00, like QagPaginatedV2UseCaseTest.
var fixedNow = time.Date(2024, 1, 8, 12, 0, 0, 0, time.Local)

var th = thematique.Thematique{ID: "thematiqueId", Label: "label", Picto: "picto"}

// ---------------------------------------------------------------------------
// fakes (the Mockito mocks of QagPaginatedV2UseCaseTest)
// ---------------------------------------------------------------------------

type fakeShared struct {
	count     int
	countErr  error
	pages     map[string][]qag.QagInfoWithSupportCount
	countsFor []*string
	pageCalls []string
}

func (f *fakeShared) Count(_ context.Context, t *string) (int, error) {
	f.countsFor = append(f.countsFor, t)
	return f.count, f.countErr
}

func (f *fakeShared) Page(_ context.Context, filter string, offset int, t *string) ([]qag.QagInfoWithSupportCount, error) {
	key := filter + ":" + strconv.Itoa(offset)
	if t != nil {
		key += ":" + *t
	}
	f.pageCalls = append(f.pageCalls, key)
	return f.pages[key], nil
}

type fakeSupported struct {
	list  []qag.QagInfoWithSupportCount
	calls []string
}

func (f *fakeSupported) GetSupportedQagsPaginatedV2(_ context.Context, userID string, offset int, t *string) ([]qag.QagInfoWithSupportCount, error) {
	key := userID + ":" + strconv.Itoa(offset)
	if t != nil {
		key += ":" + *t
	}
	f.calls = append(f.calls, key)
	return f.list, nil
}

type fakeSupports struct {
	ids       []string
	count     int
	countCall []string
}

func (f *fakeSupports) GetUserSupportedQagIDs(context.Context, string) ([]string, error) {
	return f.ids, nil
}

func (f *fakeSupports) GetSupportedQagCount(_ context.Context, userID string, t *string) (int, error) {
	key := userID
	if t != nil {
		key += ":" + *t
	}
	f.countCall = append(f.countCall, key)
	return f.count, nil
}

type fakeThemes map[string]thematique.Thematique

func (f fakeThemes) ByID(_ context.Context, id string) *thematique.Thematique {
	if t, ok := f[id]; ok {
		return &t
	}
	return nil
}

type fakeHeaders struct {
	headers map[string]*HeaderQag
	calls   []string
}

func (f *fakeHeaders) Header(_ context.Context, filterType string) (*HeaderQag, error) {
	f.calls = append(f.calls, filterType)
	return f.headers[filterType], nil
}

type fakeTrending struct {
	list  []qag.QagInfoWithSupportCount
	err   error
	calls int
}

func (f *fakeTrending) Candidates(context.Context) ([]qag.QagInfoWithSupportCount, error) {
	f.calls++
	return f.list, f.err
}

type fakeTheme struct {
	libre bool
	err   error
	calls int
}

func (f *fakeTheme) Current(context.Context) (themehebdo.ThemeHebdo, error) {
	f.calls++
	return themehebdo.ThemeHebdo{EstThemeLibre: f.libre}, f.err
}

type fakeClusters struct {
	clusters []TrendingCluster
	calls    int
}

func (f *fakeClusters) Clusters(context.Context) []TrendingCluster {
	f.calls++
	return f.clusters
}

type harness struct {
	uc       *PaginatedUseCase
	shared   *fakeShared
	supp     *fakeSupported
	supports *fakeSupports
	headers  *fakeHeaders
	trending *fakeTrending
	theme    *fakeTheme
	clusters *fakeClusters
}

func newHarness() *harness {
	h := &harness{
		shared: &fakeShared{pages: map[string][]qag.QagInfoWithSupportCount{}}, supp: &fakeSupported{}, supports: &fakeSupports{},
		headers: &fakeHeaders{headers: map[string]*HeaderQag{}}, trending: &fakeTrending{}, theme: &fakeTheme{}, clusters: &fakeClusters{},
	}
	h.uc = &PaginatedUseCase{
		supported: h.supp, shared: h.shared, themes: fakeThemes{"thematiqueId": th}, headers: h.headers, trending: h.trending,
		supports: h.supports, themeHebdo: h.theme, clusters: h.clusters, exponent: 1.5, now: func() time.Time { return fixedNow },
	}
	return h
}

// buildQag is QagPaginatedV2UseCaseTest.buildQag: moderated hoursAgo before fixedNow.
func buildQag(id string, hoursAgo int, supportCount int) qag.QagInfoWithSupportCount {
	return buildQagFull(id, "thematiqueId", "authorId", "title", hoursAgo, supportCount)
}

func buildQagFull(id, thematiqueID, userID, title string, hoursAgo, supportCount int) qag.QagInfoWithSupportCount {
	moderated := fixedNow.Add(-time.Duration(hoursAgo) * time.Hour)
	return qag.QagInfoWithSupportCount{
		ID: id, ThematiqueID: thematiqueID, Title: title, Description: "description", Date: moderated,
		Status: qag.StatusModeratedAccepted, Username: "username", UserID: userID, SupportCount: supportCount, ModeratedDate: &moderated,
	}
}

func ids(r *QagsAndMaxPageCountV2) []string {
	out := []string{}
	for _, q := range r.Qags {
		out = append(out, q.ID)
	}
	return out
}

// ---------------------------------------------------------------------------
// getTrendingQag
// ---------------------------------------------------------------------------

func TestTrendingEmptyList(t *testing.T) {
	h := newHarness()
	r, err := h.uc.GetTrendingQag(ctx, "userId")
	if err != nil || r == nil || len(r.Qags) != 0 || r.MaxPageCount != 1 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if h.theme.calls != 0 || h.clusters.calls != 0 {
		t.Fatal("the weekly theme is not read when there is no candidate")
	}
	if !reflect.DeepEqual(h.headers.calls, []string{"trending"}) {
		t.Fatalf("header calls %v", h.headers.calls)
	}
}

func TestTrendingPinsTheMostRecent(t *testing.T) {
	h := newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("newest", 1, 0), buildQag("older", 10, 100)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if got := ids(r); !reflect.DeepEqual(got, []string{"newest", "older"}) {
		t.Fatalf("got %v", got)
	}
	// only one QaG: only the pinned slot
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("single", 2, 10)}
	r, _ = h.uc.GetTrendingQag(ctx, "userId")
	if got := ids(r); !reflect.DeepEqual(got, []string{"single"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrendingScoreOrdering(t *testing.T) {
	// pinned = most recent; highScore: 52 / 4^1.5 = 6.5; lowScore: 2 / 52^1.5 ~ 0.0053
	h := newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("pinned", 1, 0), buildQag("lowScore", 50, 1), buildQag("highScore", 2, 51)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if got := ids(r); !reflect.DeepEqual(got, []string{"pinned", "highScore", "lowScore"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrendingEqualScoresKeepTheSqlOrder(t *testing.T) {
	h := newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("pinned", 1, 0), buildQag("a", 5, 10), buildQag("b", 5, 10), buildQag("c", 5, 10)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if got := ids(r); !reflect.DeepEqual(got, []string{"pinned", "a", "b", "c"}) {
		t.Fatalf("sortedByDescending is stable: got %v", got)
	}
}

func TestTrendingKeepsTheDuplicatesOfNonPinnedQags(t *testing.T) {
	// getTrendingQagsV3 returns a QaG twice when it was accepted twice: the pinned one is removed from the
	// candidates by id, any other duplicate stays
	h := newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("pinned", 1, 3), buildQag("pinned", 1, 3), buildQag("dup", 5, 4), buildQag("dup", 5, 4)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if got := ids(r); !reflect.DeepEqual(got, []string{"pinned", "dup", "dup"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrendingAntiMonopolyGuard(t *testing.T) {
	h := newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{
		buildQag("pinned", 1, 0), buildQag("old1", 100, 50), buildQag("old2", 101, 40), buildQag("old3", 102, 30),
		buildQag("old4", 103, 20), buildQag("old5", 104, 10), buildQag("fresh", 5, 1),
	}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	// pinned + old1 + old2 + old3 + fresh: old4 and old5 are skipped
	if len(r.Qags) != 5 {
		t.Fatalf("got %v", ids(r))
	}
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("pinned", 1, 0), buildQag("old1", 100, 50), buildQag("old2", 101, 40), buildQag("fresh", 5, 1)}
	r, _ = h.uc.GetTrendingQag(ctx, "userId")
	if len(r.Qags) != 4 {
		t.Fatalf("got %v", ids(r))
	}
	// exactly 72 hours is not old
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("pinned", 1, 0), buildQag("a", 72, 5), buildQag("b", 72, 5), buildQag("c", 72, 5), buildQag("d", 72, 5)}
	r, _ = h.uc.GetTrendingQag(ctx, "userId")
	if len(r.Qags) != 5 {
		t.Fatalf("72 h is not older than 72 h: got %v", ids(r))
	}
}

func TestTrendingUserContext(t *testing.T) {
	h := newHarness()
	h.supports.ids = []string{"qagId"}
	h.trending.list = []qag.QagInfoWithSupportCount{buildQagFull("qagId", "thematiqueId", "authorId", "t", 1, 1), buildQagFull("mine", "thematiqueId", "userId", "t", 2, 1)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if !r.Qags[0].IsSupportedByUser || r.Qags[0].IsAuthor || r.Qags[1].IsSupportedByUser || !r.Qags[1].IsAuthor {
		t.Fatalf("got %+v", r.Qags)
	}
	// unknown thematique: excluded
	h.trending.list = []qag.QagInfoWithSupportCount{buildQagFull("qagId", "unknownThematiqueId", "authorId", "t", 1, 1)}
	r, _ = h.uc.GetTrendingQag(ctx, "userId")
	if len(r.Qags) != 0 {
		t.Fatalf("got %+v", r.Qags)
	}
}

func TestTrendingHeader(t *testing.T) {
	h := newHarness()
	h.headers.headers["trending"] = &HeaderQag{HeaderID: "h", Title: "t", Message: "m"}
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("a", 1, 1)}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if r.Header == nil || r.Header.HeaderID != "h" {
		t.Fatalf("got %+v", r.Header)
	}
}

func TestTrendingMaxSlots(t *testing.T) {
	h := newHarness()
	for i := 1; i <= 15; i++ {
		h.trending.list = append(h.trending.list, buildQag("qag"+strconv.Itoa(i), i, 100-i))
	}
	r, _ := h.uc.GetTrendingQag(ctx, "userId")
	if len(r.Qags) != 10 {
		t.Fatalf("1 pinned + 9 slots = 10, got %d", len(r.Qags))
	}
}

func TestTrendingErrorsArePropagated(t *testing.T) {
	h := newHarness()
	h.trending.err = errors.New("db down")
	if _, err := h.uc.GetTrendingQag(ctx, "userId"); err == nil {
		t.Fatal("expected the error")
	}
	h = newHarness()
	h.trending.list = []qag.QagInfoWithSupportCount{buildQag("a", 1, 1), buildQag("b", 2, 1)}
	h.theme.err = errors.New("bad date")
	if _, err := h.uc.GetTrendingQag(ctx, "userId"); err == nil {
		t.Fatal("expected the error of the weekly theme")
	}
}

func TestTrendingClusterFilter(t *testing.T) {
	titles := func(ts ...string) []qag.QagInfoWithSupportCount {
		var out []qag.QagInfoWithSupportCount
		for i, title := range ts {
			id := "q" + strconv.Itoa(i)
			if i == 0 {
				id = "pinned"
			}
			out = append(out, buildQagFull(id, "thematiqueId", "authorId", title, i+1, 40-10*i))
		}
		return out
	}
	sante := TrendingCluster{ID: "sante", Mots: []string{"santé"}}
	t.Run("only two per cluster", func(t *testing.T) {
		h := newHarness()
		h.theme.libre = true
		h.clusters.clusters = []TrendingCluster{sante}
		h.trending.list = titles("Autre sujet", "Santé mentale", "santé et prévention", "Coût de la santé", "Réforme santé")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		if len(r.Qags) != 3 {
			t.Fatalf("pinned + 2 of the cluster, got %v", ids(r))
		}
	})
	t.Run("a QaG of two clusters is rejected when one is full", func(t *testing.T) {
		h := newHarness()
		h.theme.libre = true
		h.clusters.clusters = []TrendingCluster{{ID: "cluster-alpha", Mots: []string{"alpha"}}, {ID: "cluster-beta", Mots: []string{"beta"}}}
		h.trending.list = titles("Sujet neutre", "Question alpha un", "Question alpha deux", "Question alpha et beta")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		if len(r.Qags) != 3 {
			t.Fatalf("got %v", ids(r))
		}
		// the rejected QaG did not count for the other cluster
		h.trending.list = titles("Sujet neutre", "Question alpha un", "Question alpha deux", "Question alpha et beta", "Question beta un", "Question beta deux", "Question beta trois")
		r, _ = h.uc.GetTrendingQag(ctx, "userId")
		if got := len(r.Qags); got != 5 { // pinned, alpha 1, alpha 2, beta 1, beta 2
			t.Fatalf("got %d: %v", got, ids(r))
		}
	})
	t.Run("two per cluster in different clusters", func(t *testing.T) {
		h := newHarness()
		h.theme.libre = true
		h.clusters.clusters = []TrendingCluster{sante, {ID: "emploi", Mots: []string{"emploi", "chômage"}}}
		h.trending.list = titles("Autre sujet", "Santé mentale", "santé et prévention", "Emploi et formation", "Chômage en hausse")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		if len(r.Qags) != 5 {
			t.Fatalf("got %v", ids(r))
		}
	})
	t.Run("no match means no limit", func(t *testing.T) {
		h := newHarness()
		h.theme.libre = true
		h.clusters.clusters = []TrendingCluster{{ID: "sante", Mots: []string{"santé", "médecin"}}}
		h.trending.list = titles("Sujet neutre", "Route nationale", "Autoroute gratuite", "Circulation en ville", "Mobilité douce")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		if len(r.Qags) != 5 {
			t.Fatalf("got %v", ids(r))
		}
	})
	t.Run("not a free theme: no filter and no cluster read", func(t *testing.T) {
		h := newHarness()
		h.clusters.clusters = []TrendingCluster{sante}
		h.trending.list = titles("Santé au travail", "Santé mentale", "santé et prévention", "Coût de la santé", "Réforme santé")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		if len(r.Qags) != 5 || h.clusters.calls != 0 {
			t.Fatalf("got %v, cluster calls %d", ids(r), h.clusters.calls)
		}
	})
	t.Run("the pinned QaG is never filtered", func(t *testing.T) {
		h := newHarness()
		h.theme.libre = true
		h.clusters.clusters = []TrendingCluster{sante}
		h.trending.list = titles("Santé au travail", "Santé mentale", "santé et prévention", "Coût de la santé")
		r, _ := h.uc.GetTrendingQag(ctx, "userId")
		// the pinned QaG does not count in its cluster: two more santé QaGs fit
		if len(r.Qags) != 3 {
			t.Fatalf("got %v", ids(r))
		}
	})
}

func TestHoursSinceTruncatesTowardZero(t *testing.T) {
	at := func(d time.Duration) qag.QagInfoWithSupportCount {
		m := fixedNow.Add(-d)
		return qag.QagInfoWithSupportCount{Date: time.Unix(0, 0), ModeratedDate: &m}
	}
	for _, tc := range []struct {
		d    time.Duration
		want int64
	}{
		{0, 0}, {59*time.Minute + 59*time.Second, 0}, {time.Hour, 1}, {72*time.Hour + time.Nanosecond, 72},
		{-30 * time.Minute, 0}, {-time.Hour, -1}, {-time.Hour - time.Second, -1}, {-2*time.Hour + time.Second, -1},
	} {
		if got := hoursSince(at(tc.d), fixedNow); got != tc.want {
			t.Errorf("%v: got %d want %d", tc.d, got, tc.want)
		}
	}
	// without a moderated date the post date is used
	q := qag.QagInfoWithSupportCount{Date: fixedNow.Add(-5 * time.Hour)}
	if got := hoursSince(q, fixedNow); got != 5 {
		t.Errorf("post date: got %d", got)
	}
}

func TestJavaDoubleCompare(t *testing.T) {
	nan := math.NaN()
	negZero := math.Copysign(0, -1)
	for _, tc := range []struct {
		a, b float64
		want int
	}{
		{1, 2, -1}, {2, 1, 1}, {1, 1, 0}, {negZero, 0, -1}, {0, negZero, 1}, {nan, nan, 0}, {nan, math.Inf(1), 1}, {math.Inf(1), nan, -1},
		{nan, -1, 1}, {math.Inf(-1), -1, -1},
	} {
		if got := javaDoubleCompare(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%v, %v) = %d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getPopularQagPaginated / getLatestQagPaginated / getSupportedQagPaginated
// ---------------------------------------------------------------------------

func TestPaginatedPageNumberLowerThanOne(t *testing.T) {
	h := newHarness()
	for _, page := range []int{0, -1, math.MinInt32} {
		r, err := h.uc.GetPopularQagPaginated(ctx, "userId", page, nil)
		if r != nil || err != nil {
			t.Fatalf("page %d: %v %v", page, r, err)
		}
	}
	if len(h.shared.countsFor) != 0 {
		t.Fatal("nothing is read for a page lower than 1")
	}
}

func TestPaginatedOffsetBeyondTheCount(t *testing.T) {
	h := newHarness()
	h.shared.count = 20
	if r, _ := h.uc.GetLatestQagPaginated(ctx, "userId", 3, nil); r != nil {
		t.Fatal("offset 40 > count 20 gives null")
	}
	// the offset equal to the count is not beyond the end
	r, _ := h.uc.GetLatestQagPaginated(ctx, "userId", 2, nil)
	if r == nil || len(r.Qags) != 0 || r.MaxPageCount != 1 || r.Header != nil {
		t.Fatalf("got %+v", r)
	}
	if !reflect.DeepEqual(h.shared.pageCalls, []string{"latest:20"}) {
		t.Fatalf("page calls %v", h.shared.pageCalls)
	}
}

func TestPaginatedOffsetOverflowsLikeKotlinInt(t *testing.T) {
	h := newHarness()
	h.shared.count = 5
	// (1073741825 - 1) * 20 = 21474836480 = 5 * 2^32 wraps to 0: the first page, without header
	r, _ := h.uc.GetPopularQagPaginated(ctx, "userId", 1073741825, nil)
	if r == nil || !reflect.DeepEqual(h.shared.pageCalls, []string{"top:0"}) || r.Header != nil {
		t.Fatalf("got %+v, calls %v", r, h.shared.pageCalls)
	}
	// 107374184: 2147483660 wraps to a negative offset, which is never beyond the end
	h = newHarness()
	r, _ = h.uc.GetPopularQagPaginated(ctx, "userId", 107374184, nil)
	if r == nil || !reflect.DeepEqual(h.shared.pageCalls, []string{"top:-2147483636"}) {
		t.Fatalf("got %+v, calls %v", r, h.shared.pageCalls)
	}
}

func TestPaginatedPopularAndLatest(t *testing.T) {
	h := newHarness()
	h.shared.count = 45
	h.shared.pages["top:20:th"] = []qag.QagInfoWithSupportCount{
		buildQagFull("a", "thematiqueId", "userId", "A", 1, 3), buildQagFull("b", "unknown", "x", "B", 1, 2), buildQagFull("c", "thematiqueId", "x", "C", 1, 1),
	}
	h.supports.ids = []string{"c", "zzz"}
	h.headers.headers["top"] = &HeaderQag{HeaderID: "h"}
	r, err := h.uc.GetPopularQagPaginated(ctx, "userId", 2, ptr("th"))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if r.MaxPageCount != 3 || r.Header != nil || !reflect.DeepEqual(ids(r), []string{"a", "c"}) {
		t.Fatalf("got %+v", r)
	}
	if !r.Qags[0].IsAuthor || r.Qags[0].IsSupportedByUser || r.Qags[1].IsAuthor || !r.Qags[1].IsSupportedByUser {
		t.Fatalf("overlay: %+v", r.Qags)
	}
	if len(h.headers.calls) != 0 {
		t.Fatal("the header is only read for the first page")
	}
	// first page: the header of the tab
	h.shared.pages["latest:0"] = h.shared.pages["top:20:th"]
	r, _ = h.uc.GetLatestQagPaginated(ctx, "userId", 1, nil)
	if r.Header != nil || !reflect.DeepEqual(h.headers.calls, []string{"latest"}) {
		t.Fatalf("header %+v calls %v", r.Header, h.headers.calls)
	}
	h.headers.headers["latest"] = &HeaderQag{HeaderID: "hl"}
	r, _ = h.uc.GetLatestQagPaginated(ctx, "userId", 1, nil)
	if r.Header == nil || r.Header.HeaderID != "hl" {
		t.Fatalf("header %+v", r.Header)
	}
	// 45 / 20 = 2.25: three pages; 40 / 20 = 2
	h.shared.count = 40
	r, _ = h.uc.GetLatestQagPaginated(ctx, "userId", 1, nil)
	if r.MaxPageCount != 2 {
		t.Fatalf("max page count %d", r.MaxPageCount)
	}
	h.shared.count = 0
	r, _ = h.uc.GetLatestQagPaginated(ctx, "userId", 1, nil)
	if r == nil || r.MaxPageCount != 0 {
		t.Fatalf("an empty list still has a first page: %+v", r)
	}
}

func TestPaginatedSupportedUsesThePersonalCountAndPage(t *testing.T) {
	h := newHarness()
	h.supports.count = 21
	h.supp.list = []qag.QagInfoWithSupportCount{buildQagFull("a", "thematiqueId", "other", "A", 1, 3)}
	h.supports.ids = []string{"a"}
	r, _ := h.uc.GetSupportedQagPaginated(ctx, "userId", 2, ptr("th"))
	if r == nil || r.MaxPageCount != 2 || len(r.Qags) != 1 || !r.Qags[0].IsSupportedByUser {
		t.Fatalf("got %+v", r)
	}
	if !reflect.DeepEqual(h.supports.countCall, []string{"userId:th"}) || !reflect.DeepEqual(h.supp.calls, []string{"userId:20:th"}) {
		t.Fatalf("calls %v %v", h.supports.countCall, h.supp.calls)
	}
	if len(h.shared.countsFor) != 0 || len(h.shared.pageCalls) != 0 {
		t.Fatal("the shared micro-cache is not used for the supporting tab")
	}
	if !reflect.DeepEqual(h.headers.calls, []string(nil)) {
		t.Fatal("page 2 has no header")
	}
	if r, _ = h.uc.GetSupportedQagPaginated(ctx, "userId", 3, nil); r != nil {
		t.Fatal("offset 40 > 21 gives null")
	}
}

func TestPaginatedErrors(t *testing.T) {
	h := newHarness()
	h.shared.countErr = errors.New("db down")
	if _, err := h.uc.GetPopularQagPaginated(ctx, "userId", 1, nil); err == nil {
		t.Fatal("expected the error")
	}
}

// ---------------------------------------------------------------------------
// TrendingClusterRepositoryImplTest
// ---------------------------------------------------------------------------

func dtos(pairs ...string) []trendingClusterStrapiDTO {
	var out []trendingClusterStrapiDTO
	for i := 0; i < len(pairs); i += 2 {
		d := trendingClusterStrapiDTO{Titre: pairs[i]}
		if pairs[i+1] != "<nil>" {
			d.Keywords = ptr(pairs[i+1])
		}
		out = append(out, d)
	}
	return out
}

func TestMapClusters(t *testing.T) {
	got := mapClusters(dtos("tesla", "Tesla, FSD, conduite autonome", "sante", "santé, médecin, hôpital"), nil)
	want := []TrendingCluster{{ID: "tesla", Mots: []string{"Tesla", "FSD", "conduite autonome"}}, {ID: "sante", Mots: []string{"santé", "médecin", "hôpital"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	var ignored []string
	got = mapClusters(dtos("tesla", "Tesla, FSD", "vide", "<nil>", "blank", "  ,  , ", "empty", ""), func(titre string) { ignored = append(ignored, titre) })
	if len(got) != 1 || got[0].ID != "tesla" || !reflect.DeepEqual(ignored, []string{"vide", "blank", "empty"}) {
		t.Fatalf("got %+v ignored %v", got, ignored)
	}
	got = mapClusters(dtos("tesla", "  Tesla  ,  FSD  ,  conduite autonome  "), nil)
	if !reflect.DeepEqual(got[0].Mots, []string{"Tesla", "FSD", "conduite autonome"}) {
		t.Fatalf("got %+v", got)
	}
	// Kotlin trim() removes the Unicode white space (NBSP, tab) but not other characters
	got = mapClusters(dtos("x", " a ,\tb\t,c​"), nil)
	if !reflect.DeepEqual(got[0].Mots, []string{"a", "b", "c​"}) {
		t.Fatalf("got %q", got[0].Mots)
	}
	if got = mapClusters(nil, nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

type fakeFetcher struct {
	data  []trendingClusterStrapiDTO
	calls int
}

func (f *fakeFetcher) Fetch(context.Context) []trendingClusterStrapiDTO { f.calls++; return f.data }

func testApp() *app.App {
	return &app.App{
		Cfg:   &config.Config{MicroCacheTTL: 5 * time.Second},
		Cache: cache.New(nil, nil, false), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock: func() time.Time { return fixedNow },
	}
}

func TestClusterStoreCachesNonEmptyOnly(t *testing.T) {
	a := testApp()
	f := &fakeFetcher{data: dtos("tesla", "Tesla, FSD")}
	s := clusterStore{a: a, fetcher: f}
	for i := 0; i < 3; i++ {
		if got := s.Clusters(ctx); len(got) != 1 {
			t.Fatalf("got %+v", got)
		}
	}
	if f.calls != 1 {
		t.Fatalf("a cached list is not requested again, got %d calls", f.calls)
	}
	// nothing usable: requested again at every call
	a = testApp()
	f = &fakeFetcher{data: dtos("vide", "<nil>", "blank", " , ")}
	s = clusterStore{a: a, fetcher: f}
	for i := 0; i < 3; i++ {
		if got := s.Clusters(ctx); len(got) != 0 {
			t.Fatalf("got %+v", got)
		}
	}
	if f.calls != 3 {
		t.Fatalf("an empty result is not cached, got %d calls", f.calls)
	}
}

// ---------------------------------------------------------------------------
// headers, micro-cache and trending cache
// ---------------------------------------------------------------------------

type countingSource struct {
	header *HeaderQag
	calls  map[string]int
}

func (c *countingSource) LastHeader(_ context.Context, filterType string) *HeaderQag {
	c.calls[filterType]++
	return c.header
}

func TestHeaderStoreKeepsFoundAndNotFound(t *testing.T) {
	for _, header := range []*HeaderQag{{HeaderID: "h", Title: "t", Message: "m"}, nil} {
		src := &countingSource{header: header, calls: map[string]int{}}
		s := headerStore{a: testApp(), source: src}
		for i := 0; i < 3; i++ {
			got, err := s.Header(ctx, "top")
			if err != nil || !reflect.DeepEqual(got, header) {
				t.Fatalf("got %+v, %v", got, err)
			}
		}
		if _, _ = s.Header(ctx, "latest"); src.calls["top"] != 1 || src.calls["latest"] != 1 {
			t.Fatalf("calls %v: the answer, even \"not found\", is kept per tab", src.calls)
		}
	}
}

type countingInfo struct {
	counts, pages, trending int
	list                    []qag.QagInfoWithSupportCount
	err                     error
}

func (c *countingInfo) GetQagsCount(context.Context, *string) (int, error) {
	c.counts++
	return 7, c.err
}
func (c *countingInfo) GetPopularQagsPaginatedV2(context.Context, int, *string) ([]qag.QagInfoWithSupportCount, error) {
	c.pages++
	return c.list, c.err
}

func (c *countingInfo) GetLatestQagsPaginatedV2(context.Context, int, *string) ([]qag.QagInfoWithSupportCount, error) {
	c.pages++
	return c.list, c.err
}

func (c *countingInfo) GetTrendingQagsV3(context.Context) ([]qag.QagInfoWithSupportCount, error) {
	c.trending++
	return c.list, c.err
}

func TestMicroPages(t *testing.T) {
	info := &countingInfo{list: []qag.QagInfoWithSupportCount{{ID: "a"}}}
	m := &microPages{a: testApp(), info: info}
	for i := 0; i < 3; i++ {
		if n, err := m.Count(ctx, nil); n != 7 || err != nil {
			t.Fatal(n, err)
		}
		if p, err := m.Page(ctx, filterTop, 0, nil); len(p) != 1 || err != nil {
			t.Fatal(p, err)
		}
	}
	m.Count(ctx, ptr("th"))
	m.Page(ctx, filterLatest, 0, nil)
	m.Page(ctx, filterTop, 20, nil)
	m.Page(ctx, filterTop, 0, ptr("th"))
	if info.counts != 2 || info.pages != 4 {
		t.Fatalf("one load per key: counts %d pages %d", info.counts, info.pages)
	}
	// errors are never kept
	info = &countingInfo{err: errors.New("down")}
	m = &microPages{a: testApp(), info: info}
	for i := 0; i < 3; i++ {
		if _, err := m.Count(ctx, nil); err == nil {
			t.Fatal("expected the error")
		}
	}
	if info.counts != 3 {
		t.Fatalf("counts %d", info.counts)
	}
	// AGORA_MICROCACHE_TTL=0: every call reads the database
	a := testApp()
	a.Cfg.MicroCacheTTL = 0
	info = &countingInfo{}
	m = &microPages{a: a, info: info}
	m.Count(ctx, nil)
	m.Count(ctx, nil)
	m.Page(ctx, filterTop, 0, nil)
	m.Page(ctx, filterTop, 0, nil)
	if info.counts != 2 || info.pages != 2 {
		t.Fatalf("counts %d pages %d", info.counts, info.pages)
	}
}

func TestTrendingCache(t *testing.T) {
	info := &countingInfo{} // an empty list is kept as well
	c := &trendingCache{a: testApp(), info: info}
	for i := 0; i < 3; i++ {
		if _, err := c.Candidates(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if info.trending != 1 {
		t.Fatalf("trending loads %d", info.trending)
	}
	info = &countingInfo{err: errors.New("down")}
	c = &trendingCache{a: testApp(), info: info}
	c.Candidates(ctx)
	c.Candidates(ctx)
	if info.trending != 2 {
		t.Fatalf("an error is not kept: %d", info.trending)
	}
}

// ---------------------------------------------------------------------------
// controllers and mappers
// ---------------------------------------------------------------------------

func TestFilterKeywords(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		in   *string
		want *string
	}{
		{nil, nil},
		{str(""), nil},
		{str("   "), nil},
		{str("  "), nil},
		{str("transport"), str("transport")},
		{str("Écologie"), str("Ecologie")},
		{str("écologie"), str("ecologie")},
		{str("100%"), str("100")},
		{str("%_"), str("")},
		{str("a  b"), str("a  b")},
		{str("tab\there"), str("tabhere")},
		{str("straße"), str("strae")},
		{str("🚀 fusée"), str(" fusee")},
		{str(strings.Repeat("a", 80)), str(strings.Repeat("a", 75))},
		// the 75 limit counts UTF-16 units: 38 emojis = 76 units, the 38th is cut
		{str(strings.Repeat("🚀", 40) + "abc"), str("")},
		{str("ab" + strings.Repeat("🚀", 40) + "abc"), str("ab")},
		{str(strings.Repeat("é", 75) + "xyz"), str(strings.Repeat("e", 75))},
		{str(strings.Repeat("a", 74) + "é"), str(strings.Repeat("a", 74) + "e")},
	} {
		got := filterKeywords(tc.in)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			var g string
			if got != nil {
				g = *got
			}
			t.Errorf("filterKeywords(%v) = %q want %v", tc.in, g, tc.want)
		}
	}
}

func TestPaginatedJSON(t *testing.T) {
	date := time.Date(2024, 3, 4, 5, 6, 7, 0, time.Local)
	preview := qag.QagPreview{
		ID: "id", Thematique: th, Title: "t", Description: "d", Username: "u", Date: date, SupportCount: 4,
		IsSupportedByUser: true, IsAuthor: true, CanShare: true,
	}
	j := ToPaginatedJSON(QagsAndMaxPageCountV2{MaxPageCount: 2, Qags: []qag.QagPreview{preview}})
	if j.Header != nil || j.MaxPageNumber != 2 || len(j.Qags) != 1 {
		t.Fatalf("%+v", j)
	}
	q := j.Qags[0]
	if q.Date != "2024-03-04 05:06:07" || q.Support.SupportCount != 4 || !q.Support.IsSupportedByUser || !q.IsAuthor || !q.CanShare ||
		q.Thematique.Label != "label" || q.QagID != "id" {
		t.Fatalf("%+v", q)
	}
	j = ToPaginatedJSON(QagsAndMaxPageCountV2{Header: &HeaderQag{HeaderID: "h", Title: "t", Message: "m"}})
	if j.Header == nil || j.Header.HeaderID != "h" || j.Qags == nil {
		t.Fatalf("%+v", j)
	}
	if l := ToPreviewListJSON(nil); l.Results == nil || len(l.Results) != 0 {
		t.Fatal("an empty result list is written []")
	}
}
