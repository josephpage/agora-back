package consultation

import (
	"context"
	"errors"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/modules/login"
	"agora/internal/store"
)

// ---------------------------------------------------------------------------
// FeedbackConsultationUpdateRepository (FeedbackConsultationUpdateRepositoryImpl,
// FeedbackConsultationUpdateDatabaseRepository, FeedbackConsultationUpdateMapper)
// ---------------------------------------------------------------------------

const (
	isPositiveTrueValue  = 1
	isPositiveFalseValue = 0
)

// FeedbackRepository is the repository of the feedbacks on consultation updates
// (table feedbacks_consultation_update, unique (consultation_update_id, user_id)).
type FeedbackRepository struct{ a *app.App }

func errNullColumn(table, column string) error {
	return errors.New("java.lang.NullPointerException: " + table + "." + column + " is NULL")
}

func boolToInt(b bool) int {
	if b {
		return isPositiveTrueValue
	}
	return isPositiveFalseValue
}

// userFeedback reads the feedback row of a user (getUserConsultationUpdateFeedback):
// nil when there is none or when the user id is not a UUID. A NULL date column
// crashes the Kotlin entity mapping.
func (r *FeedbackRepository) userFeedback(ctx context.Context, updateID, userID string) (isPositive *int, id *string, err error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil, nil, nil
	}
	rows, err := r.a.DB.Pool.Query(ctx, `SELECT id, is_positive, created_date, updated_date FROM feedbacks_consultation_update
            WHERE consultation_update_id = $1
            AND user_id = $2
            LIMIT 1
        `, updateID, uid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil, rows.Err()
	}
	var rowID string
	var positive int32
	var created, updated *time.Time
	if err := rows.Scan(&rowID, &positive, &created, &updated); err != nil {
		return nil, nil, err
	}
	if created == nil {
		return nil, nil, errNullColumn("feedbacks_consultation_update", "created_date")
	}
	if updated == nil {
		return nil, nil, errNullColumn("feedbacks_consultation_update", "updated_date")
	}
	p := int(positive)
	return &p, &rowID, nil
}

// GetUserFeedback is getUserFeedback(consultationUpdateId, userId): nil when the
// user gave no feedback.
func (r *FeedbackRepository) GetUserFeedback(ctx context.Context, updateID, userID string) (*bool, error) {
	p, _, err := r.userFeedback(ctx, updateID, userID)
	if err != nil || p == nil {
		return nil, err
	}
	b := *p == isPositiveTrueValue
	return &b, nil
}

// UpdateFeedback is updateFeedback: false when the user has no feedback yet.
func (r *FeedbackRepository) UpdateFeedback(ctx context.Context, updateID, userID string, isPositive bool) (bool, error) {
	p, id, err := r.userFeedback(ctx, updateID, userID)
	if err != nil || p == nil {
		return false, err
	}
	_, err = r.a.DB.Pool.Exec(ctx, "UPDATE feedbacks_consultation_update SET is_positive = $1, updated_date = $2 WHERE id = $3",
		boolToInt(isPositive), store.Millis(r.a.Now()), *id)
	return err == nil, err
}

// InsertFeedback is insertFeedback: nothing is written when the user id is not a UUID.
func (r *FeedbackRepository) InsertFeedback(ctx context.Context, in FeedbackInserting) error {
	uid, ok := javacompat.ToUUIDOrNull(in.UserID)
	if !ok {
		return nil
	}
	created := store.Millis(r.a.Now())
	_, err := r.a.DB.Pool.Exec(ctx,
		"INSERT INTO feedbacks_consultation_update (id, consultation_update_id, created_date, is_positive, updated_date, user_id) VALUES ($1, $2, $3, $4, $3, $5)",
		randomUUID(), in.ConsultationUpdateID, created, boolToInt(in.IsPositive), uid)
	return err
}

