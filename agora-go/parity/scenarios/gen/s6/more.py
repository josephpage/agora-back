"""Request bodies, headers, HTTP matrix and rate limit of POST /consultations/{consultationId}/responses."""
from helpers import *  # noqa: F401,F403
from helpers import U, step, scenario, setup, post, post_raw, results, results_xml, tsv, r_, body, qid, cid_, xff, TAG, SKIP, NA  # noqa: F401
from helpers import C1, C2, C3, C4, C5, C6, C7, IDLE, ADMIN, MODERATOR, PUBLISHER, BANNED, R, NOFCM1, NOFCM2, P, INVALID_PROFILE, FRESH_C1, ANY

SC = lambda name, steps, **kw: scenario(name, steps, TAG, dbdiff="step", setup=kw.pop("setup_", setup()), **kw)

# users that can be used once in a scenario (NoFcm1 has no last connection date: avoid it)
USERS = [u for u in ANY if u != NOFCM1]
# the reference keeps the AgoraQueue slot of a user whose request threw (B-AGORAQUEUE): the 500 cases use users (UserMass, regular
# users never used elsewhere) that are not used again
MASS = ["00000000-0000-4000-9000-0000000000%02x" % (0x40 + i) for i in range(10)]
Q1 = '{"questionId":"%s","choiceIds":["%s"],"responseText":null}' % (qid(2, 1), cid_(2, 1, 1))


def chunks(items, n):
    return [items[i:i + n] for i in range(0, len(items), n)]


