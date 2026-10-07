package seed

import (
	"fmt"
	"math"
	"time"
)

// Bulk data (scale > 1). Everything here is generated with the builder's
// math/rand source (fixed seed) in a fixed order, so two runs with the same
// (now, scale) produce byte-identical tables. The handcrafted set (scale = 1)
// is never altered: bulk rows use their own id ranges:
//
//	users: 00000000-0000-4000-9001-<n>
//	qags:  00000000-0000-4000-8001-<n>
const (
	bulkUsersPerScale  = 40
	bulkQagsPerScale   = 30
	bulkParticipations = 25 // per extra scale unit, for each of consultations 1, 4 and 7
)

func bulkUserID(n int) string { return fmt.Sprintf("00000000-0000-4000-9001-%012x", n) }
func bulkQagID(n int) string  { return fmt.Sprintf("00000000-0000-4000-8001-%012x", n) }

var (
	bulkSubjects = []string{"l’écologie", "le transport", "la santé", "l’éducation", "le logement", "l’emploi", "l’énergie", "la culture", "la sécurité", "le numérique"}
	bulkPlaces   = []string{"les zones rurales", "les grandes villes", "les territoires d’outre-mer", "les quartiers prioritaires", "les petites communes", "les régions frontalières"}
	bulkNames    = []string{"Camille", "Léo", "Inès", "Hugo", "Zoé", "Nathan", "Jade", "Louis", "Emma", "Adam", "Chloé", "Lina", "Mathis", "Sarah", "Yanis"}
	bulkMotifs   = []string{"motif_hors_sujet", "motif_doublon", "motif_injure", "motif_publicite"}
	bulkDepts    = []string{"01", "06", "13", "21", "2A", "2B", "31", "33", "34", "35", "38", "44", "59", "62", "67", "69", "75", "76", "92", "971"}
)

