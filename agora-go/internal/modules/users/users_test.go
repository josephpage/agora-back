package users

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
)

// fakeStore records the SQL-level calls of the repository (the Mockito
// verifications of UserRepositoryImplTest).
type fakeStore struct {
	calls   []string
	rows    map[string]*userRow
	all     []*userRow
	updated []string
	inserts []*userRow
	deleted [][]string
	levels  []int
	fail    error
}

func (f *fakeStore) getUserByID(_ context.Context, id string) (*userRow, error) {
	f.calls = append(f.calls, "getUserByID "+id)
	return f.rows[id], f.fail
}
func (f *fakeStore) findAll(context.Context) ([]*userRow, error) {
	f.calls = append(f.calls, "findAll")
	return f.all, nil
}
func (f *fakeStore) insertUser(_ context.Context, r *userRow) error {
	f.calls = append(f.calls, "insertUser")
	f.inserts = append(f.inserts, r)
	return f.fail
}
func (f *fakeStore) updateLogin(_ context.Context, id, fcm string, _ time.Time) error {
	f.calls = append(f.calls, "updateLogin "+id+" "+fcm)
	f.updated = append(f.updated, fcm)
	return f.fail
}
func (f *fakeStore) usersNotAnsweredConsultation(context.Context, string) ([]*userRow, error) {
	return f.all, nil
}
func (f *fakeStore) usersLivingInDepartement(context.Context, string) ([]*userRow, error) {
	return f.all, nil
}
func (f *fakeStore) usersInterestedInDepartement(context.Context, string) ([]*userRow, error) {
	return f.all, nil
}
func (f *fakeStore) deleteUsers(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids)
	return nil
}
func (f *fakeStore) updateAuthorizationLevel(_ context.Context, ids []string, level int) (int, error) {
	f.levels = append(f.levels, level)
	return len(ids), nil
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

var (
	t0     = time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	userID = "bc9e81be-eb4d-11ed-a05b-0242ac120003"
)

func goodRow(id string) *userRow {
	return &userRow{ID: id, Password: sp(""), FCMToken: sp("fcm"), CreatedDate: &t0, AuthorizationLevel: 0, IsBanned: ip(0), LastConnectionDate: &t0}
}

func newTestService(st *fakeStore) *Service {
	a := &app.App{Cache: cache.New(nil, nil, false), Clock: func() time.Time { return t0 }}
	return &Service{a: a, st: st}
}

func TestGetAllUsersMapsDatabaseRows(t *testing.T) {
	st := &fakeStore{all: []*userRow{goodRow(userID)}}
	s := newTestService(st)
	got, err := s.GetAllUsers(context.Background())
	if err != nil || len(got) != 1 || got[0].UserID != userID || got[0].FCMToken != "fcm" || got[0].IsBanned {
		t.Fatalf("got %+v err %v", got, err)
	}
	if !reflect.DeepEqual(st.calls, []string{"findAll"}) {
		t.Errorf("calls %v", st.calls)
	}
}

func TestGetUserByID(t *testing.T) {
	ctx := context.Background()
	t.Run("invalid UUID: nothing is touched", func(t *testing.T) {
		st := &fakeStore{}
		got, err := newTestService(st).FindUser(ctx, "invalid userId")
		if got != nil || err != nil || len(st.calls) != 0 {
			t.Errorf("got %v %v calls %v", got, err, st.calls)
		}
	})
	t.Run("cache miss and unknown in database: negative entry, then no database call", func(t *testing.T) {
		st := &fakeStore{rows: map[string]*userRow{}}
		s := newTestService(st)
		for i := 0; i < 2; i++ {
			if got, err := s.FindUser(ctx, userID); got != nil || err != nil {
				t.Fatalf("got %v %v", got, err)
			}
		}
		if !reflect.DeepEqual(st.calls, []string{"getUserByID " + userID}) {
			t.Errorf("calls %v", st.calls)
		}
	})
	t.Run("cache miss and found in database: mapped result, cached", func(t *testing.T) {
		st := &fakeStore{rows: map[string]*userRow{userID: goodRow(userID)}}
		s := newTestService(st)
		for i := 0; i < 2; i++ {
			got, err := s.FindUser(ctx, userID)
			if err != nil || got == nil || got.UserID != userID || len(got.Authorizations) != 6 {
				t.Fatalf("got %+v %v", got, err)
			}
		}
		if len(st.calls) != 1 {
			t.Errorf("calls %v", st.calls)
		}
	})
	t.Run("uppercase / lenient UUID spelling resolves to the canonical key", func(t *testing.T) {
		st := &fakeStore{rows: map[string]*userRow{"00000000-0000-0000-0000-000000000001": goodRow("00000000-0000-0000-0000-000000000001")}}
		got, err := newTestService(st).FindUser(ctx, "0-0-0-0-1")
		if err != nil || got == nil || got.UserID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("got %+v %v", got, err)
		}
	})
	t.Run("NULL last_connection_date: readable once, then the Kotlin cache entry is undeserializable", func(t *testing.T) {
		row := goodRow(userID)
		row.LastConnectionDate = nil
		st := &fakeStore{rows: map[string]*userRow{userID: row}}
		s := newTestService(st)
		if got, err := s.FindUser(ctx, userID); err != nil || got == nil {
			t.Fatalf("first access: %v %v", got, err)
		}
		if _, err := s.FindUser(ctx, userID); !errors.Is(err, ErrUnreadableCacheEntry) {
			t.Errorf("second access: %v", err)
		}
		// an invalidation (cache eviction) makes the user readable again once
		s.Invalidate(ctx, userID)
		if _, err := s.FindUser(ctx, userID); err != nil {
			t.Errorf("after eviction: %v", err)
		}
	})
	t.Run("NULL fcm_token or is_banned: always an error", func(t *testing.T) {
		for _, mutate := range []func(*userRow){func(r *userRow) { r.FCMToken = nil }, func(r *userRow) { r.IsBanned = nil }} {
			row := goodRow(userID)
			mutate(row)
			s := newTestService(&fakeStore{rows: map[string]*userRow{userID: row}})
			for i := 0; i < 2; i++ {
				if _, err := s.FindUser(ctx, userID); !errors.Is(err, ErrCorruptUser) {
					t.Errorf("access %d: %v", i, err)
				}
			}
		}
	})
}

