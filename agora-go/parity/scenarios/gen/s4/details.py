"""GET /v2/consultations/{idOrSlug}, GET /v2/consultations/{idOrSlug}/updates/{updateIdOrSlug}, GET /consultations."""
from common import *


def run_details():
    out = []

    # every consultation by id and by slug, anonymous, twice (cache)
    steps = []
    for i, (cid, slug) in enumerate(zip(CONS, SLUGS), 1):
        steps.append(step("c%d-id" % i, "/v2/consultations/" + cid))
        steps.append(step("c%d-slug" % i, "/v2/consultations/" + slug))
        steps.append(step("c%d-id-again" % i, "/v2/consultations/" + cid))
    out.append(scenario("S4-details-anonymous", steps))

    # users: answered and unanswered views, banned/moderator/admin, feedback of the user
    users = [("regular1", U1), ("regular2", U("UserRegular2")), ("regular3", U("UserRegular3")), ("regular4", U("UserRegular4")),
             ("profile1", U("UserProfile1")), ("idle", IDLE), ("banned", BANNED), ("moderator", MODERATOR), ("admin", ADMIN),
             ("publisher", PUBLISHER)]
    for name, uid in users:
        steps = []
        for i, cid in enumerate(CONS, 1):
            steps.append(step("c%d" % i, "/v2/consultations/" + cid, **{"as": uid}))
        steps.append(step("c1-slug", "/v2/consultations/consultation-1", **{"as": uid}))
        steps.append(step("c4-again", "/v2/consultations/" + CONS[3], **{"as": uid}))
        out.append(scenario("S4-details-user-" + name, steps))

    # ids that do not exist / odd path segments
    odd = [
        ("unknown", "unknown"), ("empty-ish-space", "%20"), ("upper-slug", "CONSULTATION-1"), ("upper-id", "CO0000000000000000000001"),
        ("trailing-space", "consultation-1%20"), ("leading-space", "%20consultation-1"), ("unicode", "%C3%A9t%C3%A9"),
        ("emoji", "%F0%9F%99%82"), ("long", "x" * 300), ("dots", "a.b.c"), ("percent-encoded-slug", "consultation%2D1"),
        ("null-text", "null"), ("uuid", "00000000-0000-4000-8000-0000000000a1"), ("star", "*"), ("brackets", "a%5Bb%5D"),
        ("quote", "a%22b"), ("amp", "a%26b"), ("plus", "a+b"), ("semicolon-param", "consultation-1%3Bx"),
        ("comma", "co0000000000000000000001,co0000000000000000000002"), ("ampersand-filter", "consultation-1%26filters"),
        ("dollar", "%24in"), ("nul", "a%00b"), ("backslash", "a%5Cb"),
    ]
    steps = [step(n, "/v2/consultations/" + p) for n, p in odd]
    steps += [step("no-id", "/v2/consultations/"), step("double-slash", "/v2/consultations//"),
              step("encoded-slash", "/v2/consultations/a%2Fb"), step("encoded-dot", "/v2/consultations/%2E%2E")]
    out.append(scenario("S4-details-odd-ids", steps))

    # HTTP matrix
    p = "/v2/consultations/" + CONS[0]
    steps = [
        step("anon", p), step("user", p, **{"as": U1}), step("banned", p, **{"as": BANNED}),
        step("unknown-user-jwt", p, **{"as": "00000000-0000-4000-9000-0000000000ff"}), step("bad-jwt", p, bearer="abc.def.ghi"),
        step("xml", p + "?mediaType=xml"), step("xml-upper", p + "?mediaType=XML"),
        step("json-explicit", p + "?mediaType=json"), step("foo", p + "?mediaType=foo"), step("empty-media", p + "?mediaType="),
        step("other-params", p + "?x=1&y=%C3%A9"), step("accept-xml", p, headers={"Accept": "application/xml"}),
        step("accept-text", p, headers={"Accept": "text/plain"}), step("head", p, method="HEAD"), step("head-xml", p + "?mediaType=xml", method="HEAD"),
        step("head-foo", p + "?mediaType=foo", method="HEAD"),
        step("post", p, method="POST"), step("put", p, method="PUT"), step("delete", p, method="DELETE"), step("options", p, method="OPTIONS"),
        step("cors-get", p, headers={"Origin": "https://www.agora.gouv.fr"}),
        step("cors-preflight", p, method="OPTIONS", headers={"Origin": "https://www.agora.gouv.fr", "Access-Control-Request-Method": "GET"}),
        step("trailing-slash", p + "/"), step("sub-path", p + "/x"), step("unknown-xml", "/v2/consultations/unknown?mediaType=xml"),
        step("unknown-foo", "/v2/consultations/unknown?mediaType=foo"), step("unknown-head", "/v2/consultations/unknown", method="HEAD"),
        step("if-none-match", p, headers={"If-None-Match": "\"x\""}),
    ]
    out.append(scenario("S4-details-http", steps))

    # /api/public twins
    steps = []
    for i in (1, 3, 4, 6):
        steps.append(step("public-c%d" % i, "/api/public/consultations/" + CONS[i - 1]))
        steps.append(step("public-c%d-slug" % i, "/api/public/consultations/" + SLUGS[i - 1], **{"as": U1}))
    steps += [step("public-unknown", "/api/public/consultations/unknown"), step("public-xml", "/api/public/consultations/" + CONS[0] + "?mediaType=xml"),
              step("public-head", "/api/public/consultations/" + CONS[0], method="HEAD"), step("public-post", "/api/public/consultations/" + CONS[0], method="POST")]
    out.append(scenario("S4-details-api-public", steps))

    # XML of every consultation kind
    # (the unanswered view of an ongoing consultation: the other views have `goals: null`, see S4-known-xml-null-list)
    steps = [step("xml-c%d" % i, "/v2/consultations/%s?mediaType=xml" % CONS[i - 1], **{"as": IDLE}) for i in (1, 2, 3, 6)]
    steps += [step("xml-c%d-slug" % i, "/v2/consultations/consultation-%d?mediaType=xml" % i) for i in (1, 2, 3, 6)]
    out.append(scenario("S4-details-xml-ongoing", steps))

    # feature flag of the feedback on updates
    for label, value in (("disabled", "false"), ("garbage", "garbage"), ("enabled", "true")):
        steps = [step("c4-anon", "/v2/consultations/" + CONS[3]), step("c4-user", "/v2/consultations/" + CONS[3], **{"as": U1}),
                 step("c1-user", "/v2/consultations/" + CONS[0], **{"as": U1}), step("c1-anon", "/v2/consultations/" + CONS[0]),
                 step("c2-user", "/v2/consultations/" + CONS[1], **{"as": U("UserRegular3")}),
                 step("c3-user", "/v2/consultations/" + CONS[2], **{"as": U("UserProfile1")}),
                 step("c4-user-again", "/v2/consultations/" + CONS[3], **{"as": U1})]
        out.append(scenario("S4-details-feedback-flag-" + label, steps,
                            setup={"redis": {"featureFlags::IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED": value}}))

    # participant count follows the data at the first call
    sql = ["INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES "
           "('aaaaaaaa-0000-4000-a000-000000000001', 'co0000000000000000000002', now(), '00000000-0000-4000-9000-000000000001'), "
           "('aaaaaaaa-0000-4000-a000-000000000002', 'co0000000000000000000002', now(), '00000000-0000-4000-9000-000000000001'), "
           "('aaaaaaaa-0000-4000-a000-000000000003', 'co0000000000000000000002', now(), '00000000-0000-4000-9000-000000000002')"]
    steps = [step("c2-count-1", "/v2/consultations/" + CONS[1]), step("c2-user", "/v2/consultations/" + CONS[1], **{"as": U1}),
             step("c2-count-2", "/v2/consultations/" + CONS[1]), step("c2-updates", "/v2/consultations/%s/updates/lancement" % CONS[1], **{"as": U("UserRegular2")}),
             step("c4-count", "/v2/consultations/" + CONS[3])]
    out.append(scenario("S4-details-answered-by-sql", steps, setup={"sql": sql}))

    # user ids: a user that is not a UUID cannot exist; users with several answers (duplicate row)
    steps = [step("c4-profile1-duplicate-row", "/v2/consultations/" + CONS[3], **{"as": U("UserProfile1")}),
             step("c4-profile1-updates", "/v2/consultations/%s/updates/fin-de-la-consultation" % CONS[3], **{"as": U("UserProfile1")}),
             step("c1-no-profile-user", "/v2/consultations/" + CONS[0], **{"as": U("UserNoFcm1")}),
             step("c1-invalid-profile", "/v2/consultations/" + CONS[0], **{"as": U("UserProfileInvalid")})]
    out.append(scenario("S4-details-special-users", steps))
    return out


