package qag

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/modules/login"
	"agora/internal/store"
)

// Kotlin caches of the feedback packages (default RedisCacheManager, 1 hour).
const (
	feedbackResultsCache = "feedbackResults"  // key: qagId
	userFeedbackCache    = "userFeedbackQags" // key: "<userId>/<qagId>"
	feedbackCacheTTL     = time.Hour
)

// ---------------------------------------------------------------------------
// FeedbackQagRepository (FeedbackQagRepositoryImpl + FeedbackQagDatabaseRepository)
// ---------------------------------------------------------------------------

// FeedbackRepository is FeedbackQagRepositoryImpl. The feedbacks_qag.qag_id
// column is a free varchar: the QaG id is compared as the raw string the client
// sent, not as a UUID.
type FeedbackRepository struct{ a *app.App }

const (
	boolIntTrue  = 1
	boolIntFalse = 0
)

// DeleteUsersFeedbackQag is deleteUsersFeedbackQag.
func (r *FeedbackRepository) DeleteUsersFeedbackQag(ctx context.Context, userIDs []string) error {
	uids := uuidsOrNull(userIDs)
	if len(uids) == 0 {
		return nil
	}
	_, err := r.a.DB.Pool.Exec(ctx, "DELETE FROM feedbacks_qag WHERE user_id IN "+inList(1, len(uids)), toArgs(uids)...)
	return err
}

// GetFeedbackResponseForUser is getFeedbackResponseForUser: nil when the user has
// not answered (or the user id is not a UUID).
func (r *FeedbackRepository) GetFeedbackResponseForUser(ctx context.Context, qagID, userID string) (*bool, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil, nil
	}
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT is_helpful, user_id FROM feedbacks_qag WHERE qag_id = $1 and user_id = $2", qagID, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var helpful *int16
	var user *string
	if err := rows.Scan(&helpful, &user); err != nil {
		return nil, err
	}
	if helpful == nil || user == nil {
		return nil, errNullColumn("feedbacks_qag", "is_helpful")
	}
	b := int(*helpful) == boolIntTrue
	return &b, nil
}

