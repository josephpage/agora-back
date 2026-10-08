package consultation

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agora/internal/domain"
	"agora/internal/modules/login"
)

func sp2(s string) *string { return &s }

// ConsultationStrapiCacheByIdRepositoryImplTest
func TestByIDCache(t *testing.T) {
	ta := newTestApp(t)
	repo := ta.strapiRepo()
	now := baseTime
	repo.now = func() time.Time { return now }
	dto := &strapiConsultation{DocumentID: "x"}

	if repo.cached(repo.byID, "id") != nil {
		t.Fatal("miss")
	}
	repo.put(repo.byID, "id", nil)
	if repo.cached(repo.byID, "id") != nil {
		t.Fatal("a null sentinel is a miss for the caller")
	}
	repo.put(repo.byID, "id", dto)
	if repo.cached(repo.byID, "id") != dto {
		t.Fatal("hit")
	}
	now = baseTime.Add(5*time.Minute - time.Second)
	if repo.cached(repo.byID, "id") != dto {
		t.Fatal("just before the TTL")
	}
	now = baseTime.Add(5*time.Minute - time.Second + 900*time.Millisecond)
	if repo.cached(repo.byID, "id") != dto {
		t.Fatal("the age is counted in whole seconds")
	}
	now = baseTime.Add(5 * time.Minute)
	if repo.cached(repo.byID, "id") != nil {
		t.Fatal("exactly at the TTL: expired")
	}
	if _, ok := repo.byID["id"]; ok {
		t.Fatal("an expired entry is removed")
	}

	// two independent maps, evicted together
	now = baseTime
	repo.put(repo.byID, "a", dto)
	repo.put(repo.byIDUnpub, "a", dto)
	repo.put(repo.byID, "b", dto)
	repo.EvictConsultationByID("a")
	if repo.cached(repo.byID, "a") != nil || repo.cached(repo.byIDUnpub, "a") != nil || repo.cached(repo.byID, "b") != dto {
		t.Fatal("evict")
	}
	repo.put(repo.byIDUnpub, "c", dto)
	if repo.cached(repo.byID, "c") != nil {
		t.Fatal("the variants do not share their entries")
	}
}

func TestByIDCacheAvoidsStrapi(t *testing.T) {
	ta := newTestApp(t)
	ta.strapi.set(consultationSpec{id: "c1", slug: "s1"}.json())
	repo := ta.strapiRepo()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if c := repo.GetConsultationByIDWithUnpublished(ctx, "c1"); c == nil || c.DocumentID != "c1" {
			t.Fatal("not found")
		}
	}
	if ta.strapi.count() != 1 {
		t.Fatalf("one request: %d", ta.strapi.count())
	}
	// the request asks for the drafts too, with the detail populate
	u, _ := url.QueryUnescape(ta.strapi.last())
	if !strings.Contains(u, "status=draft") || !strings.Contains(u, "filters[documentId][$in]=c1") ||
		!strings.Contains(u, "[consultation_avant_reponse][populate][sections][on][consultation-section.section-titre][populate]=*") {
		t.Fatalf("%s", u)
	}
	// the published variant is another map but the same request
	repo.GetConsultationByID(ctx, "c1")
	if ta.strapi.count() != 2 {
		t.Fatalf("%d", ta.strapi.count())
	}
	// an unknown consultation is asked again every time
	repo.GetConsultationByIDWithUnpublished(ctx, "nope")
	repo.GetConsultationByIDWithUnpublished(ctx, "nope")
	if ta.strapi.count() != 4 {
		t.Fatalf("%d", ta.strapi.count())
	}
}

func TestByIDCacheSharesConcurrentLoads(t *testing.T) {
	ta := newTestApp(t)
	ta.strapi.set(consultationSpec{id: "c1", slug: "s1"}.json())
	repo := ta.strapiRepo()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repo.GetConsultationByIDWithUnpublished(context.Background(), "c1")
		}()
	}
	wg.Wait()
	if n := ta.strapi.count(); n > 5 {
		t.Fatalf("%d requests for 50 concurrent readers", n)
	}
}

