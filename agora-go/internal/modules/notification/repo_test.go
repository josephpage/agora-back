package notification

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/store"
)

// Ports of infrastructure/notification/repository/NotificationRepositoryImplTest.kt (the mocks of the
// database and of the cache are the findAll seam and the app cache).

func testApp() *app.App {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &app.App{Cache: cache.New(nil, log, false), Log: log, Clock: time.Now}
}

// fakeFindAll counts the calls of the database and returns rows.
type fakeFindAll struct {
	calls int
	uids  []string
	rows  []row
	err   error
}

func (f *fakeFindAll) find(_ context.Context, uid string) ([]row, error) {
	f.calls++
	f.uids = append(f.uids, uid)
	return f.rows, f.err
}

func strPtr(s string) *string { return &s }

func typPtr(t TypeNotification) *TypeNotification { return &t }

func timePtr(t time.Time) *time.Time { return &t }

// okRow is a well formed row of the table.
func okRow(title string, date time.Time) row {
	return row{title: strPtr(title), description: strPtr("desc " + title), typ: typPtr(HomeQags), date: timePtr(date)}
}

const testUser = "3f1c2b1e-1d8e-4b77-9f0c-7e1f5f5c6a10"

var base = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// Kotlin: getUserNotificationList - when invalid UUID - should return emptyList
func TestGetUserNotificationListInvalidUUID(t *testing.T) {
	a := testApp()
	f := &fakeFindAll{rows: []row{okRow("x", base)}}
	r := &Repository{a: a, findAll: f.find}
	page, err := r.GetNotificationPaginated(context.Background(), "invalid UUID", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || len(page.Notifications) != 0 || page.HasMoreNotifications {
		t.Fatalf("want an empty page, got %+v", page)
	}
	if f.calls != 0 {
		t.Fatalf("database called %d times", f.calls)
	}
	if _, ok := a.Cache.Get(notificationCacheName, notificationCacheKey+"invalid UUID"); ok {
		t.Fatal("an invalid id must not be cached")
	}
}

// Kotlin: when cache is not initialized and has no result - should initialize cache with database then return emptyList
func TestGetUserNotificationListCacheMissCachesEmptyList(t *testing.T) {
	a := testApp()
	f := &fakeFindAll{}
	r := &Repository{a: a, findAll: f.find}
	page, err := r.GetNotificationPaginated(context.Background(), testUser, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || len(page.Notifications) != 0 {
		t.Fatalf("want an empty page, got %+v", page)
	}
	if f.calls != 1 || f.uids[0] != testUser {
		t.Fatalf("want one database read of %s, got %d %v", testUser, f.calls, f.uids)
	}
	// the empty list is cached too (a second read does not query the database)
	if _, err := r.GetNotificationPaginated(context.Background(), testUser, 1); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("the empty list was not cached: %d reads", f.calls)
	}
}

// Kotlin: when cache is initialized and has result - should return mapped result
func TestGetUserNotificationListCacheHit(t *testing.T) {
	a := testApp()
	f := &fakeFindAll{}
	r := &Repository{a: a, findAll: f.find}
	a.Cache.Put(notificationCacheName, notificationCacheKey+testUser, []row{okRow("cached", base)}, time.Minute)
	page, err := r.GetNotificationPaginated(context.Background(), testUser, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notifications) != 1 || page.Notifications[0].Title != "cached" {
		t.Fatalf("want the cached notification, got %+v", page)
	}
	if f.calls != 0 {
		t.Fatalf("the database was read although the list is cached: %d", f.calls)
	}
}

// The list is cached for 5 minutes and nothing evicts it (Kotlin NotificationCacheRepository): a
// notification written after the first read is not visible before the entry expires.
func TestCachedListIsNotEvictedByWrites(t *testing.T) {
	a := testApp()
	now := base
	f := &fakeFindAll{rows: []row{okRow("first", base)}}
	r := &Repository{a: a, findAll: f.find}
	if _, err := r.GetNotificationPaginated(context.Background(), testUser, 1); err != nil {
		t.Fatal(err)
	}
	// a new row in the database: the cached list still holds one notification
	f.rows = append(f.rows, okRow("second", now.Add(time.Second)))
	page, err := r.GetNotificationPaginated(context.Background(), testUser, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notifications) != 1 || f.calls != 1 {
		t.Fatalf("want the stale list (1 read), got %d notifications after %d reads", len(page.Notifications), f.calls)
	}
}

// A failed read is an error (HTTP 500) and is not cached.
func TestFailedReadIsNotCached(t *testing.T) {
	a := testApp()
	f := &fakeFindAll{err: errors.New("db down")}
	r := &Repository{a: a, findAll: f.find}
	if _, err := r.GetNotificationPaginated(context.Background(), testUser, 1); err == nil {
		t.Fatal("want the database error")
	}
	f.err = nil
	if _, err := r.GetNotificationPaginated(context.Background(), testUser, 1); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("the failure was cached: %d reads", f.calls)
	}
}

