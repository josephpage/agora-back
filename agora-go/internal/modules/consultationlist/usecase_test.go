package consultationlist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/domain"
	"agora/internal/jsonjava"
	"agora/internal/modules/consultation"
	"agora/internal/modules/profile"
)

var baseTime = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func ldt(y int, m time.Month, d, h, mi int) consultation.LocalDateTime {
	return consultation.LocalDateTime{T: time.Date(y, m, d, h, mi, 0, 0, time.UTC)}
}

func newApp(microTTL time.Duration, coexistence bool) *app.App {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &app.App{
		Cfg:   &config.Config{MicroCacheTTL: microTTL},
		Cache: cache.New(nil, log, coexistence),
		Log:   log,
		Clock: func() time.Time { return baseTime },
	}
}

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeThemes struct {
	calls atomic.Int32
	list  []consultation.Thematique
}

func (f *fakeThemes) List(context.Context) []consultation.Thematique {
	f.calls.Add(1)
	return f.list
}

var theme1 = consultation.Thematique{ID: "th1", Label: "Santé", Picto: "S"}

func newThemes() *fakeThemes { return &fakeThemes{list: []consultation.Thematique{theme1}} }

// info is a ConsultationWithUpdateInfo of thematique th1 (or the given one).
func info(id string, thematiqueID ...string) consultation.ConsultationWithUpdateInfo {
	th := "th1"
	if len(thematiqueID) > 0 {
		th = thematiqueID[0]
	}
	label := "label " + id
	return consultation.ConsultationWithUpdateInfo{
		ID: id, Slug: "slug-" + id, Title: "Title " + id, CoverURL: "cover-" + id, ThematiqueID: th,
		EndDate: ldt(2026, 5, 1, 0, 0), UpdateDate: ldt(2026, 5, 20, 0, 0), UpdateLabel: &label, Territory: "France",
	}
}

type fakeFinished struct {
	mu      sync.Mutex
	count   int
	list    []consultation.ConsultationWithUpdateInfo
	counts  int
	pages   []string
	byTerr  int
	lastTer []domain.Territoire
}

func (f *fakeFinished) GetConsultationFinishedCount(context.Context) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts++
	return f.count
}

func (f *fakeFinished) GetConsultationFinishedListPage(_ context.Context, offset, pageSize int, territory domain.Territoire) []consultation.ConsultationWithUpdateInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages = append(f.pages, fmt.Sprintf("%d/%d/%s", offset, pageSize, territory.Value()))
	return f.list
}

func (f *fakeFinished) GetConsultationFinishedList(_ context.Context, territories []domain.Territoire) []consultation.ConsultationWithUpdateInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byTerr++
	f.lastTer = territories
	return f.list
}

type fakeAnswered struct {
	mu       sync.Mutex
	count    int
	list     []consultation.ConsultationWithUpdateInfo
	counts   int
	lists    []string
	countErr error
	listErr  error
}

func (f *fakeAnswered) GetConsultationAnsweredCount(_ context.Context, userID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts++
	return f.count, f.countErr
}

func (f *fakeAnswered) GetConsultationAnsweredList(_ context.Context, userID string, offset int) ([]consultation.ConsultationWithUpdateInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, fmt.Sprintf("%s/%d", userID, offset))
	return f.list, f.listErr
}

func finishedUseCase(a *app.App, repo *fakeFinished, themes *fakeThemes) *FinishedUseCase {
	return &FinishedUseCase{a: a, repo: repo, themes: themes}
}

func answeredUseCase(a *app.App, repo answeredRepository, themes *fakeThemes) *AnsweredUseCase {
	return &AnsweredUseCase{a: a, repo: repo, themes: themes}
}

// ---------------------------------------------------------------------------
// ConsultationsFinishedPaginatedListUseCaseTest
// ---------------------------------------------------------------------------

