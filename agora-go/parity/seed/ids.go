package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// This file is the SHARED ID PLAN of the parity harness. The fake Strapi
// fixtures (thematiques, consultations, government responses...) are written
// against these exact values: do not change them.

// ---------------------------------------------------------------------------
// Sentinels
// ---------------------------------------------------------------------------

const (
	// UserAnonymized is the "zero" UUID written by the anonymisation jobs
	// (qags.user_id, supports_qag.user_id, reponses_consultation.user_id /
	// participation_id).
	UserAnonymized = "00000000-0000-0000-0000-000000000000"

	// ChoiceSkipped is reponses_consultation.choice_id for a skipped question.
	ChoiceSkipped = "00000000-0000-0000-0000-000000000000"
	// ChoiceNotApplicable is reponses_consultation.choice_id for a question
	// that was not shown / not answered.
	ChoiceNotApplicable = "11111111-1111-1111-1111-111111111111"
)

// ---------------------------------------------------------------------------
// Users: 00000000-0000-4000-9000-0000000000XX (XX hex)
// ---------------------------------------------------------------------------

const (
	// Main regular users (level 0) with profiles, QaGs, supports, notifications.
	UserRegular1 = "00000000-0000-4000-9000-000000000001" // token fcm-regular-1, >20 notifications, ENABLED to ask a QaG
	UserRegular2 = "00000000-0000-4000-9000-000000000002" // token fcm-regular-2, posted a QaG minutes ago (WEEKLY_LIMIT_REACHED)
	UserRegular3 = "00000000-0000-4000-9000-000000000003" // shares token fcm-shared-A (older)
	UserRegular4 = "00000000-0000-4000-9000-000000000004" // shares token fcm-shared-A (winner of the unique-token CTE)
	UserRegular5 = "00000000-0000-4000-9000-000000000005" // shares token fcm-shared-A (middle)
	UserRegular6 = "00000000-0000-4000-9000-000000000006" // shares token fcm-shared-B (older), profile with departments only
	UserRegular7 = "00000000-0000-4000-9000-000000000007" // shares token fcm-shared-B (winner), empty profile row

	// Users with an empty fcm_token ('' – agora_users.fcm_token is never NULL
	// because the Kotlin entity is non-null).
	UserNoFcm1 = "00000000-0000-4000-9000-000000000008" // last_connection_date NULL, no profile
	UserNoFcm2 = "00000000-0000-4000-9000-000000000009" // last_connection_date set (winner of the '' partition), no profile

	// Staff and special accounts.
	UserPublisher = "00000000-0000-4000-9000-00000000000a" // authorization_level 8
	UserModerator = "00000000-0000-4000-9000-00000000000b" // authorization_level 42
	UserAdmin     = "00000000-0000-4000-9000-00000000000c" // authorization_level 1337
	UserBanned    = "00000000-0000-4000-9000-00000000000d" // is_banned = 1

	// UserNeverConnected has last_connection_date NULL (and a token). Kotlin NPEs
	// when it logs in (dto.copy on a null non-null field): never use it for login.
	UserNeverConnected = "00000000-0000-4000-9000-00000000000e"
	// UserIdle has no activity at all (no profile, QaG, support, event...).
	UserIdle = "00000000-0000-4000-9000-00000000000f"

	// Signup-abuse groups (see users_data). All are regular users.
	UserSuspectA1 = "00000000-0000-4000-9000-000000000010" // group A: 3 signups same ip+UA same day (day-2) -> flagged
	UserSuspectA2 = "00000000-0000-4000-9000-000000000011"
	UserSuspectA3 = "00000000-0000-4000-9000-000000000012"
	UserPairB1    = "00000000-0000-4000-9000-000000000013" // group B: only 2 signups same day -> NOT flagged
	UserPairB2    = "00000000-0000-4000-9000-000000000014"
	UserSpreadC1  = "00000000-0000-4000-9000-000000000015" // group C: 3 signups on 3 different days -> NOT flagged
	UserSpreadC2  = "00000000-0000-4000-9000-000000000016"
	UserSpreadC3  = "00000000-0000-4000-9000-000000000017"
	UserSuspectD1 = "00000000-0000-4000-9000-000000000018" // group D: 4 signups same ip+UA same day (day-9) -> flagged
	UserSuspectD2 = "00000000-0000-4000-9000-000000000019"
	UserSuspectD3 = "00000000-0000-4000-9000-00000000001a"
	UserSuspectD4 = "00000000-0000-4000-9000-00000000001b"
	UserOldE1     = "00000000-0000-4000-9000-00000000001c" // group E: 3 signups same day but day-20 (outside the 2 weeks window) -> NOT flagged
	UserOldE2     = "00000000-0000-4000-9000-00000000001d"
	UserOldE3     = "00000000-0000-4000-9000-00000000001e"
	// UserSuspectCrossDay signed up on another day (day-12) with the same ip+UA as group A: flagged
	// through the CONCAT(ip, user_agent) join even though that day has a single signup.
	UserSuspectCrossDay = "00000000-0000-4000-9000-00000000001f"

	// Profiled users (users_profile row with every code value) that also answer consultations.
	UserProfile1  = "00000000-0000-4000-9000-000000000020"
	UserProfile2  = "00000000-0000-4000-9000-000000000021"
	UserProfile3  = "00000000-0000-4000-9000-000000000022"
	UserProfile4  = "00000000-0000-4000-9000-000000000023"
	UserProfile5  = "00000000-0000-4000-9000-000000000024"
	UserProfile6  = "00000000-0000-4000-9000-000000000025"
	UserProfile7  = "00000000-0000-4000-9000-000000000026"
	UserProfile8  = "00000000-0000-4000-9000-000000000027"
	UserProfile9  = "00000000-0000-4000-9000-000000000028"
	UserProfile10 = "00000000-0000-4000-9000-000000000029"
	UserProfile11 = "00000000-0000-4000-9000-00000000002a"
	UserProfile12 = "00000000-0000-4000-9000-00000000002b"
	// UserProfileInvalid has a users_profile row whose codes are all unknown
	// ('Z', '?', 'XX', 'Q', 'ZZ', 'Atlantis') -> every mapper falls back to null.
	UserProfileInvalid = "00000000-0000-4000-9000-00000000002c"

	// UserMass1..10 (0x40..0x49): 10 signups same ip+UA same day (day-4) -> IsSuspiciousUser
	// (>= 10) is true for IPHashMass/UserAgentMass and the 10 users are flagged by the nightly job.
	// UserEmptyIP1..3 (0x50..0x52): 3 signups same day with ip_address_hash = '' -> NOT flagged
	// (the job ignores empty ip hashes).
	UserMass1    = "00000000-0000-4000-9000-000000000040"
	UserEmptyIP1 = "00000000-0000-4000-9000-000000000050"
)

