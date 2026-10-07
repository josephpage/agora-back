package seed

import (
	"fmt"
	"strings"
	"time"
)

// QaG statuses as stored in qags.status / qag_updates.status.
const (
	statusRejected = -1
	statusOpen     = 0
	statusAccepted = 1
	statusArchived = 2
	statusSelected = 7
)

// mod is one qag_updates row. ago is the age of the moderation.
type mod struct {
	ago    time.Duration
	status int
	by     string
	reason any // string or nil
	motif  any // string or nil
	flag   int // should_delete_flag
}

// qagSpec describes one handcrafted QaG plus everything attached to it.
type qagSpec struct {
	id         string
	status     int
	thematique string
	author     string // user id ("" -> anonymised: zero uuid)
	username   string
	title      string
	desc       string
	postAgo    time.Duration // age of post_date (a per-QaG offset of 7s*index is added to keep dates unique)
	motif      any           // qags.motif_id
	mods       []mod
	supports   int // total number of DISTINCT supporters (explicit + special + pool fill)
	locked     time.Duration
}

// specialSupport is an explicitly dated support (banned user, anonymised user...).
type specialSupport struct {
	qag  string
	user string
	ago  time.Duration
	rows int // number of rows to insert (default 1)
}

var longText = strings.Repeat("Ce texte est volontairement long afin de vérifier la troncature, le découpage et l’encodage des descriptions de questions. ", 8)