func TestFinishedPageNumberLowerOrEqualsZeroReturnsNull(t *testing.T) {
	for _, page := range []int{0, -1, -2147483648} {
		repo, themes := &fakeFinished{}, newThemes()
		uc := finishedUseCase(newApp(0, false), repo, themes)
		got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), page, "")
		if got != nil || err != nil || repo.counts != 0 || themes.calls.Load() != 0 {
			t.Errorf("page %d: %v %v, count calls %d", page, got, err, repo.counts)
		}
	}
}

func TestFinishedHasCacheReturnsCachedContent(t *testing.T) {
	repo, themes := &fakeFinished{count: 10, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}, newThemes()
	uc := finishedUseCase(newApp(0, false), repo, themes)
	first, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "Nord")
	if err != nil || first == nil {
		t.Fatalf("%v %v", first, err)
	}
	// the territory is normalized: "nord" is the key "Nord-1" as well
	second, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "nord")
	if err != nil || second != first {
		t.Fatalf("not served from the cache: %p %p %v", first, second, err)
	}
	if repo.counts != 1 || len(repo.pages) != 1 || themes.calls.Load() != 1 {
		t.Errorf("count %d, pages %v, thematiques %d", repo.counts, repo.pages, themes.calls.Load())
	}
	// another page and another territory are other keys
	if _, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 2, "Nord"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "Bretagne"); err != nil {
		t.Fatal(err)
	}
	if repo.counts != 3 {
		t.Errorf("count calls %d", repo.counts)
	}
}

func TestFinishedNoCachePageHigherThanMaxReturnsNull(t *testing.T) {
	repo, themes := &fakeFinished{count: 10}, newThemes()
	uc := finishedUseCase(newApp(0, false), repo, themes)
	for i := 0; i < 2; i++ { // a missing page is never cached
		got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 7, "Nord")
		if got != nil || err != nil {
			t.Fatalf("%v %v", got, err)
		}
	}
	if repo.counts != 2 || len(repo.pages) != 0 || themes.calls.Load() != 0 {
		t.Errorf("only the count must be read: count %d pages %v thematiques %d", repo.counts, repo.pages, themes.calls.Load())
	}
}

func TestFinishedCorrectPageMapsInfoWithAMatchingThematique(t *testing.T) {
	repo := &fakeFinished{count: 134, list: []consultation.ConsultationWithUpdateInfo{info("c1"), info("c2", "unknown")}}
	themes := newThemes()
	uc := finishedUseCase(newApp(0, false), repo, themes)
	got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "Nord")
	if err != nil {
		t.Fatal(err)
	}
	want := &FinishedList{
		Consultations: []consultation.ConsultationPreviewFinished{{
			ID: "c1", Slug: "slug-c1", Title: "Title c1", CoverURL: "cover-c1", Thematique: theme1, UpdateLabel: repo.list[0].UpdateLabel,
			LastUpdateDate: ldt(2026, 5, 20, 0, 0), EndDate: ldt(2026, 5, 1, 0, 0), Territory: "France",
		}},
		MaxPageNumber: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(repo.pages, []string{"0/100/Nord"}) {
		t.Errorf("%v", repo.pages)
	}
}