func TestUpdateUser(t *testing.T) {
	ctx := context.Background()
	req := LoginRequest{UserID: userID, FCMToken: "new-token"}
	t.Run("invalid UUID: nothing is touched", func(t *testing.T) {
		st := &fakeStore{}
		got, err := newTestService(st).UpdateUser(ctx, LoginRequest{UserID: "Invalid user UUID"})
		if got != nil || err != nil || len(st.calls) != 0 {
			t.Errorf("got %v %v calls %v", got, err, st.calls)
		}
	})
	t.Run("unknown user: null, negative entry", func(t *testing.T) {
		st := &fakeStore{rows: map[string]*userRow{}}
		s := newTestService(st)
		if got, err := s.UpdateUser(ctx, req); got != nil || err != nil {
			t.Fatalf("got %v %v", got, err)
		}
		if got, _ := s.UpdateUser(ctx, req); got != nil {
			t.Fatal("second call must stay null")
		}
		if len(st.calls) != 1 {
			t.Errorf("calls %v", st.calls)
		}
	})
	t.Run("known user: stores the token, returns the updated user, refreshes the cache", func(t *testing.T) {
		row := goodRow(userID)
		row.IsBanned = ip(1)
		row.AuthorizationLevel = LevelAdmin
		st := &fakeStore{rows: map[string]*userRow{userID: row}}
		s := newTestService(st)
		got, err := s.UpdateUser(ctx, req)
		if err != nil || got == nil || got.FCMToken != "new-token" || !got.IsBanned || len(got.Authorizations) != 9 {
			t.Fatalf("got %+v %v", got, err)
		}
		if !reflect.DeepEqual(st.updated, []string{"new-token"}) {
			t.Errorf("updates %v", st.updated)
		}
		// the entry was evicted: the next lookup reads the database again
		if _, err := s.FindUser(ctx, userID); err != nil || len(st.calls) != 3 {
			t.Errorf("err %v calls %v", err, st.calls)
		}
	})
	t.Run("NULL last_connection_date: the update fails like the Kotlin cache read", func(t *testing.T) {
		row := goodRow(userID)
		row.LastConnectionDate = nil
		st := &fakeStore{rows: map[string]*userRow{userID: row}}
		s := newTestService(st)
		if _, err := s.FindUser(ctx, userID); err != nil { // LoginUseCase.getUserById
			t.Fatal(err)
		}
		if _, err := s.UpdateUser(ctx, req); !errors.Is(err, ErrUnreadableCacheEntry) {
			t.Errorf("got %v", err)
		}
		if len(st.updated) != 0 {
			t.Errorf("updates %v", st.updated)
		}
	})
}

