package profile

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"agora/internal/domain"
)

var userUUID = "bc9e81be-eb4d-11ed-a05b-0242ac120003"

// ---------------------------------------------------------------------------
// ProfileRepositoryImplTest

type fakeProfileDB struct {
	calls  []string
	byUser map[string]*profileRow
	saved  []*profileRow
}

func (f *fakeProfileDB) getProfile(_ context.Context, u string) (*profileRow, error) {
	f.calls = append(f.calls, "db.getProfile "+u)
	return f.byUser[u], nil
}
func (f *fakeProfileDB) save(_ context.Context, r *profileRow) (*profileRow, error) {
	f.calls = append(f.calls, "db.save")
	f.saved = append(f.saved, r)
	return r, nil
}
func (f *fakeProfileDB) deleteUsersProfile(_ context.Context, ids []string) error {
	f.calls = append(f.calls, "db.delete")
	return nil
}
func (f *fakeProfileDB) insertDepartments(context.Context, string, *string, *string) error {
	f.calls = append(f.calls, "db.insertDepartments")
	return nil
}

type fakeProfileCache struct {
	calls  *[]string
	result profileCacheResult
	puts   []*profileRow
}

func (f *fakeProfileCache) getProfile(u string) profileCacheResult {
	*f.calls = append(*f.calls, "cache.getProfile "+u)
	return f.result
}
func (f *fakeProfileCache) insertProfile(_ context.Context, u string, r *profileRow) {
	*f.calls = append(*f.calls, "cache.insertProfile "+u)
	f.puts = append(f.puts, r)
}

func newProfileRepo(db *fakeProfileDB, res profileCacheResult) (*profileRepository, *fakeProfileCache) {
	calls := &db.calls
	c := &fakeProfileCache{calls: calls, result: res}
	return &profileRepository{db: db, cache: c}, c
}

func ptr[T any](v T) *T { return &v }

func TestProfileRepositoryGetProfile(t *testing.T) {
	ctx := context.Background()
	t.Run("invalid UUID: nothing is touched", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, _ := newProfileRepo(db, profileCacheResult{})
		got, err := repo.GetProfile(ctx, "invalid UUID")
		if got != nil || err != nil || len(db.calls) != 0 {
			t.Errorf("got %v %v %v", got, err, db.calls)
		}
	})
	t.Run("success: mapped object read from the database only (never the cache)", func(t *testing.T) {
		db := &fakeProfileDB{byUser: map[string]*profileRow{userUUID: {ID: "id", UserID: userUUID, Gender: ptr("F"), YearOfBirth: ptr(1990), Department: ptr("03"), PrimaryDepartment: ptr("doubs")}}}
		repo, _ := newProfileRepo(db, profileCacheResult{state: cacheProfile, row: &profileRow{Gender: ptr("M")}})
		got, err := repo.GetProfile(ctx, userUUID)
		if err != nil || got == nil || got.Gender != GenderFeminin || *got.YearOfBirth != 1990 ||
			got.Department.Name != "ALLIER_03" || got.PrimaryDepartment.Value() != "Doubs" || got.SecondaryDepartment != nil {
			t.Fatalf("got %+v %v", got, err)
		}
		if !reflect.DeepEqual(db.calls, []string{"db.getProfile " + userUUID}) {
			t.Errorf("calls %v", db.calls)
		}
	})
	t.Run("no row: null", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, _ := newProfileRepo(db, profileCacheResult{})
		if got, _ := repo.GetProfile(ctx, userUUID); got != nil {
			t.Errorf("got %v", got)
		}
	})
}

