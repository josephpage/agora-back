package qag

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/modules/login"
	"agora/internal/modules/thematique"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// SupportQagQueueTest, AgoraQueue
// ---------------------------------------------------------------------------

func TestQueueExecutesTheTask(t *testing.T) {
	var q agoraQueue[supportTask]
	got := executeTask(&q, supportTask{add: true, userID: "userId"}, func() bool { time.Sleep(20 * time.Millisecond); return true }, func() bool { return false })
	if !got {
		t.Fatal("onTaskExecuted result expected")
	}
}

func TestQueueIdenticalTasksRunOnceThenAreRejected(t *testing.T) {
	var q agoraQueue[supportTask]
	task := supportTask{add: true, userID: "userId"}
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	var first bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = executeTask(&q, task, func() bool { close(started); <-release; return true }, func() bool { return false })
	}()
	<-started
	second := executeTask(&q, task, func() bool { return true }, func() bool { return false })
	close(release)
	wg.Wait()
	if !first || second {
		t.Fatalf("first=%v second=%v, want true/false", first, second)
	}
	// the lock is released once the first task is done
	if !executeTask(&q, task, func() bool { return true }, func() bool { return false }) {
		t.Fatal("task rejected after the previous one finished")
	}
}

func TestQueueDifferentTasksRunTogether(t *testing.T) {
	var q agoraQueue[supportTask]
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	var first bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = executeTask(&q, supportTask{add: true, userID: "userId1"}, func() bool { close(started); <-release; return true }, func() bool { return false })
	}()
	<-started
	otherUser := executeTask(&q, supportTask{add: true, userID: "userId2"}, func() bool { return true }, func() bool { return false })
	otherType := executeTask(&q, supportTask{add: false, userID: "userId1"}, func() bool { return true }, func() bool { return false })
	close(release)
	wg.Wait()
	if !first || !otherUser || !otherType {
		t.Fatalf("first=%v otherUser=%v otherType=%v", first, otherUser, otherType)
	}
}

// B-AGORAQUEUE: Kotlin never removed the task when the action threw.
func TestQueueLockIsReleasedAfterAPanic(t *testing.T) {
	var q agoraQueue[feedbackTask]
	task := feedbackTask{userID: "u"}
	func() {
		defer func() { _ = recover() }()
		executeTask(&q, task, func() bool { panic("boom") }, func() bool { return false })
	}()
	if !executeTask(&q, task, func() bool { return true }, func() bool { return false }) {
		t.Fatal("the user stayed locked after an exception")
	}
}

// ---------------------------------------------------------------------------
// AdminUpdateQagStatusUseCaseTest
// ---------------------------------------------------------------------------

type fakeStatusRepo struct {
	info      *QagInfo
	update    QagUpdateResult
	updateErr error
	calls     []string
}

func (f *fakeStatusRepo) GetQagInfo(_ context.Context, id string) (*QagInfo, error) {
	f.calls = append(f.calls, "get:"+id)
	return f.info, nil
}

func (f *fakeStatusRepo) UpdateQagStatus(_ context.Context, id string, s QagStatus) (QagUpdateResult, error) {
	f.calls = append(f.calls, "update:"+id+":"+s.String())
	return f.update, f.updateErr
}

