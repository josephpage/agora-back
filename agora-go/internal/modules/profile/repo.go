package profile

import (
	"context"
	"time"

	"agora/internal/app"
	"agora/internal/cache"
	"agora/internal/domain"
	"agora/internal/javacompat"
	"agora/internal/modules/users"
)

const (
	profileCacheName = "profileCache" // Kotlin default cache manager: 1 hour
	profileCacheTTL  = time.Hour
	askDateCacheName = "demographicInfoAskDate" // shortTermCacheManager: 5 minutes
	askDateCacheTTL  = 5 * time.Minute
)

// ----------------------------------------------------------------------------
// ProfileMapper

// toDomain is ProfileMapper.toDomain.
func toDomain(dto *profileRow) Profile {
	p := Profile{
		Gender:                 toGender(dto.Gender),
		YearOfBirth:            dto.YearOfBirth,
		Department:             domain.FindDepartmentByCode(dto.Department),
		CityType:               toCityType(dto.CityType),
		JobCategory:            toJobCategory(dto.JobCategory),
		VoteFrequency:          toFrequency(dto.VoteFrequency),
		PublicMeetingFrequency: toFrequency(dto.PublicMeetingFrequency),
		ConsultationFrequency:  toFrequency(dto.ConsultationFrequency),
	}
	if dto.PrimaryDepartment != nil {
		p.PrimaryDepartment = domain.DepartementFrom(*dto.PrimaryDepartment)
	}
	if dto.SecondaryDepartment != nil {
		p.SecondaryDepartment = domain.DepartementFrom(*dto.SecondaryDepartment)
	}
	return p
}

// toDto is ProfileMapper.toDto: nil when the user id is not a UUID.
func toDto(in ProfileInserting) *profileRow {
	uid, ok := javacompat.ToUUIDOrNull(in.UserID)
	if !ok {
		return nil
	}
	return &profileRow{
		ID:                     users.RandomUUID(),
		Gender:                 fromGender(in.Gender),
		YearOfBirth:            in.YearOfBirth,
		Department:             domain.DepartmentCode(in.Department),
		CityType:               fromCityType(in.CityType),
		JobCategory:            fromJobCategory(in.JobCategory),
		VoteFrequency:          fromFrequency(in.VoteFrequency),
		PublicMeetingFrequency: fromFrequency(in.PublicMeetingFrequency),
		ConsultationFrequency:  fromFrequency(in.ConsultationFrequency),
		UserID:                 uid,
	}
}

// updateProfile is ProfileMapper.updateProfile: the new values, the old id and
// the old departments.
func updateProfile(old, new *profileRow) *profileRow {
	return &profileRow{
		ID:                     old.ID,
		Gender:                 new.Gender,
		YearOfBirth:            new.YearOfBirth,
		Department:             new.Department,
		CityType:               new.CityType,
		JobCategory:            new.JobCategory,
		VoteFrequency:          new.VoteFrequency,
		PublicMeetingFrequency: new.PublicMeetingFrequency,
		ConsultationFrequency:  new.ConsultationFrequency,
		UserID:                 new.UserID,
		PrimaryDepartment:      old.PrimaryDepartment,
		SecondaryDepartment:    old.SecondaryDepartment,
	}
}

// ----------------------------------------------------------------------------
// ProfileCacheRepository

// cacheState is ProfileCacheRepository.CacheResult.
type cacheState int

const (
	cacheNotInitialized cacheState = iota
	cacheProfileNotFound
	cacheProfile
)

type profileCacheResult struct {
	state cacheState
	row   *profileRow
}

// profileCache is ProfileCacheRepository.
type profileCache interface {
	getProfile(userUUID string) profileCacheResult
	insertProfile(ctx context.Context, userUUID string, row *profileRow)
}

// l1ProfileCache keeps the saved ProfileDTOs for one hour in the process
// cache (Kotlin: Redis "profileCache"). Only updateProfile reads it, to find
// the previous row: a departments-only update (native SQL) does not refresh it.
type l1ProfileCache struct{ a *app.App }