func TestProfileRepositoryInsertProfile(t *testing.T) {
	ctx := context.Background()
	t.Run("mapper returns null (invalid user id): FAILURE without any interaction", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, c := newProfileRepo(db, profileCacheResult{})
		res, _ := repo.InsertProfile(ctx, ProfileInserting{UserID: "not a uuid"})
		if res != ProfileEditFailure || len(db.calls) != 0 || len(c.puts) != 0 {
			t.Errorf("res %v calls %v", res, db.calls)
		}
	})
	t.Run("saves the DTO then puts it in the cache: SUCCESS", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, c := newProfileRepo(db, profileCacheResult{})
		res, err := repo.InsertProfile(ctx, ProfileInserting{UserID: userUUID, Gender: GenderAutre, YearOfBirth: ptr(1980)})
		if res != ProfileEditSuccess || err != nil {
			t.Fatalf("res %v err %v", res, err)
		}
		want := []string{"db.save", "cache.insertProfile " + userUUID}
		if !reflect.DeepEqual(db.calls, want) {
			t.Errorf("calls %v", db.calls)
		}
		if r := c.puts[0]; *r.Gender != "A" || *r.YearOfBirth != 1980 || r.UserID != userUUID || r.PrimaryDepartment != nil || len(r.ID) != 36 {
			t.Errorf("row %+v", r)
		}
	})
}

func TestProfileRepositoryUpdateProfile(t *testing.T) {
	ctx := context.Background()
	in := ProfileInserting{UserID: userUUID, Gender: GenderFeminin}
	old := &profileRow{ID: "old-id", UserID: userUUID, Gender: ptr("M"), PrimaryDepartment: ptr("Paris"), SecondaryDepartment: ptr("Nord")}

	t.Run("mapper returns null: FAILURE without any interaction", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, _ := newProfileRepo(db, profileCacheResult{})
		if res, _ := repo.UpdateProfile(ctx, ProfileInserting{UserID: "x"}); res != ProfileEditFailure || len(db.calls) != 0 {
			t.Errorf("res %v calls %v", res, db.calls)
		}
	})
	t.Run("CachedProfileNotFound: FAILURE, only the cache is read", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, _ := newProfileRepo(db, profileCacheResult{state: cacheProfileNotFound})
		res, _ := repo.UpdateProfile(ctx, in)
		if res != ProfileEditFailure || !reflect.DeepEqual(db.calls, []string{"cache.getProfile " + userUUID}) {
			t.Errorf("res %v calls %v", res, db.calls)
		}
	})
	t.Run("CachedProfile: updates from the cached row (id and departments kept) and refreshes the cache", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, c := newProfileRepo(db, profileCacheResult{state: cacheProfile, row: old})
		res, _ := repo.UpdateProfile(ctx, in)
		want := []string{"cache.getProfile " + userUUID, "db.save", "cache.insertProfile " + userUUID}
		if res != ProfileEditSuccess || !reflect.DeepEqual(db.calls, want) {
			t.Fatalf("res %v calls %v", res, db.calls)
		}
		r := c.puts[0]
		if r.ID != "old-id" || *r.Gender != "F" || *r.PrimaryDepartment != "Paris" || *r.SecondaryDepartment != "Nord" {
			t.Errorf("row %+v", r)
		}
	})
	t.Run("CacheNotInitialized and no row in the database: FAILURE", func(t *testing.T) {
		db := &fakeProfileDB{}
		repo, _ := newProfileRepo(db, profileCacheResult{state: cacheNotInitialized})
		res, _ := repo.UpdateProfile(ctx, in)
		want := []string{"cache.getProfile " + userUUID, "db.getProfile " + userUUID}
		if res != ProfileEditFailure || !reflect.DeepEqual(db.calls, want) {
			t.Errorf("res %v calls %v", res, db.calls)
		}
	})
	t.Run("CacheNotInitialized and a row in the database: updates and fills the cache", func(t *testing.T) {
		db := &fakeProfileDB{byUser: map[string]*profileRow{userUUID: old}}
		repo, _ := newProfileRepo(db, profileCacheResult{state: cacheNotInitialized})
		res, _ := repo.UpdateProfile(ctx, in)
		want := []string{"cache.getProfile " + userUUID, "db.getProfile " + userUUID, "db.save", "cache.insertProfile " + userUUID}
		if res != ProfileEditSuccess || !reflect.DeepEqual(db.calls, want) {
			t.Errorf("res %v calls %v", res, db.calls)
		}
	})
}

// ---------------------------------------------------------------------------
// DemographicInfoAskDateRepositoryImplTest

type fakeAskDB struct {
	calls []string
	row   *askDateRow
}