def bodies():
    out = []
    q1 = {"questionId": qid(2, 1), "choiceIds": [cid_(2, 1, 1)], "responseText": None}
    cases = [
        # (name, kind, value): kind "json" (serialized by the runner) or "raw"
        ("ok", "json", {"consultationId": "x", "responses": [q1]}),
        ("no-consultation-id", "json", {"responses": [q1]}),
        ("null-consultation-id", "json", {"consultationId": None, "responses": [q1]}),
        ("number-consultation-id", "json", {"consultationId": 12, "responses": [q1]}),
        ("object-consultation-id", "json", {"consultationId": {"a": 1}, "responses": [q1]}),
        ("no-responses", "json", {"consultationId": "x"}),
        ("null-responses", "json", {"consultationId": "x", "responses": None}),
        ("string-responses", "json", {"consultationId": "x", "responses": "a"}),
        ("object-responses", "json", {"consultationId": "x", "responses": {"questionId": qid(2, 1)}}),
        ("empty-responses", "json", {"consultationId": "x", "responses": []}),
        ("string-element", "json", {"consultationId": "x", "responses": ["a"]}),
        ("number-element", "json", {"consultationId": "x", "responses": [1]}),
        ("array-element", "json", {"consultationId": "x", "responses": [[]]}),
        ("empty-object-element", "json", {"consultationId": "x", "responses": [{}]}),
        ("no-question-id", "json", {"consultationId": "x", "responses": [{"choiceIds": [cid_(2, 1, 1)], "responseText": None}]}),
        ("null-question-id", "json", {"consultationId": "x", "responses": [{"questionId": None, "choiceIds": [], "responseText": None}]}),
        ("number-question-id", "json", {"consultationId": "x", "responses": [{"questionId": int(qid(2, 1)), "choiceIds": [cid_(2, 1, 1)], "responseText": None}]}),
        ("float-question-id", "json", {"consultationId": "x", "responses": [{"questionId": 201.0, "choiceIds": [cid_(2, 1, 1)], "responseText": None}]}),
        ("bool-question-id", "json", {"consultationId": "x", "responses": [{"questionId": True, "choiceIds": [], "responseText": None}]}),
        ("no-choice-ids", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "responseText": "t"}]}),
        ("null-choice-ids", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": None, "responseText": "t"}]}),
        ("string-choice-ids", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 1), "choiceIds": cid_(2, 1, 1), "responseText": None}]}),
        ("object-choice-ids", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 1), "choiceIds": {"a": 1}, "responseText": None}]}),
        ("number-choice-id", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 1), "choiceIds": [2011], "responseText": None}]}),
        ("object-choice-id", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 1), "choiceIds": [{}], "responseText": None}]}),
        ("no-response-text", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": []}]}),
        ("number-response-text", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": [], "responseText": 42}]}),
        ("bool-response-text", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": [], "responseText": False}]}),
        ("object-response-text", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": [], "responseText": {"a": 1}}]}),
        ("array-response-text", "json", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": [], "responseText": ["a"]}]}),
        ("extra-fields", "json", {"consultationId": "x", "extra": [1, {"a": 2}], "responses": [dict(q1, extra="y")]}),
        ("wrong-case", "json", {"consultationid": "x", "Responses": [q1]}),
        ("duplicate-keys", "raw", '{"consultationId": "x", "responses": [], "responses": [%s]}' % Q1),
        ("duplicate-keys-reverse", "raw", '{"consultationId": "x", "responses": [%s], "responses": []}' % Q1),
        ("unicode-escapes", "raw", '{"consultationId": "x", "responses": [{"questionId": "%s", "choiceIds": [], "responseText": "caf\\u00e9 \\ud83d\\ude00 \\u003cb\\u003e"}]}' % qid(2, 3)),
        ("trailing-garbage", "raw", '{"consultationId": "x", "responses": []} garbage'),
        ("comment", "raw", '{"consultationId": "x", /* c */ "responses": []}'),
        ("single-quotes", "raw", "{'consultationId': 'x', 'responses': []}"),
        ("invalid-json", "raw", "{"),
        ("empty", "raw", ""),
        ("json-null", "raw", "null"),
        ("json-array", "raw", "[]"),
        ("json-string", "raw", '"x"'),
        ("json-number", "raw", "1"),
        ("bom", "raw", "﻿" + '{"consultationId": "x", "responses": []}'),
        ("leading-whitespace", "raw", '  \n {"consultationId": "x", "responses": []}'),
        ("big-number", "raw", '{"consultationId": 1e999, "responses": []}'),
        ("deep", "raw", '{"consultationId": "x", "responses": [], "deep": ' + "[" * 500 + "]" * 500 + "}"),
    ]
    steps = []
    for i, (name, kind, value) in enumerate(cases):
        u = USERS[i % len(USERS)]
        if kind == "json":
            steps.append(step("body-%s" % name, "/consultations/%s/responses" % C2, **{"method": "POST", "body": value, "as": u, "headers": xff()}))
        else:
            steps.append(post_raw("body-%s" % name, C2, value, u))
    for n, ch in enumerate(chunks(steps, 22)):
        out.append(SC("S6-answer-bodies-%d" % (n + 1), ch))

    # content types and headers
    ct = [
        ("charset-iso", "application/json;charset=ISO-8859-1", '{"consultationId": "x", "responses": [{"questionId": "%s", "choiceIds": [], "responseText": "café"}]}' % qid(2, 3)),
        ("charset-utf16", "application/json;charset=UTF-16", None),
        ("text-plain", "text/plain", '{"consultationId": "x", "responses": []}'),
        ("form", "application/x-www-form-urlencoded", "consultationId=x"),
        ("missing", None, '{"consultationId": "x", "responses": []}'),
        ("json-vendor", "application/vnd.api+json", '{"consultationId": "x", "responses": []}'),
        ("json-star", "application/*+json", '{"consultationId": "x", "responses": []}'),
        ("json-upper", "APPLICATION/JSON", '{"consultationId": "x", "responses": []}'),
        ("json-bad-charset", "application/json;charset=nope", '{"consultationId": "x", "responses": []}'),
        ("multipart", "multipart/form-data; boundary=x", "--x--"),
    ]
    steps = []
    for i, (name, ctype, raw) in enumerate(ct):
        u = USERS[i]
        if raw is None:
            continue
        steps.append(post_raw("ct-%s" % name, C2, raw, u, content_type=ctype))
    steps.append(step("no-body", "/consultations/%s/responses" % C2, **{"method": "POST", "as": USERS[20], "headers": xff()}))
    steps.append(step("no-body-no-ct", "/consultations/%s/responses" % C2, **{"method": "POST", "as": USERS[21], "headers": xff(), "contentType": "none"}))
    out.append(SC("S6-answer-content-types", steps))

    # null elements: the Kotlin NullPointerException is a 500 (and the user stays locked on the reference: one user per case)
    nulls = [
        ("null-response-element", {"consultationId": "x", "responses": [None]}),
        ("null-response-element-after-ok", {"consultationId": "x", "responses": [q1, None]}),
        ("null-choice-unique", {"consultationId": "x", "responses": [{"questionId": qid(2, 1), "choiceIds": [None], "responseText": None}]}),
        ("null-choice-multiple", {"consultationId": "x", "responses": [{"questionId": qid(2, 2), "choiceIds": [None, cid_(2, 2, 1)], "responseText": None}]}),
        ("null-choices-multiple-dup", {"consultationId": "x", "responses": [{"questionId": qid(2, 2), "choiceIds": [None, None], "responseText": None}]}),
        ("null-choice-open", {"consultationId": "x", "responses": [{"questionId": qid(2, 3), "choiceIds": [None], "responseText": "t"}]}),
        ("null-choice-conditional", {"consultationId": "x", "responses": [{"questionId": qid(2, 4), "choiceIds": [None], "responseText": None}]}),
    ]
    steps = []
    for i, (name, value) in enumerate(nulls):
        steps.append(step("body-%s" % name, "/consultations/%s/responses" % C2, **{"method": "POST", "body": value, "as": MASS[i], "headers": xff()}))
    steps.append(results("results", C2))
    steps.append(step("answered", "/consultations/answered/1", **{"as": MASS[0]}))
    out.append(SC("S6-answer-null-elements", steps))
    return out


def http_matrix():
    ok = [r_(qid(2, 1), [cid_(2, 1, 1)])]
    steps = [
        post("anonymous", C2, ok),
        step("bad-jwt", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok), "bearer": "abc.def.ghi", "headers": xff()}),
        step("unknown-user", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok), "as": "00000000-0000-4000-9000-0000000000ff", "headers": xff()}),
        post("banned", C2, ok, BANNED),
        post("moderator", C2, ok, MODERATOR),
        post("admin", C2, ok, ADMIN),
        post("publisher", C2, ok, PUBLISHER),
        post("xml-response", C2, [], IDLE, after="?mediaType=xml"),
        post("foo-media-type", C2, [], R[4], after="?mediaType=foo"),
        post("accept-text", C2, [], R[5], headers={"Accept": "text/plain"}),
        post("accept-xml", C2, [], R[1], headers={"Accept": "application/xml"}),
        post("accept-json", C2, [], R[2], headers={"Accept": "application/json"}),
        step("get", "/consultations/%s/responses" % C2, **{"as": IDLE}),
        step("head", "/consultations/%s/responses" % C2, **{"method": "HEAD", "as": IDLE}),
        step("put", "/consultations/%s/responses" % C2, **{"method": "PUT", "as": IDLE, "body": body(ok)}),
        step("delete", "/consultations/%s/responses" % C2, **{"method": "DELETE", "as": IDLE}),
        step("options", "/consultations/%s/responses" % C2, **{"method": "OPTIONS", "as": IDLE}),
        step("cors-preflight", "/consultations/%s/responses" % C2, **{"method": "OPTIONS", "headers": {"Origin": "https://www.agora.gouv.fr", "Access-Control-Request-Method": "POST"}}),
        post("cors-post", C2, [], P[1], headers={"Origin": "https://www.agora.gouv.fr"}),
        post("cors-bad-origin", C2, [], P[2], headers={"Origin": "https://evil.example"}),
        post("trailing-slash", C2, [], P[3], after="/"),
        step("no-consultation-id", "/consultations//responses", **{"method": "POST", "as": P[4], "body": body([]), "headers": xff()}),
        post("path-semicolon", C2, [], P[5], after=";x=1"),
        post("cookie-auth", C2, [], P[6], headers={"Cookie": "auth-jwt=garbage"}),
    ]
    return [SC("S6-answer-http", steps)]


