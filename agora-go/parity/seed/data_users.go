package seed

import (
	"fmt"
	"time"
)

// addUser inserts an agora_users row. lastConn is a time.Time or nil (NULL).
// password is always ” (what the Kotlin signup writes) and fcm_token is never
// NULL: the Kotlin UserDTO / UserInfo are non-null on those fields.
func (b *builder) addUser(id string, level int, created time.Time, fcm string, lastConn any, banned int) {
	b.add("agora_users", uid(id), level, created, fcm, lastConn, "", int16(banned))
}

// addEvent inserts a users_data row (signup / login).
func (b *builder) addEvent(eventType string, at time.Time, userID, fcm, ip, platform, userAgent, versionName, versionCode string) {
	b.add("users_data", b.rowID("users_data"), at, eventType, fcm, ip, platform, userAgent, userID, versionCode, versionName)
}

// platformOf gives a deterministic platform / UA / version for a user index.
func platformOf(i int) (platform, userAgent, versionName, versionCode string) {
	switch i % 3 {
	case 0:
		return "android", "Agora/1.4.0 (Android 14; Pixel 8)", "1.4.0", "140"
	case 1:
		return "ios", "Agora/1.4.0 (iPhone; iOS 17.4)", "1.4.0", "140"
	default:
		return "web", "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0", "1", "1"
	}
}

// signupAndLogin records the signup event of a user (and a login at lastConn when set).
func (b *builder) signupAndLogin(id string, idx int, created time.Time, fcm string, lastConn any) {
	platform, ua, vn, vc := platformOf(idx)
	ip := ipHash("user-" + id)
	b.addEvent("signup", created, id, fcm, ip, platform, ua, vn, vc)
	if lc, ok := lastConn.(time.Time); ok {
		b.addEvent("login", lc, id, fcm, ip, platform, ua, vn, vc)
	}
}

