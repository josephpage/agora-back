package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A load that overlaps an invalidation of its key must not be stored, and a
// caller arriving after the invalidation must not join it.
func TestGetOrLoadInvalidationDuringLoad(t *testing.T) {
	c := New(nil, nil, false)
	ctx := context.Background()
	var loads atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	slow := func() (string, error) {
		n := loads.Add(1)
		if n == 1 {
			close(started)
			<-release
			return "stale", nil
		}
		return "fresh", nil
	}

	var wg sync.WaitGroup
	var first string
	wg.Add(1)
	go func() {
		defer wg.Done()
		first, _ = GetOrLoad(c, "users", "u1", time.Minute, slow)
	}()
	<-started
	c.Invalidate(ctx, "users", "u1") // the write happens while the first load runs

	second, err := GetOrLoad(c, "users", "u1", time.Minute, slow)
	if err != nil || second != "fresh" {
		t.Fatalf("caller after the invalidation got %q, %v; want a new load", second, err)
	}
	close(release)
	wg.Wait()
	if first != "stale" {
		t.Fatalf("first caller got %q", first)
	}
	if v, ok := c.Get("users", "u1"); !ok || v != "fresh" {
		t.Fatalf("cached %v %v; the overlapping load must not overwrite the fresh value", v, ok)
	}
	if loads.Load() != 2 {
		t.Fatalf("loads = %d", loads.Load())
	}
}

func TestGetOrLoadNameAndFlushInvalidation(t *testing.T) {
	c := New(nil, nil, false)
	ctx := context.Background()
	for _, inval := range []func(){
		func() { c.InvalidateAll(ctx, "users") },
		func() { c.FlushLocal() },
	} {
		release := make(chan struct{})
		started := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = GetOrLoad(c, "users", "u2", time.Minute, func() (int, error) {
				close(started)
				<-release
				return 1, nil
			})
		}()
		<-started
		inval()
		close(release)
		<-done
		if _, ok := c.Get("users", "u2"); ok {
			t.Fatal("value loaded across an invalidation was stored")
		}
	}
	// without invalidation the value is stored
	if v, _ := GetOrLoad(c, "users", "u2", time.Minute, func() (int, error) { return 7, nil }); v != 7 {
		t.Fatal(v)
	}
	if v, ok := c.Get("users", "u2"); !ok || v != 7 {
		t.Fatal("value not stored")
	}
	// an invalidation of another key does not prevent the store
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = GetOrLoad(c, "users", "u3", time.Minute, func() (int, error) {
			close(started)
			<-release
			return 3, nil
		})
	}()
	<-started
	c.Invalidate(ctx, "users", "other")
	close(release)
	<-done
	if v, ok := c.Get("users", "u3"); !ok || v != 3 {
		t.Fatal("unrelated invalidation dropped the store")
	}
}

// The caller running a shared load is cancelled (its client went away): a caller that
// joined it loads again with its own function instead of failing.
func TestGetOrLoadJoinerSurvivesLeaderCancellation(t *testing.T) {
	c := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	started := make(chan struct{})
	leaderDone := make(chan error, 1)
	go func() {
		_, err := GetOrLoad(c, "n", "k", time.Minute, func() (string, error) {
			close(started)
			<-leaderCtx.Done()
			return "", fmt.Errorf("query: %w", leaderCtx.Err())
		})
		leaderDone <- err
	}()
	<-started
	joinerDone := make(chan struct{})
	var got string
	var gotErr error
	go func() {
		defer close(joinerDone)
		got, gotErr = GetOrLoad(c, "n", "k", time.Minute, func() (string, error) { return "fresh", nil })
	}()
	time.Sleep(20 * time.Millisecond) // let the joiner join the leader's flight
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader: %v", err)
	}
	<-joinerDone
	if gotErr != nil || got != "fresh" {
		t.Fatalf("joiner: %q, %v", got, gotErr)
	}
	// any other error is shared as before
	boom := errors.New("boom")
	if _, err := GetOrLoad(c, "n", "other", time.Minute, func() (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