# the IP hashes of 10.7.0.N (computed by the reference's IpAddressUtils: REMOTE_ADDRESS_* of parity/env/common.env)
IPH = {
    1: "0189f4db9d43f745d13eb33b2cc20d84dac2e03f2e5ef030ac4844434367997d", 2: "8a4a27bcf1b5f3d826ede5edb50284f3a40a17c094d7ddc2f4f1ec235000462a",
    3: "c41677c3ba05c7cff9831efff0f871bc8ca41bc75c92744693335da85488ae36", 4: "7791da260ff5e7e38760b18b0a10b7567046068bbe4043a1ebd2a40e5e86fe46",
    5: "c785020e09dac89533b2d298d62539c1891989637330c6ad2166be8fadef8b75", 6: "0a72222140c968ff9f9b9aabb277bdc48499ecf74a99ddbd18f5c57d6966cd9e",
    7: "e3b47d0faf7c990a266438615124e1e19b8b143e4e3fb1b24bb81b57c019f1ef", 8: "6aa9e54a9d67918ac23350c06038e961c5c34daddbb0b9bfc289c831bccaa58f",
    9: "87fd67f4474de09a9836f134f19b2c362c68ac52605bb39d89e794bc7407945b", 10: "200b9221f6a77559c3153368fef3a328f9c933df2b81ff0dcd3cf0be72413d65",
    11: "7c0fe176b4bed557163c5c4c4a3db260a698f7af6ae0cb6fdd8453e0a3eabad3", 12: "2701c1b993d438c69f14a127119a93b08f4e0a6e728e2dd2ee3ee5be8f2a8817",
}


