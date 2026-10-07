package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Performance migrations (Go-only, never required by the Kotlin backend).
//
// Each file migrations/NNNN_name.up.sql (and its .down.sql) holds statements
// separated by ";" at the end of a line. They are idempotent (IF NOT EXISTS /
// IF EXISTS) and run one by one outside any transaction, because CREATE INDEX
// CONCURRENTLY cannot run inside one: applying them never blocks writes, and
// re-running them is harmless. No tracking table is added to the shared
// schema. A concurrent build that was interrupted leaves an INVALID index:
// "up" drops and rebuilds it.
//
//	agora --migrate=up | --migrate=down | --migrate=status

//go:embed migrations/*.sql
var migrationsFS embed.FS

var createIndexRe = regexp.MustCompile(`(?is)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+IF\s+NOT\s+EXISTS\s+("?[A-Za-z0-9_]+"?)`)

// MigrationStatements returns the statements of every migration for a
// direction ("up": in file order, "down": in reverse file order).
func MigrationStatements(direction string) ([]string, error) {
	suffix := "." + direction + ".sql"
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if direction == "down" {
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
	}
	var out []string
	for _, n := range names {
		b, err := migrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			return nil, err
		}
		out = append(out, splitStatements(string(b))...)
	}
	return out, nil
}

// splitStatements splits on ";" at the end of a line and drops "--" comments.
func splitStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasSuffix(t, ";") {
			if s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cur.String()), ";")); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// Migrate applies the migrations in the given direction ("up" or "down").
func Migrate(ctx context.Context, pool *pgxpool.Pool, direction string, log *slog.Logger) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("unknown migration direction %q", direction)
	}
	stmts, err := MigrationStatements(direction)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if m := createIndexRe.FindStringSubmatch(s); m != nil {
			name := strings.Trim(m[1], `"`)
			var valid bool
			err := pool.QueryRow(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
				WHERE c.relname = $1 AND pg_catalog.pg_table_is_visible(c.oid)`, name).Scan(&valid)
			if err == nil && !valid {
				log.Warn("dropping invalid index left by an interrupted build", "index", name)
				if _, err := pool.Exec(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+m[1]); err != nil {
					return fmt.Errorf("drop invalid %s: %w", name, err)
				}
			}
		}
		start := time.Now()
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", firstLine(s), err)
		}
		log.Info("migration statement applied", "stmt", firstLine(s), "took", time.Since(start).Round(time.Millisecond))
	}
	return nil
}

// MigrationStatus lists the indexes the "up" migrations create, with their
// state: missing, valid or INVALID.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	stmts, err := MigrationStatements("up")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range stmts {
		m := createIndexRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		name := strings.Trim(m[1], `"`)
		var valid bool
		state := "missing"
		if err := pool.QueryRow(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relname = $1 AND pg_catalog.pg_table_is_visible(c.oid)`, name).Scan(&valid); err == nil {
			state = "valid"
			if !valid {
				state = "INVALID"
			}
		}
		out = append(out, name+": "+state)
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
