package profile

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/javacompat"
	"agora/internal/modules/users"
	"agora/internal/store"
)

// civilDate is a LocalDate: midnight UTC of the calendar date.
func civilDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// askDateCache is DemographicInfoAskDateCacheRepository (5 minutes).
type askDateCache interface {
	getDate(userUUID string) *time.Time
	insertDate(ctx context.Context, userUUID string, askDate time.Time)
	deleteDate(ctx context.Context, userUUID string)
}

type l1AskDateCache struct{ a *app.App }

func (c l1AskDateCache) getDate(userUUID string) *time.Time {
	if v, ok := c.a.Cache.Get(askDateCacheName, userUUID); ok {
		d := v.(time.Time)
		return &d
	}
	return nil
}

func (c l1AskDateCache) insertDate(ctx context.Context, userUUID string, askDate time.Time) {
	c.a.Cache.Put(askDateCacheName, userUUID, askDate, askDateCacheTTL)
	c.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(askDateCacheName, userUUID))
}

func (c l1AskDateCache) deleteDate(ctx context.Context, userUUID string) {
	c.a.Cache.Invalidate(ctx, askDateCacheName, userUUID)
	c.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(askDateCacheName, userUUID))
}

// DemographicInfoAskDateRepository is usecase/profile/repository/DemographicInfoAskDateRepository.
type DemographicInfoAskDateRepository interface {
	GetDate(ctx context.Context, userID string) (*time.Time, error)
	InsertDate(ctx context.Context, userID string) error
	DeleteDate(ctx context.Context, userID string) error
}

type askDateRepository struct {
	db    askDateDB
	cache askDateCache
	now   func() time.Time
}

// toDate is DemographicInfoAskDateMapper.toDate: `dto.askDate.toLocalDate()`
// (a NULL ask_date is a NullPointerException).
func toDate(dto *askDateRow) time.Time {
	if dto.AskDate == nil {
		panic("NullPointerException: demographic_info_ask_date.ask_date is NULL")
	}
	return civilDate(*dto.AskDate)
}

// GetDate is getDate: cache, then database (the first row, no ordering).
func (r *askDateRepository) GetDate(ctx context.Context, userID string) (*time.Time, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil, nil
	}
	if d := r.cache.getDate(uid); d != nil {
		return d, nil
	}
	dto, err := r.db.getAskDate(ctx, uid)
	if err != nil || dto == nil {
		return nil, err
	}
	d := toDate(dto)
	r.cache.insertDate(ctx, uid, d)
	return &d, nil
}

// InsertDate is insertDate: a new row each time (older rows are kept).
func (r *askDateRepository) InsertDate(ctx context.Context, userID string) error {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil
	}
	now := store.Millis(r.now())
	saved, err := r.db.save(ctx, &askDateRow{ID: users.RandomUUID(), AskDate: &now, UserID: uid})
	if err != nil {
		return err
	}
	r.cache.insertDate(ctx, saved.UserID, toDate(saved))
	return nil
}

// DeleteDate is deleteDate: evicts the cache, then deletes every row.
func (r *askDateRepository) DeleteDate(ctx context.Context, userID string) error {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil
	}
	r.cache.deleteDate(ctx, uid)
	return r.db.deleteAskDate(ctx, uid)
}
