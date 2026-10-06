// Package users resolves the authenticated principal (LoginUseCase.findUser →
// UserRepositoryImpl.getUserById → UserInfoMapper.toDomain).
//
// Kotlin cached UserDTOs in Redis ("userCache", 1h, not refreshed on
// authorization changes or bans). Go keeps an L1 entry for 60s, invalidated
// on every user write (login, delete, upgrade/downgrade, ban), which is
// fresher (divergence class B) and removes the Redis round trip from the
// authentication path.
package users

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/httpx"
	"agora/internal/javacompat"
)

const (
	cacheName       = "users"
	ttlFound        = 60 * time.Second
	ttlNotFound     = 5 * time.Second
	kotlinUserCache = "userCache"
)

// Authorization levels (AuthorizationLevel).
const (
	LevelDefault   = 0
	LevelPublisher = 8
	LevelModerator = 42
	LevelAdmin     = 1337
)

// UserInfo is the domain UserInfo.
type UserInfo struct {
	UserID         string
	FCMToken       string
	IsBanned       bool
	Authorizations []string
}

// Service is the user lookup service.
type Service struct {
	a *app.App
}

// New creates the service.
func New(a *app.App) *Service { return &Service{a: a} }

type cached struct {
	user  *UserInfo
	found bool
}

// ErrCorruptUser mirrors the Hibernate/Kotlin failures on NULL columns
// (NPE / PropertyAccessException → HTTP 500).
var ErrCorruptUser = errors.New("corrupt agora_users row (NULL in non-null column)")

// AuthorizationsFor maps authorization_level to UserAuthorization lists.
func AuthorizationsFor(level int) []string {
	user := []string{httpx.AuthViewConsultation, httpx.AuthAnswerConsultation, httpx.AuthViewQag, httpx.AuthSupportQag, httpx.AuthFeedbackQagResponse, httpx.AuthAddQag}
	switch level {
	case LevelDefault:
		return user
	case LevelModerator:
		return append(user, httpx.AuthModerateQag)
	case LevelPublisher:
		return append(user, httpx.AuthViewUnpublishedConsultation)
	case LevelAdmin:
		return []string{httpx.AuthViewConsultation, httpx.AuthViewUnpublishedConsultation, httpx.AuthAnswerConsultation, httpx.AuthViewQag, httpx.AuthSupportQag, httpx.AuthFeedbackQagResponse, httpx.AuthAddQag, httpx.AuthModerateQag, httpx.AuthAdminAPIs}
	}
	return []string{}
}

// FindUser is LoginUseCase.findUser: nil when unknown or not a UUID.
func (s *Service) FindUser(ctx context.Context, userID string) (*UserInfo, error) {
	u, ok := javacompat.ParseUUID(userID)
	if !ok {
		return nil, nil
	}
	key := u.String()
	c, err := cache.GetOrLoad(s.a.Cache, cacheName, key, ttlFound, func() (cached, error) {
		user, err := s.load(ctx, key)
		if err != nil {
			return cached{}, err
		}
		return cached{user: user, found: user != nil}, nil
	})
	if err != nil {
		return nil, err
	}
	if !c.found {
		// shorter negative caching
		if v, ok := s.a.Cache.Get(cacheName, key); ok && !v.(cached).found {
			s.a.Cache.Put(cacheName, key, v, ttlNotFound)
		}
		return nil, nil
	}
	return c.user, nil
}

func (s *Service) load(ctx context.Context, id string) (*UserInfo, error) {
	var fcm *string
	var level, banned *int
	err := s.a.DB.Pool.QueryRow(ctx,
		"SELECT fcm_token, authorization_level, is_banned FROM agora_users WHERE id = $1 LIMIT 1", id,
	).Scan(&fcm, &level, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if fcm == nil || level == nil || banned == nil {
		return nil, ErrCorruptUser
	}
	return &UserInfo{
		UserID:         id,
		FCMToken:       *fcm,
		IsBanned:       *banned != 0,
		Authorizations: AuthorizationsFor(*level),
	}, nil
}

// Invalidate drops cached principals after a write, on every instance, and
// (coexistence) the Kotlin "userCache" entries.
func (s *Service) Invalidate(ctx context.Context, userIDs ...string) {
	var kotlinKeys []string
	for _, id := range userIDs {
		s.a.Cache.Invalidate(ctx, cacheName, id)
		kotlinKeys = append(kotlinKeys, cache.KotlinKey(kotlinUserCache, id))
	}
	s.a.Cache.DeleteKotlinKeys(ctx, kotlinKeys...)
}

// InvalidateAll drops every cached principal (daily ban task, bulk updates).
func (s *Service) InvalidateAll(ctx context.Context) {
	s.a.Cache.InvalidateAll(ctx, cacheName)
	s.a.Cache.DeleteKotlinPattern(ctx, kotlinUserCache+"::*")
}

// Lookup adapts FindUser to httpx.UserLookup.
func (s *Service) Lookup(r *http.Request, userID string) (*httpx.User, error) {
	u, err := s.FindUser(r.Context(), userID)
	if err != nil || u == nil {
		return nil, err
	}
	return &httpx.User{ID: u.UserID, IsBanned: u.IsBanned, Authorities: u.Authorizations}, nil
}

// Get returns the App-wide users service.
func Get(a *app.App) *Service { return app.Singleton(a, "users", func() *Service { return New(a) }) }
