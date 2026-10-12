package notification

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"agora/internal/app"
	"agora/internal/config"
	"agora/internal/fcm"
)

// Ports of the rules of NotificationSendingRepositoryImpl (createMultiMessage, sendMultiNotifications).

func tokensOf(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("tok-%04d", i)
	}
	return out
}

func chunkSizes(c *tokenChunks) []int {
	var out []int
	for _, ch := range c.chunks {
		out = append(out, len(ch))
	}
	return out
}

// blank and repeated tokens are dropped, the first occurrence of a token wins, the order of the users is kept
func TestTokenChunksFilterAndDistinct(t *testing.T) {
	c := newTokenChunks()
	for _, tok := range []string{"", "   ", "\t", "a", "b", "a", " ", "c", "b"} {
		c.add(tok)
	}
	if len(c.chunks) != 1 || fmt.Sprint(c.chunks[0]) != "[a b c]" {
		t.Fatalf("got %v", c.chunks)
	}
}

// chunks of 500 tokens (the FCM maximum, C-S8-FANOUT), the last one holds the rest
func TestTokenChunksOfFiveHundred(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want []int
	}{{0, nil}, {1, []int{1}}, {500, []int{500}}, {501, []int{500, 1}}, {1001, []int{500, 500, 1}}} {
		c := newTokenChunks()
		for _, tok := range tokensOf(tc.n) {
			c.add(tok)
		}
		if got := chunkSizes(c); fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Fatalf("n=%d: want %v, got %v", tc.n, tc.want, got)
		}
		// the order of the users is kept across the chunks
		flat := []string{}
		for _, ch := range c.chunks {
			flat = append(flat, ch...)
		}
		if fmt.Sprint(flat) != fmt.Sprint(tokensOf(tc.n)) {
			t.Fatalf("n=%d: order lost", tc.n)
		}
	}
}

func newTestSender(t *testing.T, now time.Time, send func(context.Context, fcm.Message) (fcm.BatchResponse, error)) *Sender {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &app.App{Log: log, Clock: func() time.Time { return now }, Background: ctx}
	s := &Sender{a: a, send: send, interval: batchInterval(16700)}
	// the scheduler reads a clock one hour ahead: every batch (due at now + i × 1.8 s) is already due
	s.sched = newScheduler(ctx, func() time.Time { return now.Add(time.Hour) }, func(r any) {
		t.Errorf("unexpected panic: %v", r)
	})
	return s
}

// Batch i of a notification is sent once, in order, with the title, the body and the type (and the
// pageArgument when there is one).
func TestSendChunksBatchesAndData(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	var got []fcm.Message
	done := make(chan struct{}, 4)
	s := newTestSender(t, now, func(_ context.Context, m fcm.Message) (fcm.BatchResponse, error) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
		done <- struct{}{}
		return fcm.BatchResponse{SuccessCount: len(m.Tokens)}, nil
	})
	toks := append([]string{"", "x", "x"}, tokensOf(1001)...)
	page := "f55a68a7"
	if res := s.SendGenericMultiNotification("Titre", "Corps", toks, DetailsQag, &page); res != Success {
		t.Fatal("want Success")
	}
	for i := 0; i < 3; i++ {
		waitFor(t, done)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("want 3 batches, got %d", len(got))
	}
	for i, m := range got {
		if m.Title != "Titre" || m.Body != "Corps" {
			t.Fatalf("batch %d: %+v", i, m)
		}
		if m.Data["type"] != "DETAILS_QAG" || m.Data["pageArgument"] != page || len(m.Data) != 2 {
			t.Fatalf("batch %d data: %v", i, m.Data)
		}
	}
	// "x" (once) and 1001 tokens: 1002 distinct tokens
	if len(got[0].Tokens) != 500 || len(got[1].Tokens) != 500 || len(got[2].Tokens) != 2 {
		t.Fatalf("sizes: %d %d %d", len(got[0].Tokens), len(got[1].Tokens), len(got[2].Tokens))
	}
	if got[0].Tokens[0] != "x" || got[2].Tokens[0] != "tok-0999" {
		t.Fatalf("order of the tokens: %q %q", got[0].Tokens[0], got[2].Tokens[0])
	}
}

// Without a pageArgument the data holds the type only.
func TestSendChunksWithoutPageArgument(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	done := make(chan fcm.Message, 1)
	s := newTestSender(t, now, func(_ context.Context, m fcm.Message) (fcm.BatchResponse, error) {
		done <- m
		return fcm.BatchResponse{}, nil
	})
	s.SendGenericMultiNotification("t", "d", []string{"a"}, ReponseSupport, nil)
	m := <-done
	if len(m.Data) != 1 || m.Data["type"] != "REPONSE_SUPPORT" {
		t.Fatalf("data %v", m.Data)
	}
}

