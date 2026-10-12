package notification

import (
	"context"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/modules/users"
)

// audience is the list of users a notification goes to (`userList` of the Kotlin use cases): the ids,
// in the order of the selection and possibly repeated, and the FCM tokens of those users, filtered
// (non-blank), de-duplicated and cut in batches of up to 500 (C-S8-FANOUT).
type audience struct {
	ids    [][16]byte
	tokens *tokenChunks
}

func newAudience() *audience { return &audience{tokens: newTokenChunks()} }

// userSelector holds the SQL of UserDatabaseRepository / UserRepositoryImpl that the notification use
// cases need, in a streaming form: Kotlin mapped every row to a UserInfo (one object and one list of
// authorizations per user); a notification only needs the id and the FCM token, and a storm
// ("notify all users") must not allocate more than that for each user.
type userSelector struct {
	a *app.App
}

// userRows streams the rows (id, fcm_token, is_banned[, answered]) of a query and applies
// UserInfoMapper.toDomain's failures: Hibernate cannot hydrate a NULL is_banned (primitive) and the
// UserInfo constructor refuses a NULL fcm_token, so a user with one of them makes the whole selection
// fail (HTTP 500) - for every user of the table when the query is findAll().
func (u *userSelector) userRows(ctx context.Context, sql string, args []any, keep func(answered bool) bool, withAnswered bool, out *audience) error {
	rows, err := u.a.DB.Pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id [16]byte
		var token *string
		var banned *int16
		var answered bool
		if withAnswered {
			err = rows.Scan(&id, &token, &banned, &answered)
		} else {
			err = rows.Scan(&id, &token, &banned)
		}
		if err != nil {
			return err
		}
		if banned == nil || token == nil {
			return users.ErrCorruptUser
		}
		if keep != nil && !keep(answered) {
			continue
		}
		out.ids = append(out.ids, id)
		out.tokens.add(*token)
	}
	return rows.Err()
}

// allUsers is UserRepositoryImpl.getAllUsers (findAll(): every user, banned or not, with or without
// token, in table order).
func (u *userSelector) allUsers(ctx context.Context) (*audience, error) {
	out := newAudience()
	err := u.userRows(ctx, "SELECT id, fcm_token, is_banned FROM agora_users", nil, nil, false, out)
	return out, err
}

// answeredUsers is `getAllUsers().filter { getUsersAnsweredConsultation(consultationId).contains(it.userId) }`
// of sendConsultationUpdateNotification, in one pass: every user of the table is mapped (so a corrupt
// row anywhere fails the request, like findAll) and the ones who answered are kept.
func (u *userSelector) answeredUsers(ctx context.Context, consultationID string) (*audience, error) {
	out := newAudience()
	err := u.userRows(ctx, `SELECT u.id, u.fcm_token, u.is_banned,
       EXISTS (SELECT 1 FROM user_answered_consultation a WHERE a.consultation_id = $1 AND a.user_id = u.id) AS answered
FROM agora_users u`, []any{consultationID}, func(answered bool) bool { return answered }, true, out)
	return out, err
}

// The three queries below are the native queries of UserDatabaseRepository, verbatim except for the
// select list of the outer query (id, fcm_token, is_banned instead of *): one user per distinct FCM
// token (the most recently created one; the blank token counts as a token too).

// notAnsweredUsers is UserRepositoryImpl.getUsersNotAnsweredConsultation.
func (u *userSelector) notAnsweredUsers(ctx context.Context, consultationID string) (*audience, error) {
	out := newAudience()
	err := u.userRows(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT id, fcm_token, is_banned FROM unique_fcm_token_users
                WHERE created_date_rank = 1
                AND id NOT IN (
                     SELECT user_id FROM user_answered_consultation
                     WHERE consultation_id = $1
                )
        `, []any{consultationID}, nil, false, out)
	return out, err
}

// livingIn is UserRepositoryImpl.getUsersLivingInDepartement.
func (u *userSelector) livingIn(ctx context.Context, departement *domain.Department, out *audience) error {
	code := domain.DepartmentCode(departement)
	if code == nil {
		return nil
	}
	return u.userRows(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT id, fcm_token, is_banned FROM unique_fcm_token_users
        WHERE created_date_rank = 1
        AND id IN (
             SELECT user_id FROM users_profile
             WHERE department = $1
        )
        `, []any{*code}, nil, false, out)
}

// interestedIn is UserRepositoryImpl.getUsersInterestedInDepartement.
func (u *userSelector) interestedIn(ctx context.Context, departement *domain.Department, out *audience) error {
	code := domain.DepartmentCode(departement)
	if code == nil {
		return nil
	}
	d := domain.DepartementFromCodePostal(*code)
	if d == nil {
		return nil
	}
	return u.userRows(ctx, `
        WITH unique_fcm_token_users AS (
            SELECT
                *,
                ROW_NUMBER() OVER (PARTITION BY fcm_token ORDER BY created_date DESC) AS created_date_rank
            FROM agora_users
        )

        SELECT id, fcm_token, is_banned FROM unique_fcm_token_users
        WHERE created_date_rank = 1
        AND id IN (
             SELECT user_id FROM users_profile
             WHERE primary_department = $1
                OR secondary_department = $1
        )
        `, []any{d.Value()}, nil, false, out)
}

// departementUsers is `getUsersLivingInDepartement(d) + getUsersInterestedInDepartement(d)`: a user
// living in and interested in the department is listed twice (two rows, one token).
func (u *userSelector) departementUsers(ctx context.Context, departement *domain.Department) (*audience, error) {
	out := newAudience()
	if err := u.livingIn(ctx, departement, out); err != nil {
		return nil, err
	}
	if err := u.interestedIn(ctx, departement, out); err != nil {
		return nil, err
	}
	return out, nil
}
