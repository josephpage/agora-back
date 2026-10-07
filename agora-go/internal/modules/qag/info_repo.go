package qag

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// qagInfoCache is the L1 name of the shared QaG aggregate micro-cache (see
// InfoRepository.GetQagWithSupportCountCached).
const qagInfoCache = "qagInfo"

// Fragments of QagInfoDatabaseRepository.
const (
	qagWithSupportCountProjection = "qags.id as id, title, description, post_date as postDate, qags.status, username, thematique_id as thematiqueId, qags.user_id as userId, count(DISTINCT supports_qag.user_id) as supportCount"
	qagWithSupportJoin            = "qags LEFT JOIN supports_qag ON qags.id = supports_qag.qag_id"
)

// InfoRepository is QagInfoRepositoryImpl (+ QagInfoMapper, QagInfoDatabaseRepository).
// Every SQL statement is the Kotlin @Query verbatim; `IN :list` is expanded like
// Hibernate (an empty list matches nothing).
type InfoRepository struct{ a *app.App }

// ---------------------------------------------------------------------------
// Row scanning
// ---------------------------------------------------------------------------

// qagRow is QagDTO / QagWithSupportCountDTO. The columns are nullable in the
// database while the Kotlin properties are not: a NULL makes the mapper throw.
type qagRow struct {
	ID           string
	Title        *string
	Description  *string
	PostDate     *time.Time
	Status       int
	Username     *string
	ThematiqueID *string
	UserID       *string
	SupportCount int
	Moderated    *time.Time
}

func (r *qagRow) check() error {
	switch {
	case r.Title == nil:
		return errNullColumn("qags", "title")
	case r.Description == nil:
		return errNullColumn("qags", "description")
	case r.PostDate == nil:
		return errNullColumn("qags", "post_date")
	case r.Username == nil:
		return errNullColumn("qags", "username")
	case r.ThematiqueID == nil:
		return errNullColumn("qags", "thematique_id")
	case r.UserID == nil:
		return errNullColumn("qags", "user_id")
	}
	return nil
}

func (r *qagRow) info() (QagInfo, error) {
	if err := r.check(); err != nil {
		return QagInfo{}, err
	}
	st, err := statusFromDB(r.Status)
	if err != nil {
		return QagInfo{}, err
	}
	return QagInfo{
		ID: r.ID, ThematiqueID: *r.ThematiqueID, Title: *r.Title, Description: *r.Description,
		Date: store.Local(*r.PostDate), Status: st, Username: *r.Username, UserID: *r.UserID,
	}, nil
}

func (r *qagRow) infoWithSupportCount() (QagInfoWithSupportCount, error) {
	i, err := r.info()
	if err != nil {
		return QagInfoWithSupportCount{}, err
	}
	return QagInfoWithSupportCount{
		ID: i.ID, ThematiqueID: i.ThematiqueID, Title: i.Title, Description: i.Description, Date: i.Date,
		Status: i.Status, Username: i.Username, UserID: i.UserID, SupportCount: r.SupportCount,
		ModeratedDate: store.LocalPtr(r.Moderated),
	}, nil
}

