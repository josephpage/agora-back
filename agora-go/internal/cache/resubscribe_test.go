package cache

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// The invalidations published while the subscription is down are lost: the L1 is
// cleared when the subscription comes back. Needs redis-server in PATH.
func TestResubscribeClearsL1(t *testing.T) {
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server not installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv := exec.Command(bin, "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	srv.Stdout, srv.Stderr = io.Discard, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Process.Kill(); _ = srv.Wait() }()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:" + strconv.Itoa(port)})
	defer rdb.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for rdb.Ping(ctx).Err() != nil {
		time.Sleep(20 * time.Millisecond)
	}
	c := New(rdb, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	c.Run(ctx)
	waitSubscribers := func() {
		for {
			n, _ := rdb.PubSubNumSub(ctx, invalidationChannel).Result()
			if n[invalidationChannel] == 1 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitSubscribers()
	c.Put("n", "k", "v", time.Hour)
	time.Sleep(50 * time.Millisecond)
	if _, ok := c.Get("n", "k"); !ok {
		t.Fatal("entry flushed without a resubscription")
	}
	// drop the subscriber connection (a Redis restart or a network cut would do the same)
	if err := rdb.Do(ctx, "CLIENT", "KILL", "TYPE", "pubsub").Err(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := c.Get("n", "k"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("L1 not cleared after the resubscription")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// and the invalidations flow again
	waitSubscribers()
	c.Put("n", "k2", "v", time.Hour)
	if err := rdb.Publish(ctx, invalidationChannel, "n\x00k2").Err(); err != nil {
		t.Fatal(err)
	}
	for deadline = time.Now().Add(2 * time.Second); ; {
		if _, ok := c.Get("n", "k2"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("invalidation not applied after the resubscription")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
