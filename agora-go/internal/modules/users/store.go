package users

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/store"
)

// userRow is UserDTO (agora_users). The Kotlin fields are non-null but the
// columns are nullable: NULLs are kept as pointers so that the callers can
// reproduce what Hibernate/Jackson/Kotlin do with them.
type userRow struct {
	ID                 string
	Password           *string
	FCMToken           *string
	CreatedDate        *time.Time
	AuthorizationLevel int
	IsBanned           *int
	LastConnectionDate *time.Time
}

// dataStore is the SQL side of UserRepositoryImpl (UserDatabaseRepository);
// it is an interface so that the repository logic can be unit tested.
type dataStore interface {
	// getUserById is `SELECT * FROM agora_users WHERE id = :userId LIMIT 1`.
	getUserByID(ctx context.Context, id string) (*userRow, error)
	// findAll is JpaRepository.findAll().
	findAll(ctx context.Context) ([]*userRow, error)
	// insertUser is SimpleJpaRepository.save() of a new UserDTO (merge of a
	// detached entity whose id does not exist → INSERT).
	insertUser(ctx context.Context, row *userRow) error
	// updateLogin is the UPDATE issued by save(dto.copy(fcmToken, lastConnectionDate)).
	updateLogin(ctx context.Context, id, fcmToken string, lastConnection time.Time) error
	usersNotAnsweredConsultation(ctx context.Context, consultationID string) ([]*userRow, error)
	usersLivingInDepartement(ctx context.Context, code string) ([]*userRow, error)
	usersInterestedInDepartement(ctx context.Context, departement string) ([]*userRow, error)
	deleteUsers(ctx context.Context, ids []string) error
	updateAuthorizationLevel(ctx context.Context, ids []string, level int) (int, error)
}

type pgStore struct{ db *store.DB }

// scanUsers hydrates UserDTOs from a `SELECT *` result by column name (extra
// columns such as created_date_rank are ignored, like Hibernate does).
func scanUsers(rows pgx.Rows) ([]*userRow, error) {
	defer rows.Close()
	fields := rows.FieldDescriptions()
	var out []*userRow
	for rows.Next() {
		var r userRow
		var level *int
		dest := make([]any, len(fields))
		for i, f := range fields {
			switch f.Name {
			case "id":
				dest[i] = &r.ID
			case "password":
				dest[i] = &r.Password
			case "fcm_token":
				dest[i] = &r.FCMToken
			case "created_date":
				dest[i] = &r.CreatedDate
			case "authorization_level":
				dest[i] = &level
			case "is_banned":
				dest[i] = &r.IsBanned
			case "last_connection_date":
				dest[i] = &r.LastConnectionDate
			default:
				dest[i] = new(any)
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if level != nil {
			r.AuthorizationLevel = *level
		}
		r.CreatedDate = store.LocalPtr(r.CreatedDate)
		r.LastConnectionDate = store.LocalPtr(r.LastConnectionDate)
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (p *pgStore) queryUsers(ctx context.Context, sql string, args ...any) ([]*userRow, error) {
	rows, err := p.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return scanUsers(rows)
}

func (p *pgStore) getUserByID(ctx context.Context, id string) (*userRow, error) {
	list, err := p.queryUsers(ctx, "SELECT * FROM agora_users WHERE id = $1 LIMIT 1", id)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

func (p *pgStore) findAll(ctx context.Context) ([]*userRow, error) {
	return p.queryUsers(ctx, "SELECT * FROM agora_users")
}

func (p *pgStore) insertUser(ctx context.Context, row *userRow) error {
	_, err := p.db.Pool.Exec(ctx,
		"INSERT INTO agora_users (id, password, fcm_token, created_date, authorization_level, is_banned, last_connection_date) VALUES ($1,$2,$3,$4,$5,$6,$7)",
		row.ID, row.Password, row.FCMToken, row.CreatedDate, row.AuthorizationLevel, row.IsBanned, row.LastConnectionDate)
	return err
}

func (p *pgStore) updateLogin(ctx context.Context, id, fcmToken string, lastConnection time.Time) error {
	_, err := p.db.Pool.Exec(ctx, "UPDATE agora_users SET fcm_token = $2, last_connection_date = $3 WHERE id = $1", id, fcmToken, lastConnection)
	return err
}

// The three "unique fcm token" queries are copied verbatim from
// UserDatabaseRepository.

func (p *pgStore) usersNotAnsweredConsultation(ctx context.Context, consultationID string) ([]*userRow, error) {
	return p.queryUsers(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT * FROM unique_fcm_token_users
                WHERE created_date_rank = 1
                AND id NOT IN (
                     SELECT user_id FROM user_answered_consultation
                     WHERE consultation_id = $1
                )
        `, consultationID)
}

func (p *pgStore) usersLivingInDepartement(ctx context.Context, code string) ([]*userRow, error) {
	return p.queryUsers(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT * FROM unique_fcm_token_users
        WHERE created_date_rank = 1
        AND id IN (
             SELECT user_id FROM users_profile
             WHERE department = $1
        )
        `, code)
}

func (p *pgStore) usersInterestedInDepartement(ctx context.Context, departement string) ([]*userRow, error) {
	return p.queryUsers(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT * FROM unique_fcm_token_users
        WHERE created_date_rank = 1
        AND id IN (
             SELECT user_id FROM users_profile
             WHERE primary_department = $1
                OR secondary_department = $1
        )
        `, departement)
}

// inList builds the Hibernate expansion of `IN :ids` → ($2,$3,…) starting at
// placeholder first.
func inList(first, n int) string {
	var b strings.Builder
	b.WriteByte('(')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(first + i))
	}
	b.WriteByte(')')
	return b.String()
}

func (p *pgStore) deleteUsers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		// Hibernate 6 expands an empty `IN :list` so that nothing matches
		// (checked on the reference: no error, 0 rows).
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := p.db.Pool.Exec(ctx, "DELETE FROM agora_users WHERE id IN "+inList(1, len(ids)), args...)
	return err
}

func (p *pgStore) updateAuthorizationLevel(ctx context.Context, ids []string, level int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := []any{level}
	for _, id := range ids {
		args = append(args, id)
	}
	tag, err := p.db.Pool.Exec(ctx, `UPDATE agora_users
            SET authorization_level = $1
            WHERE id IN `+inList(2, len(ids))+`
            `, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
