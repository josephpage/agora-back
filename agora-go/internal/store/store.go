// Package store owns the PostgreSQL access: connection pool, timestamp
// semantics compatible with the Kotlin/JDBC/Hibernate code, and the schema.
//
// Rules for repository code (see CONVENTIONS.md):
//   - SQL is copied VERBATIM from the Kotlin @Query strings (named parameters
//     rewritten as $n), so that plans, tie ordering and quirks are identical;
//   - `timestamp without time zone` columns hold the JVM-default-zone wall
//     clock: write time.Time values in time.Local (pgx stores the wall clock)
//     and convert scanned values with Local();
//   - java.util.Date columns have millisecond precision (Millis), LocalDateTime
//     values microsecond precision (Micros).
package store

import (
	"context"
	"embed"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// DB wraps the pgx pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Querier is satisfied by *pgxpool.Pool, pgx.Tx and *pgx.Conn.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Open connects using DATABASE_URL (postgres://user:pass@host:port/db?params).
func Open(ctx context.Context, databaseURL string, maxConns int, acquireTimeout time.Duration) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(normalizeURL(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	// Hikari keeps minimumIdle = maximumPoolSize; keep a warm pool too.
	cfg.MinConns = cfg.MaxConns / 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 10 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	if acquireTimeout > 0 {
		cfg.ConnConfig.ConnectTimeout = acquireTimeout
	}
	// Statement cache per connection (prepared statements reused).
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

// normalizeURL accepts the Scalingo/Heroku style URL and drops JDBC-only
// parameters pgx does not understand.
func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "postgresql" {
		u.Scheme = "postgres"
	}
	q := u.Query()
	for k := range q {
		switch strings.ToLower(k) {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "application_name", "connect_timeout", "target_session_attrs", "options":
		default:
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Close releases the pool.
func (db *DB) Close() { db.Pool.Close() }

// Tx runs fn in a transaction.
func (db *DB) Tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, db.Pool, fn)
}

// Local reinterprets a scanned `timestamp without time zone` (returned by pgx
// with the UTC location) as a wall clock time in the process zone.
func Local(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
}

// LocalPtr is Local for nullable columns.
func LocalPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	l := Local(*t)
	return &l
}

// Millis truncates to millisecond precision (java.util.Date).
func Millis(t time.Time) time.Time { return t.In(time.Local).Truncate(time.Millisecond) }

// Micros truncates to microsecond precision (LocalDateTime.now() on Linux JDK 17).
func Micros(t time.Time) time.Time { return t.In(time.Local).Truncate(time.Microsecond) }

// BaselineSchema returns the DDL equivalent to what Hibernate ddl-auto=update
// created (CREATE TABLE IF NOT EXISTS …), used only for fresh databases.
func BaselineSchema() (string, error) {
	b, err := schemaFS.ReadFile("schema/baseline.sql")
	return string(b), err
}

// BootstrapSchema applies the baseline (idempotent, no-op on existing tables).
func (db *DB) BootstrapSchema(ctx context.Context) error {
	sql, err := BaselineSchema()
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, sql)
	return err
}
