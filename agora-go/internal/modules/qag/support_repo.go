package qag

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// SupportRepository is SupportQagRepositoryImpl + GetSupportQagRepositoryImpl
// (SupportQagDatabaseRepository, SupportQagMapper).
type SupportRepository struct {
	a    *app.App
	info *InfoRepository
}

// GetUserSupportedQags is getUserSupportedQags: the ids (canonical) of the QaGs
// the user supports that are accepted, or open and written by the user, most
// recent support first. An id that is not a UUID gives an empty list.
func (r *SupportRepository) GetUserSupportedQags(ctx context.Context, userID string) ([]string, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return []string{}, nil
	}
	rows, err := r.a.DB.Pool.Query(ctx, `SELECT qag_id FROM supports_qag
            WHERE qag_id IN (
                SELECT id FROM qags
                WHERE status = 1
                OR (status = 0 AND user_id = $1)
            )
            AND user_id = $1
            ORDER BY support_date DESC`, uid)
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

// GetUserSupportedQagIDs is SupportQagUseCase.getUserSupportedQagIds.
func (r *SupportRepository) GetUserSupportedQagIDs(ctx context.Context, userID string) ([]string, error) {
	return r.GetUserSupportedQags(ctx, userID)
}

// IsQagSupported is isQagSupported (false when either id is not a UUID).
func (r *SupportRepository) IsQagSupported(ctx context.Context, userID, qagID string) (bool, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return false, nil
	}
	qid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return false, nil
	}
	return r.isSupported(ctx, uid, qid)
}

// isSupported is SupportQagDatabaseRepository.getSupportQag(..) != null.
func (r *SupportRepository) isSupported(ctx context.Context, userUUID, qagUUID string) (bool, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT * FROM supports_qag WHERE user_id = $1 AND qag_id = $2 LIMIT 1", userUUID, qagUUID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := rows.Next()
	rows.Close()
	return found, rows.Err()
}

// GetSupportedQagCount is getSupportedQagCount. Kotlin's SQL has an operator
// precedence quirk in the thematique variant (`status = 1 OR (status = 0 AND
// user_id = :userId) AND thematique_id = :thematiqueId`), kept verbatim.
func (r *SupportRepository) GetSupportedQagCount(ctx context.Context, userID string, thematiqueID *string) (int, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return 0, nil
	}
	var n int64
	var err error
	if thematiqueID != nil {
		err = r.a.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM supports_qag
            WHERE qag_id IN (
                SELECT id FROM qags
                WHERE status = 1
                OR (status = 0 AND user_id = $1)
                AND thematique_id = $2
            )
            AND user_id = $1 `, uid, *thematiqueID).Scan(&n)
	} else {
		err = r.a.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM supports_qag
            WHERE qag_id IN (
                SELECT id FROM qags
                WHERE status = 1
                OR (status = 0 AND user_id = $1)
            )
            AND user_id = $1 `, uid).Scan(&n)
	}
	return int(n), err
}

// insertSQL inserts the support when the QaG exists and is open or accepted and
// the user does not support it yet: the whole InsertSupportQagUseCase
// (isQagSupported, getQagInfo + status check, insert) in one round trip.
var insertSupportSQL = `INSERT INTO supports_qag (id, qag_id, support_date, user_id)
        SELECT $1, qags.id, $3, $4 FROM qags
        WHERE qags.id = $2 AND qags.status IN (0, 1) AND ` + qagMappable("qags") + `
        AND NOT EXISTS (SELECT 1 FROM supports_qag WHERE user_id = $4 AND qag_id = $2)`

// qagMappable is true for a row QagInfoMapper can map: Kotlin throws on a NULL in
// one of the non-null properties, so the one-statement paths below must not act
// on such a row (the replay then raises the same exception).
func qagMappable(t string) string {
	return t + ".title IS NOT NULL AND " + t + ".description IS NOT NULL AND " + t + ".post_date IS NOT NULL AND " +
		t + ".username IS NOT NULL AND " + t + ".thematique_id IS NOT NULL AND " + t + ".user_id IS NOT NULL"
}

