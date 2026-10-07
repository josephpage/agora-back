# parity/seed - deterministic database seed

Fills the PostgreSQL database of the Kotlin -> Go parity harness with a fixed
dataset. The same call on the reference database (Kotlin) and on the Go
database yields byte-identical tables (row ids included), so both back-ends see
exactly the same state.

```
cd agora-go

# load the schema once (psql), then seed
createdb ... ; psql -f internal/store/schema/reference_hibernate.sql ...
go run ./parity/seed/cmd/seed -db postgres://backend:agora_password@localhost:5432/agora_seed \
    -now 2026-10-06T12:00:00Z -scale 1

go vet ./parity/seed/... && go test ./parity/seed/     # builds a throw-away DB from the schema dump
```

Flags: `-db <url>` (default `$DATABASE_URL`), `-now <RFC3339>` (default: current
UTC time truncated to the second), `-scale <int>` (default 1), `-q` (silent).

From Go: `seed.Seed(ctx, conn, now, scale)`; the whole operation (TRUNCATE of the
21 tables, COPY of the rows, ANALYZE) runs in **one transaction**, so it is
atomic and idempotent (running it twice gives the same content). It takes an
ACCESS EXCLUSIVE lock (30 s `lock_timeout`): seed while the applications are
idle. The `acme_*` tables are truncated and stay empty.

## Time model - read this

* Every timestamp is `now` minus a fixed offset, in UTC, truncated to the
  millisecond (columns are `timestamp without time zone`). No row is in the
  future. `now` is a parameter: the harness controls it.
* The SQL that uses `CURRENT_TIMESTAMP` (trending queries: 7 days / N hours
  windows) uses the **database clock**, and the Kotlin code uses the JVM
  clock. So pass (about) the real current time as `-now`, or freeze all three.
  Offsets leave margins of several hours around the interesting thresholds.
* Rows that must stay on a calendar day (signup abuse detection compares
  `DATE(event_date)` and "today - 2 weeks .. today 00:00") are built from
  `today 00:00 UTC - N days + hh:mm`, not from `now - N days`.
* Weekday dependent outcomes (because of "this week's Monday 10:00", see
  `seed.MondayCutoff(now)`): which of the *recently* moderated accepted QaGs are
  also archive candidates; the "ask a QaG" status of UserRegular3/4/5.
  Always deterministic: UserRegular1 -> `ENABLED` (last QaG is 9 days old),
  UserRegular2 -> `WEEKLY_LIMIT_REACHED` (posted 22 minutes ago, unless `now` is
  within ~30 min after Monday 10:00 UTC).
  QaGs moderated >= 8 days ago (0b, 0c, 0d) are always archive candidates.

## ID plan (shared with the fake Strapi fixtures)

All constants are in `ids.go`.

| Kind | Format |
|---|---|
| Users | `00000000-0000-4000-9000-0000000000XX` (XX hex) |
| QaGs | `00000000-0000-4000-8000-0000000000XX` |
| Bulk users / QaGs (scale > 1) | `00000000-0000-4000-9001-<n>` / `00000000-0000-4000-8001-<n>` |
| Row ids of join tables | `<table index>-0000-4000-a000-<sequence>` (deterministic) |
| Thematiques | `th0000000000000000000001` ... `...06` |
| Consultations | `co0000000000000000000001` ... `...07` |
| Consultation updates | `cu0000000000000000000001` ... `...03` |
| Question ids | `c*100+q` as string (c = consultation 1..7, q = 1..6): `"101"`..`"106"`, `"201"`... `seed.QuestionID(c, q)` |
| Choice ids | `c*1000+q*10+k` as string (k = 1..3): `"1011"`, `"1012"`, `"1013"`, `"1021"`... `seed.ChoiceID(c, q, k)` |
| Sentinels | `00000000-0000-0000-0000-000000000000` = skipped (`ChoiceSkipped`) and anonymised user (`UserAnonymized`); `11111111-1111-1111-1111-111111111111` = not applicable (`ChoiceNotApplicable`) |

