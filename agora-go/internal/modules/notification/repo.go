package notification

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// maxPageListSize is NotificationPaginatedUseCase.MAX_PAGE_LIST_SIZE.
const maxPageListSize = 5

// insertBatchSize bounds the size of one INSERT ... SELECT unnest(...) statement (and of its
// array parameter); every batch of a notification runs in the same transaction.
const insertBatchSize = 50_000

// The Kotlin cache of the list of a user (NotificationCacheRepository): "notificationCache" in the
// shortTermCacheManager (5 minutes), key "notificationCacheList.<userId>". Nothing evicts it: a
// notification sent to a user shows up in the list once the entry has expired.
const (
	notificationCacheName = "notificationCache"
	notificationCacheKey  = "notificationCacheList."
	notificationCacheTTL  = 5 * time.Minute
)

// Repository is NotificationRepositoryImpl (+ NotificationDatabaseRepository, NotificationMapper,
// NotificationCacheRepository).
type Repository struct {
	a *app.App
	// findAll is NotificationDatabaseRepository.findAllByUserId (replaced in the unit tests).
	findAll func(ctx context.Context, uid string) ([]row, error)
}

func newRepository(a *app.App) *Repository {
	r := &Repository{a: a}
	r.findAll = r.loadRows
	return r
}

// InsertNotifications is insertNotifications(NotificationInserting): one row per element of
// userIDs (a user listed twice gets two rows; an id that is not a UUID is dropped, mapNotNull).
func (r *Repository) InsertNotifications(ctx context.Context, title, description string, typ TypeNotification, userIDs []string) error {
	ids := make([][16]byte, 0, len(userIDs))
	for _, id := range userIDs {
		if u, ok := javacompat.ParseUUID(id); ok {
			ids = append(ids, u.Bytes())
		}
	}
	return r.insertIDs(ctx, title, description, typ, ids)
}