// Pages (NotificationPaginatedUseCase): page 0 and negative pages are a 400 without a read.
func TestPaginationBoundaries(t *testing.T) {
	five := func(n int) []row {
		var out []row
		for i := 0; i < n; i++ {
			out = append(out, okRow("n"+string(rune('a'+i)), base.Add(time.Duration(-i)*time.Hour)))
		}
		return out
	}
	cases := []struct {
		name    string
		rows    int
		page    int
		nilPage bool
		count   int
		more    bool
	}{
		{"page zero", 3, 0, true, 0, false},
		{"negative page", 3, -1, true, 0, false},
		{"empty list, first page", 0, 1, false, 0, false},
		{"empty list, second page is beyond", 0, 2, true, 0, false},
		{"five rows, first page is the end", 5, 1, false, 5, false},
		{"five rows, second page is empty and not beyond", 5, 2, false, 0, false},
		{"five rows, third page is beyond", 5, 3, true, 0, false},
		{"six rows, first page", 6, 1, false, 5, true},
		{"six rows, second page", 6, 2, false, 1, false},
		{"six rows, third page is beyond", 6, 3, true, 0, false},
		{"huge page", 6, 1 << 30, true, 0, false},
		{"largest int32 page", 6, 2147483647, true, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeFindAll{rows: five(c.rows)}
			r := &Repository{a: testApp(), findAll: f.find}
			page, err := r.GetNotificationPaginated(context.Background(), testUser, c.page)
			if err != nil {
				t.Fatal(err)
			}
			if c.page <= 0 {
				if page != nil || f.calls != 0 {
					t.Fatalf("want nil without a read, got %+v (%d reads)", page, f.calls)
				}
				return
			}
			if c.nilPage {
				if page != nil {
					t.Fatalf("want nil (400), got %+v", page)
				}
				return
			}
			if len(page.Notifications) != c.count || page.HasMoreNotifications != c.more {
				t.Fatalf("want %d notifications and more=%v, got %d and %v", c.count, c.more, len(page.Notifications), page.HasMoreNotifications)
			}
		})
	}
}

// subList(minIndex, maxIndex) with a negative fromIndex (the int arithmetic wraps): IndexOutOfBounds.
func TestPaginationWrappedIndexIsAnException(t *testing.T) {
	f := &fakeFindAll{rows: []row{okRow("a", base)}}
	r := &Repository{a: testApp(), findAll: f.find}
	_, err := r.GetNotificationPaginated(context.Background(), testUser, 500000000)
	if !errors.Is(err, errJavaException) {
		t.Fatalf("want a Java exception, got %v", err)
	}
}

// The list is sorted by descending date; equal dates keep the order of the database (stable sort).
func TestSortedByDescendingDateStable(t *testing.T) {
	rows := []row{
		okRow("old", base.Add(-time.Hour)),
		okRow("tie-first", base),
		okRow("new", base.Add(time.Hour)),
		okRow("tie-second", base),
	}
	f := &fakeFindAll{rows: rows}
	r := &Repository{a: testApp(), findAll: f.find}
	page, err := r.GetNotificationPaginated(context.Background(), testUser, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new", "tie-first", "tie-second", "old"}
	for i, n := range page.Notifications {
		if n.Title != want[i] {
			t.Fatalf("position %d: want %q, got %q (%v)", i, want[i], n.Title, page.Notifications)
		}
	}
}

// A NULL column is an exception for the whole request, even outside the requested page (the mapper runs on
// every row) and the rows stay cached (the next read fails the same way without a database read).
func TestMalformedRowFailsEveryPage(t *testing.T) {
	bad := okRow("ok", base)
	bad.title = nil
	f := &fakeFindAll{rows: []row{okRow("a", base), bad}}
	r := &Repository{a: testApp(), findAll: f.find}
	for _, page := range []int{1, 9} {
		if _, err := r.GetNotificationPaginated(context.Background(), testUser, page); !errors.Is(err, errJavaException) {
			t.Fatalf("page %d: want an exception, got %v", page, err)
		}
	}
	if f.calls != 1 {
		t.Fatalf("the malformed rows are read once and cached: %d reads", f.calls)
	}
}

// A NULL type (ORDINAL enum) is a NullPointerException for the whole request.
func TestNullTypeIsAnException(t *testing.T) {
	bad := okRow("typed", base)
	bad.typ = nil
	r := &Repository{a: testApp(), findAll: (&fakeFindAll{rows: []row{bad}}).find}
	if _, err := r.GetNotificationPaginated(context.Background(), testUser, 1); !errors.Is(err, errJavaException) {
		t.Fatalf("want an exception, got %v", err)
	}
}

// The dates are converted to the process zone (Hibernate reads timestamp without time zone).
func TestDateIsReadInTheProcessZone(t *testing.T) {
	d := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	got, err := toNotifications([]row{okRow("d", d)})
	if err != nil {
		t.Fatal(err)
	}
	want := store.Local(d)
	if !got[0].Date.Equal(want) {
		t.Fatalf("want %v, got %v", want, got[0].Date)
	}
}

// Insert: an id that is not a UUID is dropped; with no id the database is not written (no pool here).
func TestInsertWithoutValidIDsDoesNothing(t *testing.T) {
	r := &Repository{a: testApp()}
	if err := r.InsertNotifications(context.Background(), "t", "d", ReponseSupport, []string{"nope", ""}); err != nil {
		t.Fatal(err)
	}
}