func TestFinishedOffsetAndMaxPageNumber(t *testing.T) {
	cases := []struct{ count, page, offset, maxPage int }{
		{1, 1, 0, 1}, {121, 1, 0, 2}, {139, 2, 100, 2}, {380, 3, 200, 4},
		// the page of an exact multiple still exists (offset > count is false) and the next one does not
		{100, 2, 100, 1}, {0, 1, 0, 0}, {100, 1, 0, 1}, {101, 2, 100, 2}, {200, 3, 200, 2},
		// Int arithmetic: (pageNumber - 1) * 100 wraps around
		{5, 21474837, 2147483600, 1},  // 21474836 * 100 = 2147483600: still positive, the page does not exist
		{5, 21474838, -2147483596, 1}, // 21474837 * 100 overflows: the page exists
		{5, 2147483647, -200, 1},
		{2147483647, 21474837, 2147483600, 21474837},
	}
	for _, tc := range cases {
		repo := &fakeFinished{count: tc.count, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
		uc := finishedUseCase(newApp(0, false), repo, newThemes())
		got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), tc.page, "Nord")
		exists := tc.offset <= tc.count
		if err != nil || (got != nil) != exists {
			t.Errorf("count %d page %d: got %v (%v), page should exist: %v", tc.count, tc.page, got, err, exists)
			continue
		}
		if exists {
			if got.MaxPageNumber != tc.maxPage {
				t.Errorf("count %d page %d: maxPageNumber %d, want %d", tc.count, tc.page, got.MaxPageNumber, tc.maxPage)
			}
			if want := fmt.Sprintf("%d/100/Nord", tc.offset); repo.pages[0] != want {
				t.Errorf("count %d page %d: %s, want %s", tc.count, tc.page, repo.pages[0], want)
			}
		}
	}
}

func TestFinishedNegativeCountAndInvalidTerritory(t *testing.T) {
	repo := &fakeFinished{count: -5}
	uc := finishedUseCase(newApp(0, false), repo, newThemes())
	if got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "Nord"); got != nil || err != nil {
		t.Errorf("a negative count has no page: %v %v", got, err)
	}
	// the page is checked before the territory
	if got, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 0, "Inconnu"); got != nil || err != nil {
		t.Errorf("%v %v", got, err)
	}
	_, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, "Inconnu")
	var invalid *domain.InvalidTerritoryError
	if !errors.As(err, &invalid) || err.Error() != "Le territoire Inconnu n'existe pas." {
		t.Errorf("%v", err)
	}
	if _, err := uc.GetConsultationFinishedPaginatedList(context.Background(), 1, ""); !errors.As(err, &invalid) {
		t.Errorf("%v", err)
	}
	if repo.counts != 1 {
		t.Errorf("an invalid territory reads nothing: %d", repo.counts)
	}
}

// The pages are cleared by the hook of ConsultationCacheClearUseCase.
func TestFinishedPagesAreClearedByInvalidateAll(t *testing.T) {
	a := newApp(0, false)
	repo := &fakeFinished{count: 10, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	s := &Service{a: a}
	uc := finishedUseCase(a, repo, newThemes())
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := uc.GetConsultationFinishedPaginatedList(ctx, 1, "France"); err != nil {
			t.Fatal(err)
		}
	}
	if repo.counts != 1 {
		t.Fatalf("not cached: %d", repo.counts)
	}
	s.clearFinishedPages(ctx)
	if _, err := uc.GetConsultationFinishedPaginatedList(ctx, 1, "France"); err != nil || repo.counts != 2 {
		t.Fatalf("not cleared: %d %v", repo.counts, err)
	}
}

func TestFinishedTTL(t *testing.T) {
	if got := (&FinishedUseCase{a: newApp(0, false)}).ttl(); got != time.Hour {
		t.Errorf("%v", got)
	}
	if got := (&FinishedUseCase{a: newApp(0, true)}).ttl(); got != 5*time.Second {
		t.Errorf("coexistence: %v", got)
	}
	if got := (&AnsweredUseCase{a: newApp(0, true)}).ttl(); got != 5*time.Second {
		t.Errorf("coexistence: %v", got)
	}
}

// ---------------------------------------------------------------------------
// ConsultationsAnsweredPaginatedListUseCaseTest
// ---------------------------------------------------------------------------

func TestAnsweredPageNumberLowerOrEqualsZeroReturnsNull(t *testing.T) {
	for _, page := range []int{0, -1, -2147483648} {
		repo, themes := &fakeAnswered{}, newThemes()
		uc := answeredUseCase(newApp(0, false), repo, themes)
		got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "userId", page)
		if got != nil || err != nil || repo.counts != 0 || themes.calls.Load() != 0 {
			t.Errorf("page %d: %v %v", page, got, err)
		}
	}
}