// No token: nothing is scheduled and the notification is a success.
func TestSendWithoutTokens(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s := newTestSender(t, now, func(context.Context, fcm.Message) (fcm.BatchResponse, error) {
		t.Error("nothing must be sent")
		return fcm.BatchResponse{}, nil
	})
	if res := s.SendGenericMultiNotification("t", "d", []string{"", " "}, HomeQags, nil); res != Success {
		t.Fatal("want Success")
	}
	if n := s.sched.pending(); n != 0 {
		t.Fatalf("%d tasks pending", n)
	}
}

// A task is run at its due time; tasks due at the same time keep their order of scheduling.
func TestSchedulerRunsInDueOrder(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := newScheduler(ctx, func() time.Time { return now.Add(time.Hour) }, nil)
	var mu sync.Mutex
	var order []string
	done := make(chan struct{}, 3)
	rec := func(name string) func() {
		return func() {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			done <- struct{}{}
		}
	}
	s.schedule(now.Add(2*time.Minute), rec("late"))
	s.schedule(now, rec("first"))
	s.schedule(now, rec("second"))
	for i := 0; i < 3; i++ {
		waitFor(t, done)
	}
	if fmt.Sprint(order) != "[first second late]" {
		t.Fatalf("order %v", order)
	}
}

// A panicking task is reported and the worker keeps running the next tasks.
func TestSchedulerSurvivesPanic(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reported := make(chan any, 1)
	s := newScheduler(ctx, func() time.Time { return now.Add(time.Hour) }, func(r any) { reported <- r })
	ran := make(chan struct{}, 1)
	s.schedule(now, func() { panic("boom") })
	s.schedule(now.Add(time.Minute), func() { ran <- struct{}{} })
	if r := <-reported; r != "boom" {
		t.Fatalf("reported %v", r)
	}
	waitFor(t, ran)
}

// The default rate sends a million tokens in about an hour: 2000 batches of 500, 1.796 s apart.
func TestBatchInterval(t *testing.T) {
	if got := batchInterval(16700); got != 1796407185*time.Nanosecond {
		t.Fatalf("interval %v", got)
	}
	if total := 2000 * batchInterval(16700); total < 59*time.Minute || total > 61*time.Minute {
		t.Fatalf("a million tokens take %v", total)
	}
	if got := batchInterval(500); got != time.Minute {
		t.Fatalf("500 a minute: %v", got)
	}
}

// Batch i is due at now + i × interval (instants: a DST change does not move them).
func TestScheduleBatchesDueTimes(t *testing.T) {
	now := time.Date(2026, 10, 25, 2, 59, 59, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &app.App{Log: log, Clock: func() time.Time { return now }, Background: ctx}
	s := &Sender{a: a, interval: 1800 * time.Millisecond, send: func(context.Context, fcm.Message) (fcm.BatchResponse, error) {
		t.Error("nothing is due yet")
		return fcm.BatchResponse{}, nil
	}}
	// the scheduler's clock is one hour behind: nothing runs, the queue can be inspected
	s.sched = newScheduler(ctx, func() time.Time { return now.Add(-time.Hour) }, nil)
	s.SendGenericMultiNotification("t", "d", tokensOf(1201), HomeQags, nil)
	s.sched.mu.Lock()
	defer s.sched.mu.Unlock()
	var due []time.Duration
	for _, task := range s.sched.queue {
		due = append(due, task.at.Sub(now))
	}
	slices.Sort(due)
	if fmt.Sprint(due) != "[0s 1.8s 3.6s]" {
		t.Fatalf("due after %v", due)
	}
}

// newSender reads the rate of the configuration, else the default.
func TestNewSenderRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if s := newSender(&app.App{Log: log, Clock: time.Now, Background: ctx}); s.interval != batchInterval(config.DefaultNotificationFanoutPerMinute) {
		t.Fatalf("default interval %v", s.interval)
	}
	cfg := &config.Config{NotificationFanoutPerMinute: 30000}
	if s := newSender(&app.App{Cfg: cfg, Log: log, Clock: time.Now, Background: ctx}); s.interval != time.Second {
		t.Fatalf("interval %v", s.interval)
	}
}

func waitFor[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

// A single-batch notification (one user) is sent at once, even when the batches of a send to
// many users are stuck behind a slow FCM.
func TestSingleBatchDoesNotWaitBehindBulk(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &app.App{Log: log, Clock: func() time.Time { return now }, Background: ctx}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	solo := make(chan struct{}, 1)
	s := &Sender{a: a, interval: time.Millisecond, send: func(_ context.Context, m fcm.Message) (fcm.BatchResponse, error) {
		if m.Tokens[0] == "solo" {
			solo <- struct{}{}
			return fcm.BatchResponse{SuccessCount: 1}, nil
		}
		<-release // FCM never answers the bulk batches
		return fcm.BatchResponse{}, nil
	}}
	clock := func() time.Time { return now.Add(time.Hour) }
	s.sched = newScheduler(ctx, clock, nil)
	s.immediate = newScheduler(ctx, clock, nil)
	s.SendGenericMultiNotification("t", "d", tokensOf(1001), HomeQags, nil)
	s.SendGenericMultiNotification("t", "d", []string{"solo"}, ReponseSupport, nil)
	waitFor(t, solo)
}
