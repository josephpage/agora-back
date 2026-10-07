package seed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var testNow = time.Date(2026, 10, 7, 12, 34, 56, 789_000_000, time.UTC) // a Wednesday, with sub-second noise

// TestBuildDeterministic needs no database: two builds with the same inputs
// must produce exactly the same rows, and the row shapes must match the schema.
func TestBuildDeterministic(t *testing.T) {
	for _, scale := range []int{1, 4} {
		a, b := newBuilder(testNow, scale), newBuilder(testNow, scale)
		if err := a.build(); err != nil {
			t.Fatalf("scale %d: build: %v", scale, err)
		}
		if err := b.build(); err != nil {
			t.Fatalf("scale %d: build: %v", scale, err)
		}
		if !reflect.DeepEqual(a.rows, b.rows) {
			t.Fatalf("scale %d: two builds differ", scale)
		}
		for table := range a.rows {
			if _, ok := insertColumns[table]; !ok {
				t.Errorf("table %s has rows but no column list", table)
			}
		}
		for _, acme := range []string{"acme_account", "acme_certificate", "acme_challenge", "acme_order"} {
			if len(a.rows[acme]) != 0 {
				t.Errorf("%s must stay empty", acme)
			}
		}
	}
}

// TestTimesAreMillisecondUTC checks that no timestamp has sub-millisecond precision or a non-UTC zone.
func TestTimesAreMillisecondUTC(t *testing.T) {
	b := newBuilder(testNow, 3)
	if err := b.build(); err != nil {
		t.Fatal(err)
	}
	for table, rows := range b.rows {
		for _, row := range rows {
			for _, v := range row {
				if ts, ok := v.(time.Time); ok {
					if ts.Location() != time.UTC {
						t.Fatalf("%s: non UTC time %v", table, ts)
					}
					if ts.Nanosecond()%int(time.Millisecond) != 0 {
						t.Fatalf("%s: sub-millisecond time %v", table, ts)
					}
					if ts.After(b.now) {
						t.Fatalf("%s: time %v after now %v", table, ts, b.now)
					}
				}
			}
		}
	}
}

func adminURL() string {
	if u := os.Getenv("AGORA_SEED_TEST_PG_URL"); u != "" {
		return u
	}
	return "postgres://backend:agora_password@localhost:5432/postgres"
}