func TestAnsweredHasCacheReturnsCachedContent(t *testing.T) {
	repo := &fakeAnswered{count: 3, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	themes := newThemes()
	uc := answeredUseCase(newApp(0, false), repo, themes)
	ctx := context.Background()
	first, err := uc.GetConsultationAnsweredPaginatedList(ctx, "userId", 1)
	if err != nil || first == nil {
		t.Fatalf("%v %v", first, err)
	}
	second, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "userId", 1)
	if second != first || repo.counts != 1 || len(repo.lists) != 1 || themes.calls.Load() != 1 {
		t.Errorf("count %d lists %v thematiques %d", repo.counts, repo.lists, themes.calls.Load())
	}
	// another user has their own entry
	if _, err := uc.GetConsultationAnsweredPaginatedList(ctx, "other", 1); err != nil || repo.counts != 2 || len(repo.lists) != 2 {
		t.Errorf("count %d lists %v", repo.counts, repo.lists)
	}
}

func TestAnsweredNoCachePageHigherThanMaxReturnsNull(t *testing.T) {
	repo, themes := &fakeAnswered{count: 10}, newThemes()
	uc := answeredUseCase(newApp(0, false), repo, themes)
	for i := 0; i < 3; i++ {
		got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "userId", 7)
		if got != nil || err != nil {
			t.Fatalf("%v %v", got, err)
		}
	}
	// the count is read once (the entry holds it); neither the list nor the thematiques are ever read
	if len(repo.lists) != 0 || themes.calls.Load() != 0 {
		t.Errorf("lists %v thematiques %d", repo.lists, themes.calls.Load())
	}
}

func TestAnsweredCorrectPageMapsInfoWithAMatchingThematique(t *testing.T) {
	repo := &fakeAnswered{count: 34, list: []consultation.ConsultationWithUpdateInfo{info("c1"), info("c2", "unknown")}}
	uc := answeredUseCase(newApp(0, false), repo, newThemes())
	got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "userId", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Consultations) != 1 || got.Consultations[0].ID != "c1" || got.MaxPageNumber != 2 {
		t.Fatalf("%+v", got)
	}
	if !reflect.DeepEqual(repo.lists, []string{"userId/0"}) {
		t.Errorf("%v", repo.lists)
	}
}

func TestAnsweredOffsetAndMaxPageNumber(t *testing.T) {
	cases := []struct {
		count, page, offset, maxPage int
		exists                       bool
	}{
		{1, 1, 0, 1, true}, {21, 1, 0, 2, true}, {39, 2, 20, 2, true}, {80, 3, 40, 4, true},
		{10, 7, 120, 1, false}, {20, 2, 20, 1, true}, {20, 3, 40, 1, false}, {0, 1, 0, 0, true}, {0, 2, 20, 0, false},
		{5, 107374183, 2147483640, 1, false}, // 107374182 * 20 = 2147483640
		{5, 107374184, -2147483636, 1, true}, // 107374183 * 20 overflows: the page exists
		{5, 2147483647, -40, 1, true},
	}
	for _, tc := range cases {
		repo := &fakeAnswered{count: tc.count, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
		uc := answeredUseCase(newApp(0, false), repo, newThemes())
		got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "u", tc.page)
		if err != nil || (got != nil) != tc.exists {
			t.Errorf("count %d page %d: got %v (%v), exists %v", tc.count, tc.page, got, err, tc.exists)
			continue
		}
		if got != nil {
			if got.MaxPageNumber != tc.maxPage {
				t.Errorf("count %d page %d: maxPageNumber %d, want %d", tc.count, tc.page, got.MaxPageNumber, tc.maxPage)
			}
			if tc.count > 0 && repo.lists[0] != fmt.Sprintf("u/%d", tc.offset) {
				t.Errorf("count %d page %d: %v, want offset %d", tc.count, tc.page, repo.lists, tc.offset)
			}
		}
	}
}

