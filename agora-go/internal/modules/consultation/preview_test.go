package consultation

import (
	"context"
	"reflect"
	"testing"

	"agora/internal/domain"
	"agora/internal/modules/profile"
)

// fakeInfo is the ConsultationInfoRepository the preview use case reads.
type fakeInfo struct {
	ongoing, ongoingDraft   []ConsultationPreview
	finished, finishedDraft []ConsultationPreviewFinished
	answered                []ConsultationPreviewFinished
	calls                   []string
}

func (f *fakeInfo) GetAnsweredConsultations(context.Context, string) ([]ConsultationPreviewFinished, error) {
	f.calls = append(f.calls, "answered")
	return f.answered, nil
}
func (f *fakeInfo) GetOngoingConsultations(context.Context, []domain.Territoire) []ConsultationPreview {
	f.calls = append(f.calls, "ongoing")
	return f.ongoing
}
func (f *fakeInfo) GetOngoingConsultationsWithUnpublished(context.Context, []domain.Territoire) []ConsultationPreview {
	f.calls = append(f.calls, "ongoingDraft")
	return f.ongoingDraft
}
func (f *fakeInfo) GetFinishedConsultations(context.Context, []domain.Territoire) []ConsultationPreviewFinished {
	f.calls = append(f.calls, "finished")
	return f.finished
}
func (f *fakeInfo) GetFinishedConsultationsWithUnpublished(context.Context, []domain.Territoire) []ConsultationPreviewFinished {
	f.calls = append(f.calls, "finishedDraft")
	return f.finishedDraft
}

type fakeProfiles struct{ asked []string }

func (f *fakeProfiles) GetProfile(_ context.Context, userID string) (*profile.Profile, error) {
	f.asked = append(f.asked, userID)
	return nil, nil
}

func ongoingAt(id string, days int) ConsultationPreview {
	return ConsultationPreview{ID: id, EndDate: ldt(2026, 6, 1, 12, 0, 0).PlusDays(days)}
}

func finishedAt(id string, days int) ConsultationPreviewFinished {
	return ConsultationPreviewFinished{ID: id, LastUpdateDate: ldt(2026, 6, 1, 12, 0, 0).PlusDays(days)}
}

func ids[T any](list []T, id func(T) string) []string {
	out := []string{}
	for _, e := range list {
		out = append(out, id(e))
	}
	return out
}

// ConsultationPreviewUseCaseTest
func TestPreviewUseCase(t *testing.T) {
	ctx := context.Background()
	user := "user123"
	ongoingID := func(c ConsultationPreview) string { return c.ID }
	finishedID := func(c ConsultationPreviewFinished) string { return c.ID }

	newUseCase := func(info *fakeInfo) (*PreviewUseCase, *fakeProfiles) {
		p := &fakeProfiles{}
		return &PreviewUseCase{info: info, profiles: p}, p
	}

	// who can see the unpublished consultations reads the "with unpublished" queries only
	info := &fakeInfo{
		ongoingDraft: []ConsultationPreview{ongoingAt("ongoing1", 10), ongoingAt("ongoingDraft", 5)}, finishedDraft: []ConsultationPreviewFinished{finishedAt("finished1", -3), finishedAt("finishedDraft", -5)},
	}
	u, _ := newUseCase(info)
	page, err := u.GetConsultationPreviewPage(ctx, nil, true)
	if err != nil || !reflect.DeepEqual(info.calls, []string{"ongoingDraft", "finishedDraft"}) {
		t.Fatalf("%v %v", info.calls, err)
	}
	if !reflect.DeepEqual(ids(page.Ongoing, ongoingID), []string{"ongoingDraft", "ongoing1"}) || !reflect.DeepEqual(ids(page.Finished, finishedID), []string{"finishedDraft", "finished1"}) {
		t.Fatalf("sorted by end date / last update date: %+v", page)
	}
	if page.Answered == nil || len(page.Answered) != 0 {
		t.Fatalf("an anonymous user has an empty (not null) answered list: %#v", page.Answered)
	}

	// the others read the published ones only
	info = &fakeInfo{ongoing: []ConsultationPreview{ongoingAt("o1", 10)}, finished: []ConsultationPreviewFinished{finishedAt("f1", -1)}}
	u, p := newUseCase(info)
	page, _ = u.GetConsultationPreviewPage(ctx, nil, false)
	if !reflect.DeepEqual(info.calls, []string{"ongoing", "finished"}) || len(p.asked) != 0 || len(page.Ongoing) != 1 {
		t.Fatalf("%v %v", info.calls, p.asked)
	}

	// the answered consultations leave the ongoing list, the profile is read, the answered list is returned as is
	for _, canViewUnpublished := range []bool{true, false} {
		info = &fakeInfo{answered: []ConsultationPreviewFinished{finishedAt("answered1", -2)}}
		list := []ConsultationPreview{ongoingAt("ongoing1", 10), ongoingAt("answered1", 5)}
		if canViewUnpublished {
			info.ongoingDraft = list
		} else {
			info.ongoing = list
		}
		u, p = newUseCase(info)
		page, _ = u.GetConsultationPreviewPage(ctx, &user, canViewUnpublished)
		if !reflect.DeepEqual(ids(page.Ongoing, ongoingID), []string{"ongoing1"}) || !reflect.DeepEqual(ids(page.Answered, finishedID), []string{"answered1"}) || !reflect.DeepEqual(p.asked, []string{user}) {
			t.Fatalf("%+v %v", page, p.asked)
		}
	}

	// ongoing consultations sorted by end date, ties keep the order
	info = &fakeInfo{ongoing: []ConsultationPreview{ongoingAt("o1", 10), ongoingAt("o2", 5), ongoingAt("o3", 7), ongoingAt("o4", 5)}}
	u, _ = newUseCase(info)
	page, _ = u.GetConsultationPreviewPage(ctx, nil, false)
	if got := ids(page.Ongoing, ongoingID); !reflect.DeepEqual(got, []string{"o2", "o4", "o3", "o1"}) {
		t.Fatalf("%v", got)
	}
}

