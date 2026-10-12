package moderatus

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"agora/internal/modules/qag"
)

// Ports of ArchiveOldQagUseCaseTest, AnonymizeOldQagUseCaseTest, SelectMostPopularQagUseCaseTest and
// WeeklyTasksHandlerTest (the Mockito mocks are recording fakes), and of the parts of
// ModeratusQagLockRepositoryImplTest that need no database (the database paths are checked by the scenarios).

// lifecycleQags records the calls of the archive, anonymization and selection use cases.
type lifecycleQags struct {
	calls       []string
	archiveAt   []time.Time
	anonymizeAt []time.Time
	popular     []qag.QagInfoWithSupportCount
	selectedOK  bool
	popularErr  error
}

func (f *lifecycleQags) ArchiveOldQags(_ context.Context, d time.Time) error {
	f.calls = append(f.calls, "archiveOldQags")
	f.archiveAt = append(f.archiveAt, d)
	return nil
}

func (f *lifecycleQags) AnonymizeOldQags(_ context.Context, d time.Time) error {
	f.calls = append(f.calls, "anonymizeOldQags")
	f.anonymizeAt = append(f.anonymizeAt, d)
	return nil
}

func (f *lifecycleQags) GetMostPopularQags(context.Context) ([]qag.QagInfoWithSupportCount, error) {
	f.calls = append(f.calls, "getMostPopularQags")
	return f.popular, f.popularErr
}

func (f *lifecycleQags) SelectQagForResponse(_ context.Context, id string) (qag.QagUpdateResult, error) {
	f.calls = append(f.calls, "selectQagForResponse("+id+")")
	if f.selectedOK {
		return qag.QagUpdateResult{Info: &qag.QagInfo{ID: id}}, nil
	}
	return qag.QagUpdateResult{}, nil
}

// indexRandom is RandomQagSelector with a chosen index (the Mockito stub of chooseRandom).
type indexRandom struct {
	index int
	seen  [][]qag.QagInfoWithSupportCount
}

func (r *indexRandom) ChooseRandom(qags []qag.QagInfoWithSupportCount) qag.QagInfoWithSupportCount {
	r.seen = append(r.seen, qags)
	return qags[r.index]
}

func quietLog(string, ...any) {}

func noAfterSave(context.Context) {}

// lifecycleAt is LocalDateTime.of(...) on the test clock (the Kotlin tests use the system zone; the
// computations only depend on the wall clock of the given time).
func lifecycleAt(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestArchiveOldQag(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		now     time.Time
		want    ArchiveQagListResult
		reset   time.Time // zero when nothing is archived
	}{
		{"feature disabled: FAILURE, nothing archived", false, lifecycleAt(2024, time.January, 4, 16, 0), ArchiveFailure, time.Time{}},
		{"thursday: Monday of this week at 10h", true, lifecycleAt(2024, time.January, 4, 16, 0), ArchiveSuccess, lifecycleAt(2024, time.January, 1, 10, 0)},
		{"on the reset date", true, lifecycleAt(2024, time.January, 1, 10, 0), ArchiveSuccess, lifecycleAt(2024, time.January, 1, 10, 0)},
		{"monday before the reset hour: same Monday", true, lifecycleAt(2024, time.January, 1, 9, 59), ArchiveSuccess, lifecycleAt(2024, time.January, 1, 10, 0)},
		{"sunday: the Monday of the same week", true, lifecycleAt(2024, time.January, 7, 23, 30), ArchiveSuccess, lifecycleAt(2024, time.January, 1, 10, 0)},
		{"nanoseconds of now are kept", true,
			time.Date(2024, time.January, 4, 16, 0, 0, 123456789, time.UTC), ArchiveSuccess,
			time.Date(2024, time.January, 1, 10, 0, 0, 123456789, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &lifecycleQags{}
			u := &ArchiveOldQagUseCase{qagSelect: constFlag(c.enabled), qags: f, now: func() time.Time { return c.now }, afterSave: noAfterSave, logf: quietLog}
			got, err := u.ArchiveOldQag(ctx)
			if err != nil || got != c.want {
				t.Fatalf("got %v %v, want %v", got, err, c.want)
			}
			if c.reset.IsZero() {
				if len(f.calls) != 0 {
					t.Fatalf("calls %v, want none", f.calls)
				}
				return
			}
			if !reflect.DeepEqual(f.calls, []string{"archiveOldQags"}) || !f.archiveAt[0].Equal(c.reset) {
				t.Fatalf("calls %v at %v, want archiveOldQags at %v", f.calls, f.archiveAt, c.reset)
			}
		})
	}
}