// StrapiRepository request URIs
func TestStrapiRepositoryURIs(t *testing.T) {
	ta := newTestApp(t)
	repo := ta.strapiRepo()
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 12, 0, 0, 123456000, time.UTC)

	repo.GetConsultationsOngoing(ctx, now, nil)
	got := ta.strapi.last()
	if !strings.HasPrefix(got, "consultations?pagination[pageSize]=100&populate[thematique]=*&populate[questions][populate]=*&populate[consultation_contenu_a_venir]=*&populate[consultation_avant_reponse][populate]=*") ||
		!strings.HasSuffix(got, "&filters[datetime_de_debut][$lt]=2026-06-01T12:00:00.123456&filters[datetime_de_fin][$gt]=2026-06-01T12:00:00.123456") {
		t.Fatalf("%s", got)
	}
	repo.GetConsultationsFinishedWithUnpublished(ctx, now, []domain.Territoire{domain.PaysValues[0]})
	got = ta.strapi.last()
	if !strings.Contains(got, "pageSize]=100&status=draft&populate") || !strings.HasSuffix(got, "&filters[datetime_de_fin][$lt]=2026-06-01T12:00:00.123456&filters[territoire][$in]=France") {
		t.Fatalf("%s", got)
	}
	n := ta.strapi.count()
	if r := repo.GetConsultationsByIDs(ctx, nil); len(r) != 0 || ta.strapi.count() != n {
		t.Fatal("no request without ids")
	}
	repo.GetConsultationsByIDs(ctx, []string{"a", "b"})
	if got = ta.strapi.last(); !strings.HasSuffix(got, "&filters[documentId][$in]=a&filters[documentId][$in]=b") {
		t.Fatalf("%s", got)
	}
	repo.GetConsultationBySlug(ctx, "my slug")
	if got = ta.strapi.last(); !strings.HasSuffix(got, "&filters[slug][$in]=my+slug") && !strings.HasSuffix(got, "&filters[slug][$in]=my%20slug") {
		t.Fatalf("%s", got)
	}
	repo.GetConsultationsEnded14DaysAgo(ctx, now)
	if got = ta.strapi.last(); !strings.HasSuffix(got, "&filters[datetime_de_fin][$lt]=2026-05-18T12:00:00.123456") {
		t.Fatalf("%s", got)
	}
	repo.IsConsultationExists(ctx, "a")
	if got = ta.strapi.last(); !strings.Contains(got, "status=draft") || strings.Contains(got, "populate[thematique]") || !strings.HasSuffix(got, "&populate=*&filters[documentId][$in]=a") {
		t.Fatalf("%s", got)
	}
	repo.CountFinishedConsultations(ctx, now)
	if got = ta.strapi.last(); got != "consultations?pagination[pageSize]=100&populate=*&filters[datetime_de_fin][$lt]=2026-06-01T12:00:00.123456" {
		t.Fatalf("%s", got)
	}
}

