"""POST /consultations/{consultationId}/responses: valid answers, rejections, texts, bodies, users."""
from helpers import *  # noqa: F401,F403
from helpers import U, step, scenario, setup, post, post_raw, results, results_xml, tsv, r_, body, qid, cid_, xff, TAG, SKIP, NA  # noqa: F401
from helpers import C1, C2, C3, C4, C5, C6, C7, IDLE, ADMIN, MODERATOR, PUBLISHER, BANNED, R, NOFCM1, NOFCM2, P, INVALID_PROFILE, FRESH_C1, ANY, all_unique_answer

SC = lambda name, steps, **kw: scenario(name, steps, TAG, dbdiff="step", setup=kw.pop("setup_", setup()), **kw)


def happy():
    out = []
    # 1. a complete answer, and what it changes everywhere
    steps = [
        results("results-before", C2),
        step("details-before", "/v2/consultations/%s" % C2, **{"as": IDLE}),
        step("answered-before", "/consultations/answered/1", **{"as": IDLE}),
        step("preview-before", "/consultations", **{"as": IDLE}),
        post("answer", C2, all_unique_answer(2), IDLE),
        results("results-after", C2),
        results_xml("results-after-xml", C2),
        step("details-after", "/v2/consultations/%s" % C2, **{"as": IDLE}),
        step("answered-after", "/consultations/answered/1", **{"as": IDLE}),
        step("preview-after", "/consultations", **{"as": IDLE}),
        post("answer-again", C2, all_unique_answer(2), IDLE),
        post("answer-again-other-choices", C2, [r_(qid(2, 1), [cid_(2, 1, 2)])], IDLE),
        post("answer-r4", C2, [r_(qid(2, 1), [cid_(2, 1, 2)]), r_(qid(2, 2), [cid_(2, 2, 2)])], R[4]),
        results("results-two-users", C2),
        results("results-c1-untouched", C1),
        post("answer-r4-c1", C1, [r_(qid(1, 3), [], "Un texte libre")], R[4]),
        results("results-c1-after", C1),
        step("answered-r4", "/consultations/answered/1", **{"as": R[4]}),
    ]
    out.append(SC("S6-answer-complete-cycle", steps))

    # 2. empty and partial answers: the questions with choices that are not answered get the "not applicable" choice
    steps = [
        post("empty", C2, [], IDLE),
        post("only-q1", C2, [r_(qid(2, 1), [cid_(2, 1, 1)])], R[4]),
        post("only-open", C2, [r_(qid(2, 3), [], "Seulement du texte")], PUBLISHER),
        post("only-chapter", C2, [r_(qid(2, 5), [cid_(2, 5, 1)])], MODERATOR),
        post("chapter-no-choice", C2, [r_(qid(2, 5), [], "texte du chapitre")], ADMIN),
        post("multiple-no-choice", C2, [r_(qid(2, 2), [])], R[5]),
        post("unique-open-choice-text", C2, [r_(qid(2, 6), [cid_(2, 6, 3)], "précision")], NOFCM1),
        post("conditional-only", C2, [r_(qid(2, 4), [cid_(2, 4, 1)])], NOFCM2),
        post("all-skipped", C2, [r_(qid(2, 1), [SKIP]), r_(qid(2, 2), [SKIP]), r_(qid(2, 4), [SKIP]), r_(qid(2, 6), [SKIP])], R[1]),
        post("explicit-not-applicable", C2, [r_(qid(2, 1), [NA]), r_(qid(2, 2), [NA, SKIP])], R[2]),
        results("results", C2),
    ]
    out.append(SC("S6-answer-partial", steps))

    # 3. arbitrary choice ids are not checked against the question; open questions accept choice ids; several rows per answer
    steps = [
        post("unknown-choice-id", C2, [r_(qid(2, 1), ["not-a-choice"])], IDLE),
        post("choice-of-another-question", C2, [r_(qid(2, 1), [cid_(2, 2, 1)]), r_(qid(2, 2), [cid_(2, 1, 1), cid_(2, 6, 3)])], R[4]),
        post("open-with-choices", C2, [r_(qid(2, 3), ["a", "b", "c"], "texte")], PUBLISHER),
        post("choice-id-empty-string", C2, [r_(qid(2, 1), [""])], MODERATOR),
        post("choice-id-uppercase-uuid", C2, [r_(qid(2, 1), [NA.upper()])], ADMIN),
        post("choice-id-spaces", C2, [r_(qid(2, 1), [" 2011 "])], R[5]),
        post("choice-id-unicode", C2, [r_(qid(2, 1), ["é😀"])], NOFCM1),
        post("choice-id-long", C2, [r_(qid(2, 1), ["x" * 5000])], NOFCM2),
        post("multiple-two-same-text", C2, [r_(qid(2, 2), [cid_(2, 2, 1), cid_(2, 2, 2)], "même texte sur chaque ligne")], R[1]),
        results("results", C2),
    ]
    out.append(SC("S6-answer-choice-ids", steps))

    # 4. every kind of user answers consultation 2 (askDemographicInfo and the ask date row)
    steps = [post("answer-%d" % i, C2, [r_(qid(2, 1), [cid_(2, 1, 1 + i % 3)])], u) for i, u in enumerate(ANY)]
    steps.append(results("results", C2))
    out.append(SC("S6-answer-every-user-c2", steps))

    # 5. the users who did not answer consultation 1 yet
    steps = [post("answer-%d" % i, C1, [r_(qid(1, 1), [cid_(1, 1, 1 + i % 3)]), r_(qid(1, 2), [cid_(1, 2, 1 + i % 3)])], u) for i, u in enumerate(FRESH_C1)]
    steps += [post("again-%d" % i, C1, [], u) for i, u in enumerate(FRESH_C1[:3])]
    steps += [post("seeded-participant-%d" % i, C1, [], u) for i, u in enumerate([R[1], R[2], R[3], R[6], R[7], NOFCM1, P[1], P[12], INVALID_PROFILE])]
    steps.append(results("results", C1))
    steps.append(tsv("tsv", C1))
    out.append(SC("S6-answer-c1-users", steps))

    # 6. one user, several consultations
    steps = [
        post("c2", C2, [r_(qid(2, 1), [cid_(2, 1, 1)])], IDLE),
        post("c1", C1, [r_(qid(1, 1), [cid_(1, 1, 2)])], IDLE),
        post("c6-unpublished", C6, [r_(qid(6, 1), [cid_(6, 1, 1)])], IDLE),
        post("c6-again", C6, [], IDLE),
        results("results-c1", C1), results("results-c2", C2), results("results-c6", C6),
        step("answered", "/consultations/answered/1", **{"as": IDLE}),
        step("preview", "/consultations", **{"as": IDLE}),
    ]
    out.append(SC("S6-answer-several-consultations", steps))

    # 7. the results are dropped by every answer (cache eviction), whatever the order of the reads
    steps = [results("r0", C2), results("r0-bis", C2)]
    for i, u in enumerate([IDLE, R[4], R[5], PUBLISHER, MODERATOR, ADMIN]):
        steps.append(post("answer-%d" % i, C2, [r_(qid(2, 1), [cid_(2, 1, 1 + i % 3)]), r_(qid(2, 2), [cid_(2, 2, 1 + i % 3)])], u))
        steps.append(results("r%d" % (i + 1), C2))
        steps.append(results("r%d-bis" % (i + 1), C2))
        steps.append(results_xml("r%d-xml" % (i + 1), C2))
    out.append(SC("S6-answer-results-eviction", steps))
    return out