// GetFeedbackStats is getFeedbackStats: the ratios of the positive / negative
// answers (the rows with another value are not counted); 0 / 0 / 0 without any.
func (r *FeedbackRepository) GetFeedbackStats(ctx context.Context, updateID string) (*FeedbackStats, error) {
	rows, err := r.a.DB.Pool.Query(ctx, `SELECT is_positive as hasPositiveValue, COUNT(*) as responseCount FROM feedbacks_consultation_update
            WHERE consultation_update_id = $1
            GROUP BY is_positive
        `, updateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var positive, negative int
	for rows.Next() {
		var value int32
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		switch value {
		case isPositiveTrueValue:
			positive += int(count)
		case isPositiveFalseValue:
			negative += int(count)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return statsOf(positive, positive+negative), nil
}

// statsOf is FeedbackConsultationUpdateMapper.toStats: positiveRatio =
// (positive * 100.0 / total).roundToInt(), negativeRatio = 100 - positiveRatio.
func statsOf(positive, total int) *FeedbackStats {
	if total > 0 {
		positiveRatio := javacompat.KotlinRoundToInt(float64(positive) * 100.0 / float64(total))
		return &FeedbackStats{PositiveRatio: positiveRatio, NegativeRatio: 100 - positiveRatio, ResponseCount: total}
	}
	return &FeedbackStats{PositiveRatio: 0, NegativeRatio: 0, ResponseCount: total}
}

// ---------------------------------------------------------------------------
// FeedbackConsultationUpdateUseCase
// ---------------------------------------------------------------------------

// InsertFeedbackResult is InsertFeedbackConsultationUpdateResults.
type InsertFeedbackResult struct {
	Success bool
	Results FeedbackResults
}

// FeedbackUseCase is FeedbackConsultationUpdateUseCase.
type FeedbackUseCase struct {
	a     *app.App
	flags interface {
		IsFeatureEnabled(ctx context.Context, feature login.Feature) (bool, error)
	}
	feedback *FeedbackRepository
	updates  *UpdateRepository
}

// InsertFeedback is insertFeedback(feedbackInserting).
func (u *FeedbackUseCase) InsertFeedback(ctx context.Context, in FeedbackInserting) (InsertFeedbackResult, error) {
	if !u.canAcceptFeedbacks(ctx, in.ConsultationID, in.ConsultationUpdateID) {
		return InsertFeedbackResult{}, nil
	}
	updated, err := u.feedback.UpdateFeedback(ctx, in.ConsultationUpdateID, in.UserID, in.IsPositive)
	if err != nil {
		return InsertFeedbackResult{}, err
	}
	if !updated {
		if err := u.feedback.InsertFeedback(ctx, in); err != nil {
			return InsertFeedbackResult{}, err
		}
	}

	// cacheRepository.initUserFeedback: Kotlin's "hasGivenFeedbackConsultationUpdateV2"
	// cache is not kept by Go (the user's feedback is read from the database, an
	// indexed query); a Kotlin instance must not keep the previous answer
	u.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(hasGivenFeedbackCacheName, in.ConsultationUpdateID+"/"+in.UserID))

	// the statistics shared by the readers of the update route are out of date
	u.a.Cache.Invalidate(ctx, feedbackStatsCacheName, in.ConsultationUpdateID)

	var stats *FeedbackStats
	enabled, err := u.flags.IsFeatureEnabled(ctx, login.FeatureFeedbackConsultationUpdate)
	if err != nil {
		return InsertFeedbackResult{}, err
	}
	if enabled {
		stats, err = u.feedback.GetFeedbackStats(ctx, in.ConsultationUpdateID)
		if err != nil {
			return InsertFeedbackResult{}, err
		}
		u.updateFeedbackStatsCache(ctx, in.ConsultationID, in.ConsultationUpdateID, stats)
	}
	return InsertFeedbackResult{Success: true, Results: FeedbackResults{UserResponse: in.IsPositive, Stats: stats}}, nil
}

// canAcceptFeedbacks: the update must exist (by document id) and ask for a feedback.
func (u *FeedbackUseCase) canAcceptFeedbacks(ctx context.Context, consultationID, updateID string) bool {
	update := u.updates.GetConsultationUpdate(ctx, consultationID, updateID)
	return update != nil && update.FeedbackQuestion != nil
}

// hasGivenFeedbackCacheName is the Kotlin cache "hasGivenFeedbackConsultationUpdateV2"
// (default cache manager, 1 hour), key "<updateId>/<userId>".
const hasGivenFeedbackCacheName = "hasGivenFeedbackConsultationUpdateV2"

// updateFeedbackStatsCache is updateFeedbackStatsCache: the cached latest details
// of the consultation get the new statistics when they show this update.
func (u *FeedbackUseCase) updateFeedbackStatsCache(ctx context.Context, consultationID, updateID string, stats *FeedbackStats) {
	ttl := u.a.Cache.CoexistenceTTL(detailsCacheTTL)
	if v, ok := u.a.Cache.Get(latestDetailsCacheName, consultationID); ok {
		if d := v.(*Details); d.Update.ID == updateID {
			updated := *d
			updated.FeedbackStats = stats
			// the other instances reload (their entry cannot be patched in place)
			u.a.Cache.Invalidate(ctx, latestDetailsCacheName, consultationID)
			u.a.Cache.Put(latestDetailsCacheName, consultationID, &updated, ttl)
		}
	} else {
		u.a.Cache.Invalidate(ctx, latestDetailsCacheName, consultationID)
	}
	// a Kotlin instance caches the statistics too (and cannot be told what changed)
	u.a.Cache.DeleteKotlinKeys(ctx,
		cache.KotlinKey(latestDetailsCacheName, consultationID),
		cache.KotlinKey(detailsCacheName, consultationID+"/"+updateID))
}