func TestAdminUpdateQagStatus(t *testing.T) {
	info := &QagInfo{ID: "qag-uuid-1234", ThematiqueID: "th", Title: "Ma question", Status: StatusOpen, UserID: "user-uuid"}
	t.Run("qag not found", func(t *testing.T) {
		repo := &fakeStatusRepo{}
		got, err := (&AdminUpdateQagStatusUseCase{qags: repo}).UpdateQagStatus(ctx, "qag-uuid-1234", StatusModeratedAccepted)
		if err != nil || got != AdminUpdateNotFound {
			t.Fatalf("got %v %v", got, err)
		}
		if !reflect.DeepEqual(repo.calls, []string{"get:qag-uuid-1234"}) {
			t.Fatalf("calls %v", repo.calls)
		}
	})
	for _, s := range []QagStatus{StatusModeratedAccepted, StatusModeratedRejected, StatusSelectedForResponse, StatusArchived} {
		t.Run("success "+s.String(), func(t *testing.T) {
			updated := *info
			updated.Status = s
			repo := &fakeStatusRepo{info: info, update: QagUpdateResult{Info: &updated}}
			got, err := (&AdminUpdateQagStatusUseCase{qags: repo}).UpdateQagStatus(ctx, "qag-uuid-1234", s)
			if err != nil || got != AdminUpdateSuccess {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
	t.Run("update fails", func(t *testing.T) {
		repo := &fakeStatusRepo{info: info}
		got, _ := (&AdminUpdateQagStatusUseCase{qags: repo}).UpdateQagStatus(ctx, "qag-uuid-1234", StatusModeratedAccepted)
		if got != AdminUpdateFailure {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("the update uses the id of the stored QaG", func(t *testing.T) {
		stored := *info
		stored.ID = "canonical-id"
		repo := &fakeStatusRepo{info: &stored, update: QagUpdateResult{Info: &stored}}
		_, _ = (&AdminUpdateQagStatusUseCase{qags: repo}).UpdateQagStatus(ctx, "CANONICAL-ID", StatusArchived)
		if repo.calls[1] != "update:canonical-id:ARCHIVED" {
			t.Fatalf("calls %v", repo.calls)
		}
	})
}

// ---------------------------------------------------------------------------
// GetAskQagStatusUseCaseTest
// ---------------------------------------------------------------------------

type fakeLastQag struct{ info *QagInfo }

func (f fakeLastQag) GetUserLastQagInfo(context.Context, string) (*QagInfo, error) {
	return f.info, nil
}

func TestGetAskQagStatus(t *testing.T) {
	ldt := func(m time.Month, d, h, mi, s int) time.Time { return time.Date(2024, m, d, h, mi, s, 0, time.Local) }
	for _, c := range []struct {
		name         string
		post, server time.Time
		want         AskQagStatus
	}{
		{"qagPostDate < serverDate < resetDate, same week", ldt(1, 1, 0, 0, 0), ldt(1, 1, 12, 30, 0), AskEnabled},
		{"qagPostDate < resetDate < serverDate, same week", ldt(1, 1, 0, 0, 0), ldt(1, 5, 12, 30, 0), AskEnabled},
		{"across a week", ldt(1, 1, 22, 0, 0), ldt(1, 8, 12, 30, 0), AskEnabled},
		{"one second before the weekly reset, server at the reset", ldt(1, 1, 13, 59, 59), ldt(1, 8, 14, 0, 0), AskEnabled},
		{"post date = server date, random time", ldt(1, 1, 12, 30, 0), ldt(1, 1, 12, 30, 0), AskWeeklyLimitReached},
		{"post date = server date = weekly reset", ldt(1, 1, 14, 0, 0), ldt(1, 1, 14, 0, 0), AskWeeklyLimitReached},
		{"post date more than a week after the server date", ldt(1, 8, 14, 0, 1), ldt(1, 1, 14, 5, 0), AskEnabled},
		{"post date after the server date but within a week", ldt(1, 2, 12, 0, 0), ldt(1, 1, 14, 0, 0), AskWeeklyLimitReached},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := &GetAskQagStatusUseCase{repo: fakeLastQag{&QagInfo{Date: c.post}}, now: func() time.Time { return c.server }}
			got, err := u.GetAskQagStatus(ctx, "userId")
			if err != nil || got != c.want {
				t.Fatalf("got %v %v, want %v", got, err, c.want)
			}
		})
	}
	t.Run("no QaG yet", func(t *testing.T) {
		u := &GetAskQagStatusUseCase{repo: fakeLastQag{}, now: time.Now}
		if got, _ := u.GetAskQagStatus(ctx, "u"); got != AskEnabled {
			t.Fatalf("got %v", got)
		}
	})
}

// The Monday bound keeps the nanoseconds of "now" (withSecond(0) does not reset them).
func TestAskQagStatusMondayBoundKeepsTheClockFraction(t *testing.T) {
	now := time.Date(2024, 1, 1, 10, 0, 30, 700_000_000, time.Local) // Monday 10:00:30.7
	post := time.Date(2024, 1, 1, 10, 0, 0, 100_000_000, time.Local) // Monday 10:00:00.1: before 10:00:00.7
	if isDateWithinTheWeek(post, now) {
		t.Fatal("a QaG posted at 10:00:00.1 is before the bound 10:00:00.7")
	}
	if !isDateWithinTheWeek(post.Add(time.Second), now) {
		t.Fatal("10:00:01.1 is after the bound")
	}
}

func TestGetQagErrorText(t *testing.T) {
	msgs := fakeMessages{}
	for _, c := range []struct {
		status AskQagStatus
		want   *string
	}{{AskEnabled, nil}, {AskFeatureDisabled, ptr("disabled")}, {AskWeeklyLimitReached, ptr("one by week")}} {
		u := &GetQagErrorTextUseCase{messages: msgs, status: &GetAskQagStatusUseCase{repo: fakeLastQag{}, now: time.Now}}
		// drive the status through the repository: weekly limit = a QaG posted now
		switch c.status {
		case AskWeeklyLimitReached:
			u.status.repo = fakeLastQag{&QagInfo{Date: time.Now()}}
		case AskFeatureDisabled:
			// never returned by GetAskQagStatus: exercise the mapping directly
			if got := msgs.QagDisabledErrorMessage(); got != *c.want {
				t.Fatal(got)
			}
			continue
		}
		got, err := u.GetQagErrorText(ctx, "u")
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("status %v: got %v %v, want %v", c.status, got, err, c.want)
		}
	}
}

type fakeMessages struct{}

func (fakeMessages) QagDisabledErrorMessage() string  { return "disabled" }
func (fakeMessages) QagErrorMessageOneByWeek() string { return "one by week" }

// ---------------------------------------------------------------------------
// InsertQagUseCaseTest, ContentSanitizerTest
// ---------------------------------------------------------------------------

type fakeInserter struct {
	got    QagInserting
	result QagInsertionResult
}

func (f *fakeInserter) InsertQagInfo(_ context.Context, q QagInserting) (QagInsertionResult, error) {
	f.got = q
	return f.result, nil
}

type fakeSupportInserter struct{ got []SupportQagInserting }

func (f *fakeSupportInserter) InsertSupportQagUnchecked(_ context.Context, s SupportQagInserting) (SupportQagResult, error) {
	f.got = append(f.got, s)
	return SupportSuccess, nil
}

func TestInsertQag(t *testing.T) {
	in := QagInserting{ThematiqueID: "thematiqueId", Title: "title", Description: "description", Date: time.Unix(0, 0), Status: StatusArchived, Username: "username", UserID: "userId"}
	sanitized := func(content string, max int) string {
		return map[string]string{"title|200": "sanitizedTitle", "description|400": "sanitizedDescription", "username|50": "sanitizedUsername"}[content+"|"+itoa(max)]
	}
	want := in
	want.Title, want.Description, want.Username = "sanitizedTitle", "sanitizedDescription", "sanitizedUsername"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("insert failed", func(t *testing.T) {
		q, s := &fakeInserter{}, &fakeSupportInserter{}
		got, err := (&InsertQagUseCase{sanitize: sanitized, qags: q, supports: s, log: log}).InsertQag(ctx, in)
		if err != nil || got.Success() || q.got != want || len(s.got) != 0 {
			t.Fatalf("got %+v %v; inserted %+v; supports %v", got, err, q.got, s.got)
		}
	})
	t.Run("insert success adds the author's support", func(t *testing.T) {
		q, s := &fakeInserter{result: QagInsertionResult{Info: &QagInfo{ID: "qagId"}}}, &fakeSupportInserter{}
		got, err := (&InsertQagUseCase{sanitize: sanitized, qags: q, supports: s, log: log}).InsertQag(ctx, in)
		if err != nil || !got.Success() || q.got != want {
			t.Fatalf("got %+v %v; inserted %+v", got, err, q.got)
		}
		if !reflect.DeepEqual(s.got, []SupportQagInserting{{QagID: "qagId", UserID: "userId"}}) {
			t.Fatalf("supports %v", s.got)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func TestContentSanitizer(t *testing.T) {
	for _, c := range []struct {
		name, in string
		max      int
		want     string
	}{
		{"plain text", "Coucou, ça va ?", 100, "Coucou, ça va ?"},
		{"special characters are kept", "Est-ce que (42 * 5) + 1337 >= 9000 ?!", 100, "Est-ce que (42 * 5) + 1337 >= 9000 ?!"},
		{"HTML is removed", `<a href="http://monsupersite.com">Click ici !</a>`, 100, "Click ici !"},
		{"cut to maxLength", "1234567890", 3, "123"},
		{"cut after htmlUnescape", "You&#39;re welcome !", 16, "You're welcome !"},
	} {
		if got := defaultSanitize(c.in, c.max); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// GetQagByKeywordsUseCaseTest, QagPreviewMapperTest
// ---------------------------------------------------------------------------

type fakeSearch struct {
	list []QagInfoWithSupportCount
	got  []string
}

func (f *fakeSearch) GetQagByKeywordsList(_ context.Context, keywords []string) ([]QagInfoWithSupportCount, error) {
	f.got = keywords
	return f.list, nil
}

type fakeThemes map[string]thematique.Thematique

func (f fakeThemes) ByID(_ context.Context, id string) *thematique.Thematique {
	if th, ok := f[id]; ok {
		return &th
	}
	return nil
}

type fakeSupported struct {
	ids   []string
	calls int
}

func (f *fakeSupported) GetUserSupportedQagIDs(context.Context, string) ([]string, error) {
	f.calls++
	return f.ids, nil
}

func TestGetQagByKeywords(t *testing.T) {
	th := thematique.Thematique{ID: "thematiqueId", Label: "label", Picto: "picto"}
	t.Run("no result", func(t *testing.T) {
		sup := &fakeSupported{}
		u := &GetQagByKeywordsUseCase{qags: &fakeSearch{}, themes: fakeThemes{}, supported: sup}
		got, err := u.GetQagByKeywords(ctx, "userId", []string{"keywords"})
		if err != nil || len(got) != 0 || got == nil || sup.calls != 0 {
			t.Fatalf("got %v %v calls %d", got, err, sup.calls)
		}
	})
	t.Run("unknown thematique: dropped, supports never read", func(t *testing.T) {
		sup := &fakeSupported{}
		u := &GetQagByKeywordsUseCase{qags: &fakeSearch{list: []QagInfoWithSupportCount{{ID: "q", ThematiqueID: "other"}}}, themes: fakeThemes{}, supported: sup}
		got, _ := u.GetQagByKeywords(ctx, "userId", []string{"k"})
		if len(got) != 0 || sup.calls != 0 {
			t.Fatalf("got %v calls %d", got, sup.calls)
		}
	})
	t.Run("mapped, supports read once", func(t *testing.T) {
		sup := &fakeSupported{ids: []string{"qagId"}}
		list := []QagInfoWithSupportCount{
			{ID: "qagId", UserID: "userId", ThematiqueID: "thematiqueId", Status: StatusModeratedAccepted, SupportCount: 3},
			{ID: "other", UserID: "someone", ThematiqueID: "thematiqueId", Status: StatusSelectedForResponse},
			{ID: "dropped", ThematiqueID: "unknown"},
		}
		u := &GetQagByKeywordsUseCase{qags: &fakeSearch{list: list}, themes: fakeThemes{"thematiqueId": th}, supported: sup}
		got, err := u.GetQagByKeywords(ctx, "userId", []string{"k"})
		if err != nil || sup.calls != 1 || len(got) != 2 {
			t.Fatalf("got %+v %v calls %d", got, err, sup.calls)
		}
		if !got[0].IsSupportedByUser || !got[0].IsAuthor || !got[0].CanShare || got[0].SupportCount != 3 || got[0].Thematique != th {
			t.Fatalf("first %+v", got[0])
		}
		if got[1].IsSupportedByUser || got[1].IsAuthor || !got[1].CanShare {
			t.Fatalf("second %+v", got[1])
		}
	})
}

func TestPreviewCanShare(t *testing.T) {
	for _, c := range []struct {
		s    QagStatus
		want bool
	}{{StatusModeratedAccepted, true}, {StatusSelectedForResponse, true}, {StatusOpen, false}, {StatusModeratedRejected, false}, {StatusArchived, false}} {
		if got := ToPreview(QagInfoWithSupportCount{Status: c.s, SupportCount: 10}, Thematique{ID: "t"}, false, false).CanShare; got != c.want {
			t.Errorf("%v: canShare %v", c.s, got)
		}
	}
}

// ---------------------------------------------------------------------------
// GetQagDetailsUseCaseTest, GetPublicQagDetailsUseCaseTest
// ---------------------------------------------------------------------------

type fakeAggregateSource struct {
	info   *QagInfoWithSupportCount
	themes fakeThemes
	resp   *ResponseQag
}

func (f fakeAggregateSource) GetQagWithSupportCountCached(context.Context, string) (*QagInfoWithSupportCount, error) {
	return f.info, nil
}

type fakeResponses struct {
	r     *ResponseQag
	calls int
}

func (f *fakeResponses) getResponseQag(context.Context, string) *ResponseQag { f.calls++; return f.r }

type fakeFeedbacks struct {
	results      *FeedbackResults
	resultsCalls int
	answer       *bool
	answerCalls  int
}

func (f *fakeFeedbacks) GetFeedbackResults(context.Context, string) (*FeedbackResults, error) {
	f.resultsCalls++
	return f.results, nil
}

func (f *fakeFeedbacks) GetFeedbackForQagAndUser(context.Context, string, string) (*bool, error) {
	f.answerCalls++
	return f.answer, nil
}

type fakeSupports struct {
	supported bool
	calls     int
}

func (f *fakeSupports) isQagSupportedByUser(context.Context, string, string) (bool, error) {
	f.calls++
	return f.supported, nil
}

var thematiqueTest = thematique.Thematique{ID: "th", Label: "Label", Picto: "P"}

func newDetails(info *QagInfoWithSupportCount, resp *ResponseQag, fb *fakeFeedbacks, sup *fakeSupports) (*GetQagDetailsUseCase, *fakeResponses) {
	rr := &fakeResponses{r: resp}
	agg := &DetailsAggregate{info: fakeAggregateSource{info: info}, responses: rr, feedbacks: fb, themes: fakeThemes{"th": thematiqueTest}}
	return &GetQagDetailsUseCase{aggregate: agg, supports: sup, feedbacks: fb}, rr
}

func qagInfo(status QagStatus, userID string) *QagInfoWithSupportCount {
	return &QagInfoWithSupportCount{ID: "qagId", ThematiqueID: "th", Title: "t", Status: status, UserID: userID, SupportCount: 4, Date: time.Unix(1000, 0)}
}

func TestGetQagDetailsNotFoundAndStatuses(t *testing.T) {
	for _, c := range []struct {
		name string
		info *QagInfoWithSupportCount
		want QagResultKind
	}{
		{"no QaG", nil, QagResultNotFound},
		{"archived", qagInfo(StatusArchived, "u"), QagResultNotFound},
		{"rejected", qagInfo(StatusModeratedRejected, "u"), QagResultRejectedStatus},
		{"open but not the author", qagInfo(StatusOpen, "another"), QagResultNotFound},
		{"unknown thematique", &QagInfoWithSupportCount{ID: "qagId", ThematiqueID: "unknown", Status: StatusModeratedAccepted}, QagResultNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			fb, sup := &fakeFeedbacks{}, &fakeSupports{}
			u, _ := newDetails(c.info, nil, fb, sup)
			got, err := u.GetQagDetails(ctx, "qagId", "userId")
			if err != nil || got.Kind != c.want {
				t.Fatalf("got %v %v want %v", got.Kind, err, c.want)
			}
			if fb.answerCalls != 0 || sup.calls != 0 {
				t.Fatalf("no user data must be read: feedback %d supports %d", fb.answerCalls, sup.calls)
			}
		})
	}
}

func TestGetQagDetailsSuccess(t *testing.T) {
	t.Run("open, author", func(t *testing.T) {
		u, _ := newDetails(qagInfo(StatusOpen, "userId"), nil, &fakeFeedbacks{}, &fakeSupports{})
		got, _ := u.GetQagDetails(ctx, "qagId", "userId")
		w := got.Qag
		if got.Kind != QagResultSuccess || w.CanShare || !w.CanSupport || !w.CanDelete || !w.IsAuthor || w.IsSupportedByUser || w.IsHelpful != nil {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("accepted, other user, not supported", func(t *testing.T) {
		u, _ := newDetails(qagInfo(StatusModeratedAccepted, "another"), nil, &fakeFeedbacks{}, &fakeSupports{})
		w := mustSuccess(t, u, "userId")
		if !w.CanShare || !w.CanSupport || w.CanDelete || w.IsAuthor || w.IsSupportedByUser {
			t.Fatalf("%+v", w)
		}
	})
	t.Run("accepted, author, supported", func(t *testing.T) {
		u, _ := newDetails(qagInfo(StatusModeratedAccepted, "userId"), nil, &fakeFeedbacks{}, &fakeSupports{supported: true})
		w := mustSuccess(t, u, "userId")
		if !w.CanShare || !w.CanSupport || !w.CanDelete || !w.IsAuthor || !w.IsSupportedByUser {
			t.Fatalf("%+v", w)
		}
	})
	t.Run("selected, no feedback: results removed, supports not read", func(t *testing.T) {
		fb, sup := &fakeFeedbacks{results: &FeedbackResults{PositiveRatio: 50, NegativeRatio: 50, Count: 2}}, &fakeSupports{}
		u, _ := newDetails(qagInfo(StatusSelectedForResponse, "another"), &ResponseQag{Text: &ResponseQagText{}}, fb, sup)
		w := mustSuccess(t, u, "userId")
		if !w.CanShare || w.CanSupport || w.CanDelete || w.IsAuthor || !w.IsSupportedByUser || w.IsHelpful != nil || w.QagDetails.FeedbackResults != nil || sup.calls != 0 {
			t.Fatalf("%+v", w)
		}
	})
	t.Run("selected, feedback given: results kept", func(t *testing.T) {
		res := &FeedbackResults{PositiveRatio: 50, NegativeRatio: 50, Count: 2}
		fb := &fakeFeedbacks{results: res, answer: ptr(false)}
		u, _ := newDetails(qagInfo(StatusSelectedForResponse, "userId"), &ResponseQag{Video: &ResponseQagVideo{}}, fb, &fakeSupports{})
		w := mustSuccess(t, u, "userId")
		if w.IsHelpful == nil || *w.IsHelpful || w.QagDetails.FeedbackResults != res || w.CanDelete || !w.IsAuthor {
			t.Fatalf("%+v", w)
		}
	})
	t.Run("the response is only read for a selected QaG", func(t *testing.T) {
		u, rr := newDetails(qagInfo(StatusModeratedAccepted, "u"), &ResponseQag{Text: &ResponseQagText{}}, &fakeFeedbacks{}, &fakeSupports{})
		mustSuccess(t, u, "userId")
		if rr.calls != 0 {
			t.Fatal("Strapi must not be called")
		}
	})
}

func mustSuccess(t *testing.T, u *GetQagDetailsUseCase, userID string) QagWithUserData {
	t.Helper()
	got, err := u.GetQagDetails(ctx, "qagId", userID)
	if err != nil || got.Kind != QagResultSuccess {
		t.Fatalf("got %v %v", got.Kind, err)
	}
	return got.Qag
}

func TestGetPublicQagDetails(t *testing.T) {
	for _, c := range []struct {
		status QagStatus
		want   bool
	}{{StatusOpen, false}, {StatusArchived, false}, {StatusModeratedRejected, false}, {StatusModeratedAccepted, true}, {StatusSelectedForResponse, true}} {
		agg := &DetailsAggregate{info: fakeAggregateSource{info: qagInfo(c.status, "u")}, responses: &fakeResponses{}, feedbacks: &fakeFeedbacks{}, themes: fakeThemes{"th": thematiqueTest}}
		got, err := (&GetPublicQagDetailsUseCase{aggregate: agg}).GetQagDetails(ctx, "qagId")
		if err != nil || (got != nil) != c.want {
			t.Errorf("%v: got %v %v", c.status, got, err)
		}
	}
	agg := &DetailsAggregate{info: fakeAggregateSource{}, responses: &fakeResponses{}, feedbacks: &fakeFeedbacks{}, themes: fakeThemes{}}
	if got, _ := (&GetPublicQagDetailsUseCase{aggregate: agg}).GetQagDetails(ctx, "x"); got != nil {
		t.Fatal("no QaG")
	}
}

func TestAggregateFeedbackResultsOnlyWithAResponse(t *testing.T) {
	fb := &fakeFeedbacks{results: &FeedbackResults{Count: 1}}
	agg := &DetailsAggregate{info: fakeAggregateSource{info: qagInfo(StatusSelectedForResponse, "u")}, responses: &fakeResponses{}, feedbacks: fb, themes: fakeThemes{"th": thematiqueTest}}
	d, _ := agg.GetQag(ctx, "qagId")
	if d == nil || d.Response != nil || d.FeedbackResults != nil || fb.resultsCalls != 0 {
		t.Fatalf("selected QaG without response: %+v feedback calls %d", d, fb.resultsCalls)
	}
	agg.responses = &fakeResponses{r: &ResponseQag{Text: &ResponseQagText{}}}
	d, _ = agg.GetQag(ctx, "qagId")
	if d.Response == nil || d.FeedbackResults == nil || fb.resultsCalls != 1 {
		t.Fatalf("selected QaG with response: %+v", d)
	}
}

// ---------------------------------------------------------------------------
// FeedbackQagUseCaseTest, InsertFeedbackQagUseCaseTest
// ---------------------------------------------------------------------------

type fakeFlags struct {
	enabled bool
	calls   int
}

func (f *fakeFlags) IsFeatureEnabled(context.Context, login.Feature) (bool, error) {
	f.calls++
	return f.enabled, nil
}

type fakeFeedbackRepo struct {
	previous       *bool
	total, helpful int
	insertResult   FeedbackQagResult
	updateResult   FeedbackQagResult
	calls          []string
	countCalls     int
	failCounts     bool
}

func (f *fakeFeedbackRepo) GetFeedbackResponseForUser(context.Context, string, string) (*bool, error) {
	f.calls = append(f.calls, "get")
	return f.previous, nil
}

func (f *fakeFeedbackRepo) feedbackCounts(context.Context, string) (int, int, error) {
	f.countCalls++
	if f.failCounts {
		return 0, 0, errors.New("boom")
	}
	return f.total, f.helpful, nil
}

func (f *fakeFeedbackRepo) InsertFeedbackQag(context.Context, FeedbackQagInserting) (FeedbackQagResult, error) {
	f.calls = append(f.calls, "insert")
	return f.insertResult, nil
}

func (f *fakeFeedbackRepo) UpdateFeedbackQag(_ context.Context, _, _ string, isHelpful bool) (FeedbackQagResult, error) {
	f.calls = append(f.calls, "update:"+map[bool]string{true: "yes", false: "no"}[isHelpful])
	return f.updateResult, nil
}

type fakeResultsStore struct {
	m       map[string]FeedbackResults
	loads   int
	evicted []string
}

func (f *fakeResultsStore) getOrLoad(_ context.Context, id string, load func() (FeedbackResults, error)) (FeedbackResults, error) {
	if v, ok := f.m[id]; ok {
		return v, nil
	}
	f.loads++
	v, err := load()
	if err == nil {
		if f.m == nil {
			f.m = map[string]FeedbackResults{}
		}
		f.m[id] = v
	}
	return v, err
}

func (f *fakeResultsStore) evict(_ context.Context, id string) {
	f.evicted = append(f.evicted, id)
	delete(f.m, id)
}

type fakeUserStore struct {
	m     map[string]*bool
	set_  []string
	loads int
}

func (f *fakeUserStore) getOrLoad(_ context.Context, u, q string, load func() (*bool, error)) (*bool, error) {
	if v, ok := f.m[u+"/"+q]; ok {
		return v, nil
	}
	f.loads++
	v, err := load()
	if f.m == nil {
		f.m = map[string]*bool{}
	}
	f.m[u+"/"+q] = v
	return v, err
}

func (f *fakeUserStore) set(_ context.Context, u, q string, answer bool) {
	f.set_ = append(f.set_, u+"/"+q+"="+map[bool]string{true: "yes", false: "no"}[answer])
	if f.m == nil {
		f.m = map[string]*bool{}
	}
	f.m[u+"/"+q] = &answer
}

func TestFeedbackResultsRatios(t *testing.T) {
	for _, c := range []struct {
		helpful, notHelpful, positive, negative int
	}{{0, 0, 0, 0}, {3, 1, 75, 25}, {2, 1, 67, 33}, {78, 22, 78, 22}, {1, 7, 13, 87}, {1, 0, 100, 0}, {0, 1, 0, 100}} {
		flags, rs := &fakeFlags{enabled: true}, &fakeResultsStore{}
		repo := &fakeFeedbackRepo{total: c.helpful + c.notHelpful, helpful: c.helpful}
		u := &FeedbackUseCase{flags: flags, repo: repo, results: rs, users: &fakeUserStore{}}
		got, err := u.GetFeedbackResults(ctx, "qagId")
		want := FeedbackResults{PositiveRatio: c.positive, NegativeRatio: c.negative, Count: c.helpful + c.notHelpful}
		if err != nil || got == nil || *got != want {
			t.Errorf("%d yes %d no: got %v %v, want %+v", c.helpful, c.notHelpful, got, err, want)
		}
	}
}

func TestFeedbackResultsFeatureDisabledAndCache(t *testing.T) {
	flags, rs, repo := &fakeFlags{}, &fakeResultsStore{}, &fakeFeedbackRepo{total: 2, helpful: 1}
	u := &FeedbackUseCase{flags: flags, repo: repo, results: rs, users: &fakeUserStore{}}
	if got, err := u.GetFeedbackResults(ctx, "q"); got != nil || err != nil || rs.loads != 0 || repo.countCalls != 0 {
		t.Fatalf("disabled: %v %v", got, err)
	}
	flags.enabled = true
	u.GetFeedbackResults(ctx, "q")
	u.GetFeedbackResults(ctx, "q")
	if repo.countCalls != 1 {
		t.Fatalf("the second call must be served by the cache (%d loads)", repo.countCalls)
	}
	repo.failCounts = true
	rs.evict(ctx, "q")
	if _, err := u.GetFeedbackResults(ctx, "q"); err == nil {
		t.Fatal("a repository error is an exception")
	}
}

func TestGetFeedbackForQagAndUser(t *testing.T) {
	repo := &fakeFeedbackRepo{previous: ptr(true)}
	us := &fakeUserStore{}
	u := &FeedbackUseCase{flags: &fakeFlags{}, repo: repo, results: &fakeResultsStore{}, users: us}
	for i := 0; i < 2; i++ {
		got, _ := u.GetFeedbackForQagAndUser(ctx, "qagId", "userId")
		if got == nil || !*got {
			t.Fatal(got)
		}
	}
	if us.loads != 1 || !reflect.DeepEqual(repo.calls, []string{"get"}) {
		t.Fatalf("loads %d calls %v", us.loads, repo.calls)
	}
	// a cached "not answered" is not queried again
	repo2 := &fakeFeedbackRepo{}
	u2 := &FeedbackUseCase{flags: &fakeFlags{}, repo: repo2, results: &fakeResultsStore{}, users: &fakeUserStore{}}
	u2.GetFeedbackForQagAndUser(ctx, "q", "u")
	if got, _ := u2.GetFeedbackForQagAndUser(ctx, "q", "u"); got != nil || len(repo2.calls) != 1 {
		t.Fatalf("got %v calls %v", got, repo2.calls)
	}
}

func TestInsertFeedbackQag(t *testing.T) {
	in := FeedbackQagInserting{QagID: "qagId", UserID: "userId", IsHelpful: false}
	mk := func(repo *fakeFeedbackRepo, enabled bool) (*FeedbackUseCase, *fakeResultsStore, *fakeUserStore, *fakeFlags) {
		flags, rs, us := &fakeFlags{enabled: enabled}, &fakeResultsStore{m: map[string]FeedbackResults{"qagId": {Count: 99}}}, &fakeUserStore{}
		return &FeedbackUseCase{flags: flags, repo: repo, results: rs, users: us}, rs, us, flags
	}
	t.Run("insert fails", func(t *testing.T) {
		repo := &fakeFeedbackRepo{insertResult: FeedbackFailure}
		u, rs, us, flags := mk(repo, true)
		got, res, err := u.InsertFeedbackQag(ctx, in)
		if err != nil || got != InsertFeedbackFailure || res != nil || flags.calls != 0 || len(us.set_) != 0 || len(rs.evicted) != 0 {
			t.Fatalf("%v %v %v", got, res, err)
		}
		if !reflect.DeepEqual(repo.calls, []string{"get", "insert"}) {
			t.Fatal(repo.calls)
		}
	})
	t.Run("success, feature disabled: caches untouched but the user's answer", func(t *testing.T) {
		repo := &fakeFeedbackRepo{insertResult: FeedbackSuccess}
		u, rs, us, flags := mk(repo, false)
		got, res, _ := u.InsertFeedbackQag(ctx, in)
		if got != InsertFeedbackSuccessDisabled || res != nil || flags.calls != 1 || len(rs.evicted) != 0 {
			t.Fatalf("%v %v flags %d evict %v", got, res, flags.calls, rs.evicted)
		}
		// class B: the NEW answer is cached (Kotlin cached the previous one, here none)
		if !reflect.DeepEqual(us.set_, []string{"userId/qagId=no"}) {
			t.Fatal(us.set_)
		}
	})
	t.Run("success, feature enabled: results evicted then recomputed", func(t *testing.T) {
		repo := &fakeFeedbackRepo{insertResult: FeedbackSuccess, total: 3, helpful: 1}
		u, rs, us, flags := mk(repo, true)
		got, res, err := u.InsertFeedbackQag(ctx, in)
		want := FeedbackResults{PositiveRatio: 33, NegativeRatio: 67, Count: 3}
		if err != nil || got != InsertFeedbackSuccess || res == nil || *res != want {
			t.Fatalf("%v %+v %v", got, res, err)
		}
		if !reflect.DeepEqual(rs.evicted, []string{"qagId"}) || flags.calls != 2 || len(us.set_) != 1 {
			t.Fatalf("evict %v flags %d user %v", rs.evicted, flags.calls, us.set_)
		}
	})
	t.Run("an existing feedback is updated", func(t *testing.T) {
		repo := &fakeFeedbackRepo{previous: ptr(true), updateResult: FeedbackSuccess}
		u, _, _, _ := mk(repo, false)
		got, _, _ := u.InsertFeedbackQag(ctx, in)
		if got != InsertFeedbackSuccessDisabled || !reflect.DeepEqual(repo.calls, []string{"get", "update:no"}) {
			t.Fatalf("%v %v", got, repo.calls)
		}
	})
	t.Run("update fails", func(t *testing.T) {
		repo := &fakeFeedbackRepo{previous: ptr(false), updateResult: FeedbackFailure}
		u, _, us, _ := mk(repo, true)
		if got, _, _ := u.InsertFeedbackQag(ctx, in); got != InsertFeedbackFailure || len(us.set_) != 0 {
			t.Fatal(got)
		}
	})
}

// ---------------------------------------------------------------------------
// Repositories without a database: the early returns of the Kotlin repositories
// (SupportQagInsertingMapperTest, QagInfoRepositoryImplTest "invalid UUID")
// ---------------------------------------------------------------------------

func TestRepositoriesRejectInvalidUUIDsWithoutQuerying(t *testing.T) {
	info := &InfoRepository{}
	supports := &SupportRepository{info: info}
	if got, err := info.GetQagInfo(ctx, "Invalid qag UUID"); got != nil || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.GetUserLastQagInfo(ctx, "Invalid user UUID"); got != nil || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.GetQagsInfo(ctx, []string{"x", "y"}); len(got) != 0 || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.GetSupportedQagsPaginatedV2(ctx, "x", 0, nil); len(got) != 0 || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.UpdateQagStatus(ctx, "Invalid QaG UUID", StatusModeratedAccepted); got.Success() || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.SelectQagForResponse(ctx, "Invalid QaG UUID"); got.Success() || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.DeleteQag(ctx, "Invalid qag UUID"); got.Success() || err != nil {
		t.Fatal(got, err)
	}
	if got, err := info.InsertQagInfo(ctx, QagInserting{UserID: "not a uuid"}); got.Success() || err != nil {
		t.Fatal(got, err)
	}
	for _, c := range []struct{ qag, user string }{{"qagId with invalid UUID", "userId"}, {"fda60299-fe2d-4282-bb45-284dcb4fa7ee", "userId"}} {
		if got, err := supports.InsertSupportQag(ctx, SupportQagInserting{QagID: c.qag, UserID: c.user}); got != SupportFailure || err != nil {
			t.Fatal(got, err)
		}
		if got, err := supports.DeleteSupportQag(ctx, SupportQagDeleting{QagID: c.qag, UserID: c.user}); got != SupportFailure || err != nil {
			t.Fatal(got, err)
		}
	}
	if n, err := supports.GetSupportedQagCount(ctx, "x", nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if ok, _ := supports.IsQagSupported(ctx, "x", "y"); ok {
		t.Fatal("not supported")
	}
	if ids, err := supports.GetUserSupportedQags(ctx, "x"); len(ids) != 0 || err != nil {
		t.Fatal(ids, err)
	}
	updates := &UpdatesRepository{}
	if got, err := updates.GetQagUpdates(ctx, []string{"fda60299-fe2d-4282-bb45-284dcb4fa7ee", "bad"}); len(got) != 0 || err != nil {
		t.Fatal(got, err)
	}
	if got, err := (&LowPriorityRepository{}).GetLowPriorityQagIDs(ctx, []string{"bad"}); len(got) != 0 || err != nil {
		t.Fatal(got, err)
	}
	if err := updates.InsertQagUpdates(ctx, QagInsertingUpdates{QagID: "bad"}); err != nil {
		t.Fatal(err)
	}
	if err := (&DeleteLogRepository{}).InsertQagDeleteLog(ctx, QagDeleteLog{QagID: "bad"}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDuplicates(t *testing.T) {
	d := time.UnixMilli(1000)
	a := QagInfoWithSupportCount{ID: "a", Date: d, SupportCount: 2}
	b := QagInfoWithSupportCount{ID: "b", Date: d, SupportCount: 2}
	sameAsA := QagInfoWithSupportCount{ID: "a", Date: time.UnixMilli(1000), SupportCount: 2}
	other := QagInfoWithSupportCount{ID: "a", Date: d, SupportCount: 3}
	got := removeDuplicates([]QagInfoWithSupportCount{a, b, sameAsA, other, a})
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].SupportCount != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestStatusMapping(t *testing.T) {
	for db, want := range map[int]QagStatus{0: StatusOpen, 2: StatusArchived, 1: StatusModeratedAccepted, -1: StatusModeratedRejected, 7: StatusSelectedForResponse} {
		got, err := statusFromDB(db)
		if err != nil || got != want || StatusToDB(want) != db {
			t.Errorf("%d: %v %v", db, got, err)
		}
	}
	for _, db := range []int{3, 4, 5, 6, 8, -2, 100} {
		if _, err := statusFromDB(db); err == nil {
			t.Errorf("%d must be invalid", db)
		}
	}
	if s, ok := ParseStatus("MODERATED_ACCEPTED"); !ok || s != StatusModeratedAccepted {
		t.Fatal(s, ok)
	}
	if _, ok := ParseStatus("moderated_accepted"); ok {
		t.Fatal("valueOf is exact")
	}
}

func TestNullColumnsAreExceptions(t *testing.T) {
	title := "t"
	r := qagRow{ID: "id", Title: &title}
	if _, err := r.info(); err == nil {
		t.Fatal("a NULL column is an exception")
	}
	d := time.Now()
	r = qagRow{ID: "id", Title: &title, Description: &title, PostDate: &d, Username: &title, ThematiqueID: &title, UserID: &title, Status: 9}
	if _, err := r.info(); err == nil {
		t.Fatal("invalid status")
	}
	r.Status = 1
	if got, err := r.info(); err != nil || got.Status != StatusModeratedAccepted {
		t.Fatal(got, err)
	}
}

// ---------------------------------------------------------------------------
// Java date semantics
// ---------------------------------------------------------------------------

func TestLocalDateParsing(t *testing.T) {
	ok := map[string]localDate{
		"2026-10-04": {Year: 2026, Month: 10, Day: 4}, "2024-02-29": {Year: 2024, Month: 2, Day: 29}, " 2026-10-04 ": {Year: 2026, Month: 10, Day: 4}, "+12026-01-02": {Year: 12026, Month: 1, Day: 2},
		"-0001-10-04": {Year: -1, Month: 10, Day: 4}, "2026-10-04T10:00:00": {Year: 2026, Month: 10, Day: 4}, "2026-10-04T10:00": {Year: 2026, Month: 10, Day: 4}, "2026-10-04T10:00:00.123456789": {Year: 2026, Month: 10, Day: 4},
		"2026-10-04T23:59:59Z": {Year: 2026, Month: 10, Day: 4}, "2026-10-04T10:00:00.": {Year: 2026, Month: 10, Day: 4},
	}
	for in, want := range ok {
		var d localDate
		if err := d.UnmarshalJavaTree(in); err != nil || d != want {
			t.Errorf("%q: %v %v", in, d, err)
		}
	}
	for _, in := range []string{"", "20261004", "2026-02-29", "2026-1-4", "2026-13-01", "+2026-10-04", "12026-10-04", "-0000-10-04", "2026-10-04T24:00:00",
		"2026-10-04T10:00:00.1234567890", "2026-10-04T10:00:00+02:00", "2026-10-04T10:00:00z", "2026-10-04T99:00:00Z", "2026-10-04x", "2026-10-04T10:00:60"} {
		var d localDate
		if err := d.UnmarshalJavaTree(in); err == nil {
			t.Errorf("%q must be rejected, got %v", in, d)
		}
	}
}

func TestFormatDateBeyondFourDigitYears(t *testing.T) {
	cases := map[string]time.Time{
		"2026-10-04 05:06:07":   time.Date(2026, 10, 4, 5, 6, 7, 0, time.Local),
		"+12026-10-04 05:06:07": time.Date(12026, 10, 4, 5, 6, 7, 0, time.Local),
		"0001-10-04 05:06:07":   time.Date(0, 10, 4, 5, 6, 7, 0, time.Local), // year 0 = 1 BC, year of era 1
	}
	for want, in := range cases {
		if got := formatDate(in); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// micro-cache
// ---------------------------------------------------------------------------

func TestMicroLoadStoresOnlyWhenAsked(t *testing.T) {
	a := &app.App{Cache: cache.New(nil, nil, false), Cfg: &config.Config{}}
	loads := 0
	load := func(store bool) func() (*string, bool, error) {
		return func() (*string, bool, error) { loads++; v := "v"; return &v, store, nil }
	}
	for i := 0; i < 3; i++ {
		v, err := microLoad(a, "n", "k", time.Minute, load(false))
		if err != nil || v == nil || *v != "v" {
			t.Fatal(v, err)
		}
	}
	if loads != 3 {
		t.Fatalf("a result that must not be stored was cached (%d loads)", loads)
	}
	for i := 0; i < 3; i++ {
		microLoad(a, "n", "k", time.Minute, load(true))
	}
	if loads != 4 {
		t.Fatalf("loads = %d", loads)
	}
	a.Cache.Invalidate(ctx, "n", "k")
	microLoad(a, "n", "k", time.Minute, load(true))
	if loads != 5 {
		t.Fatalf("an invalidated entry must be reloaded (%d loads)", loads)
	}
	if _, err := microLoad(a, "n", "e", time.Minute, func() (*string, bool, error) { return nil, true, errors.New("x") }); err == nil {
		t.Fatal("errors are returned")
	}
}

func TestCoexistenceCap(t *testing.T) {
	short := &app.App{Cache: cache.New(nil, nil, false)}
	capped := &app.App{Cache: cache.New(nil, nil, true)}
	if coexistenceCap(short, time.Hour) != time.Hour || coexistenceCap(capped, time.Hour) != cache.CoexistenceMaxTTL || coexistenceCap(capped, time.Second) != time.Second {
		t.Fatal("cap")
	}
}

// the JSON of a QaG with its user data (shape checked against the JVM by the oracle test)
func TestToQagJSONOmitsAbsentBlocks(t *testing.T) {
	d := QagDetails{ID: "i", Thematique: Thematique{Label: "l", Picto: "p"}, Date: time.Unix(0, 0)}
	j := ToQagJSON(QagWithUserData{QagDetails: d, IsHelpful: ptr(true)})
	if j.Response != nil || j.TextResponse != nil || j.Support == nil {
		t.Fatalf("%+v", j)
	}
	if got := unescapeLineBreaks(`a\nb\\nc`); got != "a\nb\\\nc" {
		t.Fatalf("%q", got)
	}
}