// Every page of a user shows the same consultations: one load serves them all.
func TestAnsweredPagesShareOneLoad(t *testing.T) {
	repo := &fakeAnswered{count: 45, list: []consultation.ConsultationWithUpdateInfo{info("c1"), info("c2")}}
	themes := newThemes()
	uc := answeredUseCase(newApp(0, false), repo, themes)
	ctx := context.Background()
	p3, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 3)
	p1, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1)
	p4, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 4) // offset 60 > 45
	if p3 == nil || p3 != p1 || p4 != nil || p3.MaxPageNumber != 3 {
		t.Fatalf("%v %v %v", p3, p1, p4)
	}
	if repo.counts != 1 || len(repo.lists) != 1 || themes.calls.Load() != 1 {
		t.Errorf("count %d lists %v thematiques %d", repo.counts, repo.lists, themes.calls.Load())
	}
}

// A user without answers: page 1 exists and is empty, page 2 does not; Strapi is not asked for the list (the
// thematiques are still read, as in Kotlin).
func TestAnsweredWithoutAnswer(t *testing.T) {
	repo, themes := &fakeAnswered{}, newThemes()
	uc := answeredUseCase(newApp(0, false), repo, themes)
	got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "u", 1)
	if err != nil || got == nil || len(got.Consultations) != 0 || got.MaxPageNumber != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if len(repo.lists) != 0 || themes.calls.Load() != 1 {
		t.Errorf("lists %v thematiques %d", repo.lists, themes.calls.Load())
	}
	if p2, _ := uc.GetConsultationAnsweredPaginatedList(context.Background(), "u", 2); p2 != nil {
		t.Errorf("%v", p2)
	}
}

// The eviction after an answer: the next read counts again and lists again, on a new entry.
func TestAnsweredEvictionGivesAFreshList(t *testing.T) {
	a := newApp(0, false)
	repo := &fakeAnswered{count: 1, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	uc := answeredUseCase(a, repo, newThemes())
	s := &Service{a: a}
	ctx := context.Background()
	before, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1)
	if len(before.Consultations) != 1 || before.MaxPageNumber != 1 {
		t.Fatalf("%+v", before)
	}
	// the user answers a second consultation
	repo.mu.Lock()
	repo.count, repo.list = 2, []consultation.ConsultationWithUpdateInfo{info("c1"), info("c2")}
	repo.mu.Unlock()
	if stale, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1); len(stale.Consultations) != 1 {
		t.Fatalf("expected the cached page before the eviction: %+v", stale)
	}
	s.EvictAnswered(ctx, "u")
	after, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1)
	if len(after.Consultations) != 2 || after.MaxPageNumber != 1 {
		t.Fatalf("%+v", after)
	}
	// another user's entry is untouched
	other, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "v", 1)
	s.EvictAnswered(ctx, "u")
	again, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "v", 1)
	if again != other {
		t.Error("the eviction of a user dropped another user's entry")
	}
}

// A load that overlaps the eviction is not stored: a read that started before the answer cannot
// re-fill the cache with the old count after the eviction.
func TestAnsweredLoadOverlappingAnEvictionIsNotStored(t *testing.T) {
	a := newApp(0, false)
	started, release := make(chan struct{}), make(chan struct{})
	repo := &blockingAnswered{fakeAnswered: fakeAnswered{count: 1, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}, started: started, release: release}
	uc := answeredUseCase(a, repo, newThemes())
	s := &Service{a: a}
	ctx := context.Background()
	done := make(chan *AnsweredList)
	go func() {
		got, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1)
		done <- got
	}()
	<-started // the count of the old state is being read
	repo.mu.Lock()
	repo.count = 21 // two pages now
	repo.mu.Unlock()
	s.EvictAnswered(ctx, "u")
	close(release)
	if old := <-done; old == nil {
		t.Fatal("the overlapping request must still be answered")
	}
	fresh, _ := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1)
	if fresh == nil || fresh.MaxPageNumber != 2 || repo.counts < 2 {
		t.Fatalf("the old load was stored: %+v, count calls %d", fresh, repo.counts)
	}
}

