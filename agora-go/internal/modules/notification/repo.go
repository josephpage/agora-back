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
	"agora/internal/javacompat"
	"agora/internal/store"
)

// maxPageListSize is NotificationPaginatedUseCase.MAX_PAGE_LIST_SIZE.
const maxPageListSize = 5

// insertBatchSize bounds the size of one INSERT ... SELECT unnest(...) statement (and of its
// array parameter); every batch of a notification runs in the same transaction.
const insertBatchSize = 50_000

// Repository is NotificationRepositoryImpl (+ NotificationDatabaseRepository, NotificationMapper).
//
// The Kotlin repository cached each user's whole list in Redis ("notificationCache", 5 minutes) and
// never evicted it, so a notification only showed up after the entry expired (class B, see
// parity/divergences/S8.md). Go reads the indexed table (notifications(user_id)) at each request,
// one page at a time.
type Repository struct {
	a *app.App
}

func newRepository(a *app.App) *Repository { return &Repository{a: a} }

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

// GetNotificationPaginated is NotificationPaginatedUseCase.getNotificationPaginated: nil (HTTP 400)
// when the page is not positive or beyond the end, an error (HTTP 500) where Kotlin throws.
//
// Kotlin loaded the user's whole list (findAllByUserId, no ORDER BY), sorted it by descending date
// (stable) and cut a sublist. Go asks the database for the page only, ordered by date DESC then by
// physical position (the order of the unordered Kotlin query, which the stable sort keeps for equal
// dates), with the size of the list and a check that every row of the user is well formed. A user
// with a malformed row (NULL column, unreadable type) falls back to the exact Kotlin computation on
// the whole list.
func (r *Repository) GetNotificationPaginated(ctx context.Context, userID string, pageNumber int) (*Page, error) {
	if pageNumber <= 0 {
		return nil, nil
	}
	// Int arithmetic of the Kotlin code (32 bits, wrapping)
	p := int32(pageNumber)
	minIndex := (p - 1) * maxPageListSize
	maxCandidate := p * maxPageListSize

	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		// getUserNotificationList: an id that is not a UUID gives an empty list
		return cutPage(nil, 0, minIndex, maxCandidate)
	}
	if minIndex < 0 {
		// subList(fromIndex < 0, ...): IndexOutOfBoundsException, whatever the list is
		return nil, fmt.Errorf("%w: IndexOutOfBoundsException fromIndex=%d", errJavaException, minIndex)
	}
	rows, total, clean, err := r.pageRows(ctx, uid, int64(minIndex))
	if err != nil {
		return nil, err
	}
	if !clean {
		return r.pageFromWholeList(ctx, uid, minIndex, maxCandidate)
	}
	return cutPage(rows, total, minIndex, maxCandidate)
}

// cutPage applies the end of getNotificationPaginated to the rows [minIndex, minIndex+5) of a list of
// `total` notifications ordered by descending date.
func cutPage(rows []Notification, total int, minIndex, maxCandidate int32) (*Page, error) {
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
	n := int(maxIndex - minIndex)
	if n > len(rows) {
		n = len(rows)
	}
	out := make([]Notification, n)
	copy(out, rows[:n])
	return &Page{Notifications: out, HasMoreNotifications: hasMore}, nil
}

const cleanExpr = `title IS NOT NULL AND description IS NOT NULL AND date IS NOT NULL AND coalesce(type ~ '^[0-5]$', false)`

// pageRows reads the page starting at offset and the figures about the whole list of the user.
func (r *Repository) pageRows(ctx context.Context, uid string, offset int64) (rows []Notification, total int, clean bool, err error) {
	q, err := r.a.DB.Pool.Query(ctx, `SELECT title, description, type, date,
       count(*) OVER () AS total,
       bool_and(`+cleanExpr+`) OVER () AS clean
FROM notifications
WHERE user_id = $1
ORDER BY date DESC, ctid
LIMIT `+strconv.Itoa(maxPageListSize)+` OFFSET $2`, uid, offset)
	if err != nil {
		return nil, 0, false, err
	}
	defer q.Close()
	clean = true
	for q.Next() {
		var title, description, typ *string
		var date *time.Time
		var tot int64
		var cl *bool
		if err := q.Scan(&title, &description, &typ, &date, &tot, &cl); err != nil {
			return nil, 0, false, err
		}
		total = int(tot)
		if cl == nil || !*cl {
			clean = false
			continue
		}
		t, _ := parseStoredType(*typ)
		rows = append(rows, Notification{Title: *title, Description: *description, Type: t, Date: store.Local(*date)})
	}
	if err := q.Err(); err != nil {
		return nil, 0, false, err
	}
	q.Close()
	if len(rows) == 0 && clean {
		// an empty page (the end of the list, or no notification at all): the size is still needed
		var tot int64
		var cl *bool
		err := r.a.DB.Pool.QueryRow(ctx, `SELECT count(*), coalesce(bool_and(`+cleanExpr+`), true) FROM notifications WHERE user_id = $1`, uid).Scan(&tot, &cl)
		if err != nil {
			return nil, 0, false, err
		}
		return nil, int(tot), cl == nil || *cl, nil
	}
	return rows, total, clean, nil
}

// pageFromWholeList is the exact Kotlin computation for a user with a malformed row: the whole
// list is read (getUserNotificationList), every row is mapped (a NULL column or an unreadable type
// is an exception for the whole request), then sorted and cut.
func (r *Repository) pageFromWholeList(ctx context.Context, uid string, minIndex, maxCandidate int32) (*Page, error) {
	q, err := r.a.DB.Pool.Query(ctx, `SELECT title, description, type, date FROM notifications WHERE user_id = $1 ORDER BY ctid`, uid)
	if err != nil {
		return nil, err
	}
	defer q.Close()
	var list []Notification
	for q.Next() {
		var title, description, typ *string
		var date *time.Time
		if err := q.Scan(&title, &description, &typ, &date); err != nil {
			return nil, err
		}
		if typ == nil {
			return nil, fmt.Errorf("%w: NullPointerException (notification type)", errJavaException)
		}
		t, ok := parseStoredType(*typ)
		if !ok {
			return nil, fmt.Errorf("%w: unreadable notification type %q", errJavaException, *typ)
		}
		if title == nil || description == nil || date == nil {
			return nil, fmt.Errorf("%w: NullPointerException (notification column)", errJavaException)
		}
		list = append(list, Notification{Title: *title, Description: *description, Type: t, Date: store.Local(*date)})
	}
	if err := q.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Date.After(list[j].Date) })
	if int(minIndex) > len(list) {
		return nil, nil
	}
	if minIndex < 0 {
		return nil, fmt.Errorf("%w: IndexOutOfBoundsException fromIndex=%d", errJavaException, minIndex)
	}
	return cutPage(list[min(int(minIndex), len(list)):], len(list), minIndex, maxCandidate)
}