// withDatabase creates a throw-away database loaded from the Hibernate schema
// dump, and returns a connection URL to it. It skips the test if PostgreSQL is
// not reachable.
func withDatabase(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL())
	if err != nil {
		t.Skipf("PostgreSQL not reachable (%v): set AGORA_SEED_TEST_PG_URL to run this test", err)
	}
	defer admin.Close(context.Background())

	name := fmt.Sprintf("agora_seed_test_%d_%d", os.Getpid(), time.Now().UnixNano()%1_000_000)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create database (%v)", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), adminURL())
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	dbURL := replaceDatabase(adminURL(), name)

	// The dump sets search_path = '' for its session: load it on its own connection.
	schemaPath := filepath.Join("..", "..", "internal", "store", "schema", "reference_hibernate.sql")
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var sb strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, `\`) { // psql meta commands (\restrict, \unrestrict)
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	loader, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	defer loader.Close(context.Background())
	if _, err := loader.Exec(ctx, sb.String()); err != nil {
		t.Fatalf("load schema: %v", err)
	}
	return dbURL
}

// replaceDatabase swaps the database name of a postgres:// URL.
func replaceDatabase(url, db string) string {
	base := url
	query := ""
	if i := strings.Index(base, "?"); i >= 0 {
		base, query = base[:i], base[i:]
	}
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[:i+1] + db
	}
	return base + query
}

func scalar[T any](t *testing.T, conn *pgx.Conn, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := conn.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return v
}

// contentHash is an md5 of the textual content of every table (order independent).
func contentHash(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var parts []string
	for _, table := range tables {
		h := scalar[string](t, conn, fmt.Sprintf(
			"SELECT md5(coalesce(string_agg(x::text, '|' ORDER BY x::text), '')) FROM public.%s x", table))
		parts = append(parts, table+"="+h)
	}
	return strings.Join(parts, ",")
}

func TestSeedAgainstPostgres(t *testing.T) {
	dbURL := withDatabase(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	// -- scale 1, twice (idempotent) ------------------------------------------------
	if err := Seed(ctx, conn, testNow, 1); err != nil {
		t.Fatalf("seed #1: %v", err)
	}
	first := contentHash(t, conn)
	if err := Seed(ctx, conn, testNow, 1); err != nil {
		t.Fatalf("seed #2: %v", err)
	}
	if second := contentHash(t, conn); first != second {
		t.Fatalf("seed is not idempotent:\n%s\n%s", first, second)
	}

	eq := func(name string, want, got int64) {
		t.Helper()
		if want != got {
			t.Errorf("%s: want %d, got %d", name, want, got)
		}
	}
	count := func(sql string, args ...any) int64 { return scalar[int64](t, conn, sql, args...) }

	// acme tables stay empty
	for _, acme := range []string{"acme_account", "acme_certificate", "acme_challenge", "acme_order"} {
		eq(acme, 0, count("SELECT count(*) FROM "+acme))
	}
	// every other table has data
	for _, table := range insertOrder {
		if count("SELECT count(*) FROM "+table) == 0 {
			t.Errorf("table %s is empty", table)
		}
	}

	// -- QaG statuses ---------------------------------------------------------------
	for status, want := range map[int]int64{-1: 5, 0: 7, 1: 15, 2: 5, 7: 3} {
		eq(fmt.Sprintf("qags status %d", status), want, count("SELECT count(*) FROM qags WHERE status = $1", status))
	}
	for _, id := range []string{QagSelectedVideo, QagSelectedText, QagSelectedDocument} {
		eq("selected "+id, 1, count("SELECT count(*) FROM qags WHERE id = $1 AND status = 7", id))
	}
	eq("qag post dates unique", count("SELECT count(*) FROM qags"), count("SELECT count(DISTINCT post_date) FROM qags"))
	eq("moderation before post", 0, count("SELECT count(*) FROM qag_updates u JOIN qags q ON q.id = u.qag_id WHERE u.moderated_date < q.post_date"))
	eq("support before post", 0, count("SELECT count(*) FROM supports_qag s JOIN qags q ON q.id = s.qag_id WHERE s.support_date < q.post_date"))

	// trending window quirks
	eq("two accepted updates in 7 days", 2, count(
		"SELECT count(*) FROM qag_updates WHERE qag_id = $1 AND status = 1 AND moderated_date >= $2", QagAcceptedTwoUpdates, testNow.AddDate(0, 0, -7)))
	eq("accepted without update", 0, count("SELECT count(*) FROM qag_updates WHERE qag_id = $1", QagAcceptedNoUpdate))
	eq("accepted moderated < 72h", 8, count(
		"SELECT count(DISTINCT qag_id) FROM qag_updates WHERE status = 1 AND moderated_date >= $1 AND qag_id IN (SELECT id FROM qags WHERE status = 1)", testNow.Add(-72*time.Hour)))
	cutoff := MondayCutoff(testNow)
	if got := count("SELECT count(*) FROM qags WHERE status = 1 AND id IN (SELECT qag_id FROM qag_updates WHERE status = 1 AND moderated_date < $1)", cutoff); got < 3 {
		t.Errorf("weekly archive candidates: want >= 3, got %d", got)
	}
	// duplicate support, banned supports, anonymised supporter
	eq("duplicate support rows", 2, count("SELECT count(*) FROM supports_qag WHERE qag_id = $1 AND user_id = $2", QagAcceptedDupSupport, UserRegular5))
	eq("banned supports (7d, unselected)", 5, count(
		"SELECT count(*) FROM supports_qag WHERE user_id = $1 AND support_date > $2 AND qag_id NOT IN (SELECT id FROM qags WHERE status = 7)", UserBanned, testNow.AddDate(0, 0, -7)))
	eq("banned support on selected", 1, count("SELECT count(*) FROM supports_qag WHERE user_id = $1 AND qag_id = $2", UserBanned, QagSelectedVideo))
	eq("anonymised supports", 3, count("SELECT count(*) FROM supports_qag WHERE user_id = $1", UserAnonymized))
	eq("locked qags", 2, count("SELECT count(*) FROM moderatus_locked_qags"))
	eq("open and not locked", 5, count("SELECT count(*) FROM qags WHERE status = 0 AND id NOT IN (SELECT qag_id FROM moderatus_locked_qags)"))
	eq("selected feedbacks", 7, count("SELECT count(*) FROM feedbacks_qag"))
	eq("helpful and not helpful", 2, count("SELECT count(DISTINCT is_helpful) FROM feedbacks_qag"))
	eq("low priority", 1, count("SELECT count(*) FROM low_priority_qags"))
	eq("delete log", 2, count("SELECT count(*) FROM qag_delete_log"))
	eq("author support never", 0, count("SELECT count(*) FROM supports_qag s JOIN qags q ON q.id = s.qag_id AND q.user_id = s.user_id"))

	// -- users -----------------------------------------------------------------------
	for level, want := range map[int]int64{0: 54, 8: 1, 42: 1, 1337: 1} {
		eq(fmt.Sprintf("users level %d", level), want, count("SELECT count(*) FROM agora_users WHERE authorization_level = $1", level))
	}
	eq("banned users", 1, count("SELECT count(*) FROM agora_users WHERE is_banned = 1"))
	// UserNoFcm1 + UserNeverConnected + every signup-abuse group (they never log in)
	eq("null last_connection_date", int64(2+len(UsersSuspectA)+len(UsersPairB)+len(UsersSpreadC)+len(UsersSuspectD)+len(UsersOldE)+1+len(UsersMass)+len(UsersEmptyIP)),
		count("SELECT count(*) FROM agora_users WHERE last_connection_date IS NULL"))
	eq("shared token A", 3, count("SELECT count(*) FROM agora_users WHERE fcm_token = $1", FcmTokenSharedA))
	eq("shared token B", 2, count("SELECT count(*) FROM agora_users WHERE fcm_token = $1", FcmTokenSharedB))
	eq("null fcm tokens", 0, count("SELECT count(*) FROM agora_users WHERE fcm_token IS NULL OR password IS NULL OR created_date IS NULL OR is_banned IS NULL"))
	eq("unique token winner A", 1, count(`WITH r AS (SELECT id, ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) rk FROM agora_users)
		SELECT count(*) FROM r WHERE rk = 1 AND id = $1`, UserRegular4))

	// suspicious users: the nightly job flags exactly groups A, D, cross-day and mass.
	flagged := scalar[[]string](t, conn, `
		WITH s AS (SELECT ip_address_hash, user_agent FROM users_data
			WHERE event_type = 'signup' AND ip_address_hash != '' AND event_date > $1 AND event_date < $2
			GROUP BY ip_address_hash, user_agent, DATE(event_date) HAVING count(*) >= 3)
		SELECT coalesce(array_agg(DISTINCT user_id ORDER BY user_id), '{}') FROM users_data
		WHERE CONCAT(ip_address_hash, user_agent) IN (SELECT CONCAT(ip_address_hash, user_agent) FROM s) AND event_type = 'signup'`,
		time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -14), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	wantFlagged := map[string]bool{}
	for _, g := range [][]string{UsersSuspectA, UsersSuspectD, UsersMass, {UserSuspectCrossDay}} {
		for _, u := range g {
			wantFlagged[u] = true
		}
	}
	if len(flagged) != len(wantFlagged) {
		t.Errorf("flagged users: want %d, got %d (%v)", len(wantFlagged), len(flagged), flagged)
	}
	for _, u := range flagged {
		if !wantFlagged[u] {
			t.Errorf("unexpected flagged user %s", u)
		}
	}

	// -- profiles -----------------------------------------------------------------------
	for col, wants := range map[string][]string{
		"gender":                   {"M", "F", "A"},
		"city_type":                {"R", "U", "A"},
		"job_category":             {"AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN"},
		"vote_frequency":           {"S", "P", "J"},
		"public_meeting_frequency": {"S", "P", "J"},
		"consultation_frequency":   {"S", "P", "J"},
	} {
		for _, w := range wants {
			if count(fmt.Sprintf("SELECT count(*) FROM users_profile WHERE %s = $1", col), w) == 0 {
				t.Errorf("no profile with %s = %s", col, w)
			}
		}
	}
	for _, d := range []string{"75", "13", "2A", "2B"} {
		if count("SELECT count(*) FROM users_profile WHERE department = $1", d) == 0 {
			t.Errorf("no profile with department %s", d)
		}
	}
	eq("ask dates", 3, count("SELECT count(*) FROM demographic_info_ask_date"))

	// -- notifications --------------------------------------------------------------------
	for ord := 0; ord <= 5; ord++ {
		if count("SELECT count(*) FROM notifications WHERE type = $1", fmt.Sprint(ord)) == 0 {
			t.Errorf("no notification of type %d", ord)
		}
	}
	if n := count("SELECT count(*) FROM notifications WHERE user_id = $1", UserRegular1); n <= 20 {
		t.Errorf("UserRegular1 notifications: want > 20, got %d", n)
	}

	// -- consultations -----------------------------------------------------------------------
	for _, c := range []struct {
		n  int
		id string
	}{{1, Consultation1}, {4, Consultation4}, {7, Consultation7}} {
		for _, sentinel := range []string{ChoiceSkipped, ChoiceNotApplicable} {
			if count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND choice_id = $2", c.id, sentinel) == 0 {
				t.Errorf("consultation %d: no %s row", c.n, sentinel)
			}
		}
		if count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND choice_id IS NULL AND response_text = ''", c.id) == 0 {
			t.Errorf("consultation %d: no empty open text", c.n)
		}
		if count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND length(response_text) > 0", c.id) == 0 {
			t.Errorf("consultation %d: no open text", c.n)
		}
		if p, u := count("SELECT count(DISTINCT participation_id) FROM reponses_consultation WHERE consultation_id = $1", c.id),
			count("SELECT count(DISTINCT user_id) FROM user_answered_consultation WHERE consultation_id = $1", c.id); p != u {
			t.Errorf("consultation %d: %d participations for %d participants", c.n, p, u)
		}
		eq(fmt.Sprintf("consultation %d: no aggregated results", c.n), 0, count("SELECT count(*) FROM consultation_results WHERE consultation_id = $1", c.id))
	}
	eq("c7 NULL text row", 1, count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND response_text IS NULL", Consultation7))
	// consultation 5: aggregated, raw rows reduced to anonymised texts
	if count("SELECT count(*) FROM consultation_results WHERE consultation_id = $1", Consultation5) == 0 {
		t.Errorf("consultation 5 has no aggregated results")
	}
	eq("c5 aggregated sentinels", 2, count("SELECT count(DISTINCT choice_id) FROM consultation_results WHERE consultation_id = $1 AND choice_id IN ($2, $3)", Consultation5, ChoiceSkipped, ChoiceNotApplicable))
	eq("c5 raw rows are anonymised", count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1", Consultation5),
		count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1 AND user_id = $2 AND participation_id = $2 AND length(response_text) > 0", Consultation5, UserAnonymized))
	for _, id := range []string{Consultation2, Consultation3, Consultation6} {
		eq("no rows for "+id, 0, count("SELECT count(*) FROM reponses_consultation WHERE consultation_id = $1", id)+count("SELECT count(*) FROM user_answered_consultation WHERE consultation_id = $1", id))
	}
	eq("consultation update feedbacks", 8, count("SELECT count(*) FROM feedbacks_consultation_update"))
	eq("app feedback types", 3, count("SELECT count(DISTINCT type) FROM app_feedbacks"))

	// -- scale 3: still deterministic, handcrafted rows untouched -------------------------------
	if err := Seed(ctx, conn, testNow, 3); err != nil {
		t.Fatalf("seed scale 3 #1: %v", err)
	}
	h3 := contentHash(t, conn)
	if err := Seed(ctx, conn, testNow, 3); err != nil {
		t.Fatalf("seed scale 3 #2: %v", err)
	}
	if h3b := contentHash(t, conn); h3 != h3b {
		t.Fatalf("scale 3 is not idempotent")
	}
	eq("scale 3 users", 57+2*bulkUsersPerScale, count("SELECT count(*) FROM agora_users"))
	eq("scale 3 qags", 35+2*bulkQagsPerScale, count("SELECT count(*) FROM qags"))
	eq("scale 3 selected qags", 3, count("SELECT count(*) FROM qags WHERE status = 7"))
	if err := Seed(ctx, conn, testNow, 1); err != nil { // and back to the small set
		t.Fatalf("seed back to scale 1: %v", err)
	}
	if again := contentHash(t, conn); again != first {
		t.Fatalf("scale 1 after scale 3 differs from the first scale 1 run")
	}
}