// Groups, handy to iterate over.
var (
	UsersSuspectA = []string{UserSuspectA1, UserSuspectA2, UserSuspectA3}
	UsersPairB    = []string{UserPairB1, UserPairB2}
	UsersSpreadC  = []string{UserSpreadC1, UserSpreadC2, UserSpreadC3}
	UsersSuspectD = []string{UserSuspectD1, UserSuspectD2, UserSuspectD3, UserSuspectD4}
	UsersOldE     = []string{UserOldE1, UserOldE2, UserOldE3}
	UsersMass     = massUsers()
	UsersEmptyIP  = []string{UserEmptyIP1, userID(0x51), userID(0x52)}
	UsersProfile  = profileUsers()
)

// Fcm tokens that are shared between several users.
const (
	FcmTokenSharedA = "fcm-shared-A" // users 3, 4 (winner), 5
	FcmTokenSharedB = "fcm-shared-B" // users 6, 7 (winner)
)

// Ip hashes / user agents used by users_data (64 hex chars, like the real PBKDF2 output).
var (
	IPHashSuspectA = ipHash("suspect-a")
	IPHashPairB    = ipHash("pair-b")
	IPHashSpreadC  = ipHash("spread-c")
	IPHashSuspectD = ipHash("suspect-d")
	IPHashOldE     = ipHash("old-e")
	IPHashMass     = ipHash("mass")
)

const (
	UserAgentSuspectA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) Suspect/1.0"
	UserAgentPairB    = "Agora/1.4.0 (iPhone; iOS 17.2)"
	UserAgentSpreadC  = "Agora/1.4.0 (Android 14; SM-S918B)"
	UserAgentSuspectD = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Suspect/2.0"
	UserAgentOldE     = "Mozilla/5.0 (X11; Linux x86_64) Old/1.0"
	UserAgentMass     = "curl/8.5.0 Mass"
	UserAgentEmptyIP  = "Agora/1.4.0 (Android 12; Moto G)"
)

// ---------------------------------------------------------------------------
// Strapi documentIds
// ---------------------------------------------------------------------------