func TestAnonymizeOldQag(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		now     time.Time
		want    AnonymizeQagListResult
		at      time.Time
	}{
		{"feature disabled: FAILURE, nothing anonymized", false, lifecycleAt(2024, time.January, 4, 16, 0), AnonymizeFailure, time.Time{}},
		{"three weeks ago, same wall clock time", true, lifecycleAt(2024, time.January, 4, 16, 0), AnonymizeSuccess, lifecycleAt(2023, time.December, 14, 16, 0)},
		{"exact time is kept", true, lifecycleAt(2024, time.January, 1, 10, 30), AnonymizeSuccess, lifecycleAt(2023, time.December, 11, 10, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &lifecycleQags{}
			u := &AnonymizeOldQagUseCase{anonymization: constFlag(c.enabled), qags: f, now: func() time.Time { return c.now }, afterSave: noAfterSave, logf: quietLog}
			got, err := u.AnonymizeOldQag(ctx)
			if err != nil || got != c.want {
				t.Fatalf("got %v %v, want %v", got, err, c.want)
			}
			if c.at.IsZero() {
				if len(f.calls) != 0 {
					t.Fatalf("calls %v, want none", f.calls)
				}
				return
			}
			if !reflect.DeepEqual(f.calls, []string{"anonymizeOldQags"}) || !f.anonymizeAt[0].Equal(c.at) {
				t.Fatalf("calls %v at %v, want anonymizeOldQags at %v", f.calls, f.anonymizeAt, c.at)
			}
		})
	}
}

func TestPutMostPopularQagInSelectedStatus(t *testing.T) {
	two := []qag.QagInfoWithSupportCount{{ID: "qagId1"}, {ID: "qagId2"}}
	cases := []struct {
		name    string
		enabled bool
		popular []qag.QagInfoWithSupportCount
		pick    int
		want    []string
	}{
		{"feature disabled: nothing at all", false, two, 0, nil},
		{"no QaG: nothing selected", true, nil, 0, []string{"getMostPopularQags"}},
		{"one QaG: selected without drawing", true, two[:1], 0, []string{"getMostPopularQags", "selectQagForResponse(qagId1)"}},
		{"several QaGs: a random one is selected", true, two, 1, []string{"getMostPopularQags", "selectQagForResponse(qagId2)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &lifecycleQags{popular: c.popular, selectedOK: true}
			r := &indexRandom{index: c.pick}
			u := &SelectMostPopularQagUseCase{qagSelect: constFlag(c.enabled), qags: f, random: r, afterSave: noAfterSave, logf: quietLog}
			if err := u.PutMostPopularQagInSelectedStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.calls, c.want) && !(len(f.calls) == 0 && c.want == nil) {
				t.Fatalf("calls %v, want %v", f.calls, c.want)
			}
			if len(c.popular) > 1 && c.enabled && (len(r.seen) != 1 || len(r.seen[0]) != 2) {
				t.Fatalf("the random selector was called %d times", len(r.seen))
			}
			if len(c.popular) <= 1 && len(r.seen) != 0 {
				t.Fatalf("the random selector must not be drawn for %d QaGs", len(c.popular))
			}
		})
	}
}

func TestPutMostPopularQagFailures(t *testing.T) {
	boom := errors.New("db down")
	// a failed read stops the use case (Kotlin: the exception propagates)
	f := &lifecycleQags{popularErr: boom}
	u := &SelectMostPopularQagUseCase{qagSelect: constFlag(true), qags: f, random: &indexRandom{}, afterSave: noAfterSave, logf: quietLog}
	if err := u.PutMostPopularQagInSelectedStatus(ctx); !errors.Is(err, boom) {
		t.Fatalf("err %v, want %v", err, boom)
	}
	// a failed selection is only logged: the use case returns normally
	f = &lifecycleQags{popular: []qag.QagInfoWithSupportCount{{ID: "q"}}, selectedOK: false}
	u.qags = f
	if err := u.PutMostPopularQagInSelectedStatus(ctx); err != nil {
		t.Fatalf("a failed selection must not fail the task: %v", err)
	}
}