def run_updates():
    out = []
    cs = consultations()

    # every update of every consultation, by id and by slug, anonymous and answered user
    for i, c in enumerate(cs, 1):
        cid = CONS[i - 1]
        steps = []
        for (uid, slug, kind) in updates_of(c):
            steps.append(step("%s-id" % kind + ("-" + uid[-3:] if kind == "autre" else ""), "/v2/consultations/%s/updates/%s" % (cid, uid)))
            steps.append(step("%s-slug" % kind + ("-" + uid[-3:] if kind == "autre" else ""), "/v2/consultations/%s/updates/%s" % (SLUGS[i - 1], slug), **{"as": U1}))
        steps.append(step("by-consultation-slug-anon", "/v2/consultations/%s/updates/%s" % (SLUGS[i - 1], updates_of(c)[0][1])))
        steps.append(step("unknown-update", "/v2/consultations/%s/updates/unknown" % cid))
        steps.append(step("api-public", "/api/public/consultations/%s/updates/%s" % (cid, updates_of(c)[1][0])))
        out.append(scenario("S4-updates-consultation-%d" % i, steps))

    # users with / without feedback on the seeded updates
    steps = []
    for name in ("UserRegular1", "UserRegular2", "UserRegular3", "UserRegular4", "UserIdle", "UserProfile1", "UserBanned"):
        steps.append(step("cu1-" + name, "/v2/consultations/%s/updates/cu0000000000000000000001" % CONS[0], **{"as": U(name)}))
        steps.append(step("cu2-" + name, "/v2/consultations/%s/updates/cu0000000000000000000002" % CONS[1], **{"as": U(name)}))
        steps.append(step("cu3-" + name, "/v2/consultations/%s/updates/cu0000000000000000000003" % CONS[2], **{"as": U(name)}))
    out.append(scenario("S4-updates-user-feedback", steps))

    # unknown things, odd ids, wrong consultation
    steps = [
        step("unknown-consultation", "/v2/consultations/unknown/updates/lancement"),
        step("update-of-another-consultation", "/v2/consultations/%s/updates/up0000000000000000000201" % CONS[0]),
        step("empty-update", "/v2/consultations/%s/updates/%%20" % CONS[0]),
        step("upper-update", "/v2/consultations/%s/updates/LANCEMENT" % CONS[0]),
        step("update-unicode", "/v2/consultations/%s/updates/%%C3%%A9" % CONS[0]),
        step("update-long", "/v2/consultations/%s/updates/%s" % (CONS[0], "y" * 300)),
        step("update-null", "/v2/consultations/%s/updates/null" % CONS[0]),
        step("no-update-id", "/v2/consultations/%s/updates/" % CONS[0]),
        step("unpublished-consultation", "/v2/consultations/%s/updates/lancement" % CONS[5]),
        step("unpublished-slug", "/v2/consultations/consultation-6/updates/actualite-1"),
        step("future-update", "/v2/consultations/%s/updates/up0000000000000000000107" % CONS[0]),
        step("future-commanditaire", "/v2/consultations/%s/updates/up0000000000000000000104" % CONS[0]),
        step("future-analyse", "/v2/consultations/%s/updates/up0000000000000000000203" % CONS[1]),
        step("xml", "/v2/consultations/%s/updates/lancement?mediaType=xml" % CONS[0]),
        step("foo", "/v2/consultations/%s/updates/lancement?mediaType=foo" % CONS[0]),
        step("head", "/v2/consultations/%s/updates/lancement" % CONS[0], method="HEAD"),
        step("post", "/v2/consultations/%s/updates/lancement" % CONS[0], method="POST"),
        step("options", "/v2/consultations/%s/updates/lancement" % CONS[0], method="OPTIONS"),
        step("cors", "/v2/consultations/%s/updates/lancement" % CONS[0], headers={"Origin": "https://www.agora.gouv.fr"}),
    ]
    out.append(scenario("S4-updates-odd", steps))

    for label, value in (("disabled", "false"), ("garbage", "garbage")):
        steps = [step("cu1-user", "/v2/consultations/%s/updates/cu0000000000000000000001" % CONS[0], **{"as": U1}),
                 step("cu1-anon", "/v2/consultations/%s/updates/cu0000000000000000000001" % CONS[0]),
                 step("avant-anon", "/v2/consultations/%s/updates/lancement" % CONS[0]),
                 step("avant-user", "/v2/consultations/%s/updates/lancement" % CONS[0], **{"as": U1})]
        out.append(scenario("S4-updates-flag-" + label, steps,
                            setup={"redis": {"featureFlags::IS_FEEDBACK_ON_CONSULTATION_UPDATE_ENABLED": value}}))
    return out


