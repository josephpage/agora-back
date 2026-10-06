// Command seed fills a PostgreSQL database (Hibernate schema already loaded)
// with the deterministic parity dataset.
//
//	go run ./parity/seed/cmd/seed -db postgres://backend:agora_password@localhost:5432/agora_seed \
//	    -now 2025-06-11T12:00:00Z -scale 1
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/parity/seed"
)

func main() {
	var (
		dbURL    = flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL URL (default: $DATABASE_URL)")
		nowStr   = flag.String("now", "", "reference instant, RFC3339 (default: current UTC time truncated to the second)")
		scale    = flag.Int("scale", 1, "1 = handcrafted dataset; N > 1 adds (N-1)*40 users, (N-1)*30 QaGs and their supports/responses")
		timeout  = flag.Duration("timeout", 5*time.Minute, "overall timeout")
		quiet    = flag.Bool("q", false, "do not print the summary")
		wantHelp = flag.Bool("h", false, "show usage")
	)
	flag.Parse()
	if *wantHelp {
		flag.Usage()
		return
	}
	if *dbURL == "" {
		fmt.Fprintln(os.Stderr, "seed: -db is required (or set DATABASE_URL)")
		os.Exit(2)
	}
	if *scale < 1 {
		fmt.Fprintln(os.Stderr, "seed: -scale must be >= 1")
		os.Exit(2)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if *nowStr != "" {
		t, err := time.Parse(time.RFC3339, *nowStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "seed: invalid -now %q: %v\n", *nowStr, err)
			os.Exit(2)
		}
		now = t.UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())

	start := time.Now()
	if err := seed.Seed(ctx, conn, now, *scale); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
	if *quiet {
		return
	}
	fmt.Printf("seeded now=%s scale=%d in %s\n", now.Format(time.RFC3339), *scale, time.Since(start).Round(time.Millisecond))
	rows, err := conn.Query(ctx, `
		SELECT 'agora_users', count(*) FROM agora_users UNION ALL
		SELECT 'qags', count(*) FROM qags UNION ALL
		SELECT 'supports_qag', count(*) FROM supports_qag UNION ALL
		SELECT 'reponses_consultation', count(*) FROM reponses_consultation UNION ALL
		SELECT 'notifications', count(*) FROM notifications`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int64
		if rows.Scan(&name, &n) == nil {
			fmt.Printf("  %-24s %d\n", name, n)
		}
	}
}