func (b *builder) buildBulk() {
	m := b.scale - 1
	if m <= 0 {
		return
	}
	rng := b.rng
	nUsers := bulkUsersPerScale * m
	nQags := bulkQagsPerScale * m

	// ---- users ------------------------------------------------------------------
	users := make([]string, 0, nUsers)
	banned := map[string]bool{}
	for i := 1; i <= nUsers; i++ {
		id := bulkUserID(i)
		users = append(users, id)
		created := b.ago(time.Duration(rng.Intn(400*24*60)+60) * time.Minute)
		fcm := fmt.Sprintf("fcm-bulk-%d", i)
		if i%20 == 0 { // some users share the token of the previous one
			fcm = fmt.Sprintf("fcm-bulk-%d", i-1)
		}
		var lastConn any
		if rng.Intn(100) < 80 {
			age := b.now.Sub(created)
			lastConn = b.now.Add(-time.Duration(rng.Int63n(int64(age)) + 1)).Truncate(time.Millisecond)
		}
		isBanned := 0
		if rng.Intn(100) < 2 {
			isBanned = 1
			banned[id] = true
		}
		b.addUser(id, 0, created, fcm, lastConn, isBanned)
		b.signupAndLogin(id, i, created, fcm, lastConn)

		// profile for ~60% of users
		if rng.Intn(100) < 60 {
			nullable := func(v string) any {
				if rng.Intn(10) == 0 {
					return nil
				}
				return v
			}
			p := profileRow{
				gender:  nullable(pick([]string{"M", "F", "A"}, rng.Intn(3))),
				city:    nullable(pick([]string{"R", "U", "A"}, rng.Intn(3))),
				dept:    nullable(pick(bulkDepts, rng.Intn(len(bulkDepts)))),
				job:     nullable(pick([]string{"AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN"}, rng.Intn(10))),
				vote:    nullable(pick([]string{"S", "P", "J"}, rng.Intn(3))),
				meeting: nullable(pick([]string{"S", "P", "J"}, rng.Intn(3))),
				consult: nullable(pick([]string{"S", "P", "J"}, rng.Intn(3))),
				age:     15 + rng.Intn(76),
			}
			b.addProfile(id, p)
		}
		// a few notifications
		if rng.Intn(4) == 0 {
			for k := 0; k < 1+rng.Intn(4); k++ {
				b.addNotification(id, rng.Intn(6), time.Duration(rng.Intn(60*24)+10)*time.Minute, fmt.Sprintf(" [%d]", k))
			}
		}
	}

	// Supporter candidates: bulk users (not banned) + the handcrafted pool.
	var candidates []string
	for _, u := range users {
		if !banned[u] {
			candidates = append(candidates, u)
		}
	}
	candidates = append(candidates, b.supporterPool...)

	// ---- QaGs -------------------------------------------------------------------
	for i := 1; i <= nQags; i++ {
		id := bulkQagID(i)
		author := users[rng.Intn(len(users))]
		username := pick(bulkNames, rng.Intn(len(bulkNames)))
		subject := pick(bulkSubjects, rng.Intn(len(bulkSubjects)))
		place := pick(bulkPlaces, rng.Intn(len(bulkPlaces)))
		title := fmt.Sprintf("Que fait le gouvernement pour %s dans %s ? (#%d)", subject, place, i)
		desc := fmt.Sprintf("Question n°%d sur %s : quelles mesures concrètes pour %s ? Merci d’avance 🙏", i, subject, place)
		thematique := pick([]string{Thematique1, Thematique2, Thematique3, Thematique4, Thematique5, Thematique6}, rng.Intn(6))

		roll := rng.Intn(100)
		var status int
		switch {
		case roll < 55:
			status = statusAccepted
		case roll < 75:
			status = statusOpen
		case roll < 85:
			status = statusRejected
		default:
			status = statusArchived
		}

		var post time.Time
		var motif any
		type upd struct {
			at     time.Time
			status int
			reason any
			motif  any
			flag   int
		}
		var updates []upd
		minutes := func(lo, hi int) time.Duration { return time.Duration(lo+rng.Intn(hi-lo+1)) * time.Minute }
		switch status {
		case statusOpen:
			post = b.ago(minutes(10, 20*24*60))
		case statusAccepted:
			modAge := minutes(5, 30*24*60)
			post = b.ago(modAge + minutes(60, 72*60))
			updates = append(updates, upd{at: b.ago(modAge), status: statusAccepted})
			if rng.Intn(20) == 0 { // accepted twice
				updates = append(updates, upd{at: b.ago(modAge / 2), status: statusAccepted})
			}
		case statusRejected:
			modAge := minutes(60, 60*24*60)
			post = b.ago(modAge + minutes(60, 48*60))
			mo := pick(bulkMotifs, rng.Intn(len(bulkMotifs)))
			motif = mo
			updates = append(updates, upd{at: b.ago(modAge), status: statusRejected, reason: "Rejet automatique de test", motif: mo, flag: rng.Intn(2)})
		case statusArchived:
			modAge := minutes(22*24*60, 120*24*60)
			post = b.ago(modAge + minutes(60, 72*60))
			updates = append(updates, upd{at: b.ago(modAge), status: statusAccepted})
			if rng.Intn(2) == 0 {
				username = "" // already anonymised
			}
		}
		post = post.Add(-time.Duration(i) * time.Second).Truncate(time.Millisecond)

		b.add("qags", uid(id), desc, motif, post, status, thematique, title, uid(author), username)
		for _, u := range updates {
			b.add("qag_updates", b.rowID("qag_updates"), u.at, u.motif, uid(id), u.reason, u.flag, u.status, uid(UserModerator))
		}
		if status == statusOpen && rng.Intn(5) == 0 {
			b.add("moderatus_locked_qags", b.rowID("moderatus_locked_qags"), b.ago(minutes(1, 600)), uid(id))
		}

		// supports
		var n int
		switch status {
		case statusAccepted, statusOpen:
			n = int(math.Pow(rng.Float64(), 3) * 60)
		default:
			n = rng.Intn(9)
		}
		seen := map[string]bool{author: true}
		span := b.now.Sub(post)
		for k := 0; k < n; k++ {
			u := candidates[rng.Intn(len(candidates))]
			if seen[u] {
				continue
			}
			seen[u] = true
			at := post.Add(time.Duration(rng.Int63n(int64(span)))).Truncate(time.Millisecond)
			b.add("supports_qag", b.rowID("supports_qag"), uid(id), at, uid(u))
		}
		// supports of banned bulk users in the last 7 days (daily cleanup at scale)
		if status == statusAccepted && len(banned) > 0 && rng.Intn(10) == 0 {
			var first string // first banned bulk user (slice order, not map order, for determinism)
			for _, uu := range users {
				if banned[uu] && !seen[uu] {
					first = uu
					break
				}
			}
			if first != "" {
				at := post.Add(time.Duration(rng.Int63n(int64(span)))).Truncate(time.Millisecond)
				b.add("supports_qag", b.rowID("supports_qag"), uid(id), at, uid(first))
			}
		}
	}

	// ---- consultation participations (consultations 1, 4 and 7) ------------------
	plans := b.consultationPlans()
	for _, plan := range plans {
		if plan.aggregated {
			continue
		}
		k := bulkParticipations * m
		if k > len(users) {
			k = len(users)
		}
		perm := rng.Perm(len(users))
		extra := make([]string, 0, k)
		for _, idx := range perm[:k] {
			extra = append(extra, users[idx])
		}
		// dates: spread over the consultation period (ongoing: started 10 days ago; ended: before its end)
		var base time.Duration
		switch plan.n {
		case 4:
			base = 3 * day
		case 7:
			base = 30 * day
		}
		dates := make([]time.Time, k)
		for i := range dates {
			dates[i] = b.ago(base + time.Duration(rng.Intn(9*24*60)+10)*time.Minute)
		}
		offset := len(plan.participants)
		b.addParticipations(plan, extra, offset, func(j int) time.Time { return dates[j-offset] })
	}

	// ---- app feedbacks --------------------------------------------------------------
	types := []string{AppFeedbackTypeBug, AppFeedbackTypeFeature, AppFeedbackTypeComment}
	for i := 0; i < 2*m; i++ {
		b.add("app_feedbacks", b.rowID("app_feedbacks"), "1.4.0", b.ago(time.Duration(rng.Intn(90*24*60)+10)*time.Minute),
			fmt.Sprintf("Retour utilisateur n°%d : tout est parfait ou presque 👍", i+1), "Pixel 8", "Android 14",
			types[i%3], uid(users[rng.Intn(len(users))]))
	}
}