// ConsultationInfoRepositoryImplTest
func TestInfoRepositoryByIDOrSlug(t *testing.T) {
	ctx := context.Background()
	for _, unpublished := range []bool{false, true} {
		ta := newTestApp(t)
		ta.strapi.set(consultationSpec{id: "doc-1", slug: "the-slug"}.json(), consultationSpec{id: "same", slug: "same"}.json())
		repo := &InfoRepository{a: ta.App, strapi: ta.strapiRepo(), mapper: infoMapper{log: ta.Log}}
		get := repo.GetConsultationByIDOrSlug
		if unpublished {
			get = repo.GetConsultationByIDOrSlugWithUnpublished
		}

		// by slug: stored under the slug, the id and nothing more
		info := get(ctx, "the-slug")
		if info == nil || info.ID != "doc-1" || info.Slug != "the-slug" {
			t.Fatalf("%+v", info)
		}
		if ta.strapi.count() != 1 {
			t.Fatalf("found by slug in one request: %d", ta.strapi.count())
		}
		for _, key := range []string{"the-slug", "doc-1"} {
			if v, ok := ta.Cache.Get(consultationCacheName, key); !ok || v.(*ConsultationInfo).ID != "doc-1" {
				t.Fatalf("not cached under %s", key)
			}
		}
		// the hit: no Strapi
		get(ctx, "doc-1")
		get(ctx, "the-slug")
		if ta.strapi.count() != 1 {
			t.Fatalf("%d", ta.strapi.count())
		}

		// by id: the slug is tried first, then the id
		ta2 := newTestApp(t)
		ta2.strapi.set(consultationSpec{id: "doc-2", slug: "other-slug"}.json())
		repo2 := &InfoRepository{a: ta2.App, strapi: ta2.strapiRepo(), mapper: infoMapper{log: ta2.Log}}
		get2 := repo2.GetConsultationByIDOrSlug
		if unpublished {
			get2 = repo2.GetConsultationByIDOrSlugWithUnpublished
		}
		if info := get2(ctx, "doc-2"); info == nil || info.Slug != "other-slug" {
			t.Fatalf("%+v", info)
		}
		if ta2.strapi.count() != 2 {
			t.Fatalf("slug then id: %d", ta2.strapi.count())
		}
		if _, ok := ta2.Cache.Get(consultationCacheName, "other-slug"); !ok {
			t.Fatal("stored under the slug too")
		}

		// id == slug: stored once; unknown: null and nothing cached
		if info := get(ctx, "same"); info == nil || info.ID != "same" {
			t.Fatalf("%+v", info)
		}
		if get(ctx, "unknown") != nil {
			t.Fatal("unknown")
		}
		if _, ok := ta.Cache.Get(consultationCacheName, "unknown"); ok {
			t.Fatal("not found is not cached")
		}
		// the draft variant is the only one asking for the drafts of the slug query
		first, _ := url.QueryUnescape(ta.strapi.uris[0])
		if strings.Contains(first, "status=draft") != unpublished {
			t.Fatalf("status=draft: %v\n%s", unpublished, first)
		}
	}
}

func TestInfoRepositoryGetConsultationByID(t *testing.T) {
	ta := newTestApp(t)
	ta.strapi.set(consultationSpec{id: "doc-1", slug: "the-slug"}.json())
	repo := &InfoRepository{a: ta.App, strapi: ta.strapiRepo(), mapper: infoMapper{log: ta.Log}}
	ctx := context.Background()
	if repo.GetConsultation(ctx, "the-slug") != nil {
		t.Fatal("getConsultation reads the id only")
	}
	if info := repo.GetConsultation(ctx, "doc-1"); info == nil || info.Title != "Titre doc-1" {
		t.Fatalf("%+v", info)
	}
	if _, ok := ta.Cache.Get(consultationCacheName, "the-slug"); ok {
		t.Fatal("only the asked key is cached")
	}
	n := ta.strapi.count()
	repo.GetConsultation(ctx, "doc-1")
	if ta.strapi.count() != n {
		t.Fatal("cached")
	}
}

// ConsultationInfoMapper
func TestToConsultationInfo(t *testing.T) {
	o := consultationSpec{id: "c", slug: "s"}.json()
	o["image_de_couverture"] = obj{"url": "https://cover.jpg", "formats": nil}
	o["image_page_de_contenu"] = obj{"url": "orig", "formats": obj{"medium": obj{"url": "https://medium.jpg"}}}
	info := infoMapper{}.toConsultationInfo(decodeConsultation(t, o))
	if info.CoverURL != "https://cover.jpg" || info.DetailsCoverURL != "https://medium.jpg" || info.QuestionCount != "3 questions" || info.EstimatedTime != "5 minutes" ||
		info.ParticipantCountGoal != 100 || info.Thematique.Label != "Santé" || info.Territory != "France" || info.TitreWeb != "Web" || info.SousTitreWeb != "Sous-titre" {
		t.Fatalf("%+v", info)
	}
	if !info.IsOngoing(fixedNow) || info.IsOngoing(ldt(2030, 1, 1, 0, 0, 0)) {
		t.Fatal("isOngoing")
	}
}