// GetFeedbackQagList is getFeedbackQagList.
func (r *FeedbackRepository) GetFeedbackQagList(ctx context.Context, qagID string) ([]FeedbackQag, error) {
	rows, err := r.a.DB.Pool.Query(ctx, "SELECT qag_id, user_id, is_helpful FROM feedbacks_qag WHERE qag_id = $1", qagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackQag{}
	for rows.Next() {
		var q, u *string
		var h *int16
		if err := rows.Scan(&q, &u, &h); err != nil {
			return nil, err
		}
		if q == nil || u == nil || h == nil {
			return nil, errNullColumn("feedbacks_qag", "qag_id/user_id/is_helpful")
		}
		out = append(out, FeedbackQag{QagID: *q, UserID: *u, IsHelpful: int(*h) == boolIntTrue})
	}
	return out, rows.Err()
}

// feedbackCounts is FeedbackQagUseCase.buildResults' input: the number of
// feedbacks and the number of helpful ones, computed by the database instead of
// loading every row. A row Kotlin cannot map (NULL columns) is an exception.
func (r *FeedbackRepository) feedbackCounts(ctx context.Context, qagID string) (total, helpful int, err error) {
	var t, h, bad int64
	err = r.a.DB.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE is_helpful = 1),
            count(*) FILTER (WHERE is_helpful IS NULL OR user_id IS NULL OR qag_id IS NULL)
        FROM feedbacks_qag WHERE qag_id = $1`, qagID).Scan(&t, &h, &bad)
	if err != nil {
		return 0, 0, err
	}
	if bad > 0 {
		return 0, 0, errNullColumn("feedbacks_qag", "qag_id/user_id/is_helpful")
	}
	return int(t), int(h), nil
}

func boolToInt(b bool) int {
	if b {
		return boolIntTrue
	}
	return boolIntFalse
}

// InsertFeedbackQag is insertFeedbackQag (a user id that is not a UUID is a FAILURE).
func (r *FeedbackRepository) InsertFeedbackQag(ctx context.Context, f FeedbackQagInserting) (FeedbackQagResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(f.UserID)
	if !ok {
		return FeedbackFailure, nil
	}
	created := store.Millis(r.a.Now())
	_, err := r.a.DB.Pool.Exec(ctx,
		"INSERT INTO feedbacks_qag (id, created_date, is_helpful, qag_id, updated_date, user_id) VALUES ($1, $2, $3, $4, $2, $5)",
		randomUUID(), created, boolToInt(f.IsHelpful), f.QagID, uid)
	if err != nil {
		return FeedbackFailure, err
	}
	return FeedbackSuccess, nil
}

// UpdateFeedbackQag is updateFeedbackQag: the feedback must exist.
func (r *FeedbackRepository) UpdateFeedbackQag(ctx context.Context, qagID, userID string, isHelpful bool) (FeedbackQagResult, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return FeedbackFailure, nil
	}
	tag, err := r.a.DB.Pool.Exec(ctx, "UPDATE feedbacks_qag SET is_helpful = $1, updated_date = $2 WHERE qag_id = $3 AND user_id = $4",
		boolToInt(isHelpful), store.Millis(r.a.Now()), qagID, uid)
	if err != nil {
		return FeedbackFailure, err
	}
	if tag.RowsAffected() <= 0 {
		return FeedbackFailure, nil
	}
	return FeedbackSuccess, nil
}

// ---------------------------------------------------------------------------
// Caches (FeedbackResultsCacheRepositoryImpl, UserFeedbackQagCacheRepositoryImpl)
// ---------------------------------------------------------------------------

// feedbackResultsStore is FeedbackResultsCacheRepository. getOrLoad merges
// getFeedbackResults + initFeedbackResults into one load shared by concurrent
// callers and never stored when an eviction happened meanwhile.
type feedbackResultsStore interface {
	getOrLoad(ctx context.Context, qagID string, load func() (FeedbackResults, error)) (FeedbackResults, error)
	evict(ctx context.Context, qagID string)
}

// userFeedbackStore is UserFeedbackQagCacheRepository.
type userFeedbackStore interface {
	getOrLoad(ctx context.Context, userID, qagID string, load func() (*bool, error)) (*bool, error)
	// set stores the answer the user just gave. Kotlin stored the PREVIOUS answer
	// (null for a first feedback) and kept it for an hour: class B, see
	// parity/divergences/S2.md.
	set(ctx context.Context, userID, qagID string, answer bool)
}

type l1FeedbackResults struct{ a *app.App }

func (c l1FeedbackResults) getOrLoad(_ context.Context, qagID string, load func() (FeedbackResults, error)) (FeedbackResults, error) {
	return cache.GetOrLoad(c.a.Cache, feedbackResultsCache, qagID, coexistenceCap(c.a, feedbackCacheTTL), load)
}

func (c l1FeedbackResults) evict(ctx context.Context, qagID string) {
	c.a.Cache.Invalidate(ctx, feedbackResultsCache, qagID)
	c.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(feedbackResultsCache, qagID))
}

type l1UserFeedback struct{ a *app.App }

// userFeedbackEntry distinguishes "answered" from "not answered" in L1.
type userFeedbackEntry struct{ answer *bool }

func userFeedbackKey(userID, qagID string) string { return userID + "/" + qagID }

func (c l1UserFeedback) getOrLoad(_ context.Context, userID, qagID string, load func() (*bool, error)) (*bool, error) {
	e, err := cache.GetOrLoad(c.a.Cache, userFeedbackCache, userFeedbackKey(userID, qagID), coexistenceCap(c.a, feedbackCacheTTL),
		func() (userFeedbackEntry, error) {
			v, err := load()
			return userFeedbackEntry{v}, err
		})
	return e.answer, err
}

func (c l1UserFeedback) set(ctx context.Context, userID, qagID string, answer bool) {
	key := userFeedbackKey(userID, qagID)
	// drop the entry everywhere (other instances reload from the database), then
	// keep the new answer locally: the user's next read needs no query
	c.a.Cache.Invalidate(ctx, userFeedbackCache, key)
	c.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(userFeedbackCache, key))
	c.a.Cache.Put(userFeedbackCache, key, userFeedbackEntry{&answer}, coexistenceCap(c.a, feedbackCacheTTL))
}

// ---------------------------------------------------------------------------
// FeedbackQagUseCase, InsertFeedbackQagUseCase
// ---------------------------------------------------------------------------

type featureFlags interface {
	IsFeatureEnabled(ctx context.Context, feature login.Feature) (bool, error)
}

// feedbackStorage is FeedbackQagRepository as the use cases need it.
type feedbackStorage interface {
	GetFeedbackResponseForUser(ctx context.Context, qagID, userID string) (*bool, error)
	feedbackCounts(ctx context.Context, qagID string) (total, helpful int, err error)
	InsertFeedbackQag(ctx context.Context, f FeedbackQagInserting) (FeedbackQagResult, error)
	UpdateFeedbackQag(ctx context.Context, qagID, userID string, isHelpful bool) (FeedbackQagResult, error)
}

// FeedbackUseCase is FeedbackQagUseCase + InsertFeedbackQagUseCase.
type FeedbackUseCase struct {
	flags   featureFlags
	repo    feedbackStorage
	results feedbackResultsStore
	users   userFeedbackStore
}

// GetFeedbackResults is FeedbackQagUseCase.getFeedbackResults: nil when the
// feature is disabled. The QaG id is the raw string the caller got.
func (u *FeedbackUseCase) GetFeedbackResults(ctx context.Context, qagID string) (*FeedbackResults, error) {
	enabled, err := u.flags.IsFeatureEnabled(ctx, login.FeatureFeedbackResponseQag)
	if err != nil || !enabled {
		return nil, err
	}
	res, err := u.results.getOrLoad(ctx, qagID, func() (FeedbackResults, error) { return u.buildResults(ctx, qagID) })
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetFeedbackForQagAndUser is FeedbackQagUseCase.getFeedbackForQagAndUser: the
// answer of the user (nil = none yet), from the cache or the database.
func (u *FeedbackUseCase) GetFeedbackForQagAndUser(ctx context.Context, qagID, userID string) (*bool, error) {
	return u.users.getOrLoad(ctx, userID, qagID, func() (*bool, error) {
		return u.repo.GetFeedbackResponseForUser(ctx, qagID, userID)
	})
}

// buildResults is FeedbackQagUseCase.buildResults.
func (u *FeedbackUseCase) buildResults(ctx context.Context, qagID string) (FeedbackResults, error) {
	total, helpful, err := u.repo.feedbackCounts(ctx, qagID)
	if err != nil {
		return FeedbackResults{}, err
	}
	return resultsFromCounts(total, helpful), nil
}

// resultsFromCounts: no feedback gives 0 / 0; else positiveRatio =
// (helpful * 100.0 / size).roundToInt() and negativeRatio = 100 - positiveRatio.
func resultsFromCounts(total, helpful int) FeedbackResults {
	if total == 0 {
		return FeedbackResults{}
	}
	positive := javacompat.KotlinRoundToInt(float64(helpful) * 100.0 / float64(total))
	return FeedbackResults{PositiveRatio: positive, NegativeRatio: 100 - positive, Count: total}
}

// InsertFeedbackQag is InsertFeedbackQagUseCase.insertFeedbackQag.
func (u *FeedbackUseCase) InsertFeedbackQag(ctx context.Context, in FeedbackQagInserting) (InsertFeedbackQagResult, *FeedbackResults, error) {
	previous, err := u.repo.GetFeedbackResponseForUser(ctx, in.QagID, in.UserID)
	if err != nil {
		return InsertFeedbackFailure, nil, err
	}
	var result FeedbackQagResult
	if previous == nil {
		result, err = u.repo.InsertFeedbackQag(ctx, in)
	} else {
		result, err = u.repo.UpdateFeedbackQag(ctx, in.QagID, in.UserID, in.IsHelpful)
	}
	if err != nil {
		return InsertFeedbackFailure, nil, err
	}
	if result != FeedbackSuccess {
		return InsertFeedbackFailure, nil, nil
	}
	u.users.set(ctx, in.UserID, in.QagID, in.IsHelpful)
	enabled, err := u.flags.IsFeatureEnabled(ctx, login.FeatureFeedbackResponseQag)
	if err != nil {
		return InsertFeedbackFailure, nil, err
	}
	if !enabled {
		return InsertFeedbackSuccessDisabled, nil, nil
	}
	u.results.evict(ctx, in.QagID)
	res, err := u.GetFeedbackResults(ctx, in.QagID)
	if err != nil {
		return InsertFeedbackFailure, nil, err
	}
	if res == nil {
		return InsertFeedbackSuccessDisabled, nil, nil
	}
	return InsertFeedbackSuccess, res, nil
}