const (
	Thematique1 = "th0000000000000000000001"
	Thematique2 = "th0000000000000000000002"
	Thematique3 = "th0000000000000000000003"
	Thematique4 = "th0000000000000000000004"
	Thematique5 = "th0000000000000000000005"
	Thematique6 = "th0000000000000000000006"
)

// Consultations (documentIds). 1,2,3 ongoing; 4 ended 3 days ago; 5 ended 10 days ago
// (already aggregated); 6 unpublished ongoing; 7 ended 30 days ago, NOT yet aggregated.
const (
	Consultation1 = "co0000000000000000000001"
	Consultation2 = "co0000000000000000000002"
	Consultation3 = "co0000000000000000000003"
	Consultation4 = "co0000000000000000000004"
	Consultation5 = "co0000000000000000000005"
	Consultation6 = "co0000000000000000000006"
	Consultation7 = "co0000000000000000000007"
)

// Consultation update documentIds (feedbacks_consultation_update.consultation_update_id).
const (
	ConsultationUpdate1 = "cu0000000000000000000001"
	ConsultationUpdate2 = "cu0000000000000000000002"
	ConsultationUpdate3 = "cu0000000000000000000003"
)

// Consultation question kinds, per consultation c: question id = c*100+q,
// choice id = c*1000+q*10+k (k = 1..3).
const (
	QuestionKindUniqueChoice   = 1 // q1
	QuestionKindMultipleChoice = 2 // q2
	QuestionKindOpen           = 3 // q3
	QuestionKindConditional    = 4 // q4 (shown only when q1 = choice 1, otherwise answered "not applicable")
	QuestionKindDescription    = 5 // q5 (no answers)
	QuestionKindUniqueOpenText = 6 // q6 (unique choice, choice k=3 has an open text field)
)

// QuestionID returns the question id (as stored in reponses_consultation.question_id)
// of question q (1..6) of consultation c (1..7): c*100+q.
func QuestionID(c, q int) string { return fmt.Sprintf("%d", c*100+q) }

// ChoiceID returns the choice id of choice k (1..3) of question q of consultation c:
// c*1000+q*10+k.
func ChoiceID(c, q, k int) string { return fmt.Sprintf("%d", c*1000+q*10+k) }

// ---------------------------------------------------------------------------
// QaGs: 00000000-0000-4000-8000-0000000000XX (XX hex)
// ---------------------------------------------------------------------------