Per consultation: q1 unique choice, q2 multiple choice, q3 open, q4
conditional (shown only if q1 = choice 1, otherwise "not applicable"), q5
description (never answered), q6 unique choice whose choice 3 has an open text.
Consultations 1, 2, 3 ongoing; 4 ended 3 days ago; 5 ended 10 days ago
(already aggregated); 6 unpublished ongoing; 7 ended 30 days ago, not
aggregated. (Dates of consultations themselves live in Strapi; the DB only
holds answers.)

## Users (`agora_users`, `users_data`, `users_profile`, `demographic_info_ask_date`)

`password` is `''` (what signup writes), `fcm_token` is **never NULL** (the Kotlin
`UserDTO`/`UserInfo` are non-null; users "without FCM" have `''`).

| Constant(s) | id suffix | What / quirk |
|---|---|---|
| `UserRegular1` | 01 | level 0, token `fcm-regular-1`, logged in 1h ago, **25 notifications** (pagination), profile, ask date 2d ago, author of 0b / 27 / 41 / a1, `ENABLED` to ask a QaG |
| `UserRegular2` | 02 | level 0, token `fcm-regular-2`, exactly one notification of each type, posted QaG 21 20 min ago (`WEEKLY_LIMIT_REACHED`), old ask date (200d) |
| `UserRegular3/4/5` | 03/04/05 | **share token `fcm-shared-A`**, created 80d/20d/60d ago -> the unique-token CTE keeps UserRegular4 only |
| `UserRegular6/7` | 06/07 | **share `fcm-shared-B`** (created 40d/10d ago -> 7 wins); 6 has a profile with only `primary_department`, 7 an all-NULL profile row (`isCompleted() == false`) |
| `UserNoFcm1/2` | 08/09 | `fcm_token = ''` (same CTE partition: 9 wins, created more recently). 8 has `last_connection_date NULL`; no profile |
| `UserPublisher` / `UserModerator` / `UserAdmin` | 0a/0b/0c | levels 8 / 42 / 1337 |
| `UserBanned` | 0d | `is_banned = 1`, supports QaGs (see below), authors open QaG 25 |
| `UserNeverConnected` | 0e | `last_connection_date NULL`. **Do not log in with it on the Kotlin side** (`dto.copy` NPEs on the null non-null field). Same for every user of the abuse groups |
| `UserIdle` | 0f | no QaG, support, event, profile... |
| `UsersSuspectA` (A1..A3) | 10-12 | 3 signups, same ip hash + UA, same day (today-2, 10:00/10:05/10:09) -> **flagged** by the nightly job (>= 3) |
| `UsersPairB` | 13-14 | only 2 signups the same day -> not flagged |
| `UsersSpreadC` | 15-17 | same ip+UA but on 3 different days -> not flagged |
| `UsersSuspectD` (D1..D4) | 18-1b | 4 signups the same day (today-9, inside the 2 weeks window) -> flagged |
| `UsersOldE` | 1c-1e | 3 signups the same day but today-20 (outside the window) -> not flagged |
| `UserSuspectCrossDay` | 1f | single signup (today-12) with the **same ip+UA as group A** -> flagged through the `CONCAT(ip, user_agent)` join |
| `UsersMass` (UserMass1..10) | 40-49 | 10 signups the same day (today-4) -> `IsSuspiciousUser` (>= 10) is true for `IPHashMass`/`UserAgentMass`, and they are flagged |
| `UsersEmptyIP` | 50-52 | 3 signups the same day with `ip_address_hash = ''` -> ignored by the job (`ip_address_hash != ''`) |
| `UsersProfile` (UserProfile1..12) | 20-2b | profiles covering **every code**: gender M/F/A, city R/U/A, job AG/AR/CA/PI/EM/OU/ET/RE/AU/UN, frequencies S/P/J (vote, public meeting, consultation), departments `75 13 2A 2B 69 31 59 33 06 34 971 99`, ages 16..72 (`year_of_birth = now.Year - age`; the 12th has NULL), `primary_department` / `secondary_department`. Consultation participants |
| `UserProfileInvalid` | 2c | profile with unknown codes (`Z ? ZZ XX Q Atlantis Mordor`): every mapper falls back to null. With UserRegular7 (all NULL) in consultation 1 it makes the `null` key of the demographic maps collide |

