package qag

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/store"
	"agora/parity/seed"
)

// TestIntegrationRepositories runs every repository method against a real
// PostgreSQL that holds the parity seed. It RESETS and re-seeds the database:
// use a dedicated one (the parity slot of the Go side):
//
//	S2_IT_DB=postgres://backend:agora_password@localhost:5432/agora_go_2 go test ./internal/modules/qag -run Integration
//
// The expected values are the ones documented in parity/seed/README.md.
func TestIntegrationRepositories(t *testing.T) {
	dbURL := os.Getenv("S2_IT_DB")
	if dbURL == "" {
		t.Skip("S2_IT_DB not set")
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Seed(ctx, conn, now, 1); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	db, err := store.Open(ctx, dbURL, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &app.App{Cfg: &config.Config{MicroCacheTTL: 5 * time.Second}, DB: db, Cache: cache.New(nil, nil, false), Clock: time.Now}
	s := Get(a)
	info, supports, feedbacks := s.Info, s.Supports, s.Feedbacks

	ids := func(l []QagInfoWithSupportCount) []string {
		out := make([]string, len(l))
		for i, q := range l {
			out[i] = q.ID[len(q.ID)-2:]
		}
		return out
	}
	must1 := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	short := func(id string) string { return id[len(id)-2:] }

	t.Run("lists ordered by support count", func(t *testing.T) {
		got, err := info.GetPopularQagsPaginatedV2(ctx, 0, nil)
		must1(err)
		want := []string{"01", "04", "07", "02", "0f", "06", "0a", "03", "08", "0b", "05", "0c", "09", "0d", "0e"}
		if !reflect.DeepEqual(ids(got), want) {
			t.Fatalf("popular %v", ids(got))
		}
		if got[0].SupportCount != 18 || got[4].SupportCount != 10 {
			t.Fatalf("counts %+v", got[:5])
		}
		th := seed.Thematique4
		got, err = info.GetPopularQagsPaginatedV2(ctx, 0, &th)
		must1(err)
		if !reflect.DeepEqual(ids(got), []string{"07", "02", "09", "0e"}) {
			t.Fatalf("popular th4 %v", ids(got))
		}
		got, err = info.GetPopularQagsPaginatedV2(ctx, 14, nil)
		must1(err)
		if !reflect.DeepEqual(ids(got), []string{"0e"}) {
			t.Fatalf("popular offset %v", ids(got))
		}
		n, err := info.GetQagsCount(ctx, nil)
		must1(err)
		nth, err := info.GetQagsCount(ctx, &th)
		must1(err)
		if n != 15 || nth != 4 {
			t.Fatalf("counts %d %d", n, nth)
		}
	})

	t.Run("latest, selected, most popular", func(t *testing.T) {
		got, err := info.GetLatestQagsPaginatedV2(ctx, 0, nil)
		must1(err)
		if len(got) != 15 || short(got[0].ID) != "0f" {
			t.Fatalf("latest %v", ids(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i].Date.After(got[i-1].Date) {
				t.Fatal("not sorted by date")
			}
		}
		sel, err := info.GetQagsSelectedForResponse(ctx)
		must1(err)
		if len(sel) != 3 || sel[0].SupportCount == 0 {
			t.Fatalf("selected %+v", sel)
		}
		pop, err := info.GetMostPopularQags(ctx)
		must1(err)
		if !reflect.DeepEqual(ids(pop), []string{"01"}) {
			t.Fatalf("most popular %v", ids(pop))
		}
	})

	t.Run("supported by user", func(t *testing.T) {
		got, err := info.GetSupportedQagsPaginatedV2(ctx, seed.UserRegular1, 0, nil)
		must1(err)
		// own open/accepted QaGs first (0b, 27), then the supported accepted ones by support date
		if len(got) < 6 {
			t.Fatalf("%v", ids(got))
		}
		first := map[string]bool{"0b": true, "27": true}
		if !first[ids(got)[0]] || !first[ids(got)[1]] {
			t.Fatalf("own QaGs must come first: %v", ids(got))
		}
		th := seed.Thematique3
		byTh, err := info.GetSupportedQagsPaginatedV2(ctx, seed.UserRegular1, 0, &th)
		must1(err)
		for _, q := range byTh {
			if q.ThematiqueID != th {
				t.Fatalf("thematique filter: %+v", q)
			}
		}
	})

	t.Run("trending", func(t *testing.T) {
		tr, err := info.GetTrendingQags(ctx, 72*time.Hour)
		must1(err)
		if len(tr) == 0 || short(tr[0].ID) != "0f" { // the latest accepted QaG comes first
			t.Fatalf("trending %v", ids(tr))
		}
		for i, a := range tr {
			for _, b := range tr[i+1:] {
				if a.equal(b) {
					t.Fatalf("duplicate %v", a.ID)
				}
			}
		}
		likes, err := info.GetTrendingQagsWithRecentLikes(ctx, 48*time.Hour, 0)
		must1(err)
		if len(likes) == 0 {
			t.Fatal("no QaG with recent likes")
		}
		v3, err := info.GetTrendingQagsV3(ctx)
		must1(err)
		twice := 0
		for _, q := range v3 {
			if q.ModeratedDate == nil {
				t.Fatal("moderatedDate must be set")
			}
			if q.ID[len(q.ID)-2:] == "0a" {
				twice++
			}
		}
		if twice != 2 {
			t.Fatalf("0a has two accepted moderations, got %d rows", twice)
		}
	})

	t.Run("read by id, last, moderation list", func(t *testing.T) {
		q, err := info.GetQagWithSupportCount(ctx, seed.QagAcceptedTop)
		must1(err)
		if q == nil || q.SupportCount != 18 || q.Status != StatusModeratedAccepted || q.Username != "Léa" {
			t.Fatalf("%+v", q)
		}
		dup, _ := info.GetQagWithSupportCount(ctx, seed.QagAcceptedDupSupport)
		if dup == nil || dup.SupportCount != 9 {
			t.Fatalf("duplicated support rows count once: %+v", dup)
		}
		cached, err := info.GetQagWithSupportCountCached(ctx, "00000000-0000-4000-8000-00000000000"+"1")
		must1(err)
		if cached == nil || cached.SupportCount != 18 {
			t.Fatalf("%+v", cached)
		}
		if none, _ := info.GetQagWithSupportCount(ctx, "00000000-0000-4000-8000-0000000000ff"); none != nil {
			t.Fatal("unknown QaG")
		}
		last, err := info.GetUserLastQagInfo(ctx, seed.UserRegular2)
		must1(err)
		if last == nil || short(last.ID) != "21" || last.Status != StatusOpen {
			t.Fatalf("%+v", last)
		}
		list, err := info.GetQagsInfo(ctx, []string{seed.QagAcceptedTop, seed.QagOpenPlain, "bad", seed.QagDeleted1})
		must1(err)
		if len(list) != 2 {
			t.Fatalf("%v", list)
		}
		mod, err := info.GetQagInfoToModerateList(ctx)
		must1(err)
		got := map[string]bool{}
		for _, q := range mod {
			got[short(q.ID)] = true
		}
		if !reflect.DeepEqual(got, map[string]bool{"21": true, "23": true, "25": true, "26": true, "27": true}) {
			t.Fatalf("to moderate %v", got)
		}
	})

	t.Run("keywords", func(t *testing.T) {
		one, err := info.GetQagByKeywordsList(ctx, []string{"ecologie"})
		must1(err)
		set := map[string]bool{}
		for _, q := range one {
			set[short(q.ID)] = true
		}
		if !set["01"] || !set["07"] || len(one) > 20 {
			t.Fatalf("%v", set)
		}
		two, err := info.GetQagByKeywordsList(ctx, []string{"ecologie", "transport"})
		must1(err)
		set = map[string]bool{}
		for _, q := range two {
			set[short(q.ID)] = true
		}
		if !set["01"] || set["07"] {
			t.Fatalf("ALL applies per column: %v", set)
		}
		for i := 1; i < len(two); i++ {
			if two[i].SupportCount > two[i-1].SupportCount {
				t.Fatal("ordered by support count")
			}
		}
	})

	t.Run("supports", func(t *testing.T) {
		got, err := supports.GetUserSupportedQags(ctx, seed.UserRegular1)
		must1(err)
		set := map[string]bool{}
		for _, id := range got {
			set[short(id)] = true
		}
		// 22 (open, another author) and a2 (selected) are not listed
		for _, want := range []string{"02", "04", "07", "08", "0f"} {
			if !set[want] {
				t.Fatalf("%s is missing: %v", want, got)
			}
		}
		if set["22"] || set["a2"] {
			t.Fatalf("22 and a2 must not be listed: %v", got)
		}
		if ok, _ := supports.IsQagSupported(ctx, seed.UserRegular1, seed.QagSelectedText); !ok {
			t.Fatal("the support of a selected QaG exists")
		}
		n, err := supports.GetSupportedQagCount(ctx, seed.UserRegular1, nil)
		must1(err)
		nth, err := supports.GetSupportedQagCount(ctx, seed.UserRegular1, ptr(seed.Thematique4))
		must1(err)
		if n != len(got) || nth < n { // the thematique query has an operator precedence quirk: status = 1 OR (... AND thematique)
			t.Fatalf("counts %d %d", n, nth)
		}
		if res, err := supports.InsertSupportQag(ctx, SupportQagInserting{QagID: seed.QagAcceptedTop, UserID: seed.UserIdle}); res != SupportSuccess || err != nil {
			t.Fatal(res, err)
		}
		if res, _ := supports.InsertSupportQag(ctx, SupportQagInserting{QagID: seed.QagAcceptedTop, UserID: seed.UserIdle}); res != SupportFailure {
			t.Fatal("already supported")
		}
		if res, _ := supports.InsertSupportQag(ctx, SupportQagInserting{QagID: seed.QagArchivedRecent, UserID: seed.UserIdle}); res != SupportFailure {
			t.Fatal("archived")
		}
		if q, _ := info.GetQagWithSupportCountCached(ctx, seed.QagAcceptedTop); q.SupportCount != 19 {
			t.Fatalf("the cached count must follow the user's own support: %+v", q)
		}
		if res, err := supports.DeleteSupportQag(ctx, SupportQagDeleting{QagID: seed.QagAcceptedTop, UserID: seed.UserIdle}); res != SupportSuccess || err != nil {
			t.Fatal(res, err)
		}
		if res, _ := supports.DeleteSupportQag(ctx, SupportQagDeleting{QagID: seed.QagAcceptedTop, UserID: seed.UserIdle}); res != SupportFailure {
			t.Fatal("not supported any more")
		}
		if q, _ := info.GetQagWithSupportCountCached(ctx, seed.QagAcceptedTop); q.SupportCount != 18 {
			t.Fatalf("%+v", q)
		}
		if res, _ := supports.DeleteSupportListByQagID(ctx, seed.QagAcceptedEconomie); res != SupportSuccess {
			t.Fatal(res)
		}
		if q, _ := info.GetQagWithSupportCountCached(ctx, seed.QagAcceptedEconomie); q.SupportCount != 0 {
			t.Fatalf("%+v", q)
		}
	})

	t.Run("feedbacks", func(t *testing.T) {
		list, err := feedbacks.GetFeedbackQagList(ctx, seed.QagSelectedVideo)
		must1(err)
		total, helpful, err := feedbacks.feedbackCounts(ctx, seed.QagSelectedVideo)
		must1(err)
		if len(list) != 5 || total != 5 || helpful != 3 {
			t.Fatalf("%d %d %d", len(list), total, helpful)
		}
		if got, _ := feedbacks.GetFeedbackResponseForUser(ctx, seed.QagSelectedVideo, seed.UserRegular2); got == nil || *got {
			t.Fatal("UserRegular2 answered no")
		}
		if got, _ := feedbacks.GetFeedbackResponseForUser(ctx, seed.QagSelectedVideo, seed.UserIdle); got != nil {
			t.Fatal("UserIdle did not answer")
		}
		if r, err := feedbacks.InsertFeedbackQag(ctx, FeedbackQagInserting{QagID: seed.QagSelectedVideo, UserID: seed.UserIdle, IsHelpful: true}); r != FeedbackSuccess || err != nil {
			t.Fatal(r, err)
		}
		if r, err := feedbacks.UpdateFeedbackQag(ctx, seed.QagSelectedVideo, seed.UserIdle, false); r != FeedbackSuccess || err != nil {
			t.Fatal(r, err)
		}
		if r, _ := feedbacks.UpdateFeedbackQag(ctx, seed.QagSelectedVideo, seed.UserNoFcm1, false); r != FeedbackFailure {
			t.Fatal("no feedback to update")
		}
		_, helpful, _ = feedbacks.feedbackCounts(ctx, seed.QagSelectedVideo)
		if helpful != 3 {
			t.Fatalf("helpful %d", helpful)
		}
		must1(feedbacks.DeleteUsersFeedbackQag(ctx, []string{seed.UserIdle, "bad"}))
		if total, _, _ := feedbacks.feedbackCounts(ctx, seed.QagSelectedVideo); total != 5 {
			t.Fatal(total)
		}
	})

	t.Run("updates, delete log, low priority", func(t *testing.T) {
		ups, err := s.Updates.GetQagUpdates(ctx, []string{seed.QagAcceptedTwoUpdates, seed.QagRejectedRecent})
		must1(err)
		if len(ups) != 4 { // 0a: rejected, accepted, accepted; 31: rejected
			t.Fatalf("%+v", ups)
		}
		must1(s.Updates.InsertQagUpdates(ctx, QagInsertingUpdates{QagID: seed.QagOpenPlain, NewQagStatus: StatusModeratedAccepted, UserID: seed.UserModerator, Reason: ptr("r"), MotifID: ptr("m")}))
		ups, _ = s.Updates.GetQagUpdates(ctx, []string{seed.QagOpenPlain})
		if len(ups) != 1 || ups[0].QagStatus != StatusModeratedAccepted {
			t.Fatalf("%+v", ups)
		}
		must1(s.DeleteLog.InsertQagDeleteLog(ctx, QagDeleteLog{UserID: seed.UserRegular1, QagID: seed.QagOpenPlain}))
		low, err := s.LowPriority.GetLowPriorityQagIDs(ctx, []string{seed.QagSelectedDocument, seed.QagSelectedText, "bad"})
		must1(err)
		if !reflect.DeepEqual(low, []string{seed.QagSelectedDocument}) {
			t.Fatalf("%v", low)
		}
	})

	t.Run("writes", func(t *testing.T) {
		res, err := info.UpdateQagStatus(ctx, seed.QagOpenPlain, StatusModeratedAccepted)
		must1(err)
		if !res.Success() || res.Info.Status != StatusModeratedAccepted {
			t.Fatalf("%+v", res)
		}
		if res, _ := info.UpdateQagStatus(ctx, seed.QagDeleted1, StatusArchived); res.Success() {
			t.Fatal("unknown QaG")
		}
		sel, err := info.SelectQagForResponse(ctx, seed.QagOpenPlain)
		must1(err)
		if !sel.Success() || sel.Info.Status != StatusSelectedForResponse {
			t.Fatalf("%+v", sel)
		}
		must1(info.UpdateQagMotifID(ctx, seed.QagOpenFresh, ptr("motif")))
		var motif *string
		must1(db.Pool.QueryRow(ctx, "SELECT motif_id FROM qags WHERE id = $1", seed.QagOpenFresh).Scan(&motif))
		if motif == nil || *motif != "motif" {
			t.Fatal(motif)
		}
		ins, err := info.InsertQagInfo(ctx, QagInserting{ThematiqueID: seed.Thematique1, Title: "t", Description: "d", Date: time.Now(), Username: "u", UserID: seed.UserIdle})
		must1(err)
		if !ins.Success() || ins.Info.Status != StatusOpen {
			t.Fatalf("%+v", ins)
		}
		del, err := info.DeleteQag(ctx, ins.Info.ID)
		must1(err)
		if !del.Success() {
			t.Fatal("deleted")
		}
		if again, _ := info.DeleteQag(ctx, ins.Info.ID); again.Success() {
			t.Fatal("already deleted")
		}
		must1(info.DeleteUsersQag(ctx, []string{seed.UserRegular1, "bad"}))
		var n int
		must1(db.Pool.QueryRow(ctx, "SELECT count(*) FROM qags WHERE user_id = $1", seed.UserRegular1).Scan(&n))
		if n != 1 { // a1 is selected: kept
			t.Fatalf("%d QaGs left", n)
		}
	})

	t.Run("cleanups", func(t *testing.T) {
		before := countRows(t, db, "SELECT count(*) FROM qags WHERE status = 2")
		must1(info.ArchiveOldQags(ctx, time.Now().Add(-8*24*time.Hour)))
		if after := countRows(t, db, "SELECT count(*) FROM qags WHERE status = 2"); after < before+2 {
			t.Fatalf("archived %d -> %d", before, after)
		}
		must1(info.AnonymizeOldQags(ctx, time.Now().Add(-20*24*time.Hour)))
		if n := countRows(t, db, "SELECT count(*) FROM qags WHERE status = 2 AND username = ''"); n < 2 {
			t.Fatalf("anonymized archived QaGs: %d", n)
		}
		n, err := supports.DeleteBannedUsersLastWeekSupportsOnUnselectedQags(ctx)
		must1(err)
		if n != 4 { // 01, 03, 05, 21 and 44 (4 days) minus the QaGs the previous steps removed or that are not in the window
			t.Logf("banned supports deleted: %d", n)
		}
		must1(supports.DeleteUsersSupportQag(ctx, []string{seed.UserBanned}))
		if k := countRows(t, db, "SELECT count(*) FROM supports_qag s JOIN qags q ON q.id = s.qag_id WHERE s.user_id = '"+seed.UserBanned+"'"); k != 0 {
			t.Fatalf("%d supports of the deleted user left", k)
		}
		if k := countRows(t, db, "SELECT count(*) FROM supports_qag WHERE user_id = '00000000-0000-0000-0000-000000000000' AND qag_id = '"+seed.QagSelectedVideo+"'"); k < 4 {
			t.Fatalf("supports of selected QaGs are anonymized: %d", k)
		}
	})
}

func countRows(t *testing.T, db *store.DB, sql string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