// ConsultationPreviewMapperTest / ConsultationPreviewFinishedMapperTest
func TestHighlightLabel(t *testing.T) {
	now := ldt(2026, 6, 1, 12, 0, 0)
	cases := []struct {
		end  LocalDateTime
		want string
	}{
		{ldt(2026, 6, 1, 23, 59, 59), "Dernier jour !"},
		{ldt(2026, 6, 2, 11, 59, 59), "Dernier jour !"},
		{ldt(2026, 6, 2, 12, 0, 0), "Plus que 2 jours !"},
		{ldt(2026, 6, 4, 12, 0, 0), "Plus que 4 jours !"},
		{ldt(2026, 6, 8, 11, 59, 59), "Plus que 7 jours !"},
		{ldt(2026, 6, 8, 12, 0, 0), ""},
		{ldt(2026, 6, 20, 12, 0, 0), ""},
		{ldt(2026, 6, 1, 11, 59, 59), ""},
		{ldt(2026, 5, 1, 11, 59, 59), ""},
	}
	for _, c := range cases {
		got := ConsultationPreview{EndDate: c.end}.HighlightLabel(now)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: %q", c.end.Format(), *got)
		case c.want != "" && (got == nil || *got != c.want):
			t.Errorf("%s: %v, want %q", c.end.Format(), got, c.want)
		}
	}
}

func TestFinishedStepAndLabel(t *testing.T) {
	now := ldt(2026, 6, 1, 12, 0, 0)
	if (ConsultationPreviewFinished{EndDate: now.PlusDays(1)}).GetStep(now) != CollectingData ||
		(ConsultationPreviewFinished{EndDate: now.PlusDays(-1)}).GetStep(now) != PoliticalCommitment ||
		(ConsultationPreviewFinished{EndDate: now}).GetStep(now) != PoliticalCommitment {
		t.Fatal("steps")
	}
	label := sp("Flamme")
	update := ldt(2026, 5, 1, 12, 0, 0)
	cases := []struct {
		label *string
		last  LocalDateTime
		now   LocalDateTime
		want  bool
	}{
		{nil, update, update.PlusDays(10), false},
		{label, update, update.PlusDays(10), true},
		{label, update, update, true},
		{label, update, update.PlusDays(90), true},
		{label, update, ldt(2026, 7, 30, 12, 0, 1), false},
		{label, update, update.PlusDays(-1), false},
	}
	for i, c := range cases {
		got := ConsultationPreviewFinished{UpdateLabel: c.label, LastUpdateDate: c.last}.GetUpdateLabel(c.now)
		if (got != nil) != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}

func TestPreviewFinishedMapper(t *testing.T) {
	themes := []Thematique{{ID: "th1", Label: "A"}, {ID: "th2", Label: "B"}}
	infos := []ConsultationWithUpdateInfo{
		{ID: "c1", ThematiqueID: "th2", UpdateLabel: sp("x"), Territory: "France"},
		{ID: "c2", ThematiqueID: "unknown"},
		{ID: "c3", ThematiqueID: "th1"},
	}
	got := ToConsultationPreviewFinished(infos, themes)
	if len(got) != 2 || got[0].ID != "c1" || got[0].Thematique.Label != "B" || *got[0].UpdateLabel != "x" || got[1].ID != "c3" {
		t.Fatalf("%+v", got)
	}
	ongoing := ToConsultationPreviewOngoing(&ConsultationInfo{ID: "i", Slug: "s", Title: "t", CoverURL: "u", EndDate: ldt(2026, 6, 1, 12, 0, 0), Territory: "Nord"}, themes[0])
	if ongoing.ID != "i" || ongoing.CoverURL != "u" || ongoing.Thematique.ID != "th1" || ongoing.Territory != "Nord" {
		t.Fatalf("%+v", ongoing)
	}
}

func TestPreviewJSONSteps(t *testing.T) {
	now := ldt(2026, 6, 1, 12, 0, 0)
	got := ToFinishedJSON(ConsultationPreviewFinished{EndDate: now.PlusDays(2), LastUpdateDate: now.PlusDays(-1), UpdateLabel: sp("L")}, now)
	if got.Step != 1 || got.UpdateLabel == nil || got.UpdateDate != "2026-05-31 12:00:00" {
		t.Fatalf("%+v", got)
	}
	got = ToFinishedJSON(ConsultationPreviewFinished{EndDate: now.PlusDays(-2), LastUpdateDate: now.PlusDays(-1), UpdateLabel: sp("L")}, now)
	if got.Step != 2 {
		t.Fatalf("%+v", got)
	}
}