Remarks:

* `primary_department` / `secondary_department` hold the **territory name**
  (`Territoire.Departement.value`: `Paris`, `Bouches-du-Rhône`, `Corse-du-Sud`...),
  `department` holds the **code** (`75`). That is how Kotlin stores them.
* `users_data`: one `signup` per user at `created_date`, one `login` at
  `last_connection_date` (3 logins for UserRegular1); platforms `android`/`ios`/`web`;
  ip hashes are 64 hex chars (`seed.IPHash*`).
* `demographic_info_ask_date`: UserRegular1 (2d ago), UserRegular2 (200d), UserRegular3 (45d).

## QaGs (`qags`, `qag_updates`, `supports_qag`, ...)

Distinct post dates (a `7s * index` offset is added). Thematiques follow the Strapi themes when the text allows
(th1 Environnement, th2 Santé, th3 Éducation, th4 Transports, th5 Numérique, th6 Démocratie); accepted QaGs per
thematique: th1 01 06 0d, th2 03 08 0a, th3 04 0b, th4 02 07 09 0e, th5 0f, th6 05 0c. In the 7 days trending window
th2 and th4 have three accepted QaGs each, so `thematiqueRowNumber < 3` in `getTrendingQags` cuts one in each.
Texts contain accents, curly quotes, emoji, ZWJ sequences, HTML, `%`, `_`, `\`,
tabs and newlines; keywords `écologie`, `transport`, `santé`, `coeur` (stored as
`cœur`: matches through `unaccent`) are spread over all statuses (the search only
returns status 1). QaG 07 has `écologie` in the title and `transport` in the
description only: it does **not** match the pair (ALL applies per column).

Support counts are `count(DISTINCT user_id)`; accepted QaGs all have distinct
counts (no ordering tie): 01:18 04:15 07:13 02:11 0f:10 06:9 0a:8 03:7 08:6
0b:5 05:4 0c:3 09:2 0d:1 0e:0.

| Constant | Status | Quirk |
|---|---|---|
| `QagSelectedVideo` (a1) | 7 | 16 distinct supporters / 18 rows (3 rows of the anonymised zero user count as one; banned user's support kept), 5 feedbacks (3 helpful, one changed his mind), authored by UserRegular1 (`canDelete` false) |
| `QagSelectedText` (a2) | 7 | 9 supports, 2 feedbacks (helpful / not) |
| `QagSelectedDocument` (a3) | 7 | 5 supports, **no feedback**, row in `low_priority_qags` |
| `QagAcceptedTop` (01) | 1 | most supported (18) -> `getMostPopularQags` has a unique winner; moderated 2h ago; "écologie" + "transport" |
| `QagAcceptedTransport` (02) .. `QagAcceptedEconomie` (05) | 1 | moderated 7h / 13h / 26h / 47h ago (trending window, varied hours); 03 and 05 carry a recent **banned** support |
| `QagAcceptedDupSupport` (06) | 1 | **duplicate support row** (UserRegular5 twice: 9 distinct, 10 rows) moderated 71h ago |
| `QagAccepted73h` (07) | 1 | moderated 73h ago: just outside a 72h window, `ÉCOLOGIE` upper case |
| `QagAcceptedOlder` (08) | 1 | 4d5h ago, ~1050 chars description |
| `QagAcceptedEdge` (09) | 1 | 6d20h ago: inside the 7 days window, a few hours before leaving it |
| `QagAcceptedTwoUpdates` (0a) | 1 | qag_updates: rejected (50h), accepted (30h), accepted (9h) -> **two rows in `getTrendingQagsV3`** and doubled `count(*)` in `getTrendingQags`; `100%` in the title |
| `QagAcceptedWeeklyArchive1/2/3` (0b/0c/0d) | 1 | moderated 9d / 10d7h / 15d ago -> always before this week's Monday 10:00 (weekly archive candidates); 0b by UserRegular1 with an old (8d) banned support that the cleanup keeps |
| `QagAcceptedNoUpdate` (0e) | 1 | **no `qag_updates`** row, 0 supports: never trending, never archived |
| `QagAcceptedSpecialChars` (0f) | 1 | HTML/XSS text, quotes, ligatures, ILIKE wildcards, emoji in title, username and description |
| `QagOpenWeeklyLimit` (21) | 0 | by UserRegular2, 22 min old, unlocked, banned support 10 min ago |
| `QagOpenLocked1` / `QagOpenLocked2` (22/24) | 0 | rows in `moderatus_locked_qags` (1h / 3h ago): excluded from `getQagToModerateList` |
| `QagOpenPlain` (23), `QagOpenBannedAuthor` (25), `QagOpenFresh` (26), `QagOpenOwnerOld` (27) | 0 | not locked; 27 is 9 days old and never moderated (author UserRegular1: visible only to him) |
| `QagRejectedRecent` (31) | -1 | `qag_updates` -1 with reason + `motif_id` (`motif_hors_sujet`), `qags.motif_id` set |
| `QagRejectedShouldDelete` (32) | -1 | `should_delete_flag = 1` |
| `QagRejectedOld` (33) | -1 | moderated 25d ago, still has user + username: anonymisation candidate |
| `QagRejectedAnonymized` (34) | -1 | already anonymised: `username = ''`, `user_id` = zero uuid |
| `QagRejectedNoReason` (35) | -1 | `reason` and `motif_id` NULL |
| `QagArchivedOld/2` (41/42) | 2 | moderated 25d / 30d ago with username: `anonymizeOldQagsBeforeDate` candidates |
| `QagArchivedAnonymized` (43) | 2 | moderated 50d ago, `username = ''` already, real `user_id` |
| `QagArchivedRecent` (44) | 2 | moderated 10d ago (< 3 weeks), has a banned support (4d ago) |
| `QagArchivedNoUpdate` (45) | 2 | no `qag_updates` row |
| `QagDeleted1/2` (d1/d2) | - | only in `qag_delete_log` (5d / 12d ago); the QaGs no longer exist |

Supports of `UserBanned`: QaGs 01 (1d), 03 (2d), 05 (42h), 21 (10 min), 44 (4d) are
**deleted by the daily cleanup** (non selected, last 7 days); a1 (selected) and 0b
(8 days) are kept.
Explicit supports (so `isSupportedByUser`, supported-list and ordering have
something to chew on): UserRegular1 -> 02 04 07 08 22 a2 0f; UserRegular2 -> 01 03 04
05 0a a1 a3; UserRegular3 -> 01 02 06 21 a1; UserRegular4 -> 01 06 0a 22 a1 a2;
UserRegular5 -> 01 04 06(x2) a1; UserRegular6 -> 01 02 04 a1; UserRegular7 -> 01 07 0f.
The rest of the supporters are taken in a fixed rotation from a pool of regular
users; `support_date` is spread between the post date and `now` (+ small ms jitter),
so the last-N-hours counters are meaningful. Authors never support their own QaG.

`feedbacks_qag`: a1 -> UserRegular1 (helpful), 2 (not helpful, updated later), 3, 4 (helpful), 6 (not);
a2 -> UserRegular1 (not), UserRegular5 (helpful, updated later); a3 none.

## Notifications

`type` is the `TypeNotification` **ordinal as text**: `'0'` ALL_REPONSES_QAGS,
`'1'` HOME_QAGS, `'2'` DETAILS_QAG, `'3'` HOME_CONSULTATIONS, `'4'`
DETAILS_CONSULTATION, `'5'` REPONSE_SUPPORT. UserRegular1: 25 rows (types cycle),
UserRegular2: one of each, UserRegular3: 3 (`'2' '5' '3'`), UserRegular4: 1
(`'4'`), UserPublisher: 2, UserAdmin: 1. Dates are unique per user.

## Consultation answers

Participants (`user_answered_consultation`, one row each, `participation_id`
per participation, shared by all rows of that participation):

| Consultation | Participants |
|---|---|
| 1 (ongoing) | UserProfile1..12, UserRegular1/2/3/6/7, UserNoFcm1/2 (no profile), UserProfileInvalid = 20 |
| 4 (ended 3d ago) | UserProfile1..8, UserRegular1, UserRegular4 = 10 (+ a **duplicate** `user_answered_consultation` row for UserProfile1: queries use `DISTINCT`) |
| 5 (ended 10d ago, **aggregated**) | UserProfile3..12, UserRegular2/3/5, UserNoFcm2 = 14 |
| 7 (ended 30d ago, **not aggregated**) | UserProfile5..12, UserRegular1/2/6 = 11 |

Answer pattern per participant j (see `answersFor`): q1 unique choice (j%7==6 -> skipped
sentinel), q2 multiple choice (several rows per participant; j%9==8 skipped, j%10==9
not applicable), q3 open (`choice_id` NULL; empty `''` text when j%5==4, no row when
j%11==10; consultation 7 has one `response_text IS NULL` row), q4 conditional (choice only
if q1 = choice 1, else not applicable sentinel), q6 unique choice (choice 3 carries an open
text unless j%4==0; j%8==7 skipped). q5 has no answers. Choice rows have
`response_text = ''`. Consultations 2, 3 and 6 have no answers.

Consultation 5 mimics the **post-aggregation state**: `consultation_results` (17 rows,
including both sentinel choice ids) holds the counts, and `reponses_consultation`
only keeps the rows that carry a text, **anonymised** (`user_id` and `participation_id` =
zero uuid). So demographic queries on consultation 5 return nothing, while 1, 4 and 7
have full raw rows (and 7 is what the aggregation job would pick up).

`feedbacks_consultation_update`: cu1 -> UserRegular1/2/4 positive, UserRegular3
negative (updated later); cu2 -> UserRegular1/2 negative; cu3 -> UserRegular3 and
UserProfile1 positive. `app_feedbacks`: `bug` (UserRegular1), `feature` (UserRegular2),
`comment` (UserRegular3, device columns NULL).

## scale > 1 (load tests)

`scale = N` keeps the handcrafted set unchanged and adds, with `math/rand` seeded
with a constant (so it is still deterministic):

* `(N-1)*40` users (2% banned, 80% with a last connection, every 20th shares the previous token,
  60% with a random profile, 25% with 1-4 notifications) and their signup/login events,
* `(N-1)*30` QaGs (55% accepted, 20% open, 10% rejected, 15% archived - never status 7,
  so Strapi keeps only a1..a3) with `qag_updates`, locks on 20% of the open ones,
  and skewed random supports (0..60, most QaGs few),
* `(N-1)*25` extra participants (random bulk users) for each of consultations 1, 4 and 7,
* `(N-1)*2` app feedbacks.

Ties in `ORDER BY supportCount DESC` become likely among bulk QaGs; the handcrafted
ones stay tie-free. `ANALYZE` is run at the end of `Seed` so that two databases seeded
identically get the same planner statistics (tables above ~30k rows are sampled by
ANALYZE, so statistics may differ slightly between runs at very large scales).

Row counts (now = anything):

| table | scale 1 | scale 50 |
|---|---|---|
| agora_users | 57 | 2017 |
| users_profile | 20 | 1203 |
| users_data | 83 | 3590 |
| qags | 35 | 1505 |
| qag_updates | 28 | 1234 |
| supports_qag | 177 | 18300 |
| notifications | 38 | 1055 |
| reponses_consultation | 239 | 20381 |
| user_answered_consultation | 56 | 3731 |
| consultation_results | 17 | 17 |
| moderatus_locked_qags | 2 | 81 |

Other tables (scale 1 = scale 50): feedbacks_qag 7, feedbacks_consultation_update 8,
demographic_info_ask_date 3, qag_delete_log 2, low_priority_qags 1, app_feedbacks 3
(101 at scale 50), acme_* 0.