func TestGenerateUser(t *testing.T) {
	st := &fakeStore{rows: map[string]*userRow{}}
	s := newTestService(st)
	got, err := s.GenerateUser(context.Background(), SignupRequest{FCMToken: "tok", UserAgent: "ua"})
	if err != nil || got == nil || got.FCMToken != "tok" || got.IsBanned || len(got.Authorizations) != 6 {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(st.inserts) != 1 {
		t.Fatalf("inserts %d", len(st.inserts))
	}
	r := st.inserts[0]
	if r.ID != got.UserID || *r.Password != "" || *r.FCMToken != "tok" || r.AuthorizationLevel != 0 || *r.IsBanned != 0 ||
		!r.CreatedDate.Equal(t0) || !r.LastConnectionDate.Equal(t0) {
		t.Errorf("row %+v", r)
	}
	// Kotlin put the saved DTO in userCache: no database read for the next lookup
	if again, err := s.FindUser(context.Background(), got.UserID); err != nil || again == nil || len(st.calls) != 1 {
		t.Errorf("again %v %v calls %v", again, err, st.calls)
	}
}

func TestDeleteAndChangeLevelKeepOnlyValidUUIDs(t *testing.T) {
	st := &fakeStore{}
	s := newTestService(st)
	ctx := context.Background()
	if err := s.DeleteUsers(ctx, []string{userID, "nope", "0-0-0-0-1"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.ChangeAuthorizationLevel(ctx, []string{"nope"}, LevelPublisher)
	if err != nil || n != 0 {
		t.Fatalf("n %d err %v", n, err)
	}
	if !reflect.DeepEqual(st.deleted, [][]string{{userID, "00000000-0000-0000-0000-000000000001"}}) {
		t.Errorf("deleted %v", st.deleted)
	}
	if !reflect.DeepEqual(st.levels, []int{LevelPublisher}) {
		t.Errorf("levels %v", st.levels)
	}
}

func TestAuthorizationsFor(t *testing.T) {
	for level, want := range map[int]int{LevelDefault: 6, LevelModerator: 7, LevelPublisher: 7, LevelAdmin: 9, 5: 0, -1: 0} {
		if got := len(AuthorizationsFor(level)); got != want {
			t.Errorf("level %d: %d authorizations, want %d", level, got, want)
		}
	}
}

func TestRandomUUIDIsVersion4(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		u := RandomUUID()
		if len(u) != 36 || u[14] != '4' || (u[19] != '8' && u[19] != '9' && u[19] != 'a' && u[19] != 'b') || seen[u] {
			t.Fatalf("bad uuid %q", u)
		}
		seen[u] = true
	}
}