// weeklyService wires the three use cases of the weekly task on the recording fake.
func weeklyService(f *lifecycleQags) *Service {
	return &Service{
		Select:    &SelectMostPopularQagUseCase{qagSelect: constFlag(true), qags: f, random: DefaultRandomQagSelector{}, afterSave: noAfterSave, logf: quietLog},
		Archive:   &ArchiveOldQagUseCase{qagSelect: constFlag(true), qags: f, now: func() time.Time { return lifecycleAt(2024, time.January, 4, 16, 0) }, afterSave: noAfterSave, logf: quietLog},
		Anonymize: &AnonymizeOldQagUseCase{anonymization: constFlag(true), qags: f, now: func() time.Time { return lifecycleAt(2024, time.January, 4, 16, 0) }, afterSave: noAfterSave, logf: quietLog},
	}
}

// TestWeeklyQagTasks is WeeklyTasksHandlerTest: the transition is checked unless force_question_selection is "true"
// (exactly, like Kotlin's ==); the selection and the archive run only when a transition or a force applies.
func TestWeeklyQagTasks(t *testing.T) {
	full := []string{"getMostPopularQags", "selectQagForResponse(qagId1)", "archiveOldQags", "anonymizeOldQags"}
	anonymizeOnly := []string{"anonymizeOldQags"}
	cases := []struct {
		name       string
		args       map[string]string
		transition bool
		transErr   error
		wantCheck  bool
		want       []string
		wantErr    bool
	}{
		{"in transition: select, archive, anonymize", nil, true, nil, true, full, false},
		{"not in transition: anonymize only", nil, false, nil, true, anonymizeOnly, false},
		{"forced: no transition check", map[string]string{"force_question_selection": "true"}, false, nil, false, full, false},
		{"force false: transition checked", map[string]string{"force_question_selection": "false"}, false, nil, true, anonymizeOnly, false},
		{"force TRUE is not forced (Kotlin ==)", map[string]string{"force_question_selection": "TRUE"}, false, nil, true, anonymizeOnly, false},
		{"transition check fails: nothing runs", nil, false, errors.New("flags"), true, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &lifecycleQags{popular: []qag.QagInfoWithSupportCount{{ID: "qagId1"}}, selectedOK: true}
			checked := false
			inTransition := func(context.Context) (bool, error) { checked = true; return c.transition, c.transErr }
			err := weeklyService(f).WeeklyQagTasks(ctx, c.args, inTransition)
			if (err != nil) != c.wantErr {
				t.Fatalf("err %v, want error %t", err, c.wantErr)
			}
			if checked != c.wantCheck {
				t.Fatalf("transition checked %t, want %t", checked, c.wantCheck)
			}
			if !c.wantErr && !reflect.DeepEqual(f.calls, c.want) {
				t.Fatalf("calls %v, want %v", f.calls, c.want)
			}
		})
	}
}

// TestLockRepositoryWithoutDatabase: ids that are not UUIDs never reach the database (a nil App would panic), and the
// lenient UUID spellings are stored canonically (ModeratusQagLockMapper.toDto).
func TestLockRepositoryWithoutDatabase(t *testing.T) {
	r := &LockRepository{}
	if err := r.AddLockedIDs(ctx, []string{"lockedQagId with invalid UUID", "", "not-a-uuid"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveLockedQagID(ctx, "lockedQagId with invalid UUID"); err != nil {
		t.Fatal(err)
	}
	got := lockedUUIDs([]string{"0-0-4000-8000-23", "00000000-0000-4000-8000-00000000002E", "x"})
	want := []string{"00000000-0000-4000-8000-000000000023", "00000000-0000-4000-8000-00000000002e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lockedUUIDs = %v, want %v", got, want)
	}
}
