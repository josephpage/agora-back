package moderatus

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"agora/internal/auth"
	"agora/internal/httpx"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

var ctx = context.Background()

func info(id string, st qag.QagStatus) *qag.QagInfo {
	return &qag.QagInfo{ID: id, Status: st, UserID: "author"}
}

// ---------------------------------------------------------------------------
// ModerateQagOptionsMapperTest
// ---------------------------------------------------------------------------

func TestToModerateQagOptions(t *testing.T) {
	expected := ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: true}
	flag := func(n int) *int { return &n }
	cases := []struct {
		name   string
		status string
		reason *string
		flag   *int
		motif  *string
		want   *ModerateQagOptions // nil = error "Status invalide"
	}{
		{"invalid status", "Coucou !", nil, nil, nil, nil},
		{"OK", "OK", sp("Parce que !"), nil, nil, &ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: true, Reason: sp("Parce que !")}},
		{"NOK", "NOK", sp("Comme ça"), flag(0), nil, &ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: false, Reason: sp("Comme ça")}},
		{"flag null", "OK", nil, nil, nil, &expected},
		{"flag other than 1", "OK", nil, flag(56), nil, &expected},
		{"flag 1 and NOK", "NOK", nil, flag(1), nil, &ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: false, ShouldDelete: true}},
		{"flag 1 but OK", "OK", nil, flag(1), nil, &expected},
		{"motif", "OK", nil, nil, sp("motif42"), &ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: true, MotifID: sp("motif42")}},
		// the status is compared as written
		{"lower case", "ok", nil, nil, nil, nil},
		{"padded", " OK", nil, nil, nil, nil},
		{"empty", "", nil, nil, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ToModerateQagOptions("qagId", "userId", c.status, c.reason, c.flag, c.motif)
			if c.want == nil {
				if got.Error == nil || *got.Error != "Status invalide" {
					t.Fatalf("want the error, got %+v", got)
				}
				return
			}
			if got.Error != nil || !reflect.DeepEqual(got.Options, *c.want) {
				t.Fatalf("got %+v want %+v", got, *c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LockModeratusQagListUseCaseTest
// ---------------------------------------------------------------------------

func TestLockQagIDs(t *testing.T) {
	run := func(t *testing.T, ids []string, locked []string, qags map[string]*qag.QagInfo) ([]ModeratusQagLockResult, []string) {
		log := &callLog{}
		u := &LockModeratusQagListUseCase{qags: &fakeQags{log: log, byID: qags}, locks: &fakeLocks{log: log, locked: locked}}
		got, err := u.LockQagIDs(ctx, ids)
		if err != nil {
			t.Fatal(err)
		}
		return got, log.list()
	}
	nf := func(id string) ModeratusQagLockResult {
		return ModeratusQagLockResult{QagID: id, LockResult: LockNotFound}
	}
	ok := func(id string) ModeratusQagLockResult {
		return ModeratusQagLockResult{QagID: id, LockResult: LockSuccess}
	}

	t.Run("empty list: nothing locked, the locked ids are still read", func(t *testing.T) {
		got, calls := run(t, nil, nil, nil)
		if len(got) != 0 || !reflect.DeepEqual(calls, []string{"getLockedQagIds"}) {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("no QaG", func(t *testing.T) {
		got, calls := run(t, []string{"qagId"}, nil, nil)
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{nf("qagId")}) || !reflect.DeepEqual(calls, []string{"getLockedQagIds", "getQagInfo(qagId)"}) {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("QaG not open", func(t *testing.T) {
		got, calls := run(t, []string{"qagId"}, nil, map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusArchived)})
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{nf("qagId")}) || len(calls) != 2 {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("open but already locked", func(t *testing.T) {
		got, calls := run(t, []string{"qagId"}, []string{"qagId"}, map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusOpen)})
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{nf("qagId")}) || len(calls) != 2 {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("open and not locked: SUCCESS and added", func(t *testing.T) {
		got, calls := run(t, []string{"qagId"}, nil, map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusOpen)})
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{ok("qagId")}) ||
			!reflect.DeepEqual(calls, []string{"getLockedQagIds", "getQagInfo(qagId)", "addLockedIds[qagId]"}) {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("duplicates processed once", func(t *testing.T) {
		got, calls := run(t, []string{"qagId", "qagId", "qagId"}, nil, map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusOpen)})
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{ok("qagId")}) ||
			!reflect.DeepEqual(calls, []string{"getLockedQagIds", "getQagInfo(qagId)", "addLockedIds[qagId]"}) {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("ids are compared as written: an upper case id is not seen as locked", func(t *testing.T) {
		id := "00000000-0000-4000-8000-00000000000A"
		got, _ := run(t, []string{id}, []string{"00000000-0000-4000-8000-00000000000a"},
			map[string]*qag.QagInfo{id: info(id, qag.StatusOpen)})
		if !reflect.DeepEqual(got, []ModeratusQagLockResult{ok(id)}) {
			t.Fatalf("%v", got)
		}
	})
	t.Run("an error before the end locks nothing", func(t *testing.T) {
		log := &callLog{}
		boom := errors.New("boom")
		u := &LockModeratusQagListUseCase{qags: &fakeQags{log: log, err: boom}, locks: &fakeLocks{log: log}}
		if _, err := u.LockQagIDs(ctx, []string{"a"}); !errors.Is(err, boom) {
			t.Fatalf("err %v", err)
		}
		for _, c := range log.list() {
			if c[:3] == "add" {
				t.Fatalf("locked after an error: %v", log.list())
			}
		}
	})
}

// ---------------------------------------------------------------------------
// ModerateModeratusQagUseCaseTest
// ---------------------------------------------------------------------------

func moderate(t *testing.T, st qag.QagStatus, accepted bool, updateFails bool, o ModerateQagOptions) (ModeratusQagModerateResult, []string) {
	t.Helper()
	log := &callLog{}
	i := info("qagId", st)
	u := &ModerateModeratusQagUseCase{
		qags:     &fakeQags{log: log, byID: map[string]*qag.QagInfo{"qagId": i}, updateFails: updateFails},
		notifier: &fakeNotifier{log: log},
		updates:  &fakeUpdates{log: log},
		locks:    &fakeLocks{log: log},
	}
	o.QagID, o.UserID, o.IsAccepted = "qagId", "userId", accepted
	got, err := u.ModerateQag(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	return got, log.list()
}

func TestModerateQag(t *testing.T) {
	const (
		get     = "getQagInfo(qagId)"
		toAcc   = "updateQagStatus(qagId,MODERATED_ACCEPTED)"
		toRej   = "updateQagStatus(qagId,MODERATED_REJECTED)"
		motifN  = "updateQagMotifId(qagId,null)"
		insAcc  = "insertQagUpdates(qagId,MODERATED_ACCEPTED,userId,null,false,null)"
		insRej  = "insertQagUpdates(qagId,MODERATED_REJECTED,userId,null,false,null)"
		unlock  = "removeLockedQagId(qagId)"
		sendAcc = "sendNotificationQagAccepted(qagId)"
		sendRej = "sendNotificationQagRejected(qagId)"
		sendAft = "sendNotificationQagAcceptedAfterReject(qagId)"
	)
	cases := []struct {
		name     string
		status   qag.QagStatus
		accepted bool
		fails    bool
		want     ModeratusQagModerateResult
		calls    []string
	}{
		{"unknown QaG is NOT_FOUND", qag.StatusOpen, true, false, ModerateNotFound, nil}, // replaced below
		{"ARCHIVED is NOT_FOUND", qag.StatusArchived, true, false, ModerateNotFound, []string{get}},
		{"SELECTED_FOR_RESPONSE is NOT_FOUND", qag.StatusSelectedForResponse, true, false, ModerateNotFound, []string{get}},
		{"valid status but the update fails", qag.StatusModeratedAccepted, false, true, ModerateFailure, []string{get, toRej}},
		{"OPEN accepted", qag.StatusOpen, true, false, ModerateSuccess, []string{get, toAcc, sendAcc, unlock, motifN, insAcc}},
		{"OPEN rejected", qag.StatusOpen, false, false, ModerateSuccess, []string{get, toRej, sendRej, unlock, motifN, insRej}},
		{"MODERATED_ACCEPTED accepted again", qag.StatusModeratedAccepted, true, false, ModerateSuccess, []string{get, motifN, insAcc}},
		{"MODERATED_ACCEPTED rejected", qag.StatusModeratedAccepted, false, false, ModerateSuccess, []string{get, toRej, sendRej, motifN, insRej}},
		{"MODERATED_REJECTED rejected again", qag.StatusModeratedRejected, false, false, ModerateSuccess, []string{get, motifN, insRej}},
		{"MODERATED_REJECTED accepted", qag.StatusModeratedRejected, true, false, ModerateSuccess, []string{get, toAcc, sendAft, motifN, insAcc}},
	}
	for _, c := range cases[1:] {
		t.Run(c.name, func(t *testing.T) {
			got, calls := moderate(t, c.status, c.accepted, c.fails, ModerateQagOptions{})
			if got != c.want || !reflect.DeepEqual(calls, c.calls) {
				t.Fatalf("got %v %v\nwant %v %v", got, calls, c.want, c.calls)
			}
		})
	}
	t.Run(cases[0].name, func(t *testing.T) {
		log := &callLog{}
		u := &ModerateModeratusQagUseCase{qags: &fakeQags{log: log}, notifier: &fakeNotifier{log: log}, updates: &fakeUpdates{log: log}, locks: &fakeLocks{log: log}}
		got, err := u.ModerateQag(ctx, ModerateQagOptions{QagID: "notFoundQagId"})
		if err != nil || got != ModerateNotFound || !reflect.DeepEqual(log.list(), []string{"getQagInfo(notFoundQagId)"}) {
			t.Fatalf("%v %v %v", got, err, log.list())
		}
	})
	t.Run("OPEN accepted passes reason, delete flag and motif to the update", func(t *testing.T) {
		got, calls := moderate(t, qag.StatusOpen, true, false, ModerateQagOptions{Reason: sp("reason"), ShouldDelete: true, MotifID: sp("motif123")})
		want := []string{"getQagInfo(qagId)", "updateQagStatus(qagId,MODERATED_ACCEPTED)", "sendNotificationQagAccepted(qagId)", "removeLockedQagId(qagId)",
			"updateQagMotifId(qagId,motif123)", "insertQagUpdates(qagId,MODERATED_ACCEPTED,userId,reason,true,motif123)"}
		if got != ModerateSuccess || !reflect.DeepEqual(calls, want) {
			t.Fatalf("%v %v", got, calls)
		}
	})
	t.Run("a notification error stops the moderation after the status update", func(t *testing.T) {
		log := &callLog{}
		boom := errors.New("NullPointerException")
		u := &ModerateModeratusQagUseCase{
			qags:     &fakeQags{log: log, byID: map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusOpen)}},
			notifier: &fakeNotifier{log: log, err: boom}, updates: &fakeUpdates{log: log}, locks: &fakeLocks{log: log},
		}
		got, err := u.ModerateQag(ctx, ModerateQagOptions{QagID: "qagId", UserID: "userId", IsAccepted: true})
		want := []string{"getQagInfo(qagId)", "updateQagStatus(qagId,MODERATED_ACCEPTED)", "sendNotificationQagAccepted(qagId)"}
		if !errors.Is(err, boom) || got != ModerateFailure || !reflect.DeepEqual(log.list(), want) {
			t.Fatalf("%v %v %v", got, err, log.list())
		}
	})
	t.Run("the shared QaG lists are dropped once the status is committed", func(t *testing.T) {
		evictions := 0
		log := &callLog{}
		u := &ModerateModeratusQagUseCase{
			qags:     &fakeQags{log: log, byID: map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusOpen)}},
			notifier: &fakeNotifier{log: log}, updates: &fakeUpdates{log: log}, locks: &fakeLocks{log: log},
			afterStatusChange: func(context.Context) { evictions++ },
		}
		if _, err := u.ModerateQag(ctx, ModerateQagOptions{QagID: "qagId", UserID: "u", IsAccepted: true}); err != nil || evictions != 1 {
			t.Fatalf("evictions %d %v", evictions, err)
		}
		// accepted again: the status does not change, nothing to drop
		u.qags = &fakeQags{log: log, byID: map[string]*qag.QagInfo{"qagId": info("qagId", qag.StatusModeratedAccepted)}}
		if _, err := u.ModerateQag(ctx, ModerateQagOptions{QagID: "qagId", UserID: "u", IsAccepted: true}); err != nil || evictions != 1 {
			t.Fatalf("evictions %d %v", evictions, err)
		}
	})
}

// ---------------------------------------------------------------------------
// ModeratusLoginUseCaseTest
// ---------------------------------------------------------------------------

type fakeTokens struct {
	user string
	err  error
}

func (f fakeTokens) Decode(string) (string, error) { return f.user, f.err }

type fakeUserFinder struct {
	user *users.UserInfo
	err  error
}

func (f fakeUserFinder) FindUser(context.Context, string) (*users.UserInfo, error) {
	return f.user, f.err
}

func TestModeratusLogin(t *testing.T) {
	login := func(tokens fakeTokens, finder fakeUserFinder) (ModeratusLoginResult, error) {
		return (&ModeratusLoginUseCase{tokens: tokens, users: finder}).Login(ctx, "loginToken")
	}
	t.Run("decode failure", func(t *testing.T) {
		got, err := login(fakeTokens{err: auth.ErrLoginTokenDecode}, fakeUserFinder{})
		if err != nil || got.Success {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("unknown user", func(t *testing.T) {
		got, err := login(fakeTokens{user: "userId"}, fakeUserFinder{})
		if err != nil || got.Success {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("no MODERATE_QAG authorization", func(t *testing.T) {
		got, err := login(fakeTokens{user: "userId"}, fakeUserFinder{user: &users.UserInfo{UserID: "userId"}})
		if err != nil || got.Success {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("MODERATE_QAG: success with the user id", func(t *testing.T) {
		got, err := login(fakeTokens{user: "userId"}, fakeUserFinder{user: &users.UserInfo{UserID: "userId", Authorizations: []string{httpx.AuthModerateQag}}})
		if err != nil || got != (ModeratusLoginResult{Success: true, UserID: "userId"}) {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a user lookup error is an exception", func(t *testing.T) {
		boom := errors.New("boom")
		if _, err := login(fakeTokens{user: "userId"}, fakeUserFinder{err: boom}); !errors.Is(err, boom) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("the user id comes from the database, not from the token", func(t *testing.T) {
		got, _ := login(fakeTokens{user: "ABC"}, fakeUserFinder{user: &users.UserInfo{UserID: "abc", Authorizations: []string{httpx.AuthModerateQag}}})
		if got.UserID != "abc" {
			t.Fatalf("%+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// GetModeratusQagListUseCase + ModeratusQagMapper
// ---------------------------------------------------------------------------

func TestGetQagModeratingList(t *testing.T) {
	d := time.Date(2024, 1, 4, 16, 0, 0, 0, time.Local)
	log := &callLog{}
	u := &GetModeratusQagListUseCase{qags: &fakeQags{log: log, toModerate: []qag.QagInfo{
		{ID: "a", ThematiqueID: "t", Title: "title", Description: "desc", Date: d, Status: qag.StatusOpen, Username: "name", UserID: "u"},
	}}}
	got, err := u.GetQagModeratingList(ctx)
	want := []ModeratusQag{{QagID: "a", Title: "title", Description: "desc", Date: d, Username: "name", UserID: "u"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSplitLockedQagIDs(t *testing.T) {
	cases := map[string][]string{
		"":        nil,
		";":       nil,
		" ; ;\t":  nil,
		"a":       {"a"},
		"a;b;;c":  {"a", "b", "c"},
		" a ; b ": {"a", "b"},
		"a;a":     {"a", "a"},
		" a ":     {"a"},  // Kotlin trim: Char.isWhitespace includes NBSP
		"​a":      {"​a"}, // ZERO WIDTH SPACE is not whitespace
		"a ;　b\n": {"a", "b"},
	}
	for in, want := range cases {
		if got := splitLockedQagIDs(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