// InsertSupportQag is InsertSupportQagUseCase.insertSupportQag followed by
// SupportQagRepositoryImpl.insertSupportQag, in a single INSERT ... SELECT.
// When nothing was inserted the Kotlin sequence is replayed to find out whether
// it ends in FAILURE (400) or in an exception (a QaG with an invalid status
// makes QagInfoMapper throw: 500).
func (r *SupportRepository) InsertSupportQag(ctx context.Context, s SupportQagInserting) (SupportQagResult, error) {
	uid, uok := javacompat.ToUUIDOrNull(s.UserID)
	qid, qok := javacompat.ToUUIDOrNull(s.QagID)
	if !uok || !qok {
		// isQagSupported is false; getQagInfo(null) → FAILURE (invalid qag), or the
		// SupportQagMapper rejects the user id.
		return SupportFailure, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, insertSupportSQL, randomUUID(), qid, store.Millis(r.a.Now()), uid)
	if err != nil {
		return SupportFailure, err
	}
	if tag.RowsAffected() > 0 {
		r.info.Evict(ctx, qid)
		return SupportSuccess, nil
	}
	return SupportFailure, r.replayFailure(ctx, uid, qid)
}

// replayFailure replays the read part of Insert/DeleteSupportQagUseCase after a
// statement that changed nothing: its only observable difference with a plain
// FAILURE is the exception thrown when the QaG has an invalid status.
func (r *SupportRepository) replayFailure(ctx context.Context, userUUID, qagUUID string) error {
	supported, err := r.isSupported(ctx, userUUID, qagUUID)
	if err != nil || supported {
		return err
	}
	_, err = r.info.getQagByUUID(ctx, qagUUID)
	return err
}

// DeleteSupportQag is DeleteSupportQagUseCase.deleteSupportQag followed by
// SupportQagRepositoryImpl.deleteSupportQag: the support must exist and the QaG
// be open or accepted; every row (user, qag) is deleted.
func (r *SupportRepository) DeleteSupportQag(ctx context.Context, s SupportQagDeleting) (SupportQagResult, error) {
	qid, qok := javacompat.ToUUIDOrNull(s.QagID)
	uid, uok := javacompat.ToUUIDOrNull(s.UserID)
	if !qok || !uok {
		return SupportFailure, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, `DELETE FROM supports_qag WHERE user_id = $1 AND qag_id = $2
        AND EXISTS (SELECT 1 FROM qags WHERE id = $2 AND status IN (0, 1) AND `+qagMappable("qags")+`)`, uid, qid)
	if err != nil {
		return SupportFailure, err
	}
	if tag.RowsAffected() > 0 {
		r.info.Evict(ctx, qid)
		return SupportSuccess, nil
	}
	supported, err := r.isSupported(ctx, uid, qid)
	if err != nil || !supported {
		return SupportFailure, err
	}
	_, err = r.info.getQagByUUID(ctx, qid)
	return SupportFailure, err
}

// InsertSupportQagUnchecked is SupportQagRepositoryImpl.insertSupportQag alone,
// without the checks of InsertSupportQagUseCase (used by InsertQagUseCase).
func (r *SupportRepository) InsertSupportQagUnchecked(ctx context.Context, s SupportQagInserting) (SupportQagResult, error) {
	uid, uok := javacompat.ToUUIDOrNull(s.UserID)
	qid, qok := javacompat.ToUUIDOrNull(s.QagID)
	if !uok || !qok {
		return SupportFailure, nil
	}
	_, err := r.a.DB.Pool.Exec(ctx,
		"INSERT INTO supports_qag (id, qag_id, support_date, user_id) VALUES ($1, $2, $3, $4)",
		randomUUID(), qid, store.Millis(r.a.Now()), uid)
	if err != nil {
		return SupportFailure, err
	}
	r.info.Evict(ctx, qid)
	return SupportSuccess, nil
}