type blockingAnswered struct {
	fakeAnswered
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingAnswered) GetConsultationAnsweredCount(ctx context.Context, userID string) (int, error) {
	n, err := b.fakeAnswered.GetConsultationAnsweredCount(ctx, userID)
	b.once.Do(func() {
		close(b.started)
		<-b.release
	})
	return n, err
}

func TestAnsweredErrorsAreNotCached(t *testing.T) {
	repo := &fakeAnswered{count: 1, list: []consultation.ConsultationWithUpdateInfo{info("c1")}, countErr: errors.New("db down")}
	uc := answeredUseCase(newApp(0, false), repo, newThemes())
	ctx := context.Background()
	if got, err := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1); err == nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	repo.countErr = nil
	repo.listErr = errors.New("db down")
	if got, err := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1); err == nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	repo.listErr = nil
	if got, err := uc.GetConsultationAnsweredPaginatedList(ctx, "u", 1); err != nil || got == nil || len(got.Consultations) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestAnsweredConcurrentReadersShareTheLoad(t *testing.T) {
	repo := &fakeAnswered{count: 5, list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	themes := newThemes()
	uc := answeredUseCase(newApp(0, false), repo, themes)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := uc.GetConsultationAnsweredPaginatedList(context.Background(), "u", 1); err != nil || got == nil {
				t.Errorf("%v %v", got, err)
			}
		}()
	}
	wg.Wait()
	if repo.counts != 1 || len(repo.lists) != 1 {
		t.Errorf("count %d lists %v", repo.counts, repo.lists)
	}
}

// ---------------------------------------------------------------------------
// ConsultationsByUserPreferencesUseCase
// ---------------------------------------------------------------------------

type fakeProfiles struct {
	calls atomic.Int32
	err   error
}

func (f *fakeProfiles) GetProfile(context.Context, string) (*profile.Profile, error) {
	f.calls.Add(1)
	return nil, f.err
}

func TestPreferencesReadsTheProfileAndShares(t *testing.T) {
	repo := &fakeFinished{list: []consultation.ConsultationWithUpdateInfo{info("c1"), info("c2", "unknown")}}
	themes, profiles := newThemes(), &fakeProfiles{}
	uc := &PreferencesUseCase{a: newApp(time.Minute, false), repo: repo, themes: themes, profiles: profiles}
	for i := 0; i < 3; i++ {
		list, err := uc.Execute(context.Background(), "u")
		if err != nil || len(list) != 1 || list[0].ID != "c1" {
			t.Fatalf("%v %v", list, err)
		}
	}
	if profiles.calls.Load() != 3 {
		t.Errorf("the profile is read at every request: %d", profiles.calls.Load())
	}
	if repo.byTerr != 1 || len(repo.lastTer) != 0 {
		t.Errorf("shared list: %d calls, territories %v (Territoire.of(profile) is empty)", repo.byTerr, repo.lastTer)
	}
}

