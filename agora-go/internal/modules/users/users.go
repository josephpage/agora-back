// Package users resolves the authenticated principal (LoginUseCase.findUser →
// UserRepositoryImpl.getUserById → UserInfoMapper.toDomain) and owns the writes
// on agora_users (signup, login, deletion, authorization level).
//
// Kotlin cached UserDTOs in Redis ("userCache", 1h, not refreshed on
// authorization changes or bans). Go keeps an L1 entry for 60s, invalidated
// on every user write (login, delete, upgrade/downgrade, ban), which is
// fresher (divergence class B) and removes the Redis round trip from the
// authentication path.
//
// Kotlin quirk reproduced here: the cached UserDTO of a user whose
// password / created_date / last_connection_date is NULL cannot be
// deserialized back (non-null Kotlin parameters), so the first access
// succeeds (DB read) and every following one fails (HTTP 500) until the
// entry expires one hour later.
package users

import (
	"context"
	"errors"
	"net/http"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/domain"
	"agora/internal/httpx"
	"agora/internal/javacompat"
)

const (
	cacheName       = "users"
	ttlFound        = 60 * time.Second
	ttlNotFound     = 5 * time.Second
	ttlUnreadable   = time.Hour // Kotlin's userCache TTL, for entries it can no longer read
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

// SignupRequest is the domain SignupRequest.
type SignupRequest struct {
	IPAddressHash string
	UserAgent     string
	FCMToken      string
	Platform      string
	VersionName   string
	VersionCode   string
}

// LoginRequest is the domain LoginRequest.
type LoginRequest struct {
	UserID        string
	IPAddressHash string
	UserAgent     string
	FCMToken      string
	Platform      string
	VersionName   string
	VersionCode   string
}

// Service is the user repository.
type Service struct {
	a  *app.App
	st dataStore
}

// New creates the service.
func New(a *app.App) *Service { return &Service{a: a, st: &pgStore{db: a.DB}} }

// cached is what the L1 holds for a user id (the Kotlin CacheResult).
type cached struct {
	user  *UserInfo
	found bool
	// unreadable: the Kotlin cache entry exists but fails to deserialize.
	unreadable bool
}

// ErrCorruptUser mirrors the Hibernate/Kotlin failures on NULL columns
// (NPE / PropertyAccessException → HTTP 500).
var ErrCorruptUser = errors.New("corrupt agora_users row (NULL in non-null column)")

// ErrUnreadableCacheEntry mirrors the SerializationException Kotlin raises when
// it reads back a cached UserDTO holding a NULL for a non-null property.
var ErrUnreadableCacheEntry = errors.New("SerializationException: cached UserDTO has a null non-null property")

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

// toDomain is UserInfoMapper.toDomain. A NULL is_banned already fails when
// Hibernate hydrates the entity (primitive int); a NULL fcm_token fails in the
// UserInfo constructor (Kotlin null check).
func toDomain(r *userRow) (*UserInfo, error) {
	if r.IsBanned == nil || r.FCMToken == nil {
		return nil, ErrCorruptUser
	}
	return &UserInfo{
		UserID:         r.ID,
		FCMToken:       *r.FCMToken,
		IsBanned:       *r.IsBanned != 0,
		Authorizations: AuthorizationsFor(r.AuthorizationLevel),
	}, nil
}

// cacheable reports whether Kotlin could read the DTO back from Redis.
func cacheable(r *userRow) bool {
	return r.Password != nil && r.CreatedDate != nil && r.LastConnectionDate != nil
}

// getUserDTO is UserRepositoryImpl.getUserDTO: the cached principal, read
// from the database on a miss.
func (s *Service) getUserDTO(ctx context.Context, key string) (cached, error) {
	loaded := false
	c, err := cache.GetOrLoad(s.a.Cache, cacheName, key, ttlFound, func() (cached, error) {
		loaded = true
		row, err := s.st.getUserByID(ctx, key)
		if err != nil {
			return cached{}, err
		}
		if row == nil {
			return cached{}, nil
		}
		user, err := toDomain(row)
		if err != nil {
			return cached{}, err
		}
		return cached{user: user, found: true, unreadable: !cacheable(row)}, nil
	})
	if err != nil {
		return cached{}, err
	}
	if !loaded {
		if c.unreadable {
			return cached{}, ErrUnreadableCacheEntry
		}
		return c, nil
	}
	switch {
	case !c.found:
		// shorter negative caching
		s.a.Cache.Put(cacheName, key, c, ttlNotFound)
	case c.unreadable:
		s.a.Cache.Put(cacheName, key, c, ttlUnreadable)
	}
	return c, nil
}

// FindUser is LoginUseCase.findUser: nil when unknown or not a UUID.
func (s *Service) FindUser(ctx context.Context, userID string) (*UserInfo, error) {
	u, ok := javacompat.ParseUUID(userID)
	if !ok {
		return nil, nil
	}
	c, err := s.getUserDTO(ctx, u.String())
	if err != nil || !c.found {
		return nil, err
	}
	return c.user, nil
}

// UpdateUser is UserRepositoryImpl.updateUser (login): stores the new FCM
// token and the connection date, returns the updated user (nil if unknown).
//
// Kotlin saved the whole cached UserDTO (including a possibly stale ban flag
// or authorization level); Go only writes the two columns that change.
func (s *Service) UpdateUser(ctx context.Context, req LoginRequest) (*UserInfo, error) {
	key, ok := javacompat.ToUUIDOrNull(req.UserID)
	if !ok {
		return nil, nil
	}
	c, err := s.getUserDTO(ctx, key)
	if err != nil || !c.found {
		return nil, err
	}
	if err := s.st.updateLogin(ctx, key, req.FCMToken, storeMillis(s.a.Now())); err != nil {
		return nil, err
	}
	s.Invalidate(ctx, key)
	updated := *c.user
	updated.FCMToken = req.FCMToken
	return &updated, nil
}

// GenerateUser is UserRepositoryImpl.generateUser (signup).
func (s *Service) GenerateUser(ctx context.Context, req SignupRequest) (*UserInfo, error) {
	now := storeMillis(s.a.Now())
	empty := ""
	zero := 0
	fcm := req.FCMToken
	row := &userRow{
		ID:                 RandomUUID(),
		Password:           &empty,
		FCMToken:           &fcm,
		CreatedDate:        &now,
		AuthorizationLevel: LevelDefault,
		IsBanned:           &zero,
		LastConnectionDate: &now,
	}
	if err := s.st.insertUser(ctx, row); err != nil {
		return nil, err
	}
	user, err := toDomain(row)
	if err != nil {
		return nil, err
	}
	// Kotlin put the saved DTO in userCache.
	s.a.Cache.Put(cacheName, row.ID, cached{user: user, found: true}, ttlFound)
	return user, nil
}

// GetAllUsers is UserRepositoryImpl.getAllUsers.
func (s *Service) GetAllUsers(ctx context.Context) ([]*UserInfo, error) {
	rows, err := s.st.findAll(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(rows)
}

// GetUsersNotAnsweredConsultation is UserRepositoryImpl.getUsersNotAnsweredConsultation.
func (s *Service) GetUsersNotAnsweredConsultation(ctx context.Context, consultationID string) ([]*UserInfo, error) {
	rows, err := s.st.usersNotAnsweredConsultation(ctx, consultationID)
	if err != nil {
		return nil, err
	}
	return mapRows(rows)
}

// GetUsersLivingInDepartement is UserRepositoryImpl.getUsersLivingInDepartement.
func (s *Service) GetUsersLivingInDepartement(ctx context.Context, departement *domain.Department) ([]*UserInfo, error) {
	code := domain.DepartmentCode(departement)
	if code == nil {
		return []*UserInfo{}, nil
	}
	rows, err := s.st.usersLivingInDepartement(ctx, *code)
	if err != nil {
		return nil, err
	}
	return mapRows(rows)
}

// GetUsersInterestedInDepartement is UserRepositoryImpl.getUsersInterestedInDepartement.
func (s *Service) GetUsersInterestedInDepartement(ctx context.Context, departement *domain.Department) ([]*UserInfo, error) {
	code := domain.DepartmentCode(departement)
	if code == nil {
		return []*UserInfo{}, nil
	}
	d := domain.DepartementFromCodePostal(*code)
	if d == nil {
		return []*UserInfo{}, nil
	}
	rows, err := s.st.usersInterestedInDepartement(ctx, d.Value())
	if err != nil {
		return nil, err
	}
	return mapRows(rows)
}

func mapRows(rows []*userRow) ([]*UserInfo, error) {
	out := make([]*UserInfo, 0, len(rows))
	for _, r := range rows {
		u, err := toDomain(r)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// uuidsOrNull is `userIDs.mapNotNull { it.toUuidOrNull() }`.
func uuidsOrNull(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			out = append(out, u)
		}
	}
	return out
}

// DeleteUsers is UserRepositoryImpl.deleteUsers (and evicts the cache).
func (s *Service) DeleteUsers(ctx context.Context, userIDs []string) error {
	uuids := uuidsOrNull(userIDs)
	if err := s.st.deleteUsers(ctx, uuids); err != nil {
		return err
	}
	s.Invalidate(ctx, uuids...)
	return nil
}

// ChangeAuthorizationLevel is UserRepositoryImpl.changeAuthorizationLevel: it
// returns the number of updated rows. Kotlin did not evict userCache (stale
// authorizations for up to one hour); Go does (class B).
func (s *Service) ChangeAuthorizationLevel(ctx context.Context, userIDs []string, level int) (int, error) {
	uuids := uuidsOrNull(userIDs)
	n, err := s.st.updateAuthorizationLevel(ctx, uuids, level)
	if err != nil {
		return 0, err
	}
	s.Invalidate(ctx, uuids...)
	return n, nil
}

// Invalidate drops cached principals after a write, on every instance, and
// (coexistence) the Kotlin "userCache" entries.
func (s *Service) Invalidate(ctx context.Context, userIDs ...string) {
	if len(userIDs) == 0 {
		return
	}
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