// DeleteSupportListByQagID is deleteSupportListByQagId: every support of the QaG.
func (r *SupportRepository) DeleteSupportListByQagID(ctx context.Context, qagID string) (SupportQagResult, error) {
	qid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return SupportFailure, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM supports_qag WHERE qag_id = $1", qid)
	r.info.Evict(ctx, qid)
	if err != nil {
		return SupportFailure, err
	}
	if tag.RowsAffected() <= 0 {
		return SupportFailure, nil
	}
	return SupportSuccess, nil
}

// DeleteUsersSupportQag is deleteUsersSupportQag: supports on QaGs that are not
// selected are deleted, those on selected QaGs are anonymized (zero user), then
// the supports of the QaGs that the deleted users wrote.
func (r *SupportRepository) DeleteUsersSupportQag(ctx context.Context, userIDs []string) error {
	uids := uuidsOrNull(userIDs)
	if len(uids) == 0 {
		return nil
	}
	defer r.info.EvictAll(ctx)
	if _, err := r.a.DB.Pool.Exec(ctx, `DELETE FROM supports_qag
        WHERE user_id IN `+inList(1, len(uids))+`
        AND qag_id IN (
            SELECT id FROM qags
            WHERE status <> 7
        )`, toArgs(uids)...); err != nil {
		return err
	}
	if _, err := r.a.DB.Pool.Exec(ctx, `UPDATE supports_qag
        SET user_id = '00000000-0000-0000-0000-000000000000'
        WHERE user_id IN `+inList(1, len(uids))+`
        AND qag_id IN (
            SELECT id FROM qags
            WHERE status = 7
        )`, toArgs(uids)...); err != nil {
		return err
	}
	_, err := r.a.DB.Pool.Exec(ctx, `DELETE FROM supports_qag
        WHERE qag_id IN (
            SELECT id FROM qags
            WHERE status <> 7
            AND user_id IN `+inList(1, len(uids))+`
        )`, toArgs(uids)...)
	return err
}

// DeleteBannedUsersLastWeekSupportsOnUnselectedQags is
// deleteBannedUsersLastWeekSupportsOnUnselectedQags: the supports of banned
// users given during the last week (from the day one week ago, midnight) on QaGs
// that are not selected. It returns the number of deleted supports.
func (r *SupportRepository) DeleteBannedUsersLastWeekSupportsOnUnselectedQags(ctx context.Context) (int, error) {
	lastWeek := r.a.Now().AddDate(0, 0, -7)
	from := time.Date(lastWeek.Year(), lastWeek.Month(), lastWeek.Day(), 0, 0, 0, 0, time.Local)
	tag, err := r.a.DB.Pool.Exec(ctx, `DELETE FROM supports_qag
                WHERE qag_id NOT IN (SELECT id FROM qags WHERE status = 7)
                AND user_id IN (SELECT id FROM agora_users WHERE is_banned = 1)
                AND $1 < supports_qag.support_date
        `, from)
	r.info.EvictAll(ctx)
	return int(tag.RowsAffected()), err
}

// isQagSupportedByUser is `getUserSupportedQags(userId).any { it == qagId }` (the
// QaG details' isSupportedByUser for an open or accepted QaG) in one indexed query.
func (r *SupportRepository) isQagSupportedByUser(ctx context.Context, userID, qagID string) (bool, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return false, nil
	}
	qid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return false, nil
	}
	var supported bool
	err := r.a.DB.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM supports_qag
            WHERE user_id = $1 AND qag_id = $2
            AND qag_id IN (SELECT id FROM qags WHERE status = 1 OR (status = 0 AND user_id = $1)))`, uid, qid).Scan(&supported)
	return supported, err
}
