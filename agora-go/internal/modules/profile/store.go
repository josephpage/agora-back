package profile

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/store"
)

// profileRow is ProfileDTO (users_profile).
type profileRow struct {
	ID                     string
	Gender                 *string
	YearOfBirth            *int
	Department             *string
	CityType               *string
	JobCategory            *string
	VoteFrequency          *string
	PublicMeetingFrequency *string
	ConsultationFrequency  *string
	UserID                 string
	PrimaryDepartment      *string
	SecondaryDepartment    *string
}

// askDateRow is DemographicInfoAskDateDTO (demographic_info_ask_date).
type askDateRow struct {
	ID      string
	AskDate *time.Time // NULL column, non-null Kotlin Date
	UserID  string
}

// profileDB is ProfileDatabaseRepository.
type profileDB interface {
	getProfile(ctx context.Context, userUUID string) (*profileRow, error)
	// save is SimpleJpaRepository.save: merge by id (insert or update).
	save(ctx context.Context, row *profileRow) (*profileRow, error)
	deleteUsersProfile(ctx context.Context, userUUIDs []string) error
	insertDepartments(ctx context.Context, userID string, primary, secondary *string) error
}

// askDateDB is DemographicInfoAskDateDatabaseRepository.
type askDateDB interface {
	getAskDate(ctx context.Context, userUUID string) (*askDateRow, error)
	save(ctx context.Context, row *askDateRow) (*askDateRow, error)
	deleteAskDate(ctx context.Context, userUUID string) error
}

type pgProfile struct{ a *app.App }

func (p *pgProfile) getProfile(ctx context.Context, userUUID string) (*profileRow, error) {
	rows, err := p.a.DB.Pool.Query(ctx, "SELECT * FROM users_profile WHERE user_id = $1 LIMIT 1", userUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var r profileRow
	var id, uid any
	dest := make([]any, len(fields))
	for i, f := range fields {
		switch f.Name {
		case "id":
			dest[i] = &id
		case "gender":
			dest[i] = &r.Gender
		case "year_of_birth":
			dest[i] = &r.YearOfBirth
		case "department":
			dest[i] = &r.Department
		case "city_type":
			dest[i] = &r.CityType
		case "job_category":
			dest[i] = &r.JobCategory
		case "vote_frequency":
			dest[i] = &r.VoteFrequency
		case "public_meeting_frequency":
			dest[i] = &r.PublicMeetingFrequency
		case "consultation_frequency":
			dest[i] = &r.ConsultationFrequency
		case "user_id":
			dest[i] = &uid
		case "primary_department":
			dest[i] = &r.PrimaryDepartment
		case "secondary_department":
			dest[i] = &r.SecondaryDepartment
		default:
			dest[i] = new(any)
		}
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	r.ID = uuidString(id)
	r.UserID = uuidString(uid)
	return &r, rows.Err()
}

// uuidString renders a uuid column scanned into an `any` ([16]byte).
func uuidString(v any) string {
	b, ok := v.([16]byte)
	if !ok {
		return ""
	}
	h := "0123456789abcdef"
	var out [36]byte
	j := 0
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}
		out[j] = h[c>>4]
		out[j+1] = h[c&15]
		j += 2
	}
	return string(out[:])
}

func (p *pgProfile) save(ctx context.Context, row *profileRow) (*profileRow, error) {
	_, err := p.a.DB.Pool.Exec(ctx, `INSERT INTO users_profile (id, city_type, consultation_frequency, department, gender, job_category,
		primary_department, public_meeting_frequency, secondary_department, user_id, vote_frequency, year_of_birth)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET city_type = EXCLUDED.city_type, consultation_frequency = EXCLUDED.consultation_frequency,
		department = EXCLUDED.department, gender = EXCLUDED.gender, job_category = EXCLUDED.job_category,
		primary_department = EXCLUDED.primary_department, public_meeting_frequency = EXCLUDED.public_meeting_frequency,
		secondary_department = EXCLUDED.secondary_department, user_id = EXCLUDED.user_id,
		vote_frequency = EXCLUDED.vote_frequency, year_of_birth = EXCLUDED.year_of_birth`,
		row.ID, row.CityType, row.ConsultationFrequency, row.Department, row.Gender, row.JobCategory,
		row.PrimaryDepartment, row.PublicMeetingFrequency, row.SecondaryDepartment, row.UserID, row.VoteFrequency, row.YearOfBirth)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (p *pgProfile) deleteUsersProfile(ctx context.Context, userUUIDs []string) error {
	if len(userUUIDs) == 0 {
		return nil // Hibernate's empty IN list matches nothing
	}
	args := make([]any, len(userUUIDs))
	ph := make([]string, len(userUUIDs))
	for i, id := range userUUIDs {
		args[i] = id
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	_, err := p.a.DB.Pool.Exec(ctx, "DELETE FROM users_profile WHERE user_id IN ("+strings.Join(ph, ",")+")", args...)
	return err
}

func (p *pgProfile) insertDepartments(ctx context.Context, userID string, primary, secondary *string) error {
	_, err := p.a.DB.Pool.Exec(ctx, `
            INSERT INTO users_profile (user_id, primary_department, secondary_department)
            VALUES ($1, $2, $3)
            ON CONFLICT (user_id) DO UPDATE
                SET primary_department = $2,
                    secondary_department = $3
        `, userID, primary, secondary)
	return err
}

type pgAskDate struct{ a *app.App }

func (p *pgAskDate) getAskDate(ctx context.Context, userUUID string) (*askDateRow, error) {
	var id, uid any
	var askDate *time.Time
	err := p.a.DB.Pool.QueryRow(ctx, "SELECT id, ask_date, user_id FROM demographic_info_ask_date WHERE user_id = $1 LIMIT 1", userUUID).Scan(&id, &askDate, &uid)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &askDateRow{ID: uuidString(id), AskDate: store.LocalPtr(askDate), UserID: uuidString(uid)}, nil
}

func (p *pgAskDate) save(ctx context.Context, row *askDateRow) (*askDateRow, error) {
	_, err := p.a.DB.Pool.Exec(ctx, `INSERT INTO demographic_info_ask_date (id, ask_date, user_id) VALUES ($1,$2,$3)
		ON CONFLICT (id) DO UPDATE SET ask_date = EXCLUDED.ask_date, user_id = EXCLUDED.user_id`, row.ID, row.AskDate, row.UserID)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (p *pgAskDate) deleteAskDate(ctx context.Context, userUUID string) error {
	_, err := p.a.DB.Pool.Exec(ctx, "DELETE FROM demographic_info_ask_date WHERE user_id = $1", userUUID)
	return err
}