func (b *builder) buildUsers() {
	// ---- main regular users ------------------------------------------------
	type u struct {
		id       string
		created  time.Duration // age of the account
		fcm      string
		lastConn any // time.Duration (age of the last connection) or nil
	}
	regulars := []u{
		{UserRegular1, 120 * day, "fcm-regular-1", 1 * hour},
		{UserRegular2, 100 * day, "fcm-regular-2", 2 * day},
		// 3, 4, 5 share the same token: the CTE keeps the most recently created one (4).
		{UserRegular3, 80 * day, FcmTokenSharedA, 5 * day},
		{UserRegular4, 20 * day, FcmTokenSharedA, 30 * time.Minute},
		{UserRegular5, 60 * day, FcmTokenSharedA, 12 * day},
		// 6, 7 share another token: 7 wins.
		{UserRegular6, 40 * day, FcmTokenSharedB, 6 * day},
		{UserRegular7, 10 * day, FcmTokenSharedB, 4 * hour},
		// empty tokens: 9 (the most recent) wins the '' partition.
		{UserNoFcm1, 50 * day, "", nil},
		{UserNoFcm2, 5 * day, "", 3 * hour},
	}
	for i, r := range regulars {
		var lc any
		if d, ok := r.lastConn.(time.Duration); ok {
			lc = b.ago(d)
		}
		created := b.ago(r.created)
		b.addUser(r.id, 0, created, r.fcm, lc, 0)
		b.signupAndLogin(r.id, i, created, r.fcm, lc)
	}
	// a few extra logins for UserRegular1 (history)
	{
		platform, ua, vn, vc := platformOf(0)
		ip := ipHash("user-" + UserRegular1)
		b.addEvent("login", b.ago(9*day), UserRegular1, "fcm-regular-1", ip, platform, ua, vn, vc)
		b.addEvent("login", b.ago(33*day), UserRegular1, "fcm-regular-1", ip, platform, ua, "1.3.2", "132")
	}

	// ---- staff, banned, never connected, idle ---------------------------------
	staff := []struct {
		id      string
		level   int
		created time.Duration
		fcm     string
		lastC   any
		banned  int
	}{
		{UserPublisher, 8, 200 * day, "fcm-publisher", 1 * day, 0},
		{UserModerator, 42, 190 * day, "fcm-moderator", 2 * hour, 0},
		{UserAdmin, 1337, 300 * day, "fcm-admin", 40 * time.Minute, 0},
		{UserBanned, 0, 70 * day, "fcm-banned", 3 * day, 1},
		{UserNeverConnected, 0, 2 * day, "fcm-never-connected", nil, 0},
		{UserIdle, 0, 400 * day, "fcm-idle", 300 * day, 0},
	}
	for i, s := range staff {
		var lc any
		if d, ok := s.lastC.(time.Duration); ok {
			lc = b.ago(d)
		}
		created := b.ago(s.created)
		b.addUser(s.id, s.level, created, s.fcm, lc, s.banned)
		if s.id != UserIdle { // the idle user has no activity at all (no event either)
			b.signupAndLogin(s.id, i+1, created, s.fcm, lc)
		}
	}

	// ---- signup abuse groups ----------------------------------------------------
	group := func(ids []string, ip, ua string, when func(i int) time.Time, tokenPrefix string) {
		for i, id := range ids {
			created := when(i)
			fcm := fmt.Sprintf("%s-%d", tokenPrefix, i+1)
			b.addUser(id, 0, created, fcm, nil, 0)
			platform, _, vn, vc := platformOf(i)
			b.addEvent("signup", created, id, fcm, ip, platform, ua, vn, vc)
		}
	}
	// A: 3 signups the same day (day-2) from the same ip+UA -> flagged by the nightly job.
	group(UsersSuspectA, IPHashSuspectA, UserAgentSuspectA, func(i int) time.Time {
		return b.dayAt(2, 10, []int{0, 5, 9}[i])
	}, "fcm-suspect-a")
	// B: only 2 signups the same day -> not flagged.
	group(UsersPairB, IPHashPairB, UserAgentPairB, func(i int) time.Time {
		return b.dayAt(6, 11, []int{0, 30}[i])
	}, "fcm-pair-b")
	// C: 3 signups on 3 different days -> not flagged.
	group(UsersSpreadC, IPHashSpreadC, UserAgentSpreadC, func(i int) time.Time {
		return b.dayAt([]int{3, 5, 8}[i], 9, 0)
	}, "fcm-spread-c")
	// D: 4 signups the same day (day-9, still inside the 2 weeks window) -> flagged.
	group(UsersSuspectD, IPHashSuspectD, UserAgentSuspectD, func(i int) time.Time {
		return b.dayAt(9, 14, 3*i)
	}, "fcm-suspect-d")
	// E: 3 signups the same day but 20 days ago (outside the window) -> not flagged.
	group(UsersOldE, IPHashOldE, UserAgentOldE, func(i int) time.Time {
		return b.dayAt(20, 8, i)
	}, "fcm-old-e")
	// Cross day: same ip+UA as group A but another day -> flagged through CONCAT(ip, user_agent).
	group([]string{UserSuspectCrossDay}, IPHashSuspectA, UserAgentSuspectA, func(int) time.Time {
		return b.dayAt(12, 16, 0)
	}, "fcm-suspect-cross")
	// Mass: 10 signups same day (day-4) -> IsSuspiciousUser (>= 10) and flagged (>= 3).
	group(UsersMass, IPHashMass, UserAgentMass, func(i int) time.Time {
		return b.dayAt(4, 12, i)
	}, "fcm-mass")
	// Empty ip hash: 3 signups same day -> ignored by the nightly job (ip_address_hash != '').
	group(UsersEmptyIP, "", UserAgentEmptyIP, func(i int) time.Time {
		return b.dayAt(3, 15, i)
	}, "fcm-empty-ip")

	// ---- profiled users (also consultation participants) ----------------------
	for i, id := range UsersProfile {
		created := b.ago(time.Duration(30+i*7) * day)
		lc := b.ago(time.Duration(i+1) * 3 * hour)
		fcm := fmt.Sprintf("fcm-profile-%d", i+1)
		b.addUser(id, 0, created, fcm, lc, 0)
		b.signupAndLogin(id, i, created, fcm, lc)
	}
	{
		created := b.ago(33 * day)
		lc := b.ago(7 * hour)
		b.addUser(UserProfileInvalid, 0, created, "fcm-profile-invalid", lc, 0)
		b.signupAndLogin(UserProfileInvalid, 2, created, "fcm-profile-invalid", lc)
	}

	// Supporters: regular, non-banned users, in a fixed order.
	b.supporterPool = []string{
		UserRegular1, UserRegular2, UserRegular3, UserRegular4, UserRegular5, UserRegular6, UserRegular7,
		UserNoFcm1, UserNoFcm2, UserNeverConnected,
	}
	b.supporterPool = append(b.supporterPool, UsersProfile...)
	b.supporterPool = append(b.supporterPool, UserProfileInvalid)
	b.supporterPool = append(b.supporterPool, UsersSuspectA...)
	b.supporterPool = append(b.supporterPool, UsersPairB...)
	b.supporterPool = append(b.supporterPool, UsersSpreadC...)
	b.supporterPool = append(b.supporterPool, UsersSuspectD...)
	b.supporterPool = append(b.supporterPool, UsersOldE...)
	b.supporterPool = append(b.supporterPool, UsersEmptyIP...)
	b.supporterPool = append(b.supporterPool, UsersMass...)
}

