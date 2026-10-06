// Package seed generates the deterministic PostgreSQL dataset used by the
// Kotlin -> Go parity harness. See README.md for the description of every row.
package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Seed wipes the 21 tables of the Agora schema (TRUNCATE) and inserts the
// deterministic dataset, all in ONE transaction: after an error the database is
// left untouched, and calling Seed twice with the same (now, scale) yields the
// same content.
//
//   - now: the reference instant; every timestamp is now minus/plus a fixed
//     offset, truncated to the millisecond, expressed in UTC (the columns are
//     `timestamp without time zone`). The queries that use CURRENT_TIMESTAMP
//     (trending, ...) use the database clock, so pass (about) the real current
//     time when the SQL clock matters.
//   - scale: 1 = the small handcrafted dataset; N > 1 additionally generates
//     (N-1)*40 users, (N-1)*30 QaGs, their supports, notifications and
//     (N-1)*25 extra participants for consultations 1, 4 and 7, with a fixed
//     math/rand seed (load tests).
//
// The acme_* tables are truncated and left empty.
func Seed(ctx context.Context, conn *pgx.Conn, now time.Time, scale int) (err error) {
	if scale < 1 {
		scale = 1
	}
	b := newBuilder(now, scale)
	if err := b.build(); err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("seed: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Do not wait forever if an application still holds a lock on the tables.
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '30s'"); err != nil {
		return fmt.Errorf("seed: set lock_timeout: %w", err)
	}
	if _, err = tx.Exec(ctx, "TRUNCATE TABLE "+qualifiedTables()); err != nil {
		return fmt.Errorf("seed: truncate: %w", err)
	}
	for _, table := range insertOrder {
		rows := b.rows[table]
		if len(rows) == 0 {
			continue
		}
		n, cerr := tx.CopyFrom(ctx, pgx.Identifier{schemaName, table}, insertColumns[table], pgx.CopyFromRows(rows))
		if cerr != nil {
			err = fmt.Errorf("seed: insert into %s: %w", table, cerr)
			return err
		}
		if int(n) != len(rows) {
			err = fmt.Errorf("seed: insert into %s: copied %d of %d rows", table, n, len(rows))
			return err
		}
	}
	// Same planner statistics on every database seeded with the same data.
	if _, err = tx.Exec(ctx, "ANALYZE "+qualifiedTables()); err != nil {
		return fmt.Errorf("seed: analyze: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("seed: commit: %w", err)
	}
	return nil
}

const schemaName = "public"

// qualifiedTables returns "public.t1, public.t2, ..." for the 21 tables.
func qualifiedTables() string {
	q := make([]string, len(tables))
	for i, t := range tables {
		q[i] = schemaName + "." + t
	}
	return strings.Join(q, ", ")
}

// build fills the in-memory tables; programming errors (panics) become errors.
func (b *builder) build() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint("seed: build failed: ", r))
		}
	}()
	b.buildUsers()
	b.buildProfiles()
	b.buildQags()
	b.buildNotifications()
	b.buildConsultations()
	b.buildAppFeedbacks()
	b.buildBulk()
	return nil
}
