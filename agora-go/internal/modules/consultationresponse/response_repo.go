package consultationresponse

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/javacompat"
)

// ResponseQueries is GetConsultationResponseRepositoryImpl + ReponseConsultationDatabaseRepository
// (the SQL is copied verbatim from the @Query strings) + ReponseConsultationMapper.
type ResponseQueries struct{ a *app.App }

// GetConsultationResponsesCount is getConsultationResponsesCount(consultationId): the number of answers
// per (question, choice) of the consultation.
func (r *ResponseQueries) GetConsultationResponsesCount(ctx context.Context, consultationID string) ([]ResponseConsultationCount, error) {
	rows, err := r.a.DB.Pool.Query(ctx, `SELECT question_id as questionId, choice_id as choiceId, count(*) as responseCount
            FROM reponses_consultation
            WHERE consultation_id = $1
            AND choice_id IS NOT NULL
            GROUP BY (question_id, choice_id)
            `, consultationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResponseConsultationCount{}
	for rows.Next() {
		var q, c string
		var n int64
		if err := rows.Scan(&q, &c, &n); err != nil {
			return nil, err
		}
		out = append(out, ResponseConsultationCount{QuestionID: q, ChoiceID: c, ResponseCount: int(n)})
	}
	return out, rows.Err()
}

// DeleteUserConsultationResponses is deleteUserConsultationResponses(userIDs): the ids that are not UUIDs
// are dropped (mapNotNull toUuidOrNull). An empty list is `IN ()`, a syntax error in
// PostgreSQL (Hibernate 6 expands an empty collection to nothing): an error, like Kotlin.
func (r *ResponseQueries) DeleteUserConsultationResponses(ctx context.Context, userIDs []string) error {
	var args []any
	var ph []string
	for _, id := range userIDs {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			args = append(args, u)
			ph = append(ph, "$"+strconv.Itoa(len(args)))
		}
	}
	if len(args) == 0 {
		return errors.New("org.postgresql.util.PSQLException: syntax error at or near \")\" (DELETE FROM reponses_consultation WHERE user_id IN ())")
	}
	_, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM reponses_consultation WHERE user_id IN ("+strings.Join(ph, ",")+")", args...)
	return err
}

// ---------------------------------------------------------------------------
// The demographic information of the participants
// ---------------------------------------------------------------------------

// demographicColumns are the attributes of users_profile, in the order of
// DemographicInfoCount.
var demographicColumns = [numDims]string{"gender", "year_of_birth", "department", "city_type", "job_category", "vote_frequency", "public_meeting_frequency", "consultation_frequency"}

// demographicKeyed is one row `<column> as key, count(DISTINCT user_id) as count`.
type demographicKeyed struct {
	choiceID string // by choice only
	key      *string
	count    int
}

func participantsSQL(column string) string {
	return `SELECT ` + column + ` as key, count(DISTINCT user_id) as count
            FROM users_profile
            WHERE user_id IN (
                SELECT DISTINCT user_id
                FROM reponses_consultation
                WHERE consultation_id = $1
            )
            GROUP BY ` + column + `
            `
}

func participantsByChoiceSQL(column string) string {
	return `SELECT choice_id as choiceId, ` + column + ` as key, count(DISTINCT users_profile.user_id) AS count
            FROM reponses_consultation LEFT JOIN users_profile
            ON reponses_consultation.user_id = users_profile.user_id
            WHERE question_id = $1
            AND choice_id IS NOT NULL
            GROUP BY (choice_id, ` + column + `)
            `
}

// GetParticipantDemographicInfo is getParticipantDemographicInfo(consultationId): eight queries
// (one round trip: they are sent as one batch).
func (r *ResponseQueries) GetParticipantDemographicInfo(ctx context.Context, consultationID string) (*DemographicInfoCount, error) {
	batch := &pgx.Batch{}
	for _, col := range demographicColumns {
		batch.Queue(participantsSQL(col), consultationID)
	}
	lists, err := r.sendDemographicBatch(ctx, batch, false)
	if err != nil {
		return nil, err
	}
	return toDemographicInfoCount(r.a.Now().Year(), lists), nil
}

// GetParticipantDemographicInfoByChoices is getParticipantDemographicInfoByChoices(questionId).
func (r *ResponseQueries) GetParticipantDemographicInfoByChoices(ctx context.Context, questionID string) (*DemographicInfoCountByChoices, error) {
	lists, err := r.queryByChoices(ctx, []string{questionID})
	if err != nil {
		return nil, err
	}
	return toDemographicInfoCountByChoices(r.a.Now().Year(), lists[0]), nil
}

// queryByChoices runs the eight queries of every question in one batch (one round trip for the
// whole export instead of 8 per question).
func (r *ResponseQueries) queryByChoices(ctx context.Context, questionIDs []string) ([][numDims][]demographicKeyed, error) {
	batch := &pgx.Batch{}
	for _, qid := range questionIDs {
		for _, col := range demographicColumns {
			batch.Queue(participantsByChoiceSQL(col), qid)
		}
	}
	lists, err := r.sendDemographicBatch(ctx, batch, true)
	if err != nil {
		return nil, err
	}
	out := make([][numDims][]demographicKeyed, len(questionIDs))
	for i := range questionIDs {
		copy(out[i][:], lists[i*numDims:(i+1)*numDims])
	}
	return out, nil
}