func (c l1ProfileCache) getProfile(userUUID string) profileCacheResult {
	if v, ok := c.a.Cache.Get(profileCacheName, userUUID); ok {
		return profileCacheResult{state: cacheProfile, row: v.(*profileRow)}
	}
	return profileCacheResult{state: cacheNotInitialized}
}

func (c l1ProfileCache) insertProfile(ctx context.Context, userUUID string, row *profileRow) {
	c.a.Cache.Put(profileCacheName, userUUID, row, profileCacheTTL)
	// coexistence: the Kotlin entry is now older than the database
	c.a.Cache.DeleteKotlinKeys(ctx, cache.KotlinKey(profileCacheName, userUUID))
}

// ----------------------------------------------------------------------------
// ProfileRepositoryImpl

// ProfileRepository is usecase/profile/repository/ProfileRepository.
type ProfileRepository interface {
	GetProfile(ctx context.Context, userID string) (*Profile, error)
	UpdateProfile(ctx context.Context, in ProfileInserting) (ProfileEditResult, error)
	InsertProfile(ctx context.Context, in ProfileInserting) (ProfileEditResult, error)
	DeleteUsersProfile(ctx context.Context, userIDs []string) error
	UpdateDepartments(ctx context.Context, userID string, primary, secondary *domain.Departement) error
}

type profileRepository struct {
	db    profileDB
	cache profileCache
}

// GetProfile is getProfile: always read from the database (the cache is
// only read by updateProfile).
func (r *profileRepository) GetProfile(ctx context.Context, userID string) (*Profile, error) {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		return nil, nil
	}
	row, err := r.db.getProfile(ctx, uid)
	if err != nil || row == nil {
		return nil, err
	}
	p := toDomain(row)
	return &p, nil
}

func (r *profileRepository) InsertProfile(ctx context.Context, in ProfileInserting) (ProfileEditResult, error) {
	dto := toDto(in)
	if dto == nil {
		return ProfileEditFailure, nil
	}
	saved, err := r.db.save(ctx, dto)
	if err != nil {
		return ProfileEditFailure, err
	}
	r.cache.insertProfile(ctx, saved.UserID, saved)
	return ProfileEditSuccess, nil
}

func (r *profileRepository) UpdateProfile(ctx context.Context, in ProfileInserting) (ProfileEditResult, error) {
	newDTO := toDto(in)
	if newDTO == nil {
		return ProfileEditFailure, nil
	}
	var old *profileRow
	switch res := r.cache.getProfile(newDTO.UserID); res.state {
	case cacheNotInitialized:
		var err error
		if old, err = r.db.getProfile(ctx, newDTO.UserID); err != nil {
			return ProfileEditFailure, err
		}
	case cacheProfileNotFound:
		old = nil
	case cacheProfile:
		old = res.row
	}
	if old == nil {
		return ProfileEditFailure, nil
	}
	saved, err := r.db.save(ctx, updateProfile(old, newDTO))
	if err != nil {
		return ProfileEditFailure, err
	}
	r.cache.insertProfile(ctx, saved.UserID, saved)
	return ProfileEditSuccess, nil
}

func (r *profileRepository) DeleteUsersProfile(ctx context.Context, userIDs []string) error {
	return r.db.deleteUsersProfile(ctx, uuidsOrNull(userIDs))
}

// UpdateDepartments is updateDepartments (`userId.toUuidOrNull()!!`).
func (r *profileRepository) UpdateDepartments(ctx context.Context, userID string, primary, secondary *domain.Departement) error {
	uid, ok := javacompat.ToUUIDOrNull(userID)
	if !ok {
		panic("NullPointerException: userId is not a UUID")
	}
	var p, s *string
	if primary != nil {
		v := primary.Value()
		p = &v
	}
	if secondary != nil {
		v := secondary.Value()
		s = &v
	}
	return r.db.insertDepartments(ctx, uid, p, s)
}

func uuidsOrNull(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			out = append(out, u)
		}
	}
	return out
}
