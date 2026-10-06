package seed

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// tables lists the 21 tables of the Hibernate schema, in the order used for
// TRUNCATE. The 1-based index is also used as the first group of the
// deterministic row ids (see builder.rowID).
var tables = []string{
	"acme_account",
	"acme_certificate",
	"acme_challenge",
	"acme_order",
	"agora_users",
	"app_feedbacks",
	"consultation_results",
	"demographic_info_ask_date",
	"feedbacks_consultation_update",
	"feedbacks_qag",
	"low_priority_qags",
	"moderatus_locked_qags",
	"notifications",
	"qag_delete_log",
	"qag_updates",
	"qags",
	"reponses_consultation",
	"supports_qag",
	"user_answered_consultation",
	"users_data",
	"users_profile",
}

// insertColumns gives, for each seeded table, the column order of the values
// passed to builder.add. acme_* tables are never filled.
var insertColumns = map[string][]string{
	"agora_users":                   {"id", "authorization_level", "created_date", "fcm_token", "last_connection_date", "password", "is_banned"},
	"app_feedbacks":                 {"id", "app_version", "created_date", "description", "device_model", "os_version", "type", "user_id"},
	"consultation_results":          {"id", "choice_id", "consultation_id", "question_id", "response_count"},
	"demographic_info_ask_date":     {"id", "ask_date", "user_id"},
	"feedbacks_consultation_update": {"id", "consultation_update_id", "created_date", "is_positive", "updated_date", "user_id"},
	"feedbacks_qag":                 {"id", "created_date", "is_helpful", "qag_id", "updated_date", "user_id"},
	"low_priority_qags":             {"id", "qag_id"},
	"moderatus_locked_qags":         {"id", "lock_date", "qag_id"},
	"notifications":                 {"id", "date", "description", "title", "type", "user_id"},
	"qag_delete_log":                {"id", "delete_date", "qag_id", "user_id"},
	"qag_updates":                   {"id", "moderated_date", "motif_id", "qag_id", "reason", "should_delete_flag", "status", "user_id"},
	"qags":                          {"id", "description", "motif_id", "post_date", "status", "thematique_id", "title", "user_id", "username"},
	"reponses_consultation":         {"id", "choice_id", "consultation_id", "participation_date", "participation_id", "question_id", "response_text", "user_id"},
	"supports_qag":                  {"id", "qag_id", "support_date", "user_id"},
	"user_answered_consultation":    {"id", "consultation_id", "participation_date", "user_id"},
	"users_data":                    {"id", "event_date", "event_type", "fcm_token", "ip_address_hash", "platform", "user_agent", "user_id", "version_code", "version_name"},
	"users_profile":                 {"id", "city_type", "consultation_frequency", "department", "gender", "job_category", "primary_department", "public_meeting_frequency", "secondary_department", "user_id", "vote_frequency", "year_of_birth"},
}

// insertOrder is the order in which tables are filled (no FK, purely cosmetic).
var insertOrder = []string{
	"agora_users", "users_profile", "demographic_info_ask_date", "users_data",
	"qags", "qag_updates", "supports_qag", "feedbacks_qag", "qag_delete_log",
	"low_priority_qags", "moderatus_locked_qags", "notifications",
	"reponses_consultation", "user_answered_consultation", "consultation_results",
	"feedbacks_consultation_update", "app_feedbacks",
}

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// builder accumulates, in memory, the rows of every table. Nothing touches the
// database until Seed has finished building (so a bug in the generator can
// never leave a half-seeded database behind).
type builder struct {
	now   time.Time
	scale int
	rng   *rand.Rand

	rows map[string][][]any
	seq  map[string]int

	// derived while building, shared between the data files
	supporterPool []string // regular, non-banned users that can support QaGs
}

func newBuilder(now time.Time, scale int) *builder {
	now = now.UTC().Truncate(time.Millisecond)
	return &builder{
		now:   now,
		scale: scale,
		rng:   rand.New(rand.NewSource(20240607)),
		rows:  map[string][][]any{},
		seq:   map[string]int{},
	}
}

// add appends one row; the values must follow insertColumns[table].
func (b *builder) add(table string, vals ...any) {
	cols, ok := insertColumns[table]
	if !ok {
		panic(fmt.Sprintf("seed: unknown table %q", table))
	}
	if len(cols) != len(vals) {
		panic(fmt.Sprintf("seed: table %s expects %d values, got %d", table, len(cols), len(vals)))
	}
	b.rows[table] = append(b.rows[table], vals)
}

// rowID returns the deterministic primary key of the next row of a table:
// <table index>-0000-4000-a000-<sequence>.
func (b *builder) rowID(table string) pgtype.UUID {
	idx := 0
	for i, t := range tables {
		if t == table {
			idx = i + 1
			break
		}
	}
	if idx == 0 {
		panic("seed: unknown table " + table)
	}
	b.seq[table]++
	return uid(fmt.Sprintf("%08x-0000-4000-a000-%012x", idx, b.seq[table]))
}

// uid converts a UUID string into a pgtype.UUID (binary COPY needs it).
func uid(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(fmt.Sprintf("seed: invalid uuid %q: %v", s, err))
	}
	return u
}

// ---------------------------------------------------------------------------
// time helpers (everything is UTC, truncated to the millisecond)
// ---------------------------------------------------------------------------

// ago returns now - d.
func (b *builder) ago(d time.Duration) time.Time { return b.now.Add(-d).Truncate(time.Millisecond) }

// at returns now + d (d may be negative).
func (b *builder) at(d time.Duration) time.Time { return b.now.Add(d).Truncate(time.Millisecond) }

// dayAt returns the instant `daysAgo` calendar days before today (UTC) at hh:mm.
// It is used where the SQL compares dates (DATE(event_date), "today - 2 weeks")
// so that the rows stay on the intended calendar day whatever the time of day
// `now` is.
func (b *builder) dayAt(daysAgo, hh, mm int) time.Time {
	y, m, d := b.now.Date()
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
}

// MondayCutoff returns "this week's Monday 10:00" (UTC) for the given instant,
// the threshold used by the weekly QaG archive job and by the weekly
// "ask a QaG" limit.
func MondayCutoff(now time.Time) time.Time {
	now = now.UTC()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	offset := (int(midnight.Weekday()) + 6) % 7 // Monday = 0
	return midnight.AddDate(0, 0, -offset).Add(10 * hour)
}

// between returns the instant at fraction i/(n+1) of [from, to], truncated to ms.
func between(from, to time.Time, i, n int) time.Time {
	span := to.Sub(from)
	return from.Add(span / time.Duration(n+1) * time.Duration(i)).Truncate(time.Millisecond)
}

// pick returns element i of s, wrapping around.
func pick[T any](s []T, i int) T { return s[((i%len(s))+len(s))%len(s)] }
