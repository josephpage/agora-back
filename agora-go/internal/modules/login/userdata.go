package login

import (
	"context"
	"strconv"
	"strings"
	"time"

	"agora/internal/app"
	"agora/internal/modules/users"
	"agora/internal/store"
)

// SignupHistoryCount is domain/SignupHistoryCount: Date is a calendar date
// (midnight UTC), the zero time stands for LocalDate.MIN.
type SignupHistoryCount struct {
	Date        time.Time
	SignupCount int
}

// UserDataRepository is usecase/login/repository/UserDataRepository.
type UserDataRepository interface {
	AddLoginData(ctx context.Context, req users.LoginRequest) error
	AddSignupData(ctx context.Context, req users.SignupRequest, generatedUserID string) error
	GetSignupHistory(ctx context.Context, ipAddressHash, userAgent string) ([]SignupHistoryCount, error)
}

// pgUserData is UserDataRepositoryImpl + UserDataDatabaseRepository
// (users_data; its columns are varchar(255): a longer value makes the insert
// fail like it does in Kotlin).
type pgUserData struct{ a *app.App }

const insertUserData = `INSERT INTO users_data (id, event_date, event_type, fcm_token, ip_address_hash, platform, user_agent, user_id, version_code, version_name)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

func (r *pgUserData) AddLoginData(ctx context.Context, req users.LoginRequest) error {
	_, err := r.a.DB.Pool.Exec(ctx, insertUserData, users.RandomUUID(), store.Millis(r.a.Now()), "login",
		req.FCMToken, req.IPAddressHash, req.Platform, req.UserAgent, req.UserID, req.VersionCode, req.VersionName)
	return err
}

func (r *pgUserData) AddSignupData(ctx context.Context, req users.SignupRequest, generatedUserID string) error {
	_, err := r.a.DB.Pool.Exec(ctx, insertUserData, users.RandomUUID(), store.Millis(r.a.Now()), "signup",
		req.FCMToken, req.IPAddressHash, req.Platform, req.UserAgent, generatedUserID, req.VersionCode, req.VersionName)
	return err
}

func (r *pgUserData) GetSignupHistory(ctx context.Context, ipAddressHash, userAgent string) ([]SignupHistoryCount, error) {
	rows, err := r.a.DB.Pool.Query(ctx, `SELECT DISTINCT DATE(event_date) as date, count(*) as signupCount
            FROM users_data
            WHERE ip_address_hash = $1
            AND user_agent = $2
            AND event_type = 'signup'
            GROUP BY ip_address_hash, DATE(event_date)`, ipAddressHash, userAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SignupHistoryCount
	for rows.Next() {
		var d time.Time
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out = append(out, SignupHistoryCount{Date: civil(d), SignupCount: int(n)})
	}
	return out, rows.Err()
}

// civil keeps the calendar date of t.
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// DeleteUsersData is UserDataRepository.deleteUsersData (`user_id IN :userIDs`;
// an empty list matches nothing).
func (r *pgUserData) DeleteUsersData(ctx context.Context, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	args := make([]any, len(userIDs))
	ph := make([]string, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	_, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM users_data WHERE user_id IN ("+strings.Join(ph, ",")+")", args...)
	return err
}

// FlagUsersWithSuspiciousActivity is UserDataRepository.flagUsersWithSuspiciousActivity.
func (r *pgUserData) FlagUsersWithSuspiciousActivity(ctx context.Context, softBanSignupCount int, startDate, endDate time.Time) (int, error) {
	tag, err := r.a.DB.Pool.Exec(ctx, `
        WITH suspiciousIpAndUserAgent AS (
            SELECT ip_address_hash, user_agent, count(*) AS signupCount FROM users_data
            WHERE event_type = 'signup'
            AND ip_address_hash != ''
            AND event_date > $2
            AND event_date < $3
            GROUP BY ip_address_hash, user_agent, DATE(event_date)
            HAVING count(*) >= $1
        ),
        suspiciousUserId AS (
            SELECT DISTINCT user_id FROM users_data
            WHERE CONCAT(ip_address_hash, user_agent) IN (SELECT CONCAT(ip_address_hash, user_agent) FROM suspiciousIpAndUserAgent)
            AND event_type = 'signup'
        )
        UPDATE agora_users
        SET is_banned = 1
        WHERE CAST(id AS TEXT) IN (SELECT user_id FROM suspiciousUserId)
        AND is_banned = 0
    `, softBanSignupCount, store.Millis(startDate), store.Millis(endDate))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