// scanQags reads a `SELECT * FROM qags` result by column name (QagDTO).
func scanQags(rows pgx.Rows) ([]qagRow, error) {
	defer rows.Close()
	fields := rows.FieldDescriptions()
	var out []qagRow
	for rows.Next() {
		var r qagRow
		var status *int
		dest := make([]any, len(fields))
		for i, f := range fields {
			switch f.Name {
			case "id":
				dest[i] = &r.ID
			case "title":
				dest[i] = &r.Title
			case "description":
				dest[i] = &r.Description
			case "post_date":
				dest[i] = &r.PostDate
			case "status":
				dest[i] = &status
			case "username":
				dest[i] = &r.Username
			case "thematique_id":
				dest[i] = &r.ThematiqueID
			case "user_id":
				dest[i] = &r.UserID
			default:
				dest[i] = new(any)
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if status != nil {
			r.Status = *status
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanProjection reads the QagWithSupportCountDTO projection: the nine columns
// of qagWithSupportCountProjection, then (withModeration) moderatedDate, then
// any extra column (columnOrder of the UNION queries).
func scanProjection(rows pgx.Rows, withModeration bool) ([]qagRow, error) {
	defer rows.Close()
	n := len(rows.FieldDescriptions())
	var out []qagRow
	for rows.Next() {
		var r qagRow
		var count int64
		dest := []any{&r.ID, &r.Title, &r.Description, &r.PostDate, &r.Status, &r.Username, &r.ThematiqueID, &r.UserID, &count}
		if withModeration {
			dest = append(dest, &r.Moderated)
		}
		for len(dest) < n {
			dest = append(dest, new(any))
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		r.SupportCount = int(count)
		out = append(out, r)
	}
	return out, rows.Err()
}

func infosOf(rows []qagRow) ([]QagInfo, error) {
	out := make([]QagInfo, 0, len(rows))
	for i := range rows {
		x, err := rows[i].info()
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func infosWithSupportCountOf(rows []qagRow) ([]QagInfoWithSupportCount, error) {
	out := make([]QagInfoWithSupportCount, 0, len(rows))
	for i := range rows {
		x, err := rows[i].infoWithSupportCount()
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func (r *InfoRepository) queryQags(ctx context.Context, sql string, args ...any) ([]qagRow, error) {
	rows, err := r.a.DB.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return scanQags(rows)
}

func (r *InfoRepository) queryProjection(ctx context.Context, withModeration bool, sql string, args ...any) ([]QagInfoWithSupportCount, error) {
	rows, err := r.a.DB.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	list, err := scanProjection(rows, withModeration)
	if err != nil {
		return nil, err
	}
	return infosWithSupportCountOf(list)
}

// ---------------------------------------------------------------------------
// QagInfoRepository
// ---------------------------------------------------------------------------

// GetQagInfoToModerateList is getQagInfoToModerateList.
func (r *InfoRepository) GetQagInfoToModerateList(ctx context.Context) ([]QagInfo, error) {
	rows, err := r.queryQags(ctx, `SELECT * FROM qags
            WHERE status = 0
            AND id NOT IN (SELECT qag_id FROM moderatus_locked_qags)
            ORDER BY post_date ASC
            LIMIT 100
        `)
	if err != nil {
		return nil, err
	}
	return infosOf(rows)
}

// GetUserLastQagInfo is getUserLastQagInfo: the latest open or accepted QaG of the user.
func (r *InfoRepository) GetUserLastQagInfo(ctx context.Context, userID string) (*QagInfo, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil, nil
	}
	rows, err := r.queryQags(ctx, `SELECT * FROM qags
        WHERE (status = 0 OR status = 1)
        AND user_id = $1
        ORDER BY post_date DESC
        LIMIT 1
        `, uid)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	i, err := rows[0].info()
	return &i, err
}

// GetQagsSelectedForResponse is getQagsSelectedForResponse.
func (r *InfoRepository) GetQagsSelectedForResponse(ctx context.Context) ([]QagInfoWithSupportCount, error) {
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.status = 7
            GROUP BY qags.id
            ORDER BY postDate DESC
            LIMIT 100
        `)
}

// GetPopularQagsPaginatedV2 is getPopularQagsPaginatedV2.
func (r *InfoRepository) GetPopularQagsPaginatedV2(ctx context.Context, offset int, thematiqueID *string) ([]QagInfoWithSupportCount, error) {
	if thematiqueID != nil {
		return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.status = 1
            AND thematique_id = $2
            GROUP BY qags.id
            ORDER BY supportCount DESC
            LIMIT 20
            OFFSET $1
        `, offset, *thematiqueID)
	}
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.status = 1
            GROUP BY qags.id
            ORDER BY supportCount DESC
            LIMIT 20
            OFFSET $1
        `, offset)
}

// GetLatestQagsPaginatedV2 is getLatestQagsPaginatedV2.
func (r *InfoRepository) GetLatestQagsPaginatedV2(ctx context.Context, offset int, thematiqueID *string) ([]QagInfoWithSupportCount, error) {
	if thematiqueID != nil {
		return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.status = 1
            AND thematique_id = $2
            GROUP BY qags.id
            ORDER BY post_date DESC
            LIMIT 20
            OFFSET $1
        `, offset, *thematiqueID)
	}
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.status = 1
            GROUP BY qags.id
            ORDER BY post_date DESC
            LIMIT 20
            OFFSET $1
        `, offset)
}

// GetSupportedQagsPaginatedV2 is getSupportedQagsPaginatedV2 (an invalid user id gives an empty list).
func (r *InfoRepository) GetSupportedQagsPaginatedV2(ctx context.Context, userID string, offset int, thematiqueID *string) ([]QagInfoWithSupportCount, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return []QagInfoWithSupportCount{}, nil
	}
	if thematiqueID != nil {
		return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE (
                (qags.status = 1 AND qags.id IN (SELECT qag_id FROM supports_qag WHERE user_id = $1))
                OR ((qags.status = 0 OR qags.status = 1) AND qags.user_id = $1)
            )
            AND thematique_id = $3
            GROUP BY qags.id
            ORDER BY CASE WHEN qags.user_id = $1 THEN 0 ELSE 1 END, (SELECT support_date FROM supports_qag WHERE qag_id = qags.id AND user_id = $1 LIMIT 1) DESC
            LIMIT 20
            OFFSET $2
        `, uid, offset, *thematiqueID)
	}
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE (
                (qags.status = 1 AND qags.id IN (SELECT qag_id FROM supports_qag WHERE user_id = $1))
                OR ((qags.status = 0 OR qags.status = 1) AND qags.user_id = $1)
            )
            GROUP BY qags.id
            ORDER BY CASE WHEN qags.user_id = $1 THEN 0 ELSE 1 END, (SELECT support_date FROM supports_qag WHERE qag_id = qags.id AND user_id = $1 LIMIT 1) DESC
            LIMIT 20
            OFFSET $2
        `, uid, offset)
}

// GetQagsCount is getQagsCount (accepted QaGs, optionally of one thematique).
func (r *InfoRepository) GetQagsCount(ctx context.Context, thematiqueID *string) (int, error) {
	var n int64
	var err error
	if thematiqueID != nil {
		err = r.a.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM qags WHERE status = 1 AND thematique_id = $1", *thematiqueID).Scan(&n)
	} else {
		err = r.a.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM qags WHERE status = 1").Scan(&n)
	}
	return int(n), err
}

// getQagByUUID is QagInfoDatabaseRepository.getQagById (uuid already canonical).
func (r *InfoRepository) getQagByUUID(ctx context.Context, uid string) (*QagInfo, error) {
	rows, err := r.queryQags(ctx, "SELECT * from qags WHERE id = $1", uid)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	i, err := rows[0].info()
	return &i, err
}

// GetQagInfo is getQagInfo: nil when the id is not a UUID or the QaG does not exist.
func (r *InfoRepository) GetQagInfo(ctx context.Context, qagID string) (*QagInfo, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil, nil
	}
	return r.getQagByUUID(ctx, uid)
}

// GetQagsInfo is getQagsInfo (ids that are not UUIDs are dropped; no id, no row).
func (r *InfoRepository) GetQagsInfo(ctx context.Context, qagIDs []string) ([]QagInfo, error) {
	uids := uuidsOrNull(qagIDs)
	if len(uids) == 0 {
		return []QagInfo{}, nil
	}
	rows, err := r.queryQags(ctx, "SELECT * from qags WHERE id IN "+inList(1, len(uids)), toArgs(uids)...)
	if err != nil {
		return nil, err
	}
	return infosOf(rows)
}

// GetQagWithSupportCount is getQagWithSupportCount: the QaG and its number of
// distinct supporters (never cached; see GetQagWithSupportCountCached).
func (r *InfoRepository) GetQagWithSupportCount(ctx context.Context, qagID string) (*QagInfoWithSupportCount, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil, nil
	}
	list, err := r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE qags.id = $1
            GROUP BY qags.id
    `, uid)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// InsertQagInfo is insertQagInfo: Hibernate merges a detached entity with a
// preset id (SELECT, then INSERT with a freshly generated random UUID).
func (r *InfoRepository) InsertQagInfo(ctx context.Context, q QagInserting) (QagInsertionResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(q.UserID)
	if !ok {
		return QagInsertionResult{}, nil
	}
	id := randomUUID()
	_, err := r.a.DB.Pool.Exec(ctx,
		`INSERT INTO qags (id, description, motif_id, post_date, status, thematique_id, title, user_id, username) VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8)`,
		id, q.Description, store.Millis(q.Date), dbStatusOpen, q.ThematiqueID, q.Title, uid, q.Username)
	if err != nil {
		return QagInsertionResult{}, err
	}
	return QagInsertionResult{Info: &QagInfo{
		ID: id, ThematiqueID: q.ThematiqueID, Title: q.Title, Description: q.Description, Date: store.Millis(q.Date),
		Status: StatusOpen, Username: q.Username, UserID: uid,
	}}, nil
}

// UpdateQagMotifID is updateQagMotifId.
func (r *InfoRepository) UpdateQagMotifID(ctx context.Context, qagID string, motifID *string) error {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil
	}
	_, err := r.a.DB.Pool.Exec(ctx, "UPDATE qags SET motif_id = $2 WHERE id = $1", uid, motifID)
	return err
}

// UpdateQagStatus is updateQagStatus. The cached QaG aggregate is dropped.
func (r *InfoRepository) UpdateQagStatus(ctx context.Context, qagID string, newStatus QagStatus) (QagUpdateResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return QagUpdateResult{}, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, "UPDATE qags SET status = $2 WHERE id = $1", uid, StatusToDB(newStatus))
	r.Evict(ctx, uid)
	if err != nil {
		return QagUpdateResult{}, err
	}
	if tag.RowsAffected() <= 0 {
		return QagUpdateResult{}, nil
	}
	info, err := r.getQagByUUID(ctx, uid)
	return QagUpdateResult{Info: info}, err
}

// GetMostPopularQags is getMostPopularQags.
func (r *InfoRepository) GetMostPopularQags(ctx context.Context) ([]QagInfoWithSupportCount, error) {
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
            FROM `+qagWithSupportJoin+`
            WHERE status = 1
            GROUP BY qags.id
            HAVING count(*) = (
                SELECT max(supportCount)
                FROM (
                    SELECT count(*) as supportCount
                    FROM `+qagWithSupportJoin+`
                    WHERE status = 1
                    GROUP BY qags.id
                ) as supportCounts
            )
        `)
}

// removeDuplicates is the fold of QagInfoRepositoryImpl: the first occurrence of
// each (data class equal) element is kept.
func removeDuplicates(list []QagInfoWithSupportCount) []QagInfoWithSupportCount {
	out := make([]QagInfoWithSupportCount, 0, len(list))
outer:
	for _, q := range list {
		for _, o := range out {
			if o.equal(q) {
				continue outer
			}
		}
		out = append(out, q)
	}
	return out
}

// GetTrendingQags is getTrendingQags(interval): interval.inWholeHours, then duplicates removed.
func (r *InfoRepository) GetTrendingQags(ctx context.Context, interval time.Duration) ([]QagInfoWithSupportCount, error) {
	// `:interval || ' HOURS'` receives a Long (bigint) from JDBC.
	list, err := r.queryProjection(ctx, false, `
            (
                SELECT `+qagWithSupportCountProjection+`, 1 as columnOrder
                FROM `+qagWithSupportJoin+`
                WHERE qags.status = 1
                GROUP BY qags.id
                ORDER BY post_date DESC
                LIMIT 1
            )
            UNION
            (
                SELECT id, title, description, postDate, status, username, thematiqueId, userId, supportCount, 2 as columnOrder
                FROM (
                    SELECT `+qagWithSupportCountProjection+`, ROW_NUMBER() OVER (PARTITION BY thematique_id ORDER BY count(*) DESC) as thematiqueRowNumber
                    FROM (qags LEFT JOIN qag_updates ON qags.id = qag_updates.qag_id) LEFT JOIN supports_qag ON qags.id = supports_qag.qag_id
                    WHERE qags.status = 1
                    AND qag_updates.status = 1
                    AND moderated_date >= (CURRENT_TIMESTAMP - CAST(($1::bigint || ' HOURS') AS INTERVAL))
                    GROUP BY qags.id
                    ORDER BY supportCount DESC
                ) as rowNumber
                WHERE thematiqueRowNumber < 3
                LIMIT 20
            )
            ORDER BY columnOrder ASC, supportCount DESC
        `, int64(interval/time.Hour))
	return removeDuplicates(list), err
}

// GetTrendingQagsWithRecentLikes is getTrendingQagsWithRecentLikes.
func (r *InfoRepository) GetTrendingQagsWithRecentLikes(ctx context.Context, interval time.Duration, minLikes int) ([]QagInfoWithSupportCount, error) {
	list, err := r.queryProjection(ctx, false, `
            (
                SELECT `+qagWithSupportCountProjection+`, 1 as columnOrder
                FROM `+qagWithSupportJoin+`
                WHERE qags.status = 1
                GROUP BY qags.id
                ORDER BY post_date DESC
                LIMIT 1
            )
            UNION
            (
                SELECT `+qagWithSupportCountProjection+`, 2 as columnOrder
                FROM `+qagWithSupportJoin+`
                WHERE qags.status = 1
                GROUP BY qags.id
                HAVING count(
                    CASE WHEN supports_qag.support_date >= (CURRENT_TIMESTAMP - CAST(($1::bigint || ' HOURS') AS INTERVAL))
                         THEN 1 END
                ) > $2
                ORDER BY supportCount DESC
                LIMIT 20
            )
            ORDER BY columnOrder ASC, supportCount DESC
        `, int64(interval/time.Hour), minLikes)
	return removeDuplicates(list), err
}

// GetTrendingQagsV3 is getTrendingQagsV3 (moderatedDate is filled).
func (r *InfoRepository) GetTrendingQagsV3(ctx context.Context) ([]QagInfoWithSupportCount, error) {
	return r.queryProjection(ctx, true, `
            SELECT
                qags.id as id,
                title,
                description,
                post_date as postDate,
                qags.status,
                username,
                thematique_id as thematiqueId,
                qags.user_id as userId,
                count(DISTINCT supports_qag.user_id) as supportCount,
                qag_updates.moderated_date as moderatedDate
            FROM qags
                LEFT JOIN qag_updates ON qags.id = qag_updates.qag_id
                LEFT JOIN supports_qag ON qags.id = supports_qag.qag_id
            WHERE qags.status = 1
                AND qag_updates.status = 1
                AND qag_updates.moderated_date >= (CURRENT_TIMESTAMP - INTERVAL '7 days')
            GROUP BY qags.id, qag_updates.moderated_date
            ORDER BY qag_updates.moderated_date DESC
        `)
}

// SelectQagForResponse is selectQagForResponse.
func (r *InfoRepository) SelectQagForResponse(ctx context.Context, qagID string) (QagUpdateResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return QagUpdateResult{}, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, `UPDATE qags
        SET status = 7
        WHERE id = $1
        `, uid)
	r.Evict(ctx, uid)
	if err != nil {
		return QagUpdateResult{}, err
	}
	if tag.RowsAffected() <= 0 {
		return QagUpdateResult{}, nil
	}
	info, err := r.getQagByUUID(ctx, uid)
	return QagUpdateResult{Info: info}, err
}

// ArchiveOldQags is archiveOldQags: archive the accepted QaGs moderated before
// resetDate, then anonymize the rejected ones.
func (r *InfoRepository) ArchiveOldQags(ctx context.Context, resetDate time.Time) error {
	d := store.Millis(resetDate)
	_, err := r.a.DB.Pool.Exec(ctx, `UPDATE qags
            SET status = 2
            WHERE status = 1
            AND id IN (
                SELECT qag_id FROM qag_updates
                WHERE status = 1
                AND moderated_date < $1
            )`, d)
	r.EvictAll(ctx)
	if err != nil {
		return err
	}
	_, err = r.a.DB.Pool.Exec(ctx, `UPDATE qags
            SET username = '', user_id = '00000000-0000-0000-0000-000000000000'
            WHERE status = -1
            AND id IN (
                SELECT qag_id FROM qag_updates
                WHERE status = -1
                AND moderated_date < $1
            )`, d)
	r.EvictAll(ctx)
	return err
}

// AnonymizeOldQags is anonymizeOldQags.
func (r *InfoRepository) AnonymizeOldQags(ctx context.Context, date time.Time) error {
	_, err := r.a.DB.Pool.Exec(ctx, `UPDATE qags
            SET username = ''
            WHERE status = 2
            AND id IN (
                SELECT qag_id FROM qag_updates
                WHERE moderated_date < $1
            )`, store.Millis(date))
	r.EvictAll(ctx)
	return err
}

// DeleteQag is deleteQag: the QaG must exist; Failure when nothing was deleted.
func (r *InfoRepository) DeleteQag(ctx context.Context, qagID string) (QagDeleteResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return QagDeleteResult{}, nil
	}
	info, err := r.getQagByUUID(ctx, uid)
	if err != nil || info == nil {
		return QagDeleteResult{}, err
	}
	return r.deleteFound(ctx, uid, info)
}

// deleteFound is the second half of deleteQag, for callers that already read the QaG.
func (r *InfoRepository) deleteFound(ctx context.Context, uid string, info *QagInfo) (QagDeleteResult, error) {
	tag, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM qags WHERE id = $1", uid)
	r.Evict(ctx, uid)
	if err != nil {
		return QagDeleteResult{}, err
	}
	if tag.RowsAffected() <= 0 {
		return QagDeleteResult{}, nil
	}
	return QagDeleteResult{Info: info}, nil
}

// GetQagByKeywordsList is getQagByKeywordsList: every keyword must match the
// (unaccented) title or every keyword the description; accepted QaGs only.
func (r *InfoRepository) GetQagByKeywordsList(ctx context.Context, keywords []string) ([]QagInfoWithSupportCount, error) {
	args := make([]any, len(keywords))
	arr := "array["
	for i, k := range keywords {
		args[i] = "%" + k + "%"
		if i > 0 {
			arr += ","
		}
		arr += "$" + strconv.Itoa(i+1)
	}
	arr += "]"
	return r.queryProjection(ctx, false, `SELECT `+qagWithSupportCountProjection+`
        FROM `+qagWithSupportJoin+`
        WHERE qags.status = 1
        AND (unaccent(title) ILIKE ALL (`+arr+`) OR unaccent(description) ILIKE ALL (`+arr+`))
        GROUP BY (qags.id)
        ORDER BY supportCount DESC
        LIMIT 20
    `, args...)
}

// DeleteUsersQag is deleteUsersQag: every QaG of the users that was not selected for a response.
func (r *InfoRepository) DeleteUsersQag(ctx context.Context, userIDs []string) error {
	uids := uuidsOrNull(userIDs)
	if len(uids) == 0 {
		return nil
	}
	_, err := r.a.DB.Pool.Exec(ctx, `DELETE FROM qags
            WHERE user_id IN `+inList(1, len(uids))+`
            AND status <> 7`, toArgs(uids)...)
	r.EvictAll(ctx)
	return err
}

// ---------------------------------------------------------------------------
// Micro-cache of the shared QaG aggregate (QaG + number of supporters)
// ---------------------------------------------------------------------------

// GetQagWithSupportCountCached is getQagWithSupportCount shared for at most
// AGORA_MICROCACHE_TTL (conventions section 7: shared aggregate that Kotlin did
// not cache). Every write of this module and of the exported write methods above
// drops the entry (Evict), on every instance, so a user always sees the effect
// of their own support / unsupport / delete / insert at once; a change made by
// the Kotlin backend or SQL run elsewhere is seen within the TTL.
func (r *InfoRepository) GetQagWithSupportCountCached(ctx context.Context, qagID string) (*QagInfoWithSupportCount, error) {
	uid, ok := javacompat.ToUUIDOrNull(qagID)
	if !ok {
		return nil, nil
	}
	ttl := r.a.Cfg.MicroCacheTTL
	if ttl <= 0 {
		return r.GetQagWithSupportCount(ctx, uid)
	}
	return microLoad(r.a, qagInfoCache, uid, ttl, func() (*QagInfoWithSupportCount, bool, error) {
		v, err := r.GetQagWithSupportCount(context.WithoutCancel(ctx), uid)
		return v, true, err
	})
}

// Evict drops the cached aggregate of QaGs (canonical UUIDs) on every instance.
func (r *InfoRepository) Evict(ctx context.Context, qagUUIDs ...string) {
	for _, id := range qagUUIDs {
		r.a.Cache.Invalidate(ctx, qagInfoCache, id)
	}
}

// EvictAll drops every cached aggregate (bulk writes: archiving, anonymization, user deletion).
func (r *InfoRepository) EvictAll(ctx context.Context) { r.a.Cache.InvalidateAll(ctx, qagInfoCache) }