func (r *ResponseQueries) sendDemographicBatch(ctx context.Context, batch *pgx.Batch, byChoice bool) ([][]demographicKeyed, error) {
	br := r.a.DB.Pool.SendBatch(ctx, batch)
	defer br.Close()
	out := make([][]demographicKeyed, 0, batch.Len())
	for n := 0; n < batch.Len(); n++ {
		rows, err := br.Query()
		if err != nil {
			return nil, err
		}
		// year_of_birth is an integer: the projection converts it to the String key
		yearColumn := n%numDims == dimAgeRange
		var list []demographicKeyed
		for rows.Next() {
			var d demographicKeyed
			var count int64
			var err error
			switch {
			case yearColumn && byChoice:
				var y *int32
				err = rows.Scan(&d.choiceID, &y, &count)
				d.key = yearKey(y)
			case yearColumn:
				var y *int32
				err = rows.Scan(&y, &count)
				d.key = yearKey(y)
			case byChoice:
				err = rows.Scan(&d.choiceID, &d.key, &count)
			default:
				err = rows.Scan(&d.key, &count)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			d.count = int(count)
			list = append(list, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out = append(out, list)
	}
	return out, nil
}

func yearKey(y *int32) *string {
	if y == nil {
		return nil
	}
	s := strconv.Itoa(int(*y))
	return &s
}

// toAgeRange is ReponseConsultationMapper.toAgeRange (the year of birth is read with toIntOrNull).
func toAgeRange(currentYear int, yearOfBirth *string) int {
	if yearOfBirth == nil {
		return noKey
	}
	y, ok := javacompat.KotlinToIntOrNull(*yearOfBirth)
	if !ok {
		return noKey
	}
	age := int32(currentYear) - int32(y) // Int arithmetic
	switch {
	case age < 0:
		return noKey
	case age <= 18: // 0..18
		return ageLessThan18
	case age <= 25: // 18..25 (18 is caught above)
		return ageBetween18And25
	case age <= 35:
		return ageBetween26And35
	case age <= 45:
		return ageBetween36And45
	case age <= 55:
		return ageBetween46And55
	case age <= 65:
		return ageBetween56And65
	}
	return ageMoreThan65
}

// toDemographicInfoCount is ReponseConsultationMapper.toDomain(genderCount, ..., consultationFrequencyCount):
// `associate` (a later row with the same enum value, null included, replaces the count and keeps
// the position of the first) and, for the age ranges, a sum per range.
func toDemographicInfoCount(currentYear int, l [][]demographicKeyed) *DemographicInfoCount {
	var arr [numDims][]demographicKeyed
	copy(arr[:], l)
	return buildDemographicInfoCount(currentYear, arr)
}

func buildDemographicInfoCount(currentYear int, l [numDims][]demographicKeyed) *DemographicInfoCount {
	assoc := func(list []demographicKeyed, f func(*string) int) *omap[int] {
		m := newOmap[int]()
		for _, d := range list {
			m.put(f(d.key), d.count)
		}
		return m
	}
	ages := newOmap[int]()
	for _, d := range l[dimAgeRange] {
		k := toAgeRange(currentYear, d.key)
		prev, _ := ages.get(k)
		ages.put(k, prev+d.count)
	}
	return &DemographicInfoCount{
		dimGender: assoc(l[dimGender], toGender), dimAgeRange: ages, dimDepartment: assoc(l[dimDepartment], toDepartment),
		dimCityType: assoc(l[dimCityType], toCityType), dimJobCategory: assoc(l[dimJobCategory], toJobCategory),
		dimVoteFrequency: assoc(l[dimVoteFrequency], toFrequency), dimPublicMeetingFrequency: assoc(l[dimPublicMeetingFrequency], toFrequency),
		dimConsultationFrequency: assoc(l[dimConsultationFrequency], toFrequency),
	}
}

// toDemographicInfoCountByChoices is ReponseConsultationMapper.toDomain(...ByChoice): for every choice id
// that appears in any of the eight lists, the same mapping over the rows of that choice.
func toDemographicInfoCountByChoices(currentYear int, l [numDims][]demographicKeyed) *DemographicInfoCountByChoices {
	perChoice := map[string]*[numDims][]demographicKeyed{}
	for dim := range l {
		for _, d := range l[dim] {
			p := perChoice[d.choiceID]
			if p == nil {
				p = &[numDims][]demographicKeyed{}
				perChoice[d.choiceID] = p
			}
			p[dim] = append(p[dim], d)
		}
	}
	out := &DemographicInfoCountByChoices{Choices: make(map[string]*DemographicInfoCount, len(perChoice))}
	for choice, p := range perChoice {
		out.Choices[choice] = buildDemographicInfoCount(currentYear, *p)
	}
	return out
}