func (b *builder) qagSpecs() []qagSpec {
	M := UserModerator
	A := UserAdmin
	return []qagSpec{
		// ------------------------------------------------------------- ACCEPTED
		{id: QagAcceptedTop, status: statusAccepted, thematique: Thematique1, author: UserProfile1, username: "Léa",
			title:    "Quelle est la stratégie du gouvernement pour l’écologie dans les transports du quotidien ? 🌱",
			desc:     "Les émissions de CO₂ liées aux transports restent élevées. Quelles mesures concrètes comptez-vous prendre pour réduire l’empreinte carbone des déplacements, notamment en zone rurale ?",
			postAgo:  76 * hour,
			mods:     []mod{{ago: 2 * hour, status: statusAccepted, by: M}},
			supports: 18},
		{id: QagAcceptedTransport, status: statusAccepted, thematique: Thematique4, author: UserRegular2, username: "Jean-Pierre",
			title:    "Comment améliorer le transport ferroviaire en zone rurale ?",
			desc:     "Dans de nombreux départements, les petites gares ferment et les lignes disparaissent. Quel plan pour garantir un service public de transport accessible à tous ?",
			postAgo:  57 * hour,
			mods:     []mod{{ago: 7 * hour, status: statusAccepted, by: M}},
			supports: 11},
		{id: QagAcceptedSante, status: statusAccepted, thematique: Thematique2, author: UserRegular3, username: "Élodie",
			title:    "Que prévoit le gouvernement pour l'accès à la santé dans les déserts médicaux ?",
			desc:     "Il faut parfois attendre six mois pour voir un spécialiste. Comment garantir l’égalité d’accès aux soins de santé sur tout le territoire ?",
			postAgo:  51 * hour,
			mods:     []mod{{ago: 13 * hour, status: statusAccepted, by: M}},
			supports: 7},
		{id: QagAcceptedEducation, status: statusAccepted, thematique: Thematique3, author: UserRegular4, username: "Zoë 🌸",
			title:    "Pourquoi ne pas rendre la cantine scolaire gratuite pour tous les élèves ?",
			desc:     "L’éducation commence par un repas équilibré. Quel serait le coût pour l’État et quelles économies pour les familles ? 🍎",
			postAgo:  72 * hour,
			mods:     []mod{{ago: 26 * hour, status: statusAccepted, by: A}},
			supports: 15},
		{id: QagAcceptedEconomie, status: statusAccepted, thematique: Thematique6, author: UserProfile2, username: "Mathéo",
			title:    "Quel avenir pour l'emploi des jeunes diplômés ?",
			desc:     "Le chômage des moins de 25 ans reste élevé. Quelles aides à l’embauche sont prévues pour les premiers emplois ?",
			postAgo:  96 * hour,
			mods:     []mod{{ago: 47 * hour, status: statusAccepted, by: M}},
			supports: 4},
		{id: QagAcceptedDupSupport, status: statusAccepted, thematique: Thematique1, author: UserProfile3, username: "Inès",
			title:    "Peut-on taxer davantage les profits des grandes entreprises énergétiques ?",
			desc:     "Les prix de l’énergie ont explosé. L’écologie doit-elle être financée par les superprofits des énergéticiens ?",
			postAgo:  98 * hour,
			mods:     []mod{{ago: 71 * hour, status: statusAccepted, by: M}},
			supports: 9},
		{id: QagAccepted73h, status: statusAccepted, thematique: Thematique4, author: UserRegular5, username: "Yann",
			title:    "ÉCOLOGIE : et si on interdisait les vols courts ?",
			desc:     "Les vols intérieurs de moins de 2h30 devraient-ils être supprimés au profit du train, transport bas carbone ?",
			postAgo:  120 * hour,
			mods:     []mod{{ago: 73 * hour, status: statusAccepted, by: M}},
			supports: 13},
		{id: QagAcceptedOlder, status: statusAccepted, thematique: Thematique2, author: UserProfile4, username: "Noémie",
			title:    "Santé mentale des adolescents : quel plan d’action ?",
			desc:     "Psychologues scolaires, lignes d’écoute, formation des enseignants. " + longText,
			postAgo:  124 * hour,
			mods:     []mod{{ago: 101 * hour, status: statusAccepted, by: M}},
			supports: 6},
		{id: QagAcceptedEdge, status: statusAccepted, thematique: Thematique4, author: UserProfile5, username: "Hugo",
			title:    "Comment développer le covoiturage et le transport à la demande ?",
			desc:     "Les zones peu denses n’ont souvent aucune alternative à la voiture individuelle.",
			postAgo:  168 * hour,
			mods:     []mod{{ago: 164 * hour, status: statusAccepted, by: M}},
			supports: 2},
		{id: QagAcceptedTwoUpdates, status: statusAccepted, thematique: Thematique2, author: UserProfile6, username: "Sacha",
			title:   "Faut-il un 100% remboursé pour les lunettes et les soins dentaires ?",
			desc:    "Le « reste à charge zéro » est-il suffisant ? Question sur la santé et le_budget des ménages (50% des Français renoncent à des soins).",
			postAgo: 78 * hour,
			mods: []mod{
				{ago: 50 * hour, status: statusRejected, by: M, reason: "Reformuler la question", motif: "motif_reformuler"},
				{ago: 30 * hour, status: statusAccepted, by: M},
				{ago: 9 * hour, status: statusAccepted, by: A},
			},
			supports: 8},
		{id: QagAcceptedWeeklyArchive1, status: statusAccepted, thematique: Thematique3, author: UserRegular1, username: "Camille",
			title:    "Quelle politique pour le logement étudiant ?",
			desc:     "Les loyers des petites surfaces explosent dans les grandes villes universitaires.",
			postAgo:  13 * day,
			mods:     []mod{{ago: 9 * day, status: statusAccepted, by: M}},
			supports: 5},
		{id: QagAcceptedWeeklyArchive2, status: statusAccepted, thematique: Thematique6, author: UserProfile8, username: "Lucas",
			title:    "Culture : comment soutenir les petites salles de cinéma ?",
			desc:     "Les cinémas indépendants peinent à survivre face aux plateformes.",
			postAgo:  14 * day,
			mods:     []mod{{ago: 10*day + 7*hour, status: statusAccepted, by: M}},
			supports: 3},
		{id: QagAcceptedWeeklyArchive3, status: statusAccepted, thematique: Thematique1, author: UserProfile9, username: "Manon",
			title:    "Écologie : que fait l'État pour la biodiversité et la santé des sols ?",
			desc:     "Pesticides, haies, zones humides : où en est le plan biodiversité ?",
			postAgo:  20 * day,
			mods:     []mod{{ago: 15 * day, status: statusAccepted, by: M}},
			supports: 1},
		{id: QagAcceptedNoUpdate, status: statusAccepted, thematique: Thematique4, author: UserProfile10, username: "Pauline",
			title:    "Pourquoi les péages augmentent-ils chaque année ?",
			desc:     "Le prix des autoroutes pèse sur le budget des automobilistes.",
			postAgo:  60 * hour,
			supports: 0},
		{id: QagAcceptedSpecialChars, status: statusAccepted, thematique: Thematique5, author: UserProfile11, username: "Aïcha 🚀",
			title: "Test <b>gras</b> & « guillemets » \"doubles\" 'simples' 🚀",
			desc: "<script>alert('xss')</script>\n<p>Paragraphe avec <a href=\"https://exemple.fr/?a=1&b=2\">lien</a> &amp; entité ;\n" +
				"cœur, œuvre, naïve, Noël, ÀÉÎÕÜ ñ ç ß — tiret long… 👩‍👩‍👧‍👦 🇫🇷\tTabulation et back\\slash, 100% sûr ?, sous_titre",
			postAgo:  48 * hour,
			mods:     []mod{{ago: 35 * hour, status: statusAccepted, by: M}},
			supports: 10},

		// ----------------------------------------------------------------- OPEN
		{id: QagOpenWeeklyLimit, status: statusOpen, thematique: Thematique1, author: UserRegular2, username: "Jean-Pierre",
			title:    "Comment lutter contre le gaspillage alimentaire dans les supermarchés ? (écologie)",
			desc:     "Les invendus sont encore trop souvent jetés.",
			postAgo:  20 * time.Minute,
			supports: 3},
		{id: QagOpenLocked1, status: statusOpen, thematique: Thematique4, author: UserRegular3, username: "Élodie",
			title:    "Les trottinettes électriques doivent-elles être mieux régulées dans le transport urbain ?",
			desc:     "Stationnement, vitesse, assurance : un cadre national est-il prévu ?",
			postAgo:  24 * hour,
			supports: 3, locked: 1 * hour},
		{id: QagOpenPlain, status: statusOpen, thematique: Thematique2, author: UserRegular4, username: "Zoë 🌸",
			title:    "Remboursement des séances chez le psychologue : jusqu'où ?",
			desc:     "Le dispositif MonParcoursPsy est limité à 12 séances.",
			postAgo:  51 * hour,
			supports: 0},
		{id: QagOpenLocked2, status: statusOpen, thematique: Thematique3, author: UserRegular5, username: "Yann",
			title:    "Quel calendrier pour la réforme du lycée professionnel ?",
			desc:     "Les enseignants attendent des précisions sur les moyens alloués.",
			postAgo:  96 * hour,
			supports: 4, locked: 3 * hour},
		{id: QagOpenBannedAuthor, status: statusOpen, thematique: Thematique5, author: UserBanned, username: "Troll42",
			title:    "Question posée par un utilisateur banni",
			desc:     "Texte quelconque.",
			postAgo:  72 * hour,
			supports: 2},
		{id: QagOpenFresh, status: statusOpen, thematique: Thematique6, author: UserProfile7, username: "Karim",
			title:    "Pourquoi le pass culture n'est-il pas étendu aux plus de 18 ans ?",
			desc:     "Beaucoup de jeunes adultes aimeraient en bénéficier.",
			postAgo:  6 * hour,
			supports: 0},
		{id: QagOpenOwnerOld, status: statusOpen, thematique: Thematique1, author: UserRegular1, username: "Camille",
			title:    "Une vieille question jamais modérée ?",
			desc:     "Elle attend dans la file depuis plus d’une semaine.",
			postAgo:  9 * day,
			supports: 1},

		// ------------------------------------------------------------- REJECTED
		{id: QagRejectedRecent, status: statusRejected, thematique: Thematique2, author: UserRegular2, username: "Jean-Pierre",
			title: "Une question sur la santé hors sujet", desc: "Ceci n’a rien à voir avec la plateforme.",
			postAgo: 4 * day, motif: "motif_hors_sujet",
			mods:     []mod{{ago: 2 * day, status: statusRejected, by: M, reason: "Question hors sujet : merci de la reformuler", motif: "motif_hors_sujet"}},
			supports: 2},
		{id: QagRejectedShouldDelete, status: statusRejected, thematique: Thematique6, author: UserRegular3, username: "Élodie",
			title: "Propos injurieux 😡", desc: "Contenu supprimé par la modération.",
			postAgo: 5 * day, motif: "motif_injure",
			mods:     []mod{{ago: 3 * day, status: statusRejected, by: M, reason: "Propos injurieux", motif: "motif_injure", flag: 1}},
			supports: 0},
		{id: QagRejectedOld, status: statusRejected, thematique: Thematique4, author: UserRegular4, username: "Zoë 🌸",
			title: "Doublon d’une question déjà posée sur le transport", desc: "Voir la question précédente.",
			postAgo: 30 * day, motif: "motif_doublon",
			mods:     []mod{{ago: 25 * day, status: statusRejected, by: A, reason: "Doublon", motif: "motif_doublon"}},
			supports: 1},
		{id: QagRejectedAnonymized, status: statusRejected, thematique: Thematique5, author: "", username: "",
			title: "Publicité déguisée", desc: "Achetez vite ! 💸",
			postAgo: 60 * day, motif: "motif_publicite",
			mods:     []mod{{ago: 40 * day, status: statusRejected, by: M, reason: "Publicité", motif: "motif_publicite"}},
			supports: 0},
		{id: QagRejectedNoReason, status: statusRejected, thematique: Thematique6, author: UserProfile8, username: "Lucas",
			title: "Rejet sans motif", desc: "Le modérateur n’a renseigné ni raison ni motif.",
			postAgo:  6 * day,
			mods:     []mod{{ago: 4 * day, status: statusRejected, by: M}},
			supports: 0},

		// ------------------------------------------------------------- ARCHIVED
		{id: QagArchivedOld, status: statusArchived, thematique: Thematique4, author: UserRegular1, username: "Camille",
			title: "Ancienne question archivée sur le transport", desc: "Archivée lors du dernier cycle hebdomadaire.",
			postAgo:  35 * day,
			mods:     []mod{{ago: 25 * day, status: statusAccepted, by: M}},
			supports: 5},
		{id: QagArchivedOld2, status: statusArchived, thematique: Thematique2, author: UserProfile9, username: "Manon",
			title: "Archivée depuis un mois (santé)", desc: "Pseudo encore présent : à anonymiser.",
			postAgo:  40 * day,
			mods:     []mod{{ago: 30 * day, status: statusAccepted, by: M}},
			supports: 3},
		{id: QagArchivedAnonymized, status: statusArchived, thematique: Thematique6, author: UserProfile10, username: "",
			title: "Archivée et déjà anonymisée", desc: "username vide, user_id conservé.",
			postAgo:  70 * day,
			mods:     []mod{{ago: 50 * day, status: statusAccepted, by: M}},
			supports: 2},
		{id: QagArchivedRecent, status: statusArchived, thematique: Thematique1, author: UserProfile11, username: "Aïcha 🚀",
			title: "Archivée il y a dix jours", desc: "Pas encore éligible à l’anonymisation (3 semaines).",
			postAgo:  20 * day,
			mods:     []mod{{ago: 10 * day, status: statusAccepted, by: M}},
			supports: 5},
		{id: QagArchivedNoUpdate, status: statusArchived, thematique: Thematique5, author: UserProfile12, username: "Alex",
			title: "Archivée sans historique de modération", desc: "Aucune ligne qag_updates.",
			postAgo:  90 * day,
			supports: 1},

		// ------------------------------------------------------------- SELECTED
		{id: QagSelectedVideo, status: statusSelected, thematique: Thematique1, author: UserRegular1, username: "Camille",
			title:    "Quelles mesures pour accélérer la rénovation énergétique des logements ? ⚡",
			desc:     "Passoires thermiques, MaPrimeRénov’, artisans RGE : où en est-on ?",
			postAgo:  45 * day,
			mods:     []mod{{ago: 44 * day, status: statusAccepted, by: M}},
			supports: 16},
		{id: QagSelectedText, status: statusSelected, thematique: Thematique2, author: UserRegular2, username: "Jean-Pierre",
			title:    "Comment lutter contre les déserts médicaux ruraux ?",
			desc:     "Maisons de santé, télémédecine, numerus clausus.",
			postAgo:  30 * day,
			mods:     []mod{{ago: 29 * day, status: statusAccepted, by: M}},
			supports: 9},
		{id: QagSelectedDocument, status: statusSelected, thematique: Thematique5, author: UserProfile2, username: "Mathéo",
			title:    "Quel plan pour l’emploi des seniors ?",
			desc:     "Le taux d’emploi des 60-64 ans reste faible.",
			postAgo:  60 * day,
			mods:     []mod{{ago: 59 * day, status: statusAccepted, by: A}},
			supports: 5},
	}
}

