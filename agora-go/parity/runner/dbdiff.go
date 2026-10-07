package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// tables compared by the DB diff (all application tables).
var dbTables = []string{
	"agora_users", "users_data", "users_profile", "demographic_info_ask_date", "qags", "qag_updates",
	"qag_delete_log", "low_priority_qags", "supports_qag", "feedbacks_qag", "moderatus_locked_qags",
	"notifications", "reponses_consultation", "user_answered_consultation", "consultation_results",
	"feedbacks_consultation_update", "app_feedbacks", "acme_account", "acme_certificate", "acme_order",
	"acme_challenge",
}

type dbRow struct {
	cols []string
	vals []string
	key  string
}

func snapshotDB(ctx context.Context, dbURL string) (map[string][]dbRow, error) {
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	out := map[string][]dbRow{}
	for _, t := range dbTables {
		rows, err := conn.Query(ctx, "SELECT * FROM "+t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		fields := rows.FieldDescriptions()
		cols := make([]string, len(fields))
		for i, f := range fields {
			cols[i] = f.Name
		}
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, err
			}
			r := dbRow{cols: cols, vals: make([]string, len(vals))}
			for i, v := range vals {
				r.vals[i] = formatDBValue(v)
			}
			out[t] = append(out[t], r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func formatDBValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "∅"
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", t[0:4], t[4:6], t[6:8], t[8:10], t[10:16])
	case time.Time:
		return t.UTC().Format("2006-01-02 15:04:05.000000")
	case []byte:
		return string(t)
	}
	return fmt.Sprint(v)
}

// normalize applies the binder (go→ref) and masks "recent" timestamps.
func (b *Binder) normalizeRow(r dbRow, goSide bool, recentAfter time.Time) dbRow {
	out := dbRow{cols: r.cols, vals: make([]string, len(r.vals))}
	for i, v := range r.vals {
		if goSide {
			if rv, ok := b.GoToRef(v); ok {
				v = rv
			}
		}
		if len(v) == 26 && v[4] == '-' && v[10] == ' ' {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05.000000", v, time.UTC); err == nil {
				wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
				if wall.After(recentAfter) {
					v = "~recent"
				}
			}
		}
		out.vals[i] = v
	}
	out.key = strings.Join(out.vals, "\x1f")
	return out
}

func isUUIDLike(s string) bool { return uuidRe.MatchString(s) }

// diffDB compares both snapshots (multisets per table) with binding of
// freshly generated UUIDs.
func (b *Binder) diffDB(ref, gov map[string][]dbRow, recentAfter time.Time) []Diff {
	var diffs []Diff
	for _, t := range dbTables {
		rrows := make([]dbRow, 0, len(ref[t]))
		for _, r := range ref[t] {
			rrows = append(rrows, b.normalizeRow(r, false, recentAfter))
		}
		grows := make([]dbRow, 0, len(gov[t]))
		for _, g := range gov[t] {
			grows = append(grows, b.normalizeRow(g, true, recentAfter))
		}
		// exact multiset matching
		count := map[string]int{}
		for _, r := range rrows {
			count[r.key]++
		}
		var gLeft []dbRow
		for _, g := range grows {
			if count[g.key] > 0 {
				count[g.key]--
			} else {
				gLeft = append(gLeft, g)
			}
		}
		var rLeft []dbRow
		for _, r := range rrows {
			if count[r.key] > 0 {
				count[r.key]--
				rLeft = append(rLeft, r)
			}
		}
		// second pass: pair rows differing only by unbound UUIDs, then bind
		var stillR []dbRow
		for _, r := range rLeft {
			matched := -1
			for j, g := range gLeft {
				if rowsBindable(b, r, g) {
					matched = j
					break
				}
			}
			if matched >= 0 {
				g := gLeft[matched]
				for i := range r.vals {
					if r.vals[i] != g.vals[i] {
						b.bind(r.vals[i], g.vals[i])
					}
				}
				gLeft = append(gLeft[:matched], gLeft[matched+1:]...)
				continue
			}
			stillR = append(stillR, r)
		}
		sort.Slice(stillR, func(i, j int) bool { return stillR[i].key < stillR[j].key })
		sort.Slice(gLeft, func(i, j int) bool { return gLeft[i].key < gLeft[j].key })
		for _, r := range stillR {
			diffs = append(diffs, Diff{Where: "db:" + t, Ref: rowString(r), Go: "(missing)"})
		}
		for _, g := range gLeft {
			diffs = append(diffs, Diff{Where: "db:" + t, Ref: "(missing)", Go: rowString(g)})
		}
	}
	return diffs
}

func rowsBindable(b *Binder, r, g dbRow) bool {
	if len(r.vals) != len(g.vals) {
		return false
	}
	for i := range r.vals {
		if r.vals[i] == g.vals[i] {
			continue
		}
		if !(isUUIDLike(r.vals[i]) && isUUIDLike(g.vals[i])) {
			return false
		}
		b.mu.Lock()
		_, rb := b.refToGo[r.vals[i]]
		_, gb := b.goToRef[g.vals[i]]
		b.mu.Unlock()
		if rb || gb {
			return false
		}
	}
	return true
}

func rowString(r dbRow) string {
	parts := make([]string, len(r.cols))
	for i, c := range r.cols {
		parts[i] = c + "=" + r.vals[i]
	}
	return strings.Join(parts, " ")
}