LONG200 = "é" * 200
EMOJI = "\U0001F600"


def texts():
    out = []
    # sanitized texts: tags removed, entities decoded, truncated by take() to 200 (choices) / 400 (open question)
    t = [
        ("plain", "Bonjour"), ("tags", "<b>gras</b> et <i>italique</i>"), ("script", "avant<script>alert(1)</script>après"), ("style", "<style>p{}</style>x"),
        ("lt", "a < b"), ("gt", "a > b"), ("amp", "Tom & Jerry"), ("entity-amp", "&amp;"), ("entity-lt", "&lt;b&gt;"), ("entity-num", "&#65;&#x42;&#67;"),
        ("entity-unknown", "&unknown; &"), ("entity-bad", "&#xD800; &#0; &#1114112;"), ("quotes", "\"guillemets\" 'simples' `back`"),
        ("braces", "{{x}} {"), ("plus-eq", "1+1=2 @ >"), ("newlines", "ligne1\nligne2\r\nligne3\ttab"), ("spaces", "  espaces  "), ("only-spaces", "   "),
        ("accents", "éèàç ÉÈ œ Œ ß"), ("emoji", "😀 et 👨‍👩‍👧‍👦"), ("rtl", "שלום عربى"), ("control", "a\u0001b\u007fc"), ("fffd", "a\ufffdb"),
        ("nbsp", "a\u00a0b\u2003c"), ("html-comment", "a<!-- c -->b"), ("link", "<a href=\"http://x\">lien</a>"), ("img", "<img src=x onerror=alert(1)>"),
        ("unclosed", "<b>gras"), ("unclosed-tag", "texte <b"), ("lone-lt", "<"), ("lone-amp", "&"), ("double-encoded", "&amp;lt;"), ("zwnj", "क्\u200cष"),
        ("percent", "100% sûr"), ("backslash", "c:\\dir\\x"), ("json", "{\"a\": [1, 2]}"), ("sql", "'; DROP TABLE x; --"), ("url", "https://www.agora.gouv.fr/a?b=c&d=e"),
        ("tab-only", "\t"), ("cr", "\r"), ("empty", ""),
    ]
    steps = []
    users = ANY
    i = 0
    for name, text in t:
        u = users[i % len(users)]
        if i < len(users):
            steps.append(post("text-%s" % name, C2, [r_(qid(2, 3), [], text)], u))
        i += 1
    out.append(SC("S6-answer-texts-open", steps))
    rest = t[len(users):]
    steps = []
    for i, (name, text) in enumerate(rest):
        if i < len(FRESH_C1):
            steps.append(post("text-%s" % name, C1, [r_(qid(1, 3), [], text)], FRESH_C1[i]))
    if steps:
        out.append(SC("S6-answer-texts-open-c1", steps))

    # the same texts on a choice question (limit 200) and on a question with choices that has an open text field
    steps = []
    for i, (name, text) in enumerate(t[:len(users)]):
        steps.append(post("other-%s" % name, C2, [r_(qid(2, 6), [cid_(2, 6, 3)], text), r_(qid(2, 1), [cid_(2, 1, 1)], text)], users[i]))
    out.append(SC("S6-answer-texts-other", steps))

    # lengths: UTF-16 units; 200 / 400 are accepted, one more is a 400
    cases = [
        ("open-400", qid(2, 3), [], "a" * 400), ("open-401", qid(2, 3), [], "a" * 401), ("open-399", qid(2, 3), [], "a" * 399),
        ("open-200-emoji", qid(2, 3), [], EMOJI * 200), ("open-201-emoji", qid(2, 3), [], EMOJI * 201), ("open-400-accents", qid(2, 3), [], "é" * 400),
        ("open-401-accents", qid(2, 3), [], "é" * 401), ("open-399-plus-emoji", qid(2, 3), [], "a" * 399 + EMOJI),
        ("open-398-plus-emoji", qid(2, 3), [], "a" * 398 + EMOJI), ("open-tags-expand", qid(2, 3), [], "<b>" * 100 + "x"),
        ("open-entities", qid(2, 3), [], "&amp;" * 80), ("open-entities-401", qid(2, 3), [], "&amp;" * 81),
        ("unique-200", qid(2, 1), [cid_(2, 1, 1)], "b" * 200), ("unique-201", qid(2, 1), [cid_(2, 1, 1)], "b" * 201),
        ("unique-emoji-100", qid(2, 1), [cid_(2, 1, 1)], EMOJI * 100), ("unique-emoji-101", qid(2, 1), [cid_(2, 1, 1)], EMOJI * 101),
        ("multiple-200", qid(2, 2), [cid_(2, 2, 1)], "c" * 200), ("multiple-201", qid(2, 2), [cid_(2, 2, 1)], "c" * 201),
        ("conditional-200", qid(2, 4), [cid_(2, 4, 1)], "d" * 200), ("conditional-201", qid(2, 4), [cid_(2, 4, 1)], "d" * 201),
        ("unique6-200", qid(2, 6), [cid_(2, 6, 3)], "é" * 200), ("unique6-201", qid(2, 6), [cid_(2, 6, 3)], "é" * 201),
        ("chapter-long", qid(2, 5), [], "x" * 5000), ("chapter-very-long", qid(2, 5), [cid_(2, 5, 1)], "y" * 100000),
        ("open-tag-only-401", qid(2, 3), [], "<i>" * 133 + "ab"),
    ]
    steps = []
    for i, (name, q, ch, text) in enumerate(cases):
        steps.append(post("len-%s" % name, C2, [r_(q, ch, text)], ANY[i % len(ANY)]))
    steps.append(results("results", C2))
    out.append(SC("S6-answer-lengths", steps))
    return out


