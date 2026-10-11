package moderatus

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/modules/users"
	"agora/internal/store"
)

// LockRepository is ModeratusQagLockRepositoryImpl on top of the moderatus_locked_qags table.
//
// The Kotlin ModeratusQagLockCacheRepository is never filled: addLockedQagIds and
// removeLockedQagId only put a value when the cache already holds one, and nothing
// ever creates the first value. getLockedQagIds therefore always reads the whole
// table (findAll) and the cache calls are no-ops. This is the "never-filled
// Moderatus lock cache" of the allowed class B list: Go has no cache at all and
// reads the table every time, exactly like Kotlin does (see DIVERGENCES.md).
type LockRepository struct{ a *app.App }

// ErrNullLockedQagID mirrors the NullPointerException of mapper.toQagId(dto) for a
// moderatus_locked_qags row whose qag_id is NULL (Hibernate loads it, the Kotlin
// non-null property is read as null).
var ErrNullLockedQagID = errors.New("NullPointerException: moderatus_locked_qags.qag_id is NULL")

// GetLockedQagIDs is getLockedQagIds: the canonical lowercase UUID of every lock row.
func (r *LockRepository) GetLockedQagIDs(ctx context.Context) ([]string, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT qag_id FROM moderatus_locked_qags")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id == nil {
			return nil, ErrNullLockedQagID
		}
		ids = append(ids, *id)
	}
	return ids, rows.Err()
}

// AddLockedIDs is addLockedIds: the ids that are not UUIDs (toUuidOrNull is lenient)
// are dropped; the others are saved by one saveAll (one transaction, a fresh random id
// and "now" as lock date per row; a QaG may therefore be locked twice).
func (r *LockRepository) AddLockedIDs(ctx context.Context, lockedQagIDs []string) error {
	uuids := make([]string, 0, len(lockedQagIDs))
	for _, id := range lockedQagIDs {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			uuids = append(uuids, u)
		}
	}
	if len(uuids) == 0 {
		return nil
	}
	now := store.Millis(r.a.Now())
	return r.a.DB.Tx(ctx, func(tx pgx.Tx) error {
		for _, u := range uuids {
			if _, err := tx.Exec(ctx, "INSERT INTO moderatus_locked_qags (id, lock_date, qag_id) VALUES ($1, $2, $3)",
				users.RandomUUID(), now, u); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveLockedQagID is removeLockedQagId: UUID.fromString (an invalid id does nothing),
// then DELETE FROM moderatus_locked_qags WHERE qag_id = :qagId.
func (r *LockRepository) RemoveLockedQagID(ctx context.Context, qagID string) error {
	u, ok := javacompat.ParseUUID(qagID)
	if !ok {
		return nil
	}
	_, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM moderatus_locked_qags WHERE qag_id = $1", u.String())
	return err
}