func TestToDomainFinishedDropsConsultationsWithoutUpdateDate(t *testing.T) {
	m := infoMapper{log: newTestApp(t).Log}
	finished := decodeConsultation(t, consultationSpec{id: "c1", slug: "s1", start: baseTime.AddDate(0, 0, -10), end: baseTime.AddDate(0, 0, -5)}.json())
	notStarted := decodeConsultation(t, consultationSpec{id: "c2", slug: "s2", start: baseTime.AddDate(0, 0, 10), end: baseTime.AddDate(0, 0, 20)}.json())
	got := m.toDomainFinished([]*strapiConsultation{finished, notStarted}, fixedNow)
	if len(got) != 1 || got[0].ID != "c1" || got[0].LastUpdateDate != ldt(2026, 5, 22, 12, 0, 0) {
		t.Fatalf("%+v", got)
	}
	withInfo := m.toConsultationsWithUpdateInfo([]*strapiConsultation{finished, notStarted}, fixedNow)
	if len(withInfo) != 1 || withInfo[0].ThematiqueID != "th1" || withInfo[0].UpdateDate != got[0].LastUpdateDate {
		t.Fatalf("%+v", withInfo)
	}
}

// the ongoing / finished lists: a micro-cache of the non empty lists
func TestListMicroCache(t *testing.T) {
	ta := newTestApp(t)
	ta.Cfg.MicroCacheTTL = time.Minute
	ta.strapi.set(consultationSpec{id: "c1", slug: "s1"}.json())
	repo := &InfoRepository{a: ta.App, strapi: ta.strapiRepo(), mapper: infoMapper{log: ta.Log}}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if got := repo.GetOngoingConsultations(ctx, nil); len(got) != 1 {
			t.Fatalf("%v", got)
		}
	}
	if ta.strapi.count() != 1 {
		t.Fatalf("one request: %d", ta.strapi.count())
	}
	// the two lists and the territories have their own entries
	repo.GetFinishedConsultations(ctx, nil)
	repo.GetOngoingConsultations(ctx, []domain.Territoire{domain.PaysValues[0]})
	if ta.strapi.count() != 3 {
		t.Fatalf("%d", ta.strapi.count())
	}
	// an empty answer (or a Strapi failure) is never kept
	ta2 := newTestApp(t)
	ta2.Cfg.MicroCacheTTL = time.Minute
	repo2 := &InfoRepository{a: ta2.App, strapi: ta2.strapiRepo(), mapper: infoMapper{log: ta2.Log}}
	repo2.GetOngoingConsultations(ctx, nil)
	repo2.GetOngoingConsultations(ctx, nil)
	if ta2.strapi.count() != 2 {
		t.Fatalf("%d", ta2.strapi.count())
	}
	// the unpublished variants are never cached
	repo.GetOngoingConsultationsWithUnpublished(ctx, nil)
	repo.GetOngoingConsultationsWithUnpublished(ctx, nil)
	if ta.strapi.count() != 5 {
		t.Fatalf("%d", ta.strapi.count())
	}
}

func TestTerritoryKey(t *testing.T) {
	if territoryKey(nil) != "all" {
		t.Fatal("all")
	}
	if got := territoryKey([]domain.Territoire{domain.PaysValues[1], domain.PaysValues[0]}); got != "France,Français de l'étranger" {
		t.Fatalf("%s", got)
	}
}