// userSupports lists, per user, the QaGs they explicitly support (readable
// test scenarios: isSupportedByUser, supported list...). Remaining supporters
// are taken from the pool.
var userSupports = []struct {
	user string
	qags []string
}{
	{UserRegular1, []string{QagAcceptedTransport, QagAcceptedEducation, QagAccepted73h, QagAcceptedOlder, QagOpenLocked1, QagSelectedText, QagAcceptedSpecialChars}},
	{UserRegular2, []string{QagAcceptedTop, QagAcceptedSante, QagAcceptedEducation, QagAcceptedEconomie, QagAcceptedTwoUpdates, QagSelectedVideo, QagSelectedDocument}},
	{UserRegular3, []string{QagAcceptedTop, QagAcceptedTransport, QagAcceptedDupSupport, QagOpenWeeklyLimit, QagSelectedVideo}},
	{UserRegular4, []string{QagAcceptedTop, QagAcceptedDupSupport, QagAcceptedTwoUpdates, QagOpenLocked1, QagSelectedVideo, QagSelectedText}},
	{UserRegular5, []string{QagAcceptedTop, QagAcceptedEducation, QagAcceptedDupSupport, QagSelectedVideo}},
	{UserRegular6, []string{QagAcceptedTop, QagAcceptedTransport, QagAcceptedEducation, QagSelectedVideo}},
	{UserRegular7, []string{QagAcceptedTop, QagAccepted73h, QagAcceptedSpecialChars}},
}

