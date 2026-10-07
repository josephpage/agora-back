package cache

import (
	"context"
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