def rejections():
    out = []
    ok = lambda: []
    # each rejection leaves the user free: the same user tries again at the end
    cases = [
        ("unique-no-choice", [r_(qid(2, 1), [])]), ("unique-two-choices", [r_(qid(2, 1), [cid_(2, 1, 1), cid_(2, 1, 2)])]),
        ("unique-same-twice", [r_(qid(2, 1), [cid_(2, 1, 1), cid_(2, 1, 1)])]),
        ("conditional-no-choice", [r_(qid(2, 4), [])]), ("conditional-two-choices", [r_(qid(2, 4), [cid_(2, 4, 1), cid_(2, 4, 2)])]),
        ("multiple-three-choices", [r_(qid(2, 2), [cid_(2, 2, 1), cid_(2, 2, 2), cid_(2, 2, 3)])]),
        ("multiple-duplicate", [r_(qid(2, 2), [cid_(2, 2, 1), cid_(2, 2, 1)])]), ("multiple-duplicate-one-ok", [r_(qid(2, 2), [cid_(2, 2, 1), cid_(2, 2, 1), cid_(2, 2, 2)])]),
        ("duplicate-question", [r_(qid(2, 1), [cid_(2, 1, 1)]), r_(qid(2, 1), [cid_(2, 1, 2)])]),
        ("duplicate-question-open", [r_(qid(2, 3), [], "a"), r_(qid(2, 3), [], "b")]),
        ("unknown-question", [r_("999", [cid_(2, 1, 1)])]), ("question-of-another-consultation", [r_(qid(1, 1), [cid_(1, 1, 1)])]),
        ("question-id-empty", [r_("", [cid_(2, 1, 1)])]), ("question-id-uppercase", [r_(qid(2, 1).upper(), [cid_(2, 1, 1)])]),
        ("question-id-space", [r_(" " + qid(2, 1), [cid_(2, 1, 1)])]),
        ("one-bad-among-good", [r_(qid(2, 1), [cid_(2, 1, 1)]), r_(qid(2, 2), []), r_(qid(2, 4), [cid_(2, 4, 1), cid_(2, 4, 2)])]),
        ("one-unknown-among-good", [r_(qid(2, 1), [cid_(2, 1, 1)]), r_("42", [])]),
        ("open-too-long", [r_(qid(2, 3), [], "a" * 401)]), ("unique-text-too-long", [r_(qid(2, 6), [cid_(2, 6, 3)], "a" * 201)]),
        ("multiple-text-too-long", [r_(qid(2, 2), [cid_(2, 2, 1)], "a" * 201)]),
    ]
    steps = []
    for i, (name, rs) in enumerate(cases):
        steps.append(post("reject-%s" % name, C2, rs, IDLE))
    steps.append(post("finally-ok", C2, [r_(qid(2, 1), [cid_(2, 1, 1)])], IDLE))
    steps.append(post("then-already", C2, [r_(qid(2, 1), [cid_(2, 1, 1)])], IDLE))
    out.append(SC("S6-answer-rejections", steps))

    # an answer that is rejected writes nothing: the user can still answer (several rejections in a row)
    steps = [post("bad-1", C1, [r_(qid(1, 1), [])], R[4]), post("bad-2", C1, [r_("999")], R[4]), results("results", C1),
             step("answered", "/consultations/answered/1", **{"as": R[4]}), post("good", C1, [r_(qid(1, 1), [cid_(1, 1, 1)])], R[4])]
    out.append(SC("S6-answer-rejection-writes-nothing", steps))

    # consultations that cannot be answered
    steps = [
        post("c4-finished", C4, [], IDLE), post("c5-finished", C5, [], IDLE), post("c7-finished", C7, [r_(qid(7, 1), [cid_(7, 1, 1)])], IDLE),
        post("unknown-empty", "co9999999999999999999999", [], IDLE), post("unknown-with-response", "co9999999999999999999999", [r_("1", ["2"])], R[4]),
        post("unknown-again", "co9999999999999999999999", [], IDLE),
        post("by-slug-empty", "consultation-2", [], PUBLISHER), post("by-slug-responses", "consultation-2", [r_(qid(2, 1), [cid_(2, 1, 1)])], MODERATOR),
        post("uppercase-id", C2.upper(), [], ADMIN), post("id-with-space", C2 + "%20", [], R[5]),
        post("id-encoded", "co000000000000000000000%32", [r_(qid(2, 1), [cid_(2, 1, 1)])], NOFCM1),
        post("id-long", "c" * 300, [], NOFCM2), post("id-unicode", "%C3%A9", [], R[1]), post("id-dot", "..", [], R[2]),
        post("c6-unpublished", C6, [r_(qid(6, 1), [cid_(6, 1, 1)]), r_(qid(6, 3), [], "texte")], R[3]),
        step("answered-after-unknown", "/consultations/answered/1", **{"as": IDLE}),
        step("preview-after-unknown", "/consultations", **{"as": IDLE}),
    ]
    out.append(SC("S6-answer-consultations", steps))
    return out
