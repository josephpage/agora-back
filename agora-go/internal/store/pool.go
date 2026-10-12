package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAcquireTimeout is Hikari's SQLTransientConnectionException ("Connection is not
// available, request timed out after 30000ms"): the request fails (HTTP 500) instead
// of waiting for a pooled connection without limit.
var ErrAcquireTimeout = errors.New("connection is not available, request timed out")

// Pool is the pgx pool with Hikari's connectionTimeout: waiting for a free connection
// is bounded by acquireTimeout, the statement itself only by the caller's context.
// pgxpool has no such bound (its Acquire waits on the caller's context only), so the
// methods below acquire first, then run on the acquired connection and release it the
// way pgxpool does (rows closed, row scanned, transaction ended, batch closed).
type Pool struct {
	*pgxpool.Pool
	acquireTimeout time.Duration
}

func (p *Pool) acquire(ctx context.Context) (*pgxpool.Conn, error) {
	if p.acquireTimeout <= 0 {
		return p.Pool.Acquire(ctx)
	}
	actx, cancel := context.WithTimeout(ctx, p.acquireTimeout)
	defer cancel()
	c, err := p.Pool.Acquire(actx)
	if err != nil && ctx.Err() == nil && actx.Err() != nil {
		return nil, fmt.Errorf("%w after %dms", ErrAcquireTimeout, p.acquireTimeout.Milliseconds())
	}
	return c, err
}

// Exec acquires a connection, runs sql and releases it.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c, err := p.acquire(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer c.Release()
	return c.Exec(ctx, sql, args...)
}

// Query acquires a connection; it is released when the rows are closed or exhausted.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c, err := p.acquire(ctx)
	if err != nil {
		return errRows{err: err}, err
	}
	rows, err := c.Query(ctx, sql, args...)
	if err != nil {
		c.Release()
		return errRows{err: err}, err
	}
	return &poolRows{Rows: rows, c: c}, nil
}

// QueryRow acquires a connection; it is released by Scan.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c, err := p.acquire(ctx)
	if err != nil {
		return errRow{err: err}
	}
	return &poolRow{r: c.QueryRow(ctx, sql, args...), c: c}
}

// SendBatch acquires a connection; it is released when the results are closed.
func (p *Pool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	c, err := p.acquire(ctx)
	if err != nil {
		return errBatchResults{err: err}
	}
	return &poolBatchResults{BatchResults: c.SendBatch(ctx, b), c: c}
}

// Begin acquires a connection; it is released when the transaction ends.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.BeginTx(ctx, pgx.TxOptions{})
}

// BeginTx acquires a connection; it is released when the transaction ends.
func (p *Pool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	c, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := c.BeginTx(ctx, opts)
	if err != nil {
		c.Release()
		return nil, err
	}
	return &poolTx{Tx: tx, c: c}, nil
}

// CopyFrom acquires a connection, copies and releases it.
func (p *Pool) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error) {
	c, err := p.acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer c.Release()
	return c.CopyFrom(ctx, table, columns, src)
}

type poolRows struct {
	pgx.Rows
	c *pgxpool.Conn
}

func (r *poolRows) release() {
	if r.c != nil {
		r.c.Release()
		r.c = nil
	}
}

func (r *poolRows) Close() {
	r.Rows.Close()
	r.release()
}

func (r *poolRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	r.Close()
	return false
}

type poolRow struct {
	r pgx.Row
	c *pgxpool.Conn
}

func (r *poolRow) Scan(dest ...any) error {
	defer r.c.Release()
	return r.r.Scan(dest...)
}

type poolTx struct {
	pgx.Tx
	c *pgxpool.Conn
}

func (t *poolTx) release() {
	if t.c != nil {
		t.c.Release()
		t.c = nil
	}
}

func (t *poolTx) Commit(ctx context.Context) error {
	err := t.Tx.Commit(ctx)
	t.release()
	return err
}

func (t *poolTx) Rollback(ctx context.Context) error {
	err := t.Tx.Rollback(ctx)
	t.release()
	return err
}

type poolBatchResults struct {
	pgx.BatchResults
	c *pgxpool.Conn
}

func (b *poolBatchResults) Close() error {
	err := b.BatchResults.Close()
	if b.c != nil {
		b.c.Release()
		b.c = nil
	}
	return err
}

// errRows, errRow and errBatchResults carry an acquisition error (like pgxpool's).
type errRows struct{ err error }

func (errRows) Close()                                       {}
func (r errRows) Err() error                                 { return r.err }
func (errRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (errRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (errRows) Next() bool                                   { return false }
func (r errRows) Scan(...any) error                          { return r.err }
func (r errRows) Values() ([]any, error)                     { return nil, r.err }
func (errRows) RawValues() [][]byte                          { return nil }
func (errRows) Conn() *pgx.Conn                              { return nil }
func (errRows) TypeMap() *pgtype.Map                         { return nil }

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type errBatchResults struct{ err error }

func (b errBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }
func (b errBatchResults) Query() (pgx.Rows, error)         { return errRows{err: b.err}, b.err }
func (b errBatchResults) QueryRow() pgx.Row                { return errRow{err: b.err} }
func (b errBatchResults) Close() error                     { return b.err }