// ---------------------------------------------------------------------------
// profiles
// ---------------------------------------------------------------------------

// profileRow mirrors users_profile; nil pointers are NULL.
type profileRow struct {
	gender, city, dept, job, vote, meeting, consult, primary, secondary any
	age                                                                 int // 0 = NULL year_of_birth
}

func (b *builder) addProfile(user string, p profileRow) {
	var yob any
	if p.age > 0 {
		yob = int32(b.now.Year() - p.age)
	}
	b.add("users_profile", b.rowID("users_profile"),
		p.city, p.consult, p.dept, p.gender, p.job, p.primary, p.meeting, p.secondary, uid(user), p.vote, yob)
}

func (b *builder) buildProfiles() {
	genders := []string{"M", "F", "A"}
	cities := []string{"R", "U", "A"}
	jobs := []string{"AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN"}
	freqs := []string{"S", "P", "J"}
	depts := []string{"75", "13", "2A", "2B", "69", "31", "59", "33", "06", "34", "971", "99"}
	// territory names, as stored in primary_department / secondary_department
	// (Territoire.Departement.value, NOT the code).
	territories := []string{"Paris", "Bouches-du-Rhône", "Corse-du-Sud", "Haute-Corse", "Rhône", "Haute-Garonne", "Nord", "Gironde", "Alpes-Maritimes", "Hérault", "Guadeloupe", "Ain"}
	ages := []int{16, 22, 30, 40, 50, 60, 72, 19, 27, 38, 48, 66}

	// Main users.
	b.addProfile(UserRegular1, profileRow{"M", "U", "75", "CA", "S", "P", "S", "Paris", "Bouches-du-Rhône", 34})
	b.addProfile(UserRegular2, profileRow{"F", "U", "13", "PI", "P", "J", "S", "Bouches-du-Rhône", "Paris", 45})
	b.addProfile(UserRegular3, profileRow{"A", "R", "2A", "AG", "J", "J", "J", "Corse-du-Sud", "Haute-Corse", 29})
	b.addProfile(UserRegular4, profileRow{"F", "U", "69", "RE", "S", "S", "P", "Rhône", nil, 71})
	b.addProfile(UserRegular5, profileRow{"M", "A", "99", "UN", "P", "P", "P", nil, "Paris", 58})
	// departments only (what POST /profile/departments writes: INSERT ... ON CONFLICT).
	b.addProfile(UserRegular6, profileRow{primary: "Nord"})
	// completely empty row (isCompleted() == false).
	b.addProfile(UserRegular7, profileRow{})

	// 12 profile users: every code value of every column shows up at least once.
	for i, id := range UsersProfile {
		p := profileRow{
			gender:  genders[i%3],
			city:    cities[(i/3)%3],
			dept:    depts[i],
			job:     jobs[i%10],
			vote:    freqs[i%3],
			meeting: freqs[(i/2)%3],
			consult: freqs[(i+1)%3],
			primary: territories[i%len(territories)],
			age:     ages[i],
		}
		if i%2 == 0 {
			p.secondary = territories[(i+3)%len(territories)]
		}
		if i == 11 {
			p.age = 0 // NULL year_of_birth
		}
		b.addProfile(id, p)
	}
	// unknown codes -> every mapper falls back to null.
	b.addProfile(UserProfileInvalid, profileRow{"Z", "?", "ZZ", "XX", "Q", "Q", "Q", "Atlantis", "Mordor", 33})

	// demographic_info_ask_date: one recent, one old, one in between.
	b.add("demographic_info_ask_date", b.rowID("demographic_info_ask_date"), b.ago(2*day), uid(UserRegular1))
	b.add("demographic_info_ask_date", b.rowID("demographic_info_ask_date"), b.ago(200*day), uid(UserRegular2))
	b.add("demographic_info_ask_date", b.rowID("demographic_info_ask_date"), b.ago(45*day), uid(UserRegular3))
}