// FeedbackConsultationUpdateMapperTest
func TestToStats(t *testing.T) {
	cases := []struct{ positive, negative, wantPositive, wantNegative int }{
		{0, 0, 0, 0}, {3, 1, 75, 25}, {2, 1, 67, 33}, {78, 22, 78, 22}, {1, 2, 33, 67}, {1, 199, 1, 99}, {1, 0, 100, 0}, {0, 5, 0, 100}, {1, 7, 13, 87},
	}
	for _, c := range cases {
		got := toStats([]statGroup{{1, c.positive}, {0, c.negative}})
		want := FeedbackStats{PositiveRatio: c.wantPositive, NegativeRatio: c.wantNegative, ResponseCount: c.positive + c.negative}
		if *got != want {
			t.Errorf("%d/%d: %+v, want %+v", c.positive, c.negative, *got, want)
		}
	}
	// unknown values are ignored
	if got := toStats([]statGroup{{42, 7}}); *got != (FeedbackStats{}) {
		t.Fatalf("%+v", got)
	}
	if got := toStats([]statGroup{{1, 1}, {42, 7}}); *got != (FeedbackStats{PositiveRatio: 100, NegativeRatio: 0, ResponseCount: 1}) {
		t.Fatalf("%+v", got)
	}
}

// the details use case on the anonymous path (no database)
type fakeFlags struct{ enabled bool }

func (f fakeFlags) IsFeatureEnabled(context.Context, login.Feature) (bool, error) {
	return f.enabled, nil
}

type fakeAnswered struct {
	mu    sync.Mutex
	calls int
	count int
	err   error
}

func (f *fakeAnswered) GetParticipantCount(context.Context, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.count, f.err
}

func newDetailsUseCase(ta *testApp, answered *fakeAnswered) *DetailsUseCase {
	strapiRepo := ta.strapiRepo()
	return &DetailsUseCase{
		a: ta.App, flags: fakeFlags{}, answered: answered,
		info:    &InfoRepository{a: ta.App, strapi: strapiRepo, mapper: infoMapper{log: ta.Log}},
		updates: &UpdateRepository{a: ta.App, strapi: strapiRepo},
		history: &HistoryRepository{a: ta.App, strapi: strapiRepo},
	}
}

