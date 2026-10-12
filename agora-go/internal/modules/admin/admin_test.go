package admin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/config"
	"agora/internal/httpx"
	"agora/internal/modules/thematique"
	"agora/internal/strapi"
)

// ClearCacheUseCaseTest: "should call flushDb on redis connection".
func TestClearAllCachesCallsFlushDB(t *testing.T) {
	calls := 0
	err := clearAllCaches(context.Background(), func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("err = %v, flush calls = %d", err, calls)
	}
}

// The Kotlin connection is closed after the flush; the Go flush has no connection to close,
// the failure is what remains observable: it propagates (the 500 of the route).
func TestClearAllCachesPropagatesFailure(t *testing.T) {
	boom := errors.New("redis down")
	if err := clearAllCaches(context.Background(), func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// The admin route clears the Strapi page: after FlushDB the next read goes to Strapi again.
func TestClearCacheMakesTheStrapiPageFetchedAgain(t *testing.T) {
	payload, err := os.ReadFile("../../../parity/fixtures/strapi/thematiques.json")
	if err != nil {
		t.Skip("fixture not available:", err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":` + string(payload) +
			`,"meta":{"pagination":{"page":1,"pageSize":100,"pageCount":1,"total":7}}}`))
	}))
	defer srv.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	a := &app.App{
		Cfg:   &config.Config{},
		Log:   logger,
		Cache: cache.New(nil, logger, false),
		Strapi: strapi.NewClient(strapi.Options{
			BaseURL: srv.URL + "/api/", Token: "test", Logger: logger,
		}),
		Clock: time.Now,
	}
	ctx := context.Background()
	if n := len(thematique.Get(a).List(ctx)); n == 0 {
		t.Fatalf("no thematique from the fake Strapi: %s", logs.String())
	}
	thematique.Get(a).List(ctx)
	if got := requests.Load(); got != 1 {
		t.Fatalf("second read went to Strapi: %d requests", got)
	}
	if err := clearAllCaches(ctx, a.Cache.FlushDB); err != nil {
		t.Fatal(err)
	}
	thematique.Get(a).List(ctx)
	if got := requests.Load(); got != 2 {
		t.Fatalf("read after the clear did not go to Strapi: %d requests", got)
	}
}

// DeleteUsersUseCase: the repository calls run in the Kotlin order, each one gets the request ids.
func TestDeleteUsersRunsTheStepsInOrder(t *testing.T) {
	var order []string
	var seen [][]string
	mk := func(name string) deletionStep {
		return deletionStep{name: name, run: func(_ context.Context, ids []string) error {
			order = append(order, name)
			seen = append(seen, ids)
			return nil
		}}
	}
	steps := []deletionStep{mk("notifications"), mk("consultation responses"), mk("supports"), mk("qags"),
		mk("feedbacks"), mk("profiles"), mk("users data"), mk("users")}
	ids := []string{"not-a-uuid", "00000000-0000-4000-9000-000000000001"}
	if err := deleteUsersInOrder(context.Background(), steps, ids); err != nil {
		t.Fatal(err)
	}
	want := []string{"notifications", "consultation responses", "supports", "qags", "feedbacks", "profiles", "users data", "users"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v", order)
	}
	for i, s := range seen {
		if !reflect.DeepEqual(s, ids) {
			t.Errorf("step %d got %v", i, s)
		}
	}
}

// A failing step stops the deletion: the earlier steps are already committed (no transaction in Kotlin).
func TestDeleteUsersStopsAtTheFirstFailure(t *testing.T) {
	boom := errors.New("constraint")
	var ran []string
	steps := []deletionStep{
		{name: "a", run: func(context.Context, []string) error { ran = append(ran, "a"); return nil }},
		{name: "b", run: func(context.Context, []string) error { ran = append(ran, "b"); return boom }},
		{name: "c", run: func(context.Context, []string) error { ran = append(ran, "c"); return nil }},
	}
	if err := deleteUsersInOrder(context.Background(), steps, []string{"x"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(ran, []string{"a", "b"}) {
		t.Fatalf("ran = %v", ran)
	}
}

// The consultation responses step is skipped when no id is a UUID, and runs with the ids otherwise.
func TestWithUUIDsOnly(t *testing.T) {
	var calls [][]string
	step := withUUIDsOnly(func(_ context.Context, ids []string) error {
		calls = append(calls, ids)
		return nil
	})
	if err := step(context.Background(), []string{"not-a-uuid", ""}); err != nil || len(calls) != 0 {
		t.Fatalf("called without a UUID: %v (err %v)", calls, err)
	}
	if err := step(context.Background(), []string{"not-a-uuid", "00000000-0000-4000-9000-000000000001"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || len(calls[0]) != 2 {
		t.Fatalf("calls = %v", calls)
	}
}

// `String.toUuidOrNull()` on a null element of the request list is a NullPointerException.
func TestNonNullUserIDsRejectsANullElement(t *testing.T) {
	one := "00000000-0000-4000-9000-000000000001"
	ids, err := nonNullUserIDs([]*string{&one, nil})
	if err == nil || ids != nil {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
	if !strings.Contains(err.Error(), "NullPointerException") {
		t.Fatalf("err = %v", err)
	}
	ids, err = nonNullUserIDs([]*string{&one})
	if err != nil || !reflect.DeepEqual(ids, []string{one}) {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
	if ids, err := nonNullUserIDs(nil); err != nil || len(ids) != 0 {
		t.Fatalf("empty list: %v %v", ids, err)
	}
}

func TestHasUserUUID(t *testing.T) {
	cases := []struct {
		ids  []string
		want bool
	}{
		{nil, false},
		{[]string{"not-a-uuid", ""}, false},
		{[]string{"not-a-uuid", "00000000-0000-4000-9000-0000000000FF"}, true},
		{[]string{"0-0-4000-9000-ff"}, true}, // Java UUID.fromString is lenient
	}
	for _, c := range cases {
		if got := hasUserUUID(c.ids); got != c.want {
			t.Errorf("hasUserUUID(%q) = %v", c.ids, got)
		}
	}
}

// boolParam is @RequestParam(defaultValue) Boolean: absent or single empty → the default,
// otherwise the first value, converted like StringToBooleanConverter (a blank text is a 400).
func TestBoolParam(t *testing.T) {
	cases := []struct {
		query string
		want  bool
		bad   bool
	}{
		{"", false, false},
		{"force_question_selection", false, false},
		{"force_question_selection=", false, false},
		{"force_question_selection=true", true, false},
		{"force_question_selection=TRUE", true, false},
		{"force_question_selection=on", true, false},
		{"force_question_selection=0", false, false},
		{"force_question_selection=true&force_question_selection=false", true, false},
		{"force_question_selection=&force_question_selection=true", false, true},
		{"force_question_selection=%20", false, true},
		{"force_question_selection=2", false, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.query, func(t *testing.T) {
			ctx := &httpx.Ctx{R: httptest.NewRequest(http.MethodPost, "/admin/weekly-tasks?"+c.query, nil)}
			got, bad := callBoolParam(ctx)
			if bad != c.bad || got != c.want {
				t.Fatalf("got %v (400: %v), want %v (400: %v)", got, bad, c.want, c.bad)
			}
		})
	}
}

// callBoolParam runs boolParam and reports the 400 of SpringBoolParam.
func callBoolParam(c *httpx.Ctx) (v bool, bad bool) {
	defer func() {
		if r := recover(); r != nil {
			bad = true
		}
	}()
	return boolParam(c, "force_question_selection", "false"), false
}
