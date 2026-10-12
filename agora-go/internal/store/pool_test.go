package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// STORE_IT_DB=postgres://backend:agora_password@localhost:5432/postgres go test ./internal/store -run Pool
func TestPoolAcquireTimeoutAndRelease(t *testing.T) {
	dsn := os.Getenv("STORE_IT_DB")
	if dsn == "" {
		t.Skip("STORE_IT_DB not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn, 1, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := db.Pool

	// with a single connection, every helper must give it back: a leak makes the next call time out
	for i := 0; i < 3; i++ {
		if _, err := p.Exec(ctx, "SELECT 1"); err != nil {
			t.Fatal("exec", err)
		}
		rows, err := p.Query(ctx, "SELECT generate_series(1, 3)")
		if err != nil {
			t.Fatal("query", err)
		}
		if _, err := pgx.CollectRows(rows, pgx.RowTo[int32]); err != nil {
			t.Fatal("collect", err)
		}
		rows, _ = p.Query(ctx, "SELECT 1")
		for rows.Next() { // exhausted without Close
		}
		var n int
		if err := p.QueryRow(ctx, "SELECT 2").Scan(&n); err != nil || n != 2 {
			t.Fatal("queryrow", n, err)
		}
		b := &pgx.Batch{}
		b.Queue("SELECT 1")
		if err := p.SendBatch(ctx, b).Close(); err != nil {
			t.Fatal("batch", err)
		}
		if err := db.Tx(ctx, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT 1"); return err }); err != nil {
			t.Fatal("tx", err)
		}
	}

	// the only connection is held: the wait is bounded like Hikari's connectionTimeout
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = p.Exec(ctx, "SELECT 1")
	if !errors.Is(err, ErrAcquireTimeout) {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 2*time.Second {
		t.Fatalf("waited %s", d)
	}
	if err := p.QueryRow(ctx, "SELECT 1").Scan(new(int)); !errors.Is(err, ErrAcquireTimeout) {
		t.Fatalf("queryrow: %v", err)
	}
	// a cancelled caller gets its own context error, not the timeout
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Exec(cctx, "SELECT 1"); errors.Is(err, ErrAcquireTimeout) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	_ = tx.Rollback(ctx)
	if _, err := p.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatal("after rollback", err)
	}
}