def ip(n):
    return {"X-Forwarded-For": "10.7.0.%d" % n}


def rl_key(n):
    return "consultationResponseRateLimit::" + IPH[n]


def ratelimit():
    out = []
    bad = [r_("999")]  # always a 400 after the counter

    # 1. the 21st request of an IP is a 429, the other IPs are free, reads are not limited
    steps = [
        step("twenty-two", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(1), "repeat": 22}),
        step("still-limited", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": R[4], "headers": ip(1)}),
        step("limited-valid-answer", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok_answer(2)), "as": R[5], "headers": ip(1)}),
        step("limited-xml", "/consultations/%s/responses?mediaType=xml" % C2, **{"method": "POST", "body": body(bad), "as": R[5], "headers": ip(1)}),
        step("other-ip", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(2)}),
        step("other-ip-valid", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok_answer(2)), "as": R[5], "headers": ip(2)}),
        results("results-not-limited", C2),
        step("details-not-limited", "/v2/consultations/%s" % C2, **{"as": IDLE}),
        step("anonymous-not-counted", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "headers": ip(3), "repeat": 25}),
        step("invalid-json-not-counted", "/consultations/%s/responses" % C2, **{"method": "POST", "bodyRaw": "{", "contentType": "application/json", "as": IDLE, "headers": ip(3), "repeat": 25}),
        step("then-counted", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(3), "repeat": 21}),
    ]
    out.append(SC("S6-ratelimit-window", steps))

    # 2. successful answers count: the 21st user from one IP is refused, even if it never answered
    steps = []
    for i, u in enumerate(USERS[:23]):
        steps.append(step("answer-%d" % (i + 1), "/consultations/%s/responses" % C2, **{"method": "POST", "body": body([r_(qid(2, 1), [cid_(2, 1, 1 + i % 3)])]), "as": u, "headers": ip(4)}))
    steps.append(results("results", C2))
    steps.append(step("answered", "/consultations/answered/1", **{"as": USERS[20]}))
    out.append(SC("S6-ratelimit-answers", steps))

    # 3. counters written by hand: the value is an Int (Jackson), anything else counts for 0
    seeds = {1: "19", 2: "20", 3: "21", 4: "0", 5: "-1", 6: "garbage", 7: '"20"', 8: "20.0", 9: "99999999999", 10: "2147483647", 11: "", 12: "00025"}
    steps = []
    for n, v in seeds.items():
        steps.append(step("ip%d-first" % n, "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(n)}))
        steps.append(step("ip%d-second" % n, "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(n)}))
    steps.append(step("ip6-twenty", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(6), "repeat": 20}))
    steps.append(step("ip4-twenty", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(4), "repeat": 20}))
    steps.append(step("ip5-twenty", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": ip(5), "repeat": 20}))
    out.append(SC("S6-ratelimit-preset-counters", steps, setup_=setup(redis={rl_key(n): v for n, v in seeds.items()})))

    # 4. a limited request does not reach the queue nor the use cases (nothing is written)
    steps = [
        step("limited", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok_answer(2)), "as": IDLE, "headers": ip(1)}),
        step("not-limited", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok_answer(2)), "as": IDLE, "headers": ip(2)}),
        step("limited-again", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ok_answer(2)), "as": R[4], "headers": ip(1)}),
        results("results", C2),
    ]
    out.append(SC("S6-ratelimit-limited-writes-nothing", steps, setup_=setup(redis={rl_key(1): "20"})))

    # 5. which address counts: X-Forwarded-For, then X-Remote-Address
    steps = [
        step("xff-list", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Forwarded-For": "10.7.0.1, 9.9.9.9"}}),
        step("xff-list-again", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Forwarded-For": "10.7.0.1, 8.8.8.8"}}),
        step("xff-first-only", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Forwarded-For": "10.7.0.1"}}),
        step("remote-address", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Remote-Address": "10.7.0.1"}}),
        step("no-header", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE}),
        step("no-header-again", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE}),
        step("xff-garbage", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Forwarded-For": "garbage"}}),
        step("xff-empty", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(bad), "as": IDLE, "headers": {"X-Forwarded-For": ""}}),
    ]
    out.append(SC("S6-ratelimit-address", steps, setup_=setup(redis={rl_key(1): "19"})))
    return out


def ok_answer(c):
    return [r_(qid(c, 1), [cid_(c, 1, 1)])]