func (f *fakeAskDB) getAskDate(_ context.Context, u string) (*askDateRow, error) {
	f.calls = append(f.calls, "db.getAskDate")
	return f.row, nil
}
func (f *fakeAskDB) save(_ context.Context, r *askDateRow) (*askDateRow, error) {
	f.calls = append(f.calls, "db.save")
	return r, nil
}
func (f *fakeAskDB) deleteAskDate(context.Context, string) error {
	f.calls = append(f.calls, "db.deleteAskDate")
	return nil
}

type fakeAskCache struct {
	calls *[]string
	date  *time.Time
}

func (f *fakeAskCache) getDate(string) *time.Time {
	*f.calls = append(*f.calls, "cache.getDate")
	return f.date
}
func (f *fakeAskCache) insertDate(_ context.Context, _ string, d time.Time) {
	*f.calls = append(*f.calls, "cache.insertDate "+d.Format("2006-01-02"))
}
func (f *fakeAskCache) deleteDate(context.Context, string) {
	*f.calls = append(*f.calls, "cache.deleteDate")
}

func TestAskDateRepository(t *testing.T) {
	ctx := context.Background()
	askLocalDate := time.Date(2023, time.September, 28, 0, 0, 0, 0, time.UTC)
	stamp := time.Date(2023, time.September, 28, 15, 4, 5, 0, time.Local)
	now := time.Date(2024, 3, 1, 10, 0, 0, 0, time.Local)
	build := func(db *fakeAskDB, cached *time.Time) *askDateRepository {
		return &askDateRepository{db: db, cache: &fakeAskCache{calls: &db.calls, date: cached}, now: func() time.Time { return now }}
	}

	t.Run("getDate with an invalid user UUID returns null", func(t *testing.T) {
		db := &fakeAskDB{}
		if got, _ := build(db, nil).GetDate(ctx, "Invalid user UUID"); got != nil || len(db.calls) != 0 {
			t.Errorf("got %v calls %v", got, db.calls)
		}
	})
	t.Run("getDate when the cache is initialized returns the cached date only", func(t *testing.T) {
		db := &fakeAskDB{}
		got, _ := build(db, &askLocalDate).GetDate(ctx, userUUID)
		if got == nil || !got.Equal(askLocalDate) || !reflect.DeepEqual(db.calls, []string{"cache.getDate"}) {
			t.Errorf("got %v calls %v", got, db.calls)
		}
	})
	t.Run("getDate when not initialized reads the database, fills the cache, returns the date", func(t *testing.T) {
		db := &fakeAskDB{row: &askDateRow{ID: "i", UserID: userUUID, AskDate: &stamp}}
		got, _ := build(db, nil).GetDate(ctx, userUUID)
		want := []string{"cache.getDate", "db.getAskDate", "cache.insertDate 2023-09-28"}
		if got == nil || !got.Equal(askLocalDate) || !reflect.DeepEqual(db.calls, want) {
			t.Errorf("got %v calls %v", got, db.calls)
		}
	})
	t.Run("getDate without any row returns null and fills nothing", func(t *testing.T) {
		db := &fakeAskDB{}
		if got, _ := build(db, nil).GetDate(ctx, userUUID); got != nil || len(db.calls) != 2 {
			t.Errorf("got %v calls %v", got, db.calls)
		}
	})
	t.Run("insertDate with an invalid UUID does nothing", func(t *testing.T) {
		db := &fakeAskDB{}
		_ = build(db, nil).InsertDate(ctx, "Invalid user UUID")
		if len(db.calls) != 0 {
			t.Errorf("calls %v", db.calls)
		}
	})
	t.Run("insertDate saves a row for today and caches the date", func(t *testing.T) {
		db := &fakeAskDB{}
		_ = build(db, nil).InsertDate(ctx, userUUID)
		if !reflect.DeepEqual(db.calls, []string{"db.save", "cache.insertDate 2024-03-01"}) {
			t.Errorf("calls %v", db.calls)
		}
	})
	t.Run("deleteDate with an invalid UUID does nothing", func(t *testing.T) {
		db := &fakeAskDB{}
		_ = build(db, nil).DeleteDate(ctx, "Invalid user UUID")
		if len(db.calls) != 0 {
			t.Errorf("calls %v", db.calls)
		}
	})
	t.Run("deleteDate evicts the cache then deletes the rows", func(t *testing.T) {
		db := &fakeAskDB{}
		_ = build(db, nil).DeleteDate(ctx, userUUID)
		if !reflect.DeepEqual(db.calls, []string{"cache.deleteDate", "db.deleteAskDate"}) {
			t.Errorf("calls %v", db.calls)
		}
	})
	t.Run("a NULL ask_date is a NullPointerException", func(t *testing.T) {
		db := &fakeAskDB{row: &askDateRow{ID: "i", UserID: userUUID}}
		defer func() {
			if recover() == nil {
				t.Error("expected a panic")
			}
		}()
		_, _ = build(db, nil).GetDate(ctx, userUUID)
	})
}

