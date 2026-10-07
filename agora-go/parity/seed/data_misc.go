package seed

import (
	"fmt"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// notifications
// ---------------------------------------------------------------------------

type notifTemplate struct{ title, description string }

// notifTemplates is indexed by the TypeNotification ordinal (0..5).
var notifTemplates = []notifTemplate{
	{"Nouvelles réponses du gouvernement 🏛️", "Découvrez les réponses aux questions les plus soutenues."},
	{"Les questions de la semaine", "De nouvelles questions citoyennes vous attendent."},
	{"Votre question a été publiée ✅", "Elle est désormais visible par tous les citoyens."},
	{"Nouvelle consultation disponible", "Venez donner votre avis sur la consultation « Transports »."},
	{"Une consultation se termine bientôt ⏳", "Plus que 48 h pour participer & répondre."},
	{"Une question que vous soutenez a obtenu une réponse 🎉", "Le gouvernement a répondu à « l’écologie dans les transports »."},
}

func (b *builder) addNotification(user string, ordinal int, ago time.Duration, suffix string) {
	t := notifTemplates[ordinal]
	b.add("notifications", b.rowID("notifications"), b.ago(ago), t.description, t.title+suffix, fmt.Sprintf("%d", ordinal), uid(user))
}

func (b *builder) buildNotifications() {
	// UserRegular1: 25 notifications (> 20) -> pagination, every type several times.
	for i := 0; i < 25; i++ {
		b.addNotification(UserRegular1, i%6, time.Duration(i+1)*5*hour+time.Duration(i)*3*time.Minute, fmt.Sprintf(" (%d)", i+1))
	}
	// UserRegular2: exactly one notification of each type.
	for i := 0; i < 6; i++ {
		b.addNotification(UserRegular2, i, time.Duration(i+1)*day+2*hour, "")
	}
	// A few others.
	b.addNotification(UserRegular3, 2, 3*hour, "")
	b.addNotification(UserRegular3, 5, 30*hour, "")
	b.addNotification(UserRegular3, 3, 9*day, "")
	b.addNotification(UserRegular4, 4, 45*time.Minute, "")
	b.addNotification(UserPublisher, 3, 2*day, "")
	b.addNotification(UserPublisher, 4, 12*day, "")
	b.addNotification(UserAdmin, 0, 1*day, "")
}

// ---------------------------------------------------------------------------
// consultations
// ---------------------------------------------------------------------------

// consultationPlan describes the participants of one consultation.
type consultationPlan struct {
	n            int // consultation number (1..7)
	id           string
	participants []string
	// date of the j-th participation
	when func(b *builder, j int) time.Time
	// aggregated consultations keep only anonymised text rows + consultation_results
	aggregated bool
}

var openTexts = []string{
	"Il faudrait plus de transports en commun dans les zones rurales 🚌",
	"Je souhaite que l’écologie soit au cœur des décisions publiques.",
	"Merci de consulter les citoyens plus souvent !",
	"<b>Trop</b> de taxes & pas assez de services « publics ».",
	"Mon avis : investir dans la santé et l’éducation avant tout.\nEt aussi la culture.",
	"Rien à ajouter 🙂",
	"Réponse très longue : " + longText,
	"Oui.",
}

var otherTexts = []string{"Autre : le vélo électrique 🚲", "Autre : tout dépend du département", "Autre : je ne sais pas encore"}

// answerRow is one reponses_consultation row (before ids/dates are attached).
type answerRow struct {
	q      int
	choice any // string or nil
	text   any // string or nil
}

// answersFor returns, deterministically, the rows written by participant j of
// consultation c. It mimics what the Kotlin insert use case stores:
//   - one row per chosen choice for choice questions (multiple choice = several rows),
//   - ChoiceSkipped when the question was skipped, ChoiceNotApplicable when it
//     was not shown / not answered,
//   - one row with choice_id NULL for an open question (text may be ” or NULL),
//   - nothing for the description question (q5).
func answersFor(c, j int) []answerRow {
	var rows []answerRow
	choice := func(q, k int) string { return ChoiceID(c, q, k) }

	// q1: unique choice
	q1 := 0 // 0 = skipped
	if j%7 != 6 {
		q1 = (j*2+c)%3 + 1
		rows = append(rows, answerRow{1, choice(1, q1), ""})
	} else {
		rows = append(rows, answerRow{1, ChoiceSkipped, ""})
	}

	// q2: multiple choice
	switch {
	case j%9 == 8:
		rows = append(rows, answerRow{2, ChoiceSkipped, ""})
	case j%10 == 9:
		rows = append(rows, answerRow{2, ChoiceNotApplicable, ""})
	default:
		mask := (j+c)%7 + 1
		for k := 1; k <= 3; k++ {
			if mask&(1<<(k-1)) != 0 {
				rows = append(rows, answerRow{2, choice(2, k), ""})
			}
		}
	}

	// q3: open question
	switch {
	case j%11 == 10:
		// not answered: no row at all
	case c == 7 && j == 2:
		rows = append(rows, answerRow{3, nil, nil}) // response_text NULL
	case j%5 == 4:
		rows = append(rows, answerRow{3, nil, ""}) // empty text
	default:
		rows = append(rows, answerRow{3, nil, openTexts[(j+c)%len(openTexts)]})
	}

	// q4: conditional (only shown when q1 = choice 1)
	if q1 == 1 {
		rows = append(rows, answerRow{4, choice(4, (j/3+c)%3+1), ""})
	} else {
		rows = append(rows, answerRow{4, ChoiceNotApplicable, ""})
	}

	// q6: unique choice, choice 3 carries an open text
	if j%8 == 7 {
		rows = append(rows, answerRow{6, ChoiceSkipped, ""})
	} else {
		k := (j*5+c)%3 + 1
		text := ""
		if k == 3 && j%4 != 0 {
			text = otherTexts[(j+c)%len(otherTexts)]
		}
		rows = append(rows, answerRow{6, choice(6, k), text})
	}
	return rows
}

func (b *builder) consultationPlans() []consultationPlan {
	profileUsers := UsersProfile
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	return []consultationPlan{
		{n: 1, id: Consultation1, // ongoing
			participants: cat(profileUsers, []string{UserRegular1, UserRegular2, UserRegular3, UserNoFcm1, UserNoFcm2, UserRegular6, UserRegular7, UserProfileInvalid}),
			when: func(b *builder, j int) time.Time {
				return b.ago(time.Duration(j+1)*5*hour + time.Duration(j)*7*time.Minute)
			}},
		{n: 4, id: Consultation4, // ended 3 days ago
			participants: cat(profileUsers[:8], []string{UserRegular1, UserRegular4}),
			when: func(b *builder, j int) time.Time {
				return b.ago(3*day + time.Duration(j+1)*9*hour + time.Duration(j)*11*time.Minute)
			}},
		{n: 5, id: Consultation5, aggregated: true, // ended 10 days ago, already aggregated
			participants: cat(profileUsers[2:], []string{UserRegular2, UserRegular3, UserRegular5, UserNoFcm2}),
			when: func(b *builder, j int) time.Time {
				return b.ago(10*day + time.Duration(j+1)*11*hour + time.Duration(j)*13*time.Minute)
			}},
		{n: 7, id: Consultation7, // ended 30 days ago, NOT aggregated
			participants: cat(profileUsers[4:], []string{UserRegular1, UserRegular2, UserRegular6}),
			when: func(b *builder, j int) time.Time {
				return b.ago(30*day + time.Duration(j+1)*13*hour + time.Duration(j)*17*time.Minute)
			}},
	}
}

func (b *builder) buildConsultations() {
	for _, plan := range b.consultationPlans() {
		plan := plan
		b.addParticipations(plan, plan.participants, 0, func(j int) time.Time { return plan.when(b, j) })
	}

	// duplicate user_answered_consultation row (the queries use DISTINCT for that reason).
	b.add("user_answered_consultation", b.rowID("user_answered_consultation"), Consultation4, b.ago(3*day+hour), uid(UserProfile1))

	// ---- feedbacks on consultation updates ----------------------------------
	fbc := func(update, user string, positive int, created, updated time.Duration) {
		b.add("feedbacks_consultation_update", b.rowID("feedbacks_consultation_update"), update, b.ago(created), positive, b.ago(updated), uid(user))
	}
	fbc(ConsultationUpdate1, UserRegular1, 1, 3*day, 3*day)
	fbc(ConsultationUpdate1, UserRegular2, 1, 3*day-hour, 3*day-hour)
	fbc(ConsultationUpdate1, UserRegular3, 0, 2*day, 1*day) // changed his mind
	fbc(ConsultationUpdate1, UserRegular4, 1, 20*hour, 20*hour)
	fbc(ConsultationUpdate2, UserRegular1, 0, 5*day, 5*day)
	fbc(ConsultationUpdate2, UserRegular2, 0, 4*day, 4*day)
	fbc(ConsultationUpdate3, UserRegular3, 1, 1*day, 1*day)
	fbc(ConsultationUpdate3, UserProfile1, 1, 6*hour, 6*hour)
}

// addParticipations writes user_answered_consultation + reponses_consultation
// rows for the given participants. indexOffset keeps participation ids (and the
// answer pattern) unique between the handcrafted set (0) and the bulk set;
// when(j) gives the participation date of the participant with global index j.
// For an aggregated consultation it also writes consultation_results (only
// when indexOffset == 0, i.e. never for bulk participants).
func (b *builder) addParticipations(plan consultationPlan, participants []string, indexOffset int, when func(j int) time.Time) {
	counts := map[[2]string]int{} // (question, choice) -> count, for aggregated consultations
	for jj, user := range participants {
		j := jj + indexOffset
		date := when(j)
		participation := uid(fmt.Sprintf("00000000-0000-4000-b000-%02x%010x", plan.n, j+1))
		b.add("user_answered_consultation", b.rowID("user_answered_consultation"), plan.id, date, uid(user))

		for _, a := range answersFor(plan.n, j) {
			if plan.aggregated {
				if ch, ok := a.choice.(string); ok {
					counts[[2]string{QuestionID(plan.n, a.q), ch}]++
				}
				// after aggregation only the rows that carry a text survive, anonymised.
				if txt, ok := a.text.(string); !ok || txt == "" {
					continue
				}
				b.add("reponses_consultation", b.rowID("reponses_consultation"), a.choice, plan.id, date,
					uid(UserAnonymized), QuestionID(plan.n, a.q), a.text, uid(UserAnonymized))
				continue
			}
			b.add("reponses_consultation", b.rowID("reponses_consultation"), a.choice, plan.id, date,
				participation, QuestionID(plan.n, a.q), a.text, uid(user))
		}
	}

	if plan.aggregated {
		keys := make([][2]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, k int) bool {
			if keys[i][0] != keys[k][0] {
				return keys[i][0] < keys[k][0]
			}
			return keys[i][1] < keys[k][1]
		})
		for _, k := range keys {
			b.add("consultation_results", b.rowID("consultation_results"), k[1], plan.id, k[0], int32(counts[k]))
		}
	}
}

// ---------------------------------------------------------------------------
// app feedbacks
// ---------------------------------------------------------------------------

func (b *builder) buildAppFeedbacks() {
	b.add("app_feedbacks", b.rowID("app_feedbacks"), "1.4.0", b.ago(3*day),
		"L'application plante quand j'ouvre une consultation 😢", "Pixel 8", "Android 14", AppFeedbackTypeBug, uid(UserRegular1))
	b.add("app_feedbacks", b.rowID("app_feedbacks"), "1.4.0", b.ago(10*day),
		"Ce serait super de pouvoir filtrer les questions par département.", "iPhone 15", "iOS 17.4", AppFeedbackTypeFeature, uid(UserRegular2))
	b.add("app_feedbacks", b.rowID("app_feedbacks"), nil, b.ago(20*day),
		"Merci pour cette plateforme, très utile !", nil, nil, AppFeedbackTypeComment, uid(UserRegular3))
}
