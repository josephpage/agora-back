"""Generates the S7 scenarios (Moderatus integration, QaG moderation lifecycle, admin QaG status).

Usage (from agora-go/):  python3 -I parity/scenarios/gen/s7/gen_all.py parity/scenarios
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

from helpers import (  # noqa: E402
    TAG, TOKEN, ADMIN, MODERATOR, REGULAR1, REGULAR2, PUBLISHER, BANNED, IDLE, UNKNOWN_USER, ID, q,
    OPEN_WEEKLY, OPEN_LOCKED1, OPEN_PLAIN, OPEN_LOCKED2, OPEN_BANNED, OPEN_FRESH, OPEN_OLD,
    ACCEPTED_TOP, ACCEPTED_TRANSPORT, REJECTED_RECENT, REJECTED_OLD, ARCHIVED_RECENT, ARCHIVED_OLD,
    SELECTED_TEXT, DELETED, UNKNOWN_QAG, NOT_UUID, st, moderate_q, lock_q, list_q, mint_path,
    sql_insert_open, gen_ids_sql, scenario, dump,
)

MOD, ADM, REG, PUB, BAN, IDL, UNK = (TOKEN[k] for k in ("MOD", "ADMIN", "REG1", "PUB", "BANNED", "IDLE", "UNK"))
NEW = "/moderatus/new_qags"
ACK = "/moderatus/ack_qags"
RES = "/moderatus/result_qag"
ADMIN_PATH = "/admin/qags/%s/status"
LIST_LATEST = "/v2/qags?filterType=latest"
LIST_SLEEP = 5300  # the Kotlin and Go lists may lag up to 5 s (see parity/ledger/S7.md)

# Uppercase and short (lenient UUID) spellings of the seeded QaG 0x23 (QagOpenPlain)
OPEN_PLAIN_UPPER = ID(0x23).upper()
OPEN_PLAIN_SHORT = "0-0-4000-8000-23"


def details(id_, qag, as_user=None, sleep=None):
    s = st(id_, "/qags/%s" % qag, as_=as_user, sleep=sleep)
    return s


def new_list(id_, password=MOD, sleep=None, query_extra=None):
    qs = list_q(password)
    if query_extra:
        qs.update(query_extra)
    return st(id_, NEW, query=qs, sleep=sleep)


def no_open_except(keep):
    """Set every OPEN QaG but `keep` (literal ids) to accepted: a known list state."""
    if not keep:
        return "UPDATE qags SET status = 1 WHERE status = 0"
    return "UPDATE qags SET status = 1 WHERE status = 0 AND id NOT IN (%s)" % ", ".join("'%s'" % k for k in keep)


SEED_OPEN_IDS = [ID(n) for n in (0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27)]
# Insert statements: literal ids (the placeholders are not expanded in setup.sql).
NULL_ROW = (
    "INSERT INTO qags (id, title, description, post_date, status, thematique_id, user_id, username) "
    "VALUES ('00000000-0000-4000-8300-000000000001', %s, %s, %s, 0, 'th0000000000000000000001', %s, %s)"
)


def null_row(title="'Question nulle'", description="'desc'", post="timestamp '2026-09-03 10:00:00'",
             user="'00000000-0000-4000-9000-000000000004'::uuid", username="'Nul'"):
    return NULL_ROW % (title, description, post, user, username)


scenarios = {}  # file name -> list of scenarios


def add(fname, sc):
    scenarios.setdefault(fname, []).append(sc)


# ---------------------------------------------------------------------------------------------------------------
# GET /moderatus/new_qags (read only)
# ---------------------------------------------------------------------------------------------------------------
F = "S7_new_qags.yaml"
add(F, scenario("S7-new-qags-auth", [
    st("no-password", NEW, query={"mediaType": "xml"}),
    st("no-media-type", NEW, query={"password": MOD}),
    st("no-params", NEW),
    st("empty-password", NEW, query=list_q("")),
    st("empty-media-type", NEW, query={"password": MOD, "mediaType": ""}),
    st("garbage-password", NEW, query=list_q("abc")),
    st("not-base64", NEW, query=list_q("not base64 !!")),
    st("tampered-last-char", NEW, query=list_q(MOD[:-1] + "x")),
    st("prefix-garbage", NEW, query=list_q("x" + MOD)),
    st("spaces-around", NEW, query=list_q(" " + MOD + " ")),
    st("newline-suffix", NEW, query=list_q(MOD + "\n")),
    st("regular-user", NEW, query=list_q(REG)),
    st("publisher", NEW, query=list_q(PUB)),
    st("banned-user", NEW, query=list_q(BAN)),
    st("idle-user", NEW, query=list_q(IDL)),
    st("unknown-user", NEW, query=list_q(UNK)),
    st("moderator", NEW, query=list_q(MOD)),
    st("admin-has-moderation", NEW, query=list_q(ADM)),
    st("media-type-json", NEW, query={"password": MOD, "mediaType": "json"}),
    st("accept-json-header", NEW, query=list_q(MOD), headers={"Accept": "application/json"}),
    st("moderator-with-regular-jwt", NEW, query=list_q(MOD), as_=REGULAR1),
    st("moderator-with-garbage-bearer", NEW, query=list_q(MOD), bearer="abc.def.ghi"),
    st("repeated-password", path=mint_path(NEW, [("password", MOD), ("password", REG), ("mediaType", "xml")])),
    st("head", NEW, method="HEAD", query=list_q(MOD)),
    st("post-method", NEW, method="POST", query=list_q(MOD)),
], tags=TAG, dbdiff="none"))

add(F, scenario("S7-new-qags-seeded", [
    st("baseline", NEW, query=list_q(MOD)),
    st("baseline-again", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none"))

add(F, scenario("S7-new-qags-empty", [
    st("empty-list", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [no_open_except([])]}))

add(F, scenario("S7-new-qags-one", [
    st("one-qag", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [no_open_except([ID(0x23)])]}))

add(F, scenario("S7-new-qags-locked-only", [
    st("locked-are-excluded", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [no_open_except([ID(0x22), ID(0x24)])]}))

add(F, scenario("S7-new-qags-many-101", [
    st("oldest-100-of-101", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [sql_insert_open(1, 96)]}))

add(F, scenario("S7-new-qags-exactly-100", [
    st("exactly-100", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [sql_insert_open(1, 95)]}))

add(F, scenario("S7-new-qags-99", [
    st("just-below-the-limit", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [sql_insert_open(1, 94)]}))

add(F, scenario("S7-new-qags-escapes", [
    st("special-characters-in-cdata", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="none", setup={"sql": [
    # no "]]>" here: a CDATA section that contains it makes the reference answer a truncated 200
    # (see S7-known-foundation-cdata-end-marker and parity/divergences/S7.md)
    "INSERT INTO qags (id, title, description, post_date, status, thematique_id, user_id, username) VALUES "
    "('00000000-0000-4000-8300-000000000002', 'Q <b>&amp;</b> ünï 😀 \"q\" ''x'' \\ fin ] > <', "
    "'line1\nline2\r\nline3\ttab <![CDATA[x]' || '>', timestamp '2026-09-02 10:00:00', 0, "
    "'th0000000000000000000001', '00000000-0000-4000-9000-000000000004'::uuid, 'O''Neil & <Co> ]] >'),"
    "('00000000-0000-4000-8300-000000000003', 'Del \u007f ligature ﬁ   nbsp  ✓', 'a ] b > c', "
    "timestamp '2026-09-02 10:01:00', 0, 'th0000000000000000000002', "
    "'00000000-0000-4000-9000-000000000004'::uuid, 'Üé')",
]}))

for name, args in (
    ("S7-new-qags-null-title", {"title": "NULL"}),
    ("S7-new-qags-null-description", {"description": "NULL"}),
    ("S7-new-qags-null-post-date", {"post": "NULL"}),
    ("S7-new-qags-null-user", {"user": "NULL"}),
    ("S7-new-qags-null-username", {"username": "NULL"}),
):
    add(F, scenario(name, [
        st("list-with-null-column", NEW, query=list_q(MOD)),
    ], tags=TAG, dbdiff="none", setup={"sql": [null_row(**args)]}))

# ---------------------------------------------------------------------------------------------------------------
# GET /moderatus/ack_qags (lock: writes moderatus_locked_qags)
# ---------------------------------------------------------------------------------------------------------------
F = "S7_ack_qags.yaml"
add(F, scenario("S7-ack-qags-auth", [
    st("no-password", ACK, query={"mediaType": "xml", "content_id": OPEN_PLAIN}),
    st("no-media-type", ACK, query={"password": MOD, "content_id": OPEN_PLAIN}),
    st("no-content-id", ACK, query={"password": MOD, "mediaType": "xml"}),
    st("no-params", ACK),
    st("empty-content-id-moderator", ACK, query=lock_q(MOD, "")),
    st("empty-content-id-bad-password", ACK, query=lock_q("abc", "")),
    st("bad-password-ids", ACK, query=lock_q("abc", "%s;%s" % (OPEN_PLAIN, OPEN_FRESH))),
    st("bad-password-blanks", ACK, query=lock_q("abc", "  ;\t ;  ")),
    st("bad-password-non-uuid", ACK, query=lock_q("abc", "not-a-uuid;;x")),
    st("regular-user-ids", ACK, query=lock_q(REG, OPEN_PLAIN)),
    st("publisher-ids", ACK, query=lock_q(PUB, OPEN_PLAIN)),
    st("banned-ids", ACK, query=lock_q(BAN, OPEN_PLAIN)),
    st("idle-ids", ACK, query=lock_q(IDL, OPEN_PLAIN)),
    st("unknown-user-ids", ACK, query=lock_q(UNK, OPEN_PLAIN)),
    st("moderator-locks", ACK, query=lock_q(MOD, OPEN_WEEKLY)),
    st("admin-locks", ACK, query=lock_q(ADM, OPEN_BANNED)),
    st("moderator-with-garbage-bearer", ACK, query=lock_q(MOD, OPEN_FRESH), bearer="abc.def.ghi"),
    st("repeated-content-id", path=mint_path(ACK, [("password", MOD), ("mediaType", "xml"), ("content_id", OPEN_OLD), ("content_id", OPEN_BANNED)])),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-ack-qags-lock-states", [
    st("open-unlocked", ACK, query=lock_q(MOD, OPEN_PLAIN)),
    st("open-unlocked-banned-author", ACK, query=lock_q(MOD, OPEN_BANNED)),
    st("open-unlocked-owner-old", ACK, query=lock_q(MOD, OPEN_OLD)),
    st("open-unlocked-fresh", ACK, query=lock_q(MOD, OPEN_FRESH)),
    st("open-unlocked-weekly-limit", ACK, query=lock_q(MOD, OPEN_WEEKLY)),
    st("open-already-locked", ACK, query=lock_q(MOD, OPEN_LOCKED1)),
    st("open-already-locked-2", ACK, query=lock_q(MOD, OPEN_LOCKED2)),
    st("accepted", ACK, query=lock_q(MOD, ACCEPTED_TOP)),
    st("moderated-rejected", ACK, query=lock_q(MOD, REJECTED_RECENT)),
    st("archived", ACK, query=lock_q(MOD, ARCHIVED_RECENT)),
    st("archived-old", ACK, query=lock_q(MOD, ARCHIVED_OLD)),
    st("selected", ACK, query=lock_q(MOD, SELECTED_TEXT)),
    st("deleted", ACK, query=lock_q(MOD, DELETED)),
    st("unknown", ACK, query=lock_q(MOD, UNKNOWN_QAG)),
    st("not-a-uuid", ACK, query=lock_q(MOD, NOT_UUID)),
    st("list-after-locks", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-ack-qags-separators", [
    st("trailing-separator", ACK, query=lock_q(MOD, OPEN_WEEKLY + ";")),
    st("leading-and-blank-parts", ACK, query=lock_q(MOD, ";;  %s  ; ;" % OPEN_BANNED)),
    st("tab-padding", ACK, query=lock_q(MOD, "\t%s\t" % OPEN_FRESH)),
    st("duplicates-processed-once", ACK, query=lock_q(MOD, "%s;%s;%s" % (OPEN_OLD, OPEN_OLD, OPEN_OLD))),
    st("uppercase-spelling", ACK, query=lock_q(MOD, OPEN_PLAIN_UPPER)),
    st("lenient-short-spelling", ACK, query=lock_q(MOD, OPEN_PLAIN_SHORT)),
    st("only-blanks-after-trim", ACK, query=lock_q(MOD, " ; \t ;")),
    st("mixed-valid-invalid-locked", ACK, query=lock_q(MOD, "%s;garbage;%s" % (OPEN_LOCKED1, ACCEPTED_TOP))),
    st("lock-again-after-success", ACK, query=lock_q(MOD, OPEN_WEEKLY)),
    st("list-excludes-locked", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-ack-qags-c1-controls-in-attributes", [
    # U+007F..U+009F are written raw in attribute values by both backends (identical bytes)
    st("c1-in-content-id-anonymous", ACK, query=lock_q("abc", "\u0080x\u007f")),
    st("c1-in-content-id-moderator-notfound", ACK, query=lock_q(MOD, "\u0085 \u009f")),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-ack-qags-many-60", [
    st("lock-sixty-at-once", ACK, query=lock_q(MOD, ";".join(gen_ids_sql(first=1, count=60)))),
    st("list-after-sixty", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="step", setup={"sql": [sql_insert_open(1, 60)]}))

add(F, scenario("S7-ack-qags-then-moderate-reject", [
    st("lock-plain", ACK, query=lock_q(MOD, OPEN_PLAIN)),
    st("list-without-locked", NEW, query=list_q(MOD)),
    st("reject-locked-open", RES, query=moderate_q(MOD, OPEN_PLAIN, "NOK", motif="hors charte", motif_id="m-1", kill="1")),
    st("lock-again-after-moderation", ACK, query=lock_q(MOD, OPEN_PLAIN)),
    st("list-after-rejection", NEW, query=list_q(MOD)),
    st("details-after-rejection", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

# ---------------------------------------------------------------------------------------------------------------
# GET /moderatus/result_qag (moderation; writes qags, qag_updates, notifications, moderatus_locked_qags)
# ---------------------------------------------------------------------------------------------------------------
F = "S7_result_qag.yaml"
add(F, scenario("S7-result-qag-params", [
    st("no-password", RES, query={"mediaType": "xml", "content_id": OPEN_PLAIN, "status": "NOK"}),
    st("no-media-type", RES, query={"password": MOD, "content_id": OPEN_PLAIN, "status": "NOK"}),
    st("no-content-id", RES, query={"password": MOD, "mediaType": "xml", "status": "NOK"}),
    st("no-status", RES, query={"password": MOD, "mediaType": "xml", "content_id": OPEN_PLAIN}),
    st("no-params", RES),
    st("empty-status", RES, query=moderate_q(MOD, OPEN_PLAIN, "")),
    st("empty-content-id", RES, query=moderate_q(MOD, "", "NOK")),
    st("empty-password", RES, query=moderate_q("", OPEN_PLAIN, "NOK")),
    st("no-optional-params", RES, query={"password": MOD, "mediaType": "xml", "content_id": OPEN_PLAIN, "status": "NOK"}),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-wrong-password", [
    st("garbage-ok", RES, query=moderate_q("abc", OPEN_PLAIN, "OK")),
    st("garbage-nok-with-kill", RES, query=moderate_q("abc", OPEN_PLAIN, "NOK", kill="1", motif="x")),
    st("garbage-invalid-status", RES, query=moderate_q("abc", OPEN_PLAIN, "FOO")),
    st("regular-user", RES, query=moderate_q(REG, OPEN_PLAIN, "NOK")),
    st("publisher", RES, query=moderate_q(PUB, OPEN_PLAIN, "NOK")),
    st("banned", RES, query=moderate_q(BAN, OPEN_PLAIN, "NOK")),
    st("idle", RES, query=moderate_q(IDL, OPEN_PLAIN, "NOK")),
    st("unknown-user", RES, query=moderate_q(UNK, OPEN_PLAIN, "NOK")),
    st("tampered-moderator", RES, query=moderate_q(MOD[:-1] + "x", OPEN_PLAIN, "NOK")),
    st("qag-untouched", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-status-values", [
    st("lowercase-ok", RES, query=moderate_q(MOD, OPEN_PLAIN, "ok")),
    st("accept-word", RES, query=moderate_q(MOD, OPEN_PLAIN, "ACCEPT")),
    st("trailing-space", RES, query=moderate_q(MOD, OPEN_PLAIN, "OK ")),
    st("leading-space", RES, query=moderate_q(MOD, OPEN_PLAIN, " NOK")),
    st("empty-after-trim", RES, query=moderate_q(MOD, OPEN_PLAIN, " ")),
    st("nok-lowercase", RES, query=moderate_q(MOD, OPEN_PLAIN, "nok")),
    st("okay", RES, query=moderate_q(MOD, OPEN_PLAIN, "OKAY")),
    st("numeric", RES, query=moderate_q(MOD, OPEN_PLAIN, "1")),
    st("enum-name", RES, query=moderate_q(MOD, OPEN_PLAIN, "MODERATED_ACCEPTED")),
    # `status` is a String here: the values are joined with "," ("OK,NOK" is not a status)
    st("repeated-status", path=mint_path(RES, [("password", MOD), ("mediaType", "xml"), ("content_id", OPEN_PLAIN),
                                               ("status", "OK"), ("status", "NOK")])),
    st("invalid-status-unknown-id", RES, query=moderate_q(MOD, UNKNOWN_QAG, "maybe")),
    st("invalid-status-not-uuid", RES, query=moderate_q(MOD, NOT_UUID, "maybe")),
    st("invalid-status-archived", RES, query=moderate_q(MOD, ARCHIVED_RECENT, "maybe")),
    st("unknown-id-valid-status", RES, query=moderate_q(MOD, UNKNOWN_QAG, "OK")),
    st("qag-still-open", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-unknown-ids", [
    st("unknown-uuid-ok", RES, query=moderate_q(MOD, UNKNOWN_QAG, "OK")),
    st("unknown-uuid-nok", RES, query=moderate_q(MOD, UNKNOWN_QAG, "NOK", kill="1")),
    st("not-a-uuid", RES, query=moderate_q(MOD, NOT_UUID, "NOK")),
    st("deleted", RES, query=moderate_q(MOD, DELETED, "OK")),
    st("archived-recent", RES, query=moderate_q(MOD, ARCHIVED_RECENT, "NOK")),
    st("archived-old-ok", RES, query=moderate_q(MOD, ARCHIVED_OLD, "OK")),
    st("selected-nok", RES, query=moderate_q(MOD, SELECTED_TEXT, "NOK")),
    st("selected-ok", RES, query=moderate_q(MOD, SELECTED_TEXT, "OK")),
    st("uppercase-open-nok", RES, query=moderate_q(MOD, OPEN_PLAIN_UPPER, "NOK", kill="1")),
    st("lenient-short-open-nok", RES, query=moderate_q(MOD, OPEN_PLAIN_SHORT, "NOK")),
    st("details-after", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

# Rejections of an open QaG: the notification text is the fixed Kotlin message (deterministic).
KILL_VARIANTS = [
    ("kill-1-with-motif", dict(kill="1", motif="hors charte", motif_id="m-1")),
    ("kill-2-no-delete", dict(kill="2")),
    ("kill-space-before", dict(kill=" 1")),
    ("kill-plus-sign", dict(kill="+1")),
    ("kill-decimal", dict(kill="1.0")),
    ("kill-empty", dict(kill="")),
    ("kill-arabic-indic-digit", dict(kill="١")),
    ("kill-without-motif", dict(kill="1", motif_id="m-2")),
    ("motif-empty", dict(kill="1", motif="", motif_id="")),
    ("motif-unicode", dict(kill="1", motif="Hors « charte » ünï 😀 \"q\" 'x' \\ fin", motif_id="m😀")),
    ("motif-repeated", None),
]
for name, extra in KILL_VARIANTS:
    if extra is None:
        path = mint_path(RES, [("password", MOD), ("mediaType", "xml"), ("content_id", OPEN_PLAIN), ("status", "NOK"),
                               ("motif", "a"), ("motif", "b"), ("kill", "1")])
        add(F, scenario("S7-result-qag-" + name, [st("reject-" + name, path=path), st("details", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1)],
                        tags=TAG, dbdiff="step"))
        continue
    add(F, scenario("S7-result-qag-" + name, [
        st("reject-" + name, RES, query=moderate_q(MOD, OPEN_PLAIN, "NOK", **extra)),
        st("details", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
    ], tags=TAG, dbdiff="step"))

# Transitions on accepted, rejected and locked QaGs (deterministic notifications only).
add(F, scenario("S7-result-qag-accepted-stays-accepted", [
    st("ok-on-accepted", RES, query=moderate_q(MOD, ACCEPTED_TOP, "OK", motif="raison", motif_id="m-ok", kill="1")),
    st("ok-again", RES, query=moderate_q(MOD, ACCEPTED_TOP, "OK")),
    st("details", "/qags/%s" % ACCEPTED_TOP, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-accepted-rejected-after", [
    st("nok-on-accepted", RES, query=moderate_q(MOD, ACCEPTED_TOP, "NOK", motif="retiree", kill="1")),
    st("nok-again", RES, query=moderate_q(MOD, ACCEPTED_TOP, "NOK")),
    st("details", "/qags/%s" % ACCEPTED_TOP, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-rejected-accepted-after-reject", [
    st("ok-on-rejected", RES, query=moderate_q(MOD, REJECTED_RECENT, "OK", motif="ok", motif_id="m-3")),
    st("details-accepted", "/qags/%s" % REJECTED_RECENT, as_=REGULAR1),
    st("nok-again-round-trip", RES, query=moderate_q(MOD, REJECTED_RECENT, "NOK", motif="again")),
    st("details-rejected", "/qags/%s" % REJECTED_RECENT, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-rejected-stays-rejected", [
    st("nok-on-rejected", RES, query=moderate_q(MOD, REJECTED_RECENT, "NOK", motif="x", kill="1")),
    st("nok-on-rejected-old", RES, query=moderate_q(MOD, REJECTED_OLD, "NOK")),
    st("details", "/qags/%s" % REJECTED_RECENT, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-result-qag-reject-locked-removes-lock", [
    st("list-before", NEW, query=list_q(MOD)),
    st("nok-locked-open", RES, query=moderate_q(MOD, OPEN_LOCKED1, "NOK", kill="1", motif="x", motif_id="m-4")),
    st("list-after", NEW, query=list_q(MOD)),
    st("nok-locked-other", RES, query=moderate_q(MOD, OPEN_LOCKED2, "NOK")),
    st("list-final", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="step"))

# The accepted notification of an open QaG is drawn at random by the Kotlin code (Set.random()): its text
# cannot match the Go one, so these scenarios do not compare the database (dbdiff none); the status, the lock
# and the list effects are compared over HTTP.
F = "S7_result_qag_open_accept.yaml"
add(F, scenario("S7-result-qag-accept-open", [
    st("ok-on-open", RES, query=moderate_q(MOD, OPEN_PLAIN, "OK", motif="ok", motif_id="m-5")),
    st("details-anonymous", "/qags/%s" % OPEN_PLAIN),
    st("details-author", "/qags/%s" % OPEN_PLAIN, as_=REGULAR2),
    st("list-open", NEW, query=list_q(MOD)),
    st("latest-after-accept", LIST_LATEST, sleep=LIST_SLEEP, as_=REGULAR1),
], tags=TAG, dbdiff="none"))

add(F, scenario("S7-result-qag-accept-locked-open", [
    st("ok-on-locked-open", RES, query=moderate_q(MOD, OPEN_LOCKED1, "OK")),
    st("list-after", NEW, query=list_q(MOD)),
    st("ack-again-not-found", ACK, query=lock_q(MOD, OPEN_LOCKED1)),
], tags=TAG, dbdiff="none"))

add(F, scenario("S7-result-qag-accept-then-reject", [
    st("ok-on-open", RES, query=moderate_q(MOD, OPEN_FRESH, "OK")),
    st("nok-after-accept", RES, query=moderate_q(MOD, OPEN_FRESH, "NOK", motif="retrait", kill="1")),
    st("details", "/qags/%s" % OPEN_FRESH, as_=REGULAR1),
], tags=TAG, dbdiff="none"))

add(F, scenario("S7-result-qag-accept-open-by-admin", [
    st("ok-by-admin", RES, query=moderate_q(ADM, OPEN_PLAIN, "OK")),
    st("list-open", NEW, query=list_q(ADM)),
], tags=TAG, dbdiff="none"))

# ---------------------------------------------------------------------------------------------------------------
# PUT /admin/qags/{qagId}/status (AdminQagController: ADMIN_APIS)
# ---------------------------------------------------------------------------------------------------------------
F = "S7_admin_qag_status.yaml"
ADM_PUT = lambda qag: ADMIN_PATH % qag
add(F, scenario("S7-admin-qag-status-auth", [
    st("anonymous", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}),
    st("anonymous-bearer-garbage", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, bearer="abc.def.ghi"),
    st("regular", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=REGULAR1),
    st("publisher", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=PUBLISHER),
    st("moderator-no-admin-apis", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=MODERATOR),
    st("banned", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=BANNED),
    st("idle", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=IDLE),
    st("unknown-user", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=UNKNOWN_USER),
    st("admin", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("details-after-admin", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-admin-qag-status-values", [
    st("open", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "OPEN"}, as_=ADMIN),
    st("moderated-accepted", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "MODERATED_ACCEPTED"}, as_=ADMIN),
    st("moderated-rejected", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "MODERATED_REJECTED"}, as_=ADMIN),
    st("archived", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("selected", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "SELECTED_FOR_RESPONSE"}, as_=ADMIN),
    st("same-status-again", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "SELECTED_FOR_RESPONSE"}, as_=ADMIN),
    st("accepted-then-on-accepted", ADM_PUT(ACCEPTED_TOP), method="PUT", query={"status": "MODERATED_ACCEPTED"}, as_=ADMIN),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-admin-qag-status-invalid", [
    st("lowercase", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "open"}, as_=ADMIN),
    st("unknown-name", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "ARCHIVED_X"}, as_=ADMIN),
    st("empty", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": ""}, as_=ADMIN),
    st("blank", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "   "}, as_=ADMIN),
    st("ok-is-not-a-status", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "OK"}, as_=ADMIN),
    st("numeric", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "0"}, as_=ADMIN),
    st("repeated-status", path=mint_path(ADM_PUT(OPEN_PLAIN), [("status", "OPEN"), ("status", "ARCHIVED")]), method="PUT", as_=ADMIN),
    st("missing-status", ADM_PUT(OPEN_PLAIN), method="PUT", as_=ADMIN),
    st("padded-with-spaces", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "  ARCHIVED  "}, as_=ADMIN),
    st("padded-with-tab", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "\tOPEN\t"}, as_=ADMIN),
    st("details-after-padded", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-admin-qag-status-ids", [
    st("unknown-id", ADM_PUT(UNKNOWN_QAG), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("not-a-uuid", ADM_PUT(NOT_UUID), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("deleted", ADM_PUT(DELETED), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("uppercase-spelling", ADM_PUT(OPEN_PLAIN_UPPER), method="PUT", query={"status": "OPEN"}, as_=ADMIN),
    st("lenient-short-spelling", ADM_PUT(OPEN_PLAIN_SHORT), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("xml-media-type", ADM_PUT(OPEN_FRESH), method="PUT", query={"status": "ARCHIVED", "mediaType": "xml"}, as_=ADMIN),
    st("get-method", ADM_PUT(OPEN_FRESH), query={"status": "ARCHIVED"}, as_=ADMIN),
    st("body-ignored", ADM_PUT(OPEN_OLD), method="PUT", query={"status": "MODERATED_ACCEPTED"}, body={"status": "ARCHIVED"}, as_=ADMIN),
    st("trailing-slash", ADM_PUT(OPEN_WEEKLY) + "/", method="PUT", query={"status": "OPEN"}, as_=ADMIN),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-admin-qag-status-effects", [
    st("list-before", NEW, query=list_q(MOD)),
    st("archive-open-locked", ADM_PUT(OPEN_LOCKED1), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("details-archived", "/qags/%s" % OPEN_LOCKED1, as_=REGULAR1),
    st("list-after-archive", NEW, query=list_q(MOD)),
    st("moderate-archived", RES, query=moderate_q(MOD, OPEN_LOCKED1, "NOK")),
    st("reopen-as-open", ADM_PUT(OPEN_LOCKED1), method="PUT", query={"status": "OPEN"}, as_=ADMIN),
    st("list-after-reopen", NEW, query=list_q(MOD)),
    st("latest-after-admin", LIST_LATEST, sleep=LIST_SLEEP, as_=REGULAR1),
], tags=TAG, dbdiff="step"))

# ---------------------------------------------------------------------------------------------------------------
# Lifecycle (lock, moderate, list, admin) and corrupt rows
# ---------------------------------------------------------------------------------------------------------------
F = "S7_lifecycle.yaml"
add(F, scenario("S7-lifecycle-lock-reject-archive", [
    st("list-0", NEW, query=list_q(MOD)),
    st("lock-plain-and-fresh", ACK, query=lock_q(MOD, OPEN_PLAIN + ";" + OPEN_FRESH)),
    st("list-1", NEW, query=list_q(MOD)),
    st("reject-plain", RES, query=moderate_q(MOD, OPEN_PLAIN, "NOK", kill="1", motif="charte", motif_id="m-9")),
    st("details-plain", "/qags/%s" % OPEN_PLAIN, as_=REGULAR1),
    st("list-2", NEW, query=list_q(MOD)),
    st("archive-fresh-while-locked", ADM_PUT(OPEN_FRESH), method="PUT", query={"status": "ARCHIVED"}, as_=ADMIN),
    st("moderate-fresh-archived", RES, query=moderate_q(MOD, OPEN_FRESH, "NOK")),
    st("lock-fresh-archived", ACK, query=lock_q(MOD, OPEN_FRESH)),
    st("list-3", NEW, query=list_q(MOD)),
    st("details-fresh", "/qags/%s" % OPEN_FRESH, as_=REGULAR2),
], tags=TAG, dbdiff="step"))

add(F, scenario("S7-lifecycle-reject-then-admin-select", [
    st("reject-locked-open", RES, query=moderate_q(MOD, OPEN_LOCKED2, "NOK", kill="1", motif="x")),
    st("admin-select", ADM_PUT(OPEN_LOCKED2), method="PUT", query={"status": "SELECTED_FOR_RESPONSE"}, as_=ADMIN),
    st("moderate-selected", RES, query=moderate_q(MOD, OPEN_LOCKED2, "OK")),
    st("admin-back-to-open", ADM_PUT(OPEN_LOCKED2), method="PUT", query={"status": "OPEN"}, as_=ADMIN),
    st("moderate-open-again", RES, query=moderate_q(MOD, OPEN_LOCKED2, "NOK")),
    st("list-final", NEW, query=list_q(MOD)),
], tags=TAG, dbdiff="step"))

F = "S7_corrupt_rows.yaml"
add(F, scenario("S7-corrupt-status-row", [
    st("list-ignores-row", NEW, query=list_q(MOD)),
    st("lock-corrupt", ACK, query=lock_q(MOD, OPEN_PLAIN)),
    st("moderate-corrupt", RES, query=moderate_q(MOD, OPEN_PLAIN, "NOK")),
    st("admin-corrupt", ADM_PUT(OPEN_PLAIN), method="PUT", query={"status": "OPEN"}, as_=ADMIN),
], tags=TAG, dbdiff="step", setup={"sql": ["UPDATE qags SET status = 99 WHERE id = '%s'" % ID(0x23)]}))

add(F, scenario("S7-corrupt-missing-thematique-row", [
    st("moderate-no-thematique", RES, query=moderate_q(MOD, OPEN_PLAIN, "NOK")),
], tags=TAG, dbdiff="step", setup={"sql": ["UPDATE qags SET thematique_id = 'th-unknown' WHERE id = '%s'" % ID(0x23)]}))

# ---------------------------------------------------------------------------------------------------------------
# Known divergences (skipped; see parity/divergences/S7.md)
# ---------------------------------------------------------------------------------------------------------------
F = "S7_known_divergences.yaml"
KNOWN = "S7-known-foundation"
# F-XML-ESC: Woodstox writes U+FFFE and U+FFFF as &#xfffe; / &#xffff; in attribute values (lock results
# content_id), internal/xmljava writes them raw. U+007F..U+009F are written raw in attributes by both (see
# S7_ack_qags.yaml); they are escaped only in element text (not reachable from the S7 routes).
add(F, scenario("S7-known-foundation-xml-escape-attribute-nonchar", [
    st("fffe-in-content-id-anonymous", ACK, query=lock_q("abc", "a￾b")),
    st("ffff-in-content-id-moderator", ACK, query=lock_q(MOD, "a￿b")),
], tags=[KNOWN], dbdiff="step",
    skip="F-XML-ESC (internal/xmljava): Woodstox writes U+FFFE / U+FFFF as &#xfffe; / &#xffff; in attributes, xmljava writes them raw"))
add(F, scenario("S7-known-c-xml-control-char-content-id", [
    # the anonymous error path echoes the content_id of the request in an attribute: U+0001 is refused by Woodstox
    # (plain text / attribute), the reference cuts the body (curl exit 18) with status 401, Go answers a clean 500
    st("anonymous-c0-in-content-id", ACK, query=lock_q("abc", "a\u0001b")),
    st("moderator-c0-in-content-id", ACK, query=lock_q(MOD, "a\u0001b")),
], tags=["S7-known-C"], dbdiff="step",
    skip="C-XML-CONTROL-CHAR (DIVERGENCES.md): Woodstox fails while writing an attribute with a control character; the reference cuts the body, Go answers 500"))
add(F, scenario("S7-known-foundation-xml-cdata-control-username", [
    st("list-with-control-username", NEW, query=list_q(MOD)),
], tags=[KNOWN], dbdiff="none",
    setup={"sql": ["UPDATE qags SET username = 'a' || chr(1) || 'b' WHERE id = '%s'" % ID(0x23)]},
    skip="F-XML-CTRL (internal/xmljava): Woodstox writes U+0001 raw in CDATA (200), MarshalChecked refuses it (500)"))

add(F, scenario("S7-known-foundation-cdata-end-marker", [
    st("list-with-end-marker-in-title", NEW, query=list_q(MOD)),
], tags=[KNOWN], dbdiff="none",
    setup={"sql": ["UPDATE qags SET title = 'x ]]> y' WHERE id = '%s'" % ID(0x23)]},
    skip="F-CDATA-END (httpx/xmljava): a CDATA with ]]> makes Woodstox fail while streaming: the reference answers 200 with a truncated body "
         "and an appended error map, Go answers 500 (see parity/divergences/S7.md)"))

HEADER_NOTE = None
if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "parity/scenarios"
    for fname, scs in scenarios.items():
        dump(scs, os.path.join(out, fname))
        print(fname, len(scs), "scenarios")