// ---------------------------------------------------------------------------
// AskForDemographicInfoUseCaseTest

type fakeProfiles struct {
	calls   *[]string
	profile *Profile
}

func (f fakeProfiles) GetProfile(_ context.Context, id string) (*Profile, error) {
	*f.calls = append(*f.calls, "profile.get "+id)
	return f.profile, nil
}
func (f fakeProfiles) UpdateProfile(context.Context, ProfileInserting) (ProfileEditResult, error) {
	return ProfileEditSuccess, nil
}
func (f fakeProfiles) InsertProfile(context.Context, ProfileInserting) (ProfileEditResult, error) {
	return ProfileEditSuccess, nil
}
func (f fakeProfiles) DeleteUsersProfile(context.Context, []string) error { return nil }
func (f fakeProfiles) UpdateDepartments(context.Context, string, *domain.Departement, *domain.Departement) error {
	return nil
}

type fakeAnswered struct {
	calls *[]string
	ids   []string
}

func (f fakeAnswered) GetAnsweredConsultationIds(_ context.Context, id string) ([]string, error) {
	*f.calls = append(*f.calls, "answered.get "+id)
	return f.ids, nil
}

type fakeAskDates struct {
	calls *[]string
	date  *time.Time
}

func (f fakeAskDates) GetDate(_ context.Context, id string) (*time.Time, error) {
	*f.calls = append(*f.calls, "askDate.get "+id)
	return f.date, nil
}
func (f fakeAskDates) InsertDate(_ context.Context, id string) error {
	*f.calls = append(*f.calls, "askDate.insert "+id)
	return nil
}
func (f fakeAskDates) DeleteDate(context.Context, string) error { return nil }

