package qag

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// ---------------------------------------------------------------------------
// QagUpdatesRepositoryImpl (+ QagUpdatesMapper, QagUpdatesDatabaseRepository)
// ---------------------------------------------------------------------------

// UpdatesRepository is QagUpdatesRepositoryImpl: the moderation history (qag_updates).
type UpdatesRepository struct{ a *app.App }

// InsertQagUpdates is insertQagUpdates: a row with status 1 for an accepted QaG,
// -1 for anything else, "now" as moderation date. Ids that are not UUIDs make the
// mapper return null: nothing is written.
func (r *UpdatesRepository) InsertQagUpdates(ctx context.Context, u QagInsertingUpdates) error {
	qid, ok := javacompat.ToUUIDOrNull(u.QagID)
	if !ok {
		return nil
	}
	uid, ok := javacompat.ToUUIDOrNull(u.UserID)
	if !ok {
		return nil
	}
	status := dbStatusModeratedRejected
	if u.NewQagStatus == StatusModeratedAccepted {
		status = dbStatusModeratedAccepted
	}
	shouldDelete := 0
	if u.ShouldDelete {
		shouldDelete = 1
	}
	_, err := r.a.DB.Pool.Exec(ctx, `INSERT INTO qag_updates (id, moderated_date, motif_id, qag_id, reason, should_delete_flag, status, user_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		randomUUID(), store.Millis(r.a.Now()), u.MotifID, qid, u.Reason, shouldDelete, status, uid)
	return err
}

// GetQagUpdates is getQagUpdates: one id that is not a UUID gives an empty list
// (the IllegalArgumentException is caught); rows with an unknown status are dropped.
func (r *UpdatesRepository) GetQagUpdates(ctx context.Context, qagIDs []string) ([]QagUpdates, error) {
	uids := make([]string, 0, len(qagIDs))
	for _, id := range qagIDs {
		u, ok := javacompat.ToUUIDOrNull(id)
		if !ok {
			return []QagUpdates{}, nil
		}
		uids = append(uids, u)
	}
	if len(uids) == 0 {
		return []QagUpdates{}, nil
	}
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT qag_id, user_id, status, moderated_date FROM qag_updates WHERE qag_id in "+inList(1, len(uids)), toArgs(uids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QagUpdates{}
	for rows.Next() {
		var qag, user *string
		var status int
		var date *time.Time
		if err := rows.Scan(&qag, &user, &status, &date); err != nil {
			return nil, err
		}
		if qag == nil {
			return nil, errNullColumn("qag_updates", "qag_id")
		}
		st, err := statusFromDB(status)
		if err != nil {
			continue // toDomain returns null
		}
		if user == nil {
			return nil, errNullColumn("qag_updates", "user_id")
		}
		if date == nil {
			return nil, errNullColumn("qag_updates", "moderated_date")
		}
		out = append(out, QagUpdates{QagID: *qag, QagStatus: st, UserID: *user, ModeratedDate: store.Local(*date)})
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// QagDeleteLogRepositoryImpl, LowPriorityQagRepositoryImpl
// ---------------------------------------------------------------------------

// DeleteLogRepository is QagDeleteLogRepositoryImpl (qag_delete_log).
type DeleteLogRepository struct{ a *app.App }

// InsertQagDeleteLog is insertQagDeleteLog (ids that are not UUIDs: nothing is written).
func (r *DeleteLogRepository) InsertQagDeleteLog(ctx context.Context, l QagDeleteLog) error {
	qid, ok := javacompat.ToUUIDOrNull(l.QagID)
	if !ok {
		return nil
	}
	uid, ok := javacompat.ToUUIDOrNull(l.UserID)
	if !ok {
		return nil
	}
	_, err := r.a.DB.Pool.Exec(ctx, "INSERT INTO qag_delete_log (id, delete_date, qag_id, user_id) VALUES ($1, $2, $3, $4)",
		randomUUID(), store.Millis(r.a.Now()), qid, uid)
	return err
}

// LowPriorityRepository is LowPriorityQagRepositoryImpl (low_priority_qags).
type LowPriorityRepository struct{ a *app.App }

// GetLowPriorityQagIDs is getLowPriorityQagIds: the given QaG ids (those that are
// UUIDs) that are flagged low priority, canonical form, in database order.
func (r *LowPriorityRepository) GetLowPriorityQagIDs(ctx context.Context, qagIDs []string) ([]string, error) {
	uids := uuidsOrNull(qagIDs)
	if len(uids) == 0 {
		return []string{}, nil
	}
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT qag_id FROM low_priority_qags WHERE qag_id IN "+inList(1, len(uids)), toArgs(uids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != nil {
			out = append(out, *id)
		}
	}
	return out, rows.Err()
}