const (
	// SELECTED_FOR_RESPONSE (status 7): they have a government response in Strapi.
	QagSelectedVideo    = "00000000-0000-4000-8000-0000000000a1" // 16 distinct supports, feedbacks (helpful / not helpful), banned support kept
	QagSelectedText     = "00000000-0000-4000-8000-0000000000a2" // 9 supports, 2 feedbacks
	QagSelectedDocument = "00000000-0000-4000-8000-0000000000a3" // 5 supports, no feedback, in low_priority_qags

	// ACCEPTED (status 1). Moderation dates are relative to `now`.
	QagAcceptedTop            = "00000000-0000-4000-8000-000000000001" // most supported (18), moderated 2h ago, accents+emoji, "écologie" + "transport"
	QagAcceptedTransport      = "00000000-0000-4000-8000-000000000002" // 11 supports, moderated 7h ago, "transport"
	QagAcceptedSante          = "00000000-0000-4000-8000-000000000003" // 7 supports (1 banned, recent), moderated 13h ago, "santé"
	QagAcceptedEducation      = "00000000-0000-4000-8000-000000000004" // 15 supports, moderated 26h ago
	QagAcceptedEconomie       = "00000000-0000-4000-8000-000000000005" // 4 supports (1 banned, recent), moderated 47h ago
	QagAcceptedDupSupport     = "00000000-0000-4000-8000-000000000006" // 9 distinct supporters but 10 rows (UserRegular5 supports twice), moderated 71h ago
	QagAccepted73h            = "00000000-0000-4000-8000-000000000007" // moderated 73h ago (just outside a 72h trending window), "ÉCOLOGIE"
	QagAcceptedOlder          = "00000000-0000-4000-8000-000000000008" // moderated 4d5h ago, 800+ chars description
	QagAcceptedEdge           = "00000000-0000-4000-8000-000000000009" // moderated 6d20h ago (inside 7d window)
	QagAcceptedTwoUpdates     = "00000000-0000-4000-8000-00000000000a" // rejected 50h ago, accepted 30h ago AND 9h ago -> two rows in trending v3, "100%"
	QagAcceptedWeeklyArchive1 = "00000000-0000-4000-8000-00000000000b" // moderated 9d ago (before this week's Monday 10:00), authored by UserRegular1, 1 old banned support
	QagAcceptedWeeklyArchive2 = "00000000-0000-4000-8000-00000000000c" // moderated 10d7h ago
	QagAcceptedWeeklyArchive3 = "00000000-0000-4000-8000-00000000000d" // moderated 15d ago, "écologie" + "santé"
	QagAcceptedNoUpdate       = "00000000-0000-4000-8000-00000000000e" // status 1 but NO qag_updates row (never trending, never archived)
	QagAcceptedSpecialChars   = "00000000-0000-4000-8000-00000000000f" // HTML-ish text, quotes, newlines, emoji, ligatures, ILIKE wildcards

	// OPEN (status 0).
	QagOpenWeeklyLimit  = "00000000-0000-4000-8000-000000000021" // by UserRegular2, posted 20 minutes ago, not locked
	QagOpenLocked1      = "00000000-0000-4000-8000-000000000022" // by UserRegular3, locked in moderatus_locked_qags
	QagOpenPlain        = "00000000-0000-4000-8000-000000000023" // by UserRegular4, not locked
	QagOpenLocked2      = "00000000-0000-4000-8000-000000000024" // by UserRegular5, locked
	QagOpenBannedAuthor = "00000000-0000-4000-8000-000000000025" // by UserBanned, not locked
	QagOpenFresh        = "00000000-0000-4000-8000-000000000026" // by UserProfile7, posted 6h ago, not locked
	QagOpenOwnerOld     = "00000000-0000-4000-8000-000000000027" // by UserRegular1, posted 9d ago, never moderated

	// REJECTED (status -1).
	QagRejectedRecent       = "00000000-0000-4000-8000-000000000031" // by UserRegular2, qag_updates status -1 with reason + motif, 2d ago
	QagRejectedShouldDelete = "00000000-0000-4000-8000-000000000032" // by UserRegular3, should_delete_flag = 1
	QagRejectedOld          = "00000000-0000-4000-8000-000000000033" // moderated 25d ago, NOT yet anonymised
	QagRejectedAnonymized   = "00000000-0000-4000-8000-000000000034" // moderated 40d ago, already anonymised (username '', zero user)
	QagRejectedNoReason     = "00000000-0000-4000-8000-000000000035" // qag_updates reason + motif NULL

	// ARCHIVED (status 2).
	QagArchivedOld        = "00000000-0000-4000-8000-000000000041" // by UserRegular1, moderated 25d ago, username still set
	QagArchivedOld2       = "00000000-0000-4000-8000-000000000042" // moderated 30d ago, username still set
	QagArchivedAnonymized = "00000000-0000-4000-8000-000000000043" // moderated 50d ago, username already ''
	QagArchivedRecent     = "00000000-0000-4000-8000-000000000044" // moderated 10d ago (< 3 weeks)
	QagArchivedNoUpdate   = "00000000-0000-4000-8000-000000000045" // no qag_updates row

	// Deleted QaGs referenced by qag_delete_log only (they do not exist in qags).
	QagDeleted1 = "00000000-0000-4000-8000-0000000000d1"
	QagDeleted2 = "00000000-0000-4000-8000-0000000000d2"
)

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

// AppFeedback types as stored in app_feedbacks.type.
const (
	AppFeedbackTypeBug     = "bug"
	AppFeedbackTypeFeature = "feature"
	AppFeedbackTypeComment = "comment"
)

// Notification type ordinals as stored in notifications.type (TypeNotification enum ordinal as text).
const (
	NotificationAllReponsesQags     = "0"
	NotificationHomeQags            = "1"
	NotificationDetailsQag          = "2"
	NotificationHomeConsultations   = "3"
	NotificationDetailsConsultation = "4"
	NotificationReponseSupport      = "5"
)

// ---------------------------------------------------------------------------
// helpers (id formatting)
// ---------------------------------------------------------------------------

func userID(n int) string { return fmt.Sprintf("00000000-0000-4000-9000-%012x", n) }

func massUsers() []string {
	out := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		out = append(out, userID(0x40+i))
	}
	return out
}

func profileUsers() []string {
	out := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		out = append(out, userID(0x20+i))
	}
	return out
}

func ipHash(label string) string {
	sum := sha256.Sum256([]byte("agora-parity-seed:" + label))
	return hex.EncodeToString(sum[:])
}