func TestDetailsViewOfAnonymousUsers(t *testing.T) {
	ctx := context.Background()
	ta := newTestApp(t)
	ongoing := consultationSpec{id: "ongoing", slug: "ongoing-slug", start: baseTime.AddDate(0, 0, -2), end: baseTime.AddDate(0, 0, 5),
		autres: []obj{autreContenu("autre", baseTime.AddDate(0, 0, -1), nil)}}
	finished := consultationSpec{id: "finished", slug: "finished-slug", start: baseTime.AddDate(0, 0, -20), end: baseTime.AddDate(0, 0, -5)}
	ta.strapi.set(ongoing.json(), finished.json())
	answered := &fakeAnswered{count: 42}
	u := newDetailsUseCase(ta, answered)

	// an ongoing consultation: the content before the answer; a finished one: the latest update
	d, err := u.GetConsultation(ctx, "ongoing-slug", nil)
	if err != nil || d.Update.ID != "avant-ongoing" || d.ParticipantCount != 42 || d.IsAnsweredByUser || d.IsUserFeedbackPositive != nil || d.FeedbackStats != nil || len(d.History) == 0 {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = u.GetConsultation(ctx, "finished", nil)
	if err != nil || d.Update.ID != "apres-finished" || d.Update.ResponsesInfo.Picto != pictoEnded || d.Consultation.ID != "finished" {
		t.Fatalf("%+v %v", d, err)
	}
	n, pc := ta.strapi.count(), answered.calls
	for i := 0; i < 5; i++ {
		u.GetConsultation(ctx, "ongoing", nil)
		u.GetConsultation(ctx, "finished-slug", nil)
	}
	if ta.strapi.count() != n || answered.calls != pc {
		t.Fatalf("everything is cached: %d new requests, %d new counts", ta.strapi.count()-n, answered.calls-pc)
	}
	// the answered users' view of the ongoing consultation is the latest update
	latest, err := u.consultationDetails(ctx, d.Consultation, false)
	_ = latest
	ongoingInfo := u.info.GetConsultationByIDOrSlugWithUnpublished(ctx, "ongoing")
	if v, err := u.consultationDetails(ctx, ongoingInfo, true); err != nil || v.Update.ID != "autre" {
		t.Fatalf("%+v %v", v, err)
	}
	if v, _ := u.consultationDetails(ctx, ongoingInfo, false); v.Update.ID != "avant-ongoing" {
		t.Fatalf("%+v", v)
	}

	// unknown consultation
	_, err = u.GetConsultation(ctx, "unknown", nil)
	var nf *ConsultationNotFoundError
	if !errors.As(err, &nf) || nf.ConsultationID != "unknown" {
		t.Fatalf("%v", err)
	}
	_, err = u.GetConsultationUpdate(ctx, "unknown", "avant", nil)
	var unf *ConsultationUpdateNotFoundError
	if !errors.As(err, &unf) {
		t.Fatalf("%v", err)
	}
	_, err = u.GetConsultationUpdate(ctx, "finished", "unknown-update", nil)
	if !errors.As(err, &unf) || unf.ConsultationID != "finished" || unf.ConsultationUpdateID != "unknown-update" {
		t.Fatalf("%v", err)
	}
	// an update by id or slug
	for _, key := range []string{"autre", "slug-autre"} {
		d, err = u.GetConsultationUpdate(ctx, "ongoing-slug", key, nil)
		if err != nil || d.Update.ID != "autre" || d.Update.FeedbackQuestion == nil || d.FeedbackStats != nil {
			t.Fatalf("%s: %+v %v", key, d, err)
		}
	}
}

func TestParticipantCountIsSharedButNotFrozen(t *testing.T) {
	ctx := context.Background()
	ta := newTestApp(t)
	ta.strapi.set(consultationSpec{id: "c", slug: "s"}.json())
	answered := &fakeAnswered{count: 7}
	u := newDetailsUseCase(ta, answered)
	for i := 0; i < 10; i++ {
		if n, _ := u.participantCount(ctx, "c"); n != 7 {
			t.Fatal(n)
		}
	}
	if answered.calls != 1 {
		t.Fatalf("one query: %d", answered.calls)
	}
	// an error is returned and not cached
	failing := &fakeAnswered{err: errors.New("db down")}
	u.answered = failing
	if _, err := u.participantCount(ctx, "other"); err == nil {
		t.Fatal("error")
	}
	if _, err := u.participantCount(ctx, "other"); err == nil || failing.calls != 2 {
		t.Fatal("not cached")
	}
}

func TestFeedbackQuestionStatsFlagAndOrder(t *testing.T) {
	ctx := context.Background()
	ta := newTestApp(t)
	u := newDetailsUseCase(ta, &fakeAnswered{})
	u.flags = fakeFlags{enabled: false}
	withQuestion := &UpdateInfo{ID: "u", FeedbackQuestion: &FeedbackQuestion{ConsultationUpdateID: "u"}}
	if stats, err := u.feedbackStatsFlagFirst(ctx, withQuestion); stats != nil || err != nil {
		t.Fatalf("%v %v", stats, err)
	}
	if stats, err := u.feedbackStatsQuestionFirst(ctx, withQuestion); stats != nil || err != nil {
		t.Fatalf("%v %v", stats, err)
	}
	u.flags = errFlags{}
	if _, err := u.feedbackStatsFlagFirst(ctx, &UpdateInfo{ID: "u"}); err == nil {
		t.Fatal("the detail use case reads the flag before looking at the question")
	}
	if stats, err := u.feedbackStatsQuestionFirst(ctx, &UpdateInfo{ID: "u"}); stats != nil || err != nil {
		t.Fatal("the update use case looks at the question first")
	}
}

type errFlags struct{}

func (errFlags) IsFeatureEnabled(context.Context, login.Feature) (bool, error) {
	return false, errors.New("garbage")
}
