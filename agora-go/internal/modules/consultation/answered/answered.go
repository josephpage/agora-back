// Package answered ports infrastructure/userAnsweredConsultation (the
// user_answered_consultation table: who answered which consultation) with the
// UserAnsweredConsultationRepository use case interface. It is owned by the
// "consultation details" slice and used by the consultation lists (S5) and the
// consultation responses / results (S6).
//
//	r := answered.Get(a)
//	r.HasAnsweredConsultation(ctx, consultationID, userID)   // UserAnsweredConsultationRepository.hasAnsweredConsultation
//	r.GetParticipantCount(ctx, consultationID)               // count(DISTINCT user_id)
//	r.GetAnsweredConsultationIDs(ctx, userID)                // DISTINCT consultation ids of the user
//	r.InsertUserAnsweredConsultation(ctx, UserAnswered{...})
//
// Nothing here is cached: every method is one indexed query (indexes
// (consultation_id, user_id) and (user_id, consultation_id)).
package answered

import (
	"context"
	"crypto/rand"
	"strconv"
	"strings"
	"sync"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// UserAnswered is domain.UserAnsweredConsultation.
type UserAnswered struct {
	UserID         string
	ConsultationID string
}

// Result is UserAnsweredConsultationResult.
type Result int

// The UserAnsweredConsultationResult constants.
const (
	Success Result = iota
	Failure
)

// Repository is UserAnsweredConsultationRepositoryImpl +
// UserAnsweredConsultationDatabaseRepository (SQL copied verbatim).
type Repository struct {
	a *app.App

	mu         sync.RWMutex
	onInserted []func(ctx context.Context, userID string)
}

// OnInserted registers a function called after InsertUserAnsweredConsultation has
// committed a row (S5 evicts the answered consultations pages of that user there,
// so the consultation responses slice has nothing to call).
func (r *Repository) OnInserted(f func(ctx context.Context, userID string)) {
	r.mu.Lock()
	r.onInserted = append(r.onInserted, f)
	r.mu.Unlock()
}

// Get returns the App-wide repository.
func Get(a *app.App) *Repository {
	return app.Singleton(a, "consultation.answered", func() *Repository { return &Repository{a: a} })
}

// GetParticipantCount is getParticipantCount(consultationId).
func (r *Repository) GetParticipantCount(ctx context.Context, consultationID string) (int, error) {
	var n int64
	err := r.a.DB.Pool.QueryRow(ctx,
		"SELECT count(DISTINCT user_id) FROM user_answered_consultation WHERE consultation_id = $1", consultationID).Scan(&n)
	return int(n), err
}

// GetAnsweredConsultationIDs is getAnsweredConsultationIds(userId): empty when
// the user id is not a UUID.
func (r *Repository) GetAnsweredConsultationIDs(ctx context.Context, userID string) ([]string, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return []string{}, nil
	}
	return r.distinctIDs(ctx, `SELECT DISTINCT consultation_id FROM user_answered_consultation
            WHERE user_id = $1
        `, uid)
}

func (r *Repository) distinctIDs(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := r.a.DB.Pool.Query(ctx, sql, args...)
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

// HasAnsweredConsultation is hasAnsweredConsultation(consultationId, userId):
// false when the user id is not a UUID. (count(DISTINCT user_id) >= 1 is an EXISTS.)
func (r *Repository) HasAnsweredConsultation(ctx context.Context, consultationID, userID string) (bool, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return false, nil
	}
	var exists bool
	err := r.a.DB.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_answered_consultation
            WHERE consultation_id = $1
            AND user_id = $2)`, consultationID, uid).Scan(&exists)
	return exists, err
}

// HasAnsweredConsultations is hasAnsweredConsultations(consultationIds, userId):
// empty for an empty list or a user id that is not a UUID.
func (r *Repository) HasAnsweredConsultations(ctx context.Context, consultationIDs []string, userID string) (map[string]bool, error) {
	if len(consultationIDs) == 0 {
		return map[string]bool{}, nil
	}
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return map[string]bool{}, nil
	}
	var in strings.Builder
	args := make([]any, 0, len(consultationIDs)+1)
	for i, id := range consultationIDs {
		if i > 0 {
			in.WriteByte(',')
		}
		in.WriteString("$" + strconv.Itoa(i+1))
		args = append(args, id)
	}
	args = append(args, uid)
	// Hibernate expands `IN :consultationIds` to a parenthesized list of placeholders
	answeredList, err := r.distinctIDs(ctx, `SELECT DISTINCT consultation_id FROM user_answered_consultation
            WHERE consultation_id IN (`+in.String()+`)
            AND user_id = $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(answeredList))
	for _, id := range answeredList {
		set[id] = true
	}
	out := make(map[string]bool, len(consultationIDs))
	for _, id := range consultationIDs {
		out[id] = set[id]
	}
	return out, nil
}

// GetUsersAnsweredConsultation is getUsersAnsweredConsultation(consultationId).
func (r *Repository) GetUsersAnsweredConsultation(ctx context.Context, consultationID string) ([]string, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT DISTINCT user_id FROM user_answered_consultation WHERE consultation_id = $1", consultationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u *string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		if u != nil {
			out = append(out, *u)
		}
	}
	return out, rows.Err()
}

// GetConsultationAnsweredCount is getConsultationAnsweredCount(userId): the
// number of consultations the user answered (0 when the id is not a UUID).
func (r *Repository) GetConsultationAnsweredCount(ctx context.Context, userID string) (int, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return 0, nil
	}
	var n int64
	err := r.a.DB.Pool.QueryRow(ctx, `SELECT count(distinct consultation_id) FROM user_answered_consultation WHERE user_id = $1`, uid).Scan(&n)
	return int(n), err
}

// InsertUserAnsweredConsultation is insertUserAnsweredConsultation: the user
// id is read with UUID.fromString (Failure when it is invalid), the row gets a
// random id and the current time (milliseconds).
func (r *Repository) InsertUserAnsweredConsultation(ctx context.Context, in UserAnswered) (Result, error) {
	uid, ok := javacompat.ParseUUID(in.UserID)
	if !ok {
		return Failure, nil
	}
	_, err := r.a.DB.Pool.Exec(ctx,
		"INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES ($1, $2, $3, $4)",
		randomUUID(), in.ConsultationID, store.Millis(r.a.Now()), uid.String())
	if err != nil {
		return Failure, err
	}
	r.mu.RLock()
	hooks := r.onInserted
	r.mu.RUnlock()
	for _, f := range hooks {
		f(ctx, in.UserID)
	}
	return Success, nil
}

// randomUUID is UUID.randomUUID(): a random version 4 UUID, canonical lowercase form.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return javacompat.UUIDFromBytes(b).String()
}
