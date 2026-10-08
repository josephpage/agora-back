"""POST/DELETE /consultations/{consultationId}/updates/{consultationUpdateId}/feedback."""
from common import *

CU1 = "cu0000000000000000000001"
CU2 = "cu0000000000000000000002"
CU3 = "cu0000000000000000000003"


def fb(cid, uid):
    return "/consultations/%s/updates/%s/feedback" % (cid, uid)


def post(id, cid, uid, positive, user, **kw):
    return step(id, fb(cid, uid), method="POST", body={"isPositive": positive}, **{"as": user}, **kw)


def run():
    out = []

    # feedback cycle on the latest update of consultation 1 with reads in between (caches follow the writes)
    d1 = "/v2/consultations/" + CONS[0]
    steps = [
        step("regular1-before", d1, **{"as": U1}), step("idle-before", d1, **{"as": IDLE}), step("anon-before", d1),
        step("updates-before", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1), **{"as": IDLE}),
        post("idle-positive", CONS[0], CU1, True, IDLE),
        step("regular1-after-idle", d1, **{"as": U1}),
        step("updates-after-idle", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1), **{"as": IDLE}),
        post("idle-positive-again", CONS[0], CU1, True, IDLE),
        post("idle-negative", CONS[0], CU1, False, IDLE),
        step("regular1-after-flip", d1, **{"as": U1}),
        post("regular3-positive", CONS[0], CU1, True, U("UserRegular3")),
        post("regular1-flip", CONS[0], CU1, False, U1),
        step("regular1-after-own", d1, **{"as": U1}),
        step("regular1-updates-after-own", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1), **{"as": U1}),
        step("anon-after", d1), step("anon-updates-after", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1)),
    ]
    out.append(scenario("S4-feedback-cycle-ongoing", steps, dbdiff="step"))

    # a finished consultation: every user reads the cached latest details, then writes
    d4 = "/v2/consultations/" + CONS[3]
    up405 = "up0000000000000000000405"
    steps = [
        step("anon-before", d4), step("idle-before", d4, **{"as": IDLE}),
        post("idle-positive", CONS[3], up405, True, IDLE),
        step("idle-after", d4, **{"as": IDLE}), step("anon-after", d4), step("regular1-after", d4, **{"as": U1}),
        post("regular1-negative", CONS[3], up405, False, U1),
        step("regular1-after-own", d4, **{"as": U1}), step("idle-after-2", d4, **{"as": IDLE}),
        post("regular4-positive-on-other-update", CONS[3], "up0000000000000000000404", True, U("UserRegular4")),
        step("regular4-after", d4, **{"as": U("UserRegular4")}),
        step("regular4-update-404", "/v2/consultations/%s/updates/up0000000000000000000404" % CONS[3], **{"as": U("UserRegular4")}),
        step("regular4-update-405", "/v2/consultations/%s/updates/%s" % (CONS[3], up405), **{"as": U("UserRegular4")}),
    ]
    out.append(scenario("S4-feedback-cycle-finished", steps, dbdiff="step"))

    # every kind of update of every consultation accepts a feedback, except the one shown before the answer
    steps = []
    for i, c in enumerate(consultations(), 1):
        for (uid, slug, kind) in updates_of(c):
            steps.append(post("c%d-%s-%s" % (i, kind, uid[-3:]), CONS[i - 1], uid, bool(i % 2), IDLE))
    out.append(scenario("S4-feedback-every-update", steps, dbdiff="step"))

    # rejections
    steps = [
        post("avant-reponse-update", CONS[0], "up0000000000000000000101", True, IDLE),
        post("unknown-update", CONS[0], "unknown", True, IDLE),
        post("unknown-consultation", "unknown", CU1, True, IDLE),
        post("update-by-slug", CONS[0], "lancement", True, IDLE),
        post("consultation-by-slug", "consultation-1", CU1, True, IDLE),
        post("update-of-another-consultation", CONS[0], "up0000000000000000000201", True, IDLE),
        post("unpublished-consultation", CONS[5], "up0000000000000000000605", True, IDLE),
        post("upper-update-id", CONS[0], CU1.upper(), True, IDLE),
        post("update-id-with-space", CONS[0], CU1 + "%20", True, IDLE),
        post("long-update-id", CONS[0], "u" * 300, True, IDLE),
        post("unicode", CONS[0], "%C3%A9", True, IDLE),
        post("future-update", CONS[0], "up0000000000000000000107", True, IDLE),
        post("future-commanditaire", CONS[0], "up0000000000000000000104", True, IDLE),
    ]
    out.append(scenario("S4-feedback-rejections", steps, dbdiff="step"))

    # request bodies
    p = fb(CONS[0], CU1)
    bodies = [
        ("empty-object", {}), ("string-true", {"isPositive": "true"}), ("string-false", {"isPositive": "false"}), ("string-yes", {"isPositive": "yes"}),
        ("one", {"isPositive": 1}), ("zero", {"isPositive": 0}), ("two", {"isPositive": 2}), ("float", {"isPositive": 1.5}), ("null", {"isPositive": None}),
        ("array", {"isPositive": [True]}), ("object", {"isPositive": {"a": 1}}), ("extra-fields", {"isPositive": True, "x": 1, "y": [1, 2]}),
        ("wrong-case", {"ispositive": True}), ("snake", {"is_positive": True}), ("json-array", [True]), ("json-null", None), ("json-string", "true"), ("json-number", 1),
        ("duplicate", None),
    ]
    steps = []
    for n, b in bodies:
        if n == "duplicate":
            steps.append(step("body-" + n, p, method="POST", bodyRaw='{"isPositive": true, "isPositive": false}', contentType="application/json", **{"as": IDLE}))
        elif n == "json-null":
            steps.append(step("body-" + n, p, method="POST", bodyRaw="null", contentType="application/json", **{"as": IDLE}))
        else:
            steps.append(step("body-" + n, p, method="POST", body=b, **{"as": IDLE}))
    steps += [
        step("raw-invalid-json", p, method="POST", bodyRaw="{", contentType="application/json", **{"as": IDLE}),
        step("raw-empty", p, method="POST", bodyRaw="", contentType="application/json", **{"as": IDLE}),
        step("raw-trailing-garbage", p, method="POST", bodyRaw='{"isPositive": true} xyz', contentType="application/json", **{"as": IDLE}),
        step("raw-comment", p, method="POST", bodyRaw='{"isPositive": true /* c */}', contentType="application/json", **{"as": IDLE}),
        step("raw-single-quotes", p, method="POST", bodyRaw="{'isPositive': true}", contentType="application/json", **{"as": IDLE}),
        step("content-type-text", p, method="POST", bodyRaw='{"isPositive": true}', contentType="text/plain", **{"as": IDLE}),
        step("content-type-form", p, method="POST", bodyRaw="isPositive=true", contentType="application/x-www-form-urlencoded", **{"as": IDLE}),
        step("content-type-missing", p, method="POST", bodyRaw='{"isPositive": true}', **{"as": IDLE}),
        step("content-type-charset", p, method="POST", bodyRaw='{"isPositive": true}', contentType="application/json;charset=ISO-8859-1", **{"as": IDLE}),
        step("no-body", p, method="POST", **{"as": IDLE}),
        step("final-valid", p, method="POST", body={"isPositive": True}, **{"as": IDLE}),
    ]
    out.append(scenario("S4-feedback-bodies", steps, dbdiff="step"))

    # authentication and other methods
    steps = [
        step("anonymous", p, method="POST", body={"isPositive": True}),
        step("bad-jwt", p, method="POST", body={"isPositive": True}, bearer="abc.def.ghi"),
        step("unknown-user", p, method="POST", body={"isPositive": True}, **{"as": "00000000-0000-4000-9000-0000000000ff"}),
        post("banned", CONS[0], CU1, True, BANNED),
        post("moderator", CONS[0], CU1, True, MODERATOR),
        post("admin", CONS[0], CU1, False, ADMIN),
        post("publisher", CONS[0], CU1, True, PUBLISHER),
        post("no-fcm", CONS[0], CU1, True, U("UserNoFcm2")),
        step("xml-response", p + "?mediaType=xml", method="POST", body={"isPositive": True}, **{"as": IDLE}),
        step("foo-response", p + "?mediaType=foo", method="POST", body={"isPositive": True}, **{"as": IDLE}),
        step("delete-anonymous", p, method="DELETE"),
        step("delete-user", p, method="DELETE", **{"as": U1}),
        step("delete-unknown-ids", fb("unknown", "unknown"), method="DELETE", **{"as": U1}),
        step("delete-xml", p + "?mediaType=xml", method="DELETE", **{"as": U1}),
        step("delete-with-body", p, method="DELETE", body={"isPositive": True}, **{"as": U1}),
        step("details-still-there", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1), **{"as": U1}),
        step("post-cors", p, method="POST", body={"isPositive": True}, headers={"Origin": "https://www.agora.gouv.fr"}, **{"as": IDLE}),
        step("cors-preflight", p, method="OPTIONS", headers={"Origin": "https://www.agora.gouv.fr", "Access-Control-Request-Method": "POST"}),
    ]
    out.append(scenario("S4-feedback-auth", steps, dbdiff="step"))

    # the statistics feature is off: the answer is recorded, without statistics
    steps = [post("idle-positive", CONS[0], CU1, True, IDLE), post("idle-negative", CONS[0], CU1, False, IDLE),
             step("details", "/v2/consultations/" + CONS[0], **{"as": U1}),
             step("updates", "/v2/consultations/%s/updates/%s" % (CONS[0], CU1), **{"as": IDLE})]
    out.append(scenario("S4-feedback-flag-disabled", steps, dbdiff="step",
                        setup={"redis": {"featureFlags::IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED": "false"}}))

    # a garbage flag: the row is written, then the statistics fail (HTTP 500); a user that made Kotlin throw stays
    # locked in the reference until it restarts (B-AGORAQUEUE): one dedicated user, last step
    steps = [post("flag-garbage-500", CONS[0], CU1, True, U("UserPairB1")),
             step("details-after", "/v2/consultations/" + CONS[0], **{"as": U1})]
    out.append(scenario("S4-feedback-flag-garbage", steps, dbdiff="step",
                        setup={"redis": {"featureFlags::IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED": "garbage"}}))

    # rounding of the ratios
    sql = []
    for n in range(1, 8):
        sql.append("INSERT INTO feedbacks_consultation_update (id, consultation_update_id, created_date, is_positive, updated_date, user_id) VALUES "
                   "('bbbbbbbb-0000-4000-a000-00000000000%d', 'up0000000000000000000405', now(), %d, now(), '00000000-0000-4000-9000-0000000000%02x')"
                   % (n, 1 if n <= 2 else 0, 0x20 + n))
    steps = [step("details-before", "/v2/consultations/" + CONS[3], **{"as": U("UserProfile1")}),
             post("regular1", CONS[3], up405, True, U1),
             step("details-after", "/v2/consultations/" + CONS[3], **{"as": U1}),
             post("regular4", CONS[3], up405, True, U("UserRegular4")),
             post("regular2", CONS[3], up405, True, U("UserRegular2"))]
    out.append(scenario("S4-feedback-ratios", steps, dbdiff="step", setup={"sql": sql}))

    return out