func TestAskForDemographicInfo(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2024, 3, 31, 9, 0, 0, 0, time.Local)
	full := &Profile{
		Gender: GenderFeminin, YearOfBirth: ptr(1990), Department: domain.FindDepartmentByCode(ptr("03")),
		CityType: CityUrbain, JobCategory: JobOuvrier, VoteFrequency: FrequencyJamais,
		PublicMeetingFrequency: FrequencyParfois, ConsultationFrequency: FrequencySouvent,
		PrimaryDepartment: domain.DepartementFrom("Doubs"), SecondaryDepartment: domain.DepartementFrom("Nord"),
	}
	day := func(daysAgo int) *time.Time {
		d := civilDate(now).AddDate(0, 0, -daysAgo)
		return &d
	}
	run := func(excluded string, profile *Profile, answered []string, date *time.Time, consultationID string) (bool, []string) {
		calls := &[]string{}
		uc := &AskForDemographicInfoUseCase{
			answered: fakeAnswered{calls: calls, ids: answered}, profiles: fakeProfiles{calls: calls, profile: profile},
			askDates: fakeAskDates{calls: calls, date: date}, excludedIDs: excluded, now: func() time.Time { return now },
		}
		got, err := uc.AskForDemographicInfo(ctx, "1234", consultationID)
		if err != nil {
			t.Fatal(err)
		}
		return got, *calls
	}
	same := func(t *testing.T, name string, got bool, calls []string, want bool, wantCalls ...string) {
		t.Helper()
		if got != want || !reflect.DeepEqual(append([]string(nil), calls...), append([]string(nil), wantCalls...)) {
			t.Errorf("%s: got %v calls %v, want %v calls %v", name, got, calls, want, wantCalls)
		}
	}

	got, calls := run("excluded-doc-id", full, nil, nil, "excluded-doc-id")
	same(t, "excluded single entry", got, calls, false)
	got, calls = run("other-id, excluded-doc-id , another-id", full, nil, nil, "excluded-doc-id")
	same(t, "excluded among several, trimmed", got, calls, false)
	got, calls = run("other-id, another-id", full, nil, nil, "not-excluded-id")
	same(t, "not excluded follows the normal behaviour", got, calls, false, "profile.get 1234")
	got, calls = run("", full, nil, nil, "any-consultation-id")
	same(t, "empty list", got, calls, false, "profile.get 1234")
	got, calls = run(" , ,", full, nil, nil, "")
	same(t, "blank entries never match", got, calls, false, "profile.get 1234")
	got, calls = run("a", full, nil, nil, "consultId")
	same(t, "completed profile", got, calls, false, "profile.get 1234")
	got, calls = run("", nil, nil, nil, "consultId")
	same(t, "no profile, no answered consultation", got, calls, false, "profile.get 1234", "answered.get 1234")
	got, calls = run("", &Profile{PrimaryDepartment: full.PrimaryDepartment}, []string{"c1"}, nil, "consultId")
	same(t, "departments only profile is not completed: first ask", got, calls, true, "profile.get 1234", "answered.get 1234", "askDate.get 1234", "askDate.insert 1234")
	got, calls = run("", nil, []string{"consultationId1"}, nil, "consultId")
	same(t, "no profile, answered once, no ask date", got, calls, true, "profile.get 1234", "answered.get 1234", "askDate.get 1234", "askDate.insert 1234")
	got, calls = run("", nil, []string{"consultationId1"}, day(31), "consultId")
	same(t, "ask date 31 days ago", got, calls, true, "profile.get 1234", "answered.get 1234", "askDate.get 1234", "askDate.insert 1234")
	got, calls = run("", nil, []string{"consultationId1"}, day(30), "consultId")
	same(t, "ask date exactly 30 days ago", got, calls, false, "profile.get 1234", "answered.get 1234", "askDate.get 1234")
	got, calls = run("", nil, []string{"consultationId1"}, day(15), "consultId")
	same(t, "ask date 15 days ago", got, calls, false, "profile.get 1234", "answered.get 1234", "askDate.get 1234")
}