func TestPreferencesDoesNotKeepAnEmptyList(t *testing.T) {
	repo := &fakeFinished{}
	uc := &PreferencesUseCase{a: newApp(time.Minute, false), repo: repo, themes: newThemes(), profiles: &fakeProfiles{}}
	for i := 0; i < 3; i++ {
		if list, err := uc.Execute(context.Background(), "u"); err != nil || len(list) != 0 {
			t.Fatalf("%v %v", list, err)
		}
	}
	if repo.byTerr != 3 {
		t.Errorf("%d", repo.byTerr)
	}
	// without a TTL nothing is shared
	repo = &fakeFinished{list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	uc = &PreferencesUseCase{a: newApp(0, false), repo: repo, themes: newThemes(), profiles: &fakeProfiles{}}
	uc.Execute(context.Background(), "u")
	uc.Execute(context.Background(), "u")
	if repo.byTerr != 2 {
		t.Errorf("%d", repo.byTerr)
	}
}

func TestPreferencesProfileErrorIsAnException(t *testing.T) {
	repo := &fakeFinished{list: []consultation.ConsultationWithUpdateInfo{info("c1")}}
	uc := &PreferencesUseCase{a: newApp(time.Minute, false), repo: repo, themes: newThemes(), profiles: &fakeProfiles{err: errors.New("boom")}}
	if list, err := uc.Execute(context.Background(), "u"); err == nil || list != nil || repo.byTerr != 0 {
		t.Fatalf("%v %v %d", list, err, repo.byTerr)
	}
}

// ---------------------------------------------------------------------------
// ConsultationPaginatedJsonMapper
// ---------------------------------------------------------------------------

func TestToJSON(t *testing.T) {
	a := newApp(0, false)
	label := "Merci !"
	list := []consultation.ConsultationPreviewFinished{
		{ID: "c1", Slug: "s1", Title: "T1", CoverURL: "u1", Thematique: theme1, UpdateLabel: &label, LastUpdateDate: ldt(2026, 5, 20, 8, 30), EndDate: ldt(2026, 5, 1, 0, 0), Territory: "France"},
		// the update label is only shown from the update date to 90 days later
		{ID: "c2", Slug: "s2", Title: "T2", CoverURL: "u2", Thematique: theme1, UpdateLabel: &label, LastUpdateDate: ldt(2026, 1, 20, 8, 30), EndDate: ldt(2026, 7, 1, 0, 0), Territory: "Nord"},
		{ID: "c3", Slug: "s3", Title: "T3", CoverURL: "u3", Thematique: theme1, LastUpdateDate: ldt(2026, 5, 20, 8, 30), EndDate: ldt(2026, 5, 1, 0, 0), Territory: "France"},
	}
	got := jsonjava.MarshalString(toJSON(a, 4, list))
	want := `{"maxPageNumber":4,"consultations":[` +
		`{"id":"c1","slug":"s1","title":"T1","coverUrl":"u1","thematique":{"label":"Santé","picto":"S"},"step":2,"updateLabel":"Merci !","updateDate":"2026-05-20 08:30:00","territory":"France"},` +
		`{"id":"c2","slug":"s2","title":"T2","coverUrl":"u2","thematique":{"label":"Santé","picto":"S"},"step":1,"updateLabel":null,"updateDate":"2026-01-20 08:30:00","territory":"Nord"},` +
		`{"id":"c3","slug":"s3","title":"T3","coverUrl":"u3","thematique":{"label":"Santé","picto":"S"},"step":2,"updateLabel":null,"updateDate":"2026-05-20 08:30:00","territory":"France"}]}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := jsonjava.MarshalString(toJSON(a, 0, nil)); got != `{"maxPageNumber":0,"consultations":[]}` {
		t.Errorf("%s", got)
	}
	if (ConsultationPaginatedJSON{}).JavaName() != "ConsultationPaginatedJson" {
		t.Error("java name")
	}
}

func TestKotlinArithmetic(t *testing.T) {
	if got := kotlinOffset(1, 100); got != 0 {
		t.Errorf("%d", got)
	}
	if got := kotlinOffset(21474838, 100); got != -2147483596 {
		t.Errorf("%d", got)
	}
	if got := kotlinOffset(2147483647, 20); got != -40 {
		t.Errorf("%d", got)
	}
	for _, tc := range [][3]int{{0, 100, 0}, {1, 100, 1}, {100, 100, 1}, {101, 100, 2}, {2147483647, 20, 107374183}, {-5, 100, 0}} {
		if got := maxPageNumber(tc[0], tc[1]); got != tc[2] {
			t.Errorf("maxPageNumber(%d, %d) = %d, want %d", tc[0], tc[1], got, tc[2])
		}
	}
}