def run_preview():
    out = []
    steps = [step("anon", "/consultations"), step("anon-again", "/consultations")]
    for name in ("UserRegular1", "UserRegular2", "UserRegular3", "UserRegular4", "UserRegular5", "UserProfile1", "UserProfile5", "UserIdle",
                 "UserBanned", "UserModerator", "UserAdmin", "UserPublisher", "UserNoFcm1", "UserProfileInvalid"):
        steps.append(step("user-" + name, "/consultations", **{"as": U(name)}))
    out.append(scenario("S4-preview-users", steps))
    p = "/consultations"
    steps = [
        step("anon", p), step("user", p, **{"as": U1}), step("banned", p, **{"as": BANNED}), step("bad-jwt", p, bearer="abc.def.ghi"),
        step("unknown-user-jwt", p, **{"as": "00000000-0000-4000-9000-0000000000ff"}),
        step("xml", p + "?mediaType=xml"), step("xml-user", p + "?mediaType=xml", **{"as": U1}), step("xml-moderator", p + "?mediaType=xml", **{"as": MODERATOR}),
        step("foo", p + "?mediaType=foo"), step("json", p + "?mediaType=json"), step("head", p, method="HEAD"), step("head-xml", p + "?mediaType=xml", method="HEAD"),
        step("post", p, method="POST"), step("put", p, method="PUT"), step("delete", p, method="DELETE"), step("options", p, method="OPTIONS"),
        step("cors", p, headers={"Origin": "https://www.agora.gouv.fr"}), step("accept-xml", p, headers={"Accept": "application/xml"}),
        step("trailing-slash", p + "/"), step("other-params", p + "?territory=Nord&x=%FF"),
    ]
    out.append(scenario("S4-preview-http", steps))
    return out