// insertIDs writes the rows with set-based statements in one transaction (Kotlin: saveAll, one
// transaction, one SELECT + one INSERT per row). The date is the instant of the call, truncated to the
// millisecond (java.util.Date), shared by every row; the type column holds the ordinal as text.
func (r *Repository) insertIDs(ctx context.Context, title, description string, typ TypeNotification, ids [][16]byte) error {
	if len(ids) == 0 {
		r.a.Log.Warn("insertNotifications - pas de notifications à insérer")
		return nil
	}
	now := store.Millis(r.a.Now())
	ordinal := strconv.Itoa(typ.Ordinal())
	return r.a.DB.Tx(ctx, func(tx pgx.Tx) error {
		for start := 0; start < len(ids); start += insertBatchSize {
			end := start + insertBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			_, err := tx.Exec(ctx, `INSERT INTO notifications (id, date, description, title, type, user_id)
SELECT gen_random_uuid(), $1::timestamp, $2::text, $3::text, $4::text, u FROM unnest($5::uuid[]) AS u`,
				now, description, title, ordinal, ids[start:end])
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// maxJDBCParameters is the largest number of bind parameters pgjdbc sends in one statement.
const maxJDBCParameters = 65535

// DeleteUsersNotifications is deleteUsersNotifications (DeleteUsersUseCase): the ids that are not UUIDs
// are dropped; an empty list deletes nothing.
func (r *Repository) DeleteUsersNotifications(ctx context.Context, userIDs []string) error {
	ids := make([][16]byte, 0, len(userIDs))
	for _, id := range userIDs {
		if u, ok := javacompat.ParseUUID(id); ok {
			ids = append(ids, u.Bytes())
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxJDBCParameters {
		// Hibernate binds one parameter per id of the IN list and pgjdbc 42.5.4 refuses more
		// than 65535 (PGStream.sendInteger2): the first step of the user deletion fails, so
		// nothing is deleted (the later steps never run).
		return errJavaException
	}
	_, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM notifications WHERE user_id = ANY($1::uuid[])", ids)
	return err
}

// Page is NotificationListAndHasMoreNotificationsFlag.
type Page struct {
	Notifications        []Notification
	HasMoreNotifications bool
}

// errJavaException stands for an exception Kotlin does not catch (HTTP 500).
var errJavaException = errors.New("uncaught exception")

// row is a NotificationDTO as Hibernate hydrates it: the nullable columns stay nil. Kotlin fails only
// when the mapper builds the domain Notification (non-null constructor parameters), after the rows
// have been cached.
type row struct {
	title       *string
	description *string
	typ         *TypeNotification
	date        *time.Time
}

// GetNotificationPaginated is NotificationPaginatedUseCase.getNotificationPaginated: nil (HTTP 400)
// when the page is not positive or beyond the end, an error (HTTP 500) where Kotlin throws.
func (r *Repository) GetNotificationPaginated(ctx context.Context, userID string, pageNumber int) (*Page, error) {
	if pageNumber <= 0 {
		return nil, nil
	}
	rows, err := r.userRows(ctx, userID)
	if err != nil {
		return nil, err
	}
	list, err := toNotifications(rows)
	if err != nil {
		return nil, err
	}
	// Int arithmetic of the Kotlin code (32 bits, wrapping)
	p := int32(pageNumber)
	minIndex := (p - 1) * maxPageListSize
	maxCandidate := p * maxPageListSize
	// sortedByDescending is stable: equal dates keep the order of the cached rows
	sort.SliceStable(list, func(i, j int) bool { return list[i].Date.After(list[j].Date) })
	return cutPage(list, minIndex, maxCandidate)
}

// cutPage applies the end of getNotificationPaginated to the sorted list: `subList(minIndex, maxIndex)`
// with the Kotlin exceptions.
func cutPage(list []Notification, minIndex, maxCandidate int32) (*Page, error) {
	total := len(list)
	if int(minIndex) > total {
		return nil, nil
	}
	maxIndex := maxCandidate
	if int32(total) < maxIndex {
		maxIndex = int32(total)
	}
	hasMore := int(maxIndex) < total
	if minIndex < 0 {
		return nil, fmt.Errorf("%w: IndexOutOfBoundsException fromIndex=%d", errJavaException, minIndex)
	}
	if minIndex > maxIndex {
		return nil, fmt.Errorf("%w: IllegalArgumentException fromIndex(%d) > toIndex(%d)", errJavaException, minIndex, maxIndex)
	}
	out := make([]Notification, maxIndex-minIndex)
	copy(out, list[minIndex:maxIndex])
	return &Page{Notifications: out, HasMoreNotifications: hasMore}, nil
}

// toNotifications is NotificationMapper.toDomain over every row (a NULL column is an exception for the
// whole request, even when the requested page does not contain the row).
func toNotifications(rows []row) ([]Notification, error) {
	out := make([]Notification, 0, len(rows))
	for _, r := range rows {
		if r.title == nil || r.description == nil || r.typ == nil || r.date == nil {
			return nil, fmt.Errorf("%w: NullPointerException (notification column)", errJavaException)
		}
		out = append(out, Notification{Title: *r.title, Description: *r.description, Type: *r.typ, Date: store.Local(*r.date)})
	}
	return out, nil
}

// userRows is getUserNotificationList: an id that is not a UUID gives an empty list (not cached); else the
// cached rows of the user, or the rows of the database, which are cached even when they cannot be mapped.
func (r *Repository) userRows(ctx context.Context, userID string) ([]row, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		r.a.Log.Error(fmt.Sprintf("getUserNotificationList - impossible de convertir l'id '%s' en UUID", userID))
		return nil, nil
	}
	return cache.GetOrLoad(r.a.Cache, notificationCacheName, notificationCacheKey+userID,
		r.a.Cache.CoexistenceTTL(notificationCacheTTL), func() ([]row, error) {
			return r.findAll(ctx, uid)
		})
}

// loadRows is findAllByUserId (no ORDER BY: the physical order, which the stable sort keeps for equal
// dates). A type that Hibernate cannot read fails the load, which is then not cached.
func (r *Repository) loadRows(ctx context.Context, uid string) ([]row, error) {
	q, err := r.a.DB.Pool.Query(ctx, `SELECT title, description, type, date FROM notifications WHERE user_id = $1 ORDER BY ctid`, uid)
	if err != nil {
		return nil, err
	}
	defer q.Close()
	var rows []row
	for q.Next() {
		var title, description, typ *string
		var date *time.Time
		if err := q.Scan(&title, &description, &typ, &date); err != nil {
			return nil, err
		}
		var t *TypeNotification
		if typ != nil {
			v, ok := parseStoredType(*typ)
			if !ok {
				return nil, fmt.Errorf("%w: unreadable notification type %q", errJavaException, *typ)
			}
			t = &v
		}
		rows = append(rows, row{title: title, description: description, typ: t, date: date})
	}
	if err := q.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}