func (b *builder) specialSupports() []specialSupport {
	return []specialSupport{
		// The banned user supports non-selected QaGs within the last 7 days (removed by the
		// daily cleanup), one selected QaG (kept) and one old support (kept, older than a week).
		{QagAcceptedTop, UserBanned, 1 * day, 1},
		{QagAcceptedSante, UserBanned, 2 * day, 1},
		{QagAcceptedEconomie, UserBanned, 42 * hour, 1},
		{QagOpenWeeklyLimit, UserBanned, 10 * time.Minute, 1},
		{QagSelectedVideo, UserBanned, 2 * day, 1},
		{QagAcceptedWeeklyArchive1, UserBanned, 8 * day, 1},
		{QagArchivedRecent, UserBanned, 4 * day, 1},
		// anonymised supporter on a selected QaG (3 rows -> counts as ONE distinct user)
		{QagSelectedVideo, UserAnonymized, 20 * day, 3},
		// duplicate support row: same user + same QaG twice
		{QagAcceptedDupSupport, UserRegular5, 30 * hour, 1}, // second row (the first one comes from userSupports)
	}
}

func (b *builder) buildQags() {
	specs := b.qagSpecs()
	special := b.specialSupports()

	// explicit supports per qag, keeping the declaration order
	explicit := map[string][]string{}
	for _, us := range userSupports {
		for _, q := range us.qags {
			explicit[q] = append(explicit[q], us.user)
		}
	}
	specialByQag := map[string][]specialSupport{}
	for _, s := range special {
		specialByQag[s.qag] = append(specialByQag[s.qag], s)
	}

	for idx, s := range specs {
		// ---- qags row --------------------------------------------------------
		post := b.ago(s.postAgo + time.Duration(idx+1)*7*time.Second)
		author := s.author
		if author == "" {
			author = UserAnonymized
		}
		b.add("qags", uid(s.id), s.desc, s.motif, post, int(s.status), s.thematique, s.title, uid(author), s.username)

		// ---- qag_updates -------------------------------------------------------
		for _, m := range s.mods {
			b.add("qag_updates", b.rowID("qag_updates"), b.ago(m.ago), m.motif, uid(s.id), m.reason, int(m.flag), int(m.status), uid(m.by))
		}

		// ---- lock ----------------------------------------------------------------
		if s.locked > 0 {
			b.add("moderatus_locked_qags", b.rowID("moderatus_locked_qags"), b.ago(s.locked), uid(s.id))
		}

		// ---- supports ----------------------------------------------------------
		type row struct {
			user string
			at   time.Time
		}
		var rows []row
		used := map[string]bool{s.author: true}
		for _, u := range explicit[s.id] {
			if !used[u] {
				used[u] = true
				rows = append(rows, row{user: u})
			}
		}
		distinctSpecial := 0
		var dated []row
		var extraRows []row
		for _, sp := range specialByQag[s.id] {
			n := sp.rows
			if n == 0 {
				n = 1
			}
			if used[sp.user] && sp.user != UserAnonymized && sp.user != UserBanned {
				// duplicate row of an explicit supporter: extra row only
				extraRows = append(extraRows, row{user: sp.user, at: b.ago(sp.ago)})
				continue
			}
			used[sp.user] = true
			distinctSpecial++
			for k := 0; k < n; k++ {
				dated = append(dated, row{user: sp.user, at: b.ago(sp.ago + time.Duration(k)*time.Hour)})
			}
		}
		fill := s.supports - len(rows) - distinctSpecial
		if fill < 0 {
			panic(fmt.Sprintf("seed: qag %s: %d explicit supporters exceed the target %d", s.id, len(rows)+distinctSpecial, s.supports))
		}
		start := (idx * 5) % len(b.supporterPool)
		for k := 0; fill > 0 && k < len(b.supporterPool); k++ {
			u := b.supporterPool[(start+k)%len(b.supporterPool)]
			if used[u] {
				continue
			}
			used[u] = true
			rows = append(rows, row{user: u})
			fill--
		}
		if fill > 0 {
			panic(fmt.Sprintf("seed: qag %s: supporter pool exhausted", s.id))
		}
		// spread the undated supports between the post date and now
		n := len(rows)
		for i := range rows {
			rows[i].at = between(post, b.now, i+1, n).Add(time.Duration((i*13+idx*7)%900) * time.Millisecond)
		}
		for _, r := range rows {
			b.add("supports_qag", b.rowID("supports_qag"), uid(s.id), r.at, uid(r.user))
		}
		for _, r := range dated {
			b.add("supports_qag", b.rowID("supports_qag"), uid(s.id), r.at, uid(r.user))
		}
		for _, r := range extraRows {
			b.add("supports_qag", b.rowID("supports_qag"), uid(s.id), r.at, uid(r.user))
		}
	}

	// ---- low priority: a selected QaG ----------------------------------------------
	b.add("low_priority_qags", b.rowID("low_priority_qags"), uid(QagSelectedDocument))

	// ---- delete log -------------------------------------------------------------------
	b.add("qag_delete_log", b.rowID("qag_delete_log"), b.ago(5*day), uid(QagDeleted1), uid(UserRegular2))
	b.add("qag_delete_log", b.rowID("qag_delete_log"), b.ago(12*day), uid(QagDeleted2), uid(UserRegular3))

	// ---- feedbacks on the selected QaGs ------------------------------------------------
	fb := func(qag, user string, helpful int, created, updated time.Duration) {
		b.add("feedbacks_qag", b.rowID("feedbacks_qag"), b.ago(created), int16(helpful), qag, b.ago(updated), uid(user))
	}
	fb(QagSelectedVideo, UserRegular1, 1, 10*day, 10*day)
	fb(QagSelectedVideo, UserRegular2, 0, 9*day, 3*day) // changed his mind
	fb(QagSelectedVideo, UserRegular3, 1, 8*day, 8*day)
	fb(QagSelectedVideo, UserRegular4, 1, 7*day, 7*day)
	fb(QagSelectedVideo, UserRegular6, 0, 2*day, 2*day)
	fb(QagSelectedText, UserRegular1, 0, 6*day, 6*day)
	fb(QagSelectedText, UserRegular5, 1, 5*day, 1*day)
}