def run_questions():
    out = []
    steps = []
    for i, cid in enumerate(CONS, 1):
        steps.append(step("c%d" % i, "/consultations/%s/questions" % cid))
        steps.append(step("c%d-user" % i, "/consultations/%s/questions" % cid, **{"as": U1}))
    steps += [step("c1-again", "/consultations/%s/questions" % CONS[0])]
    steps += [step("slug-not-resolved", "/consultations/consultation-1/questions"), step("unknown", "/consultations/unknown/questions"),
              step("upper", "/consultations/CO0000000000000000000001/questions"), step("space", "/consultations/%20/questions"),
              step("unicode", "/consultations/%C3%A9/questions"), step("long", "/consultations/%s/questions" % ("z" * 300)),
              step("xml", "/consultations/%s/questions?mediaType=xml" % CONS[0]), step("xml-c4", "/consultations/%s/questions?mediaType=XML" % CONS[3]),
              step("foo", "/consultations/%s/questions?mediaType=foo" % CONS[0]), step("json", "/consultations/%s/questions?mediaType=json" % CONS[0]),
              step("head", "/consultations/%s/questions" % CONS[0], method="HEAD"), step("head-xml", "/consultations/%s/questions?mediaType=xml" % CONS[0], method="HEAD"),
              step("post", "/consultations/%s/questions" % CONS[0], method="POST"), step("options", "/consultations/%s/questions" % CONS[0], method="OPTIONS"),
              step("cors", "/consultations/%s/questions" % CONS[0], headers={"Origin": "https://www.agora.gouv.fr"}),
              step("bad-jwt", "/consultations/%s/questions" % CONS[0], bearer="abc.def.ghi"),
              step("banned", "/consultations/%s/questions" % CONS[0], **{"as": BANNED}),
              step("if-none-match", "/consultations/%s/questions" % CONS[0], headers={"If-None-Match": "\"x\""}),
              step("trailing-slash", "/consultations/%s/questions/" % CONS[0]), step("no-id", "/consultations//questions"),
              step("api-public", "/api/public/consultations/%s/questions" % CONS[0])]
    out.append(scenario("S4-questions", steps))
    return out