func TestProfileIsCompleted(t *testing.T) {
	if (Profile{}).IsCompleted() || (Profile{PrimaryDepartment: domain.DepartementFrom("Paris")}).IsCompleted() {
		t.Error("an empty or departments-only profile is not completed")
	}
	for _, p := range []Profile{
		{Gender: GenderAutre}, {YearOfBirth: ptr(0)}, {Department: domain.FindDepartmentByCode(ptr("75"))}, {CityType: CityRural},
		{JobCategory: JobUnknown}, {VoteFrequency: FrequencyJamais}, {PublicMeetingFrequency: FrequencyJamais}, {ConsultationFrequency: FrequencyJamais},
	} {
		if !p.IsCompleted() {
			t.Errorf("%+v must be completed", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Use cases / mappers

func TestUpdateDepartmentsUseCase(t *testing.T) {
	ctx := context.Background()
	names := func(v ...any) ProfileDepartmentJSON {
		var in ProfileDepartmentJSON
		for _, e := range v {
			if e == nil {
				in.Departments = append(in.Departments, nil)
			} else {
				in.Departments = append(in.Departments, ptr(e.(string)))
			}
		}
		return in
	}
	repo := fakeProfiles{calls: &[]string{}}
	if err := updateDepartments(ctx, repo, "u", names("Paris", "Nord", "Ain")); !errors.Is(err, ErrInvalidNumberOfDepartments) {
		t.Errorf("3 departments: %v", err)
	}
	if err := updateDepartments(ctx, repo, "u", names("x", "y", "z")); !errors.Is(err, ErrInvalidNumberOfDepartments) {
		t.Errorf("3 invalid departments: the count is checked first: %v", err)
	}
	var te *domain.InvalidTerritoryError
	if err := updateDepartments(ctx, repo, "u", names("Paris", "Atlantis")); !errors.As(err, &te) || te.Error() != "Le territoire Atlantis n'existe pas." {
		t.Errorf("unknown territory: %v", err)
	}
	if err := updateDepartments(ctx, repo, "u", names(nil)); err == nil || errors.As(err, &te) || errors.Is(err, ErrInvalidNumberOfDepartments) {
		t.Errorf("null element must be an unexpected error: %v", err)
	}
	if err := updateDepartments(ctx, repo, "u", names("Atlantis", nil)); !errors.As(err, &te) {
		t.Errorf("the invalid territory comes before the null element: %v", err)
	}
	for _, ok := range []ProfileDepartmentJSON{names(), names("paris"), names("PARIS", "nord")} {
		if err := updateDepartments(ctx, repo, "u", ok); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
}

func TestProfileJSONMapper(t *testing.T) {
	s := ptr[string]
	in := ProfileJSON{Gender: s("F"), YearOfBirth: s("1990"), Department: s("2A"), CityType: s("U"), JobCategory: s("UN"),
		VoteFrequency: s("S"), PublicMeetingFrequency: s("P"), ConsultationFrequency: s("J"), PrimaryDepartment: s("Paris")}
	d := ToDomain(in, "u")
	if d.Gender != GenderFeminin || *d.YearOfBirth != 1990 || d.Department.Name != "CORSEDUSUD_2A" || d.CityType != CityUrbain ||
		d.JobCategory != JobUnknown || d.VoteFrequency != FrequencySouvent || d.PublicMeetingFrequency != FrequencyParfois ||
		d.ConsultationFrequency != FrequencyJamais || d.UserID != "u" {
		t.Errorf("%+v", d)
	}
	for _, bad := range []string{"abc", "", "1990.5", "99999999999", "1 990"} {
		if got := ToDomain(ProfileJSON{YearOfBirth: s(bad)}, "u").YearOfBirth; got != nil {
			t.Errorf("%q: %d", bad, *got)
		}
	}
	for in, want := range map[string]int{"+1990": 1990, "-5": -5, "0001990": 1990} {
		if got := ToDomain(ProfileJSON{YearOfBirth: s(in)}, "u").YearOfBirth; got == nil || *got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
	// the output: yearOfBirth is a String, departments are written by value
	out := ToJSON(Profile{Gender: GenderMasculin, YearOfBirth: ptr(1985), Department: domain.FindDepartmentByCode(ptr("971")),
		PrimaryDepartment: domain.DepartementFrom("PARIS"), SecondaryDepartment: domain.DepartementFrom("nord")})
	if *out.Gender != "M" || *out.YearOfBirth != "1985" || *out.Department != "971" || *out.PrimaryDepartment != "Paris" || *out.SecondaryDepartment != "Nord" || out.CityType != nil {
		t.Errorf("%+v", out)
	}
}

func TestCodeMappingsRoundTrip(t *testing.T) {
	for _, c := range []string{"M", "F", "A"} {
		if got := fromGender(toGender(&c)); got == nil || *got != c {
			t.Errorf("gender %s", c)
		}
	}
	for _, c := range []string{"R", "U", "A"} {
		if got := fromCityType(toCityType(&c)); got == nil || *got != c {
			t.Errorf("city %s", c)
		}
	}
	for _, c := range []string{"AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN"} {
		if got := fromJobCategory(toJobCategory(&c)); got == nil || *got != c {
			t.Errorf("job %s", c)
		}
	}
	for _, c := range []string{"S", "P", "J"} {
		if got := fromFrequency(toFrequency(&c)); got == nil || *got != c {
			t.Errorf("frequency %s", c)
		}
	}
	for _, c := range []string{"", "m", "X", "Z", "ZZ", "s", " S"} {
		if toGender(&c) != "" || toCityType(&c) != "" || toJobCategory(&c) != "" || toFrequency(&c) != "" {
			t.Errorf("%q must map to null", c)
		}
	}
}
