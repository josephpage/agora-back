"""GET /v2/consultations/{id}/responses (results) and GET /admin/consultations/{id}/responses (TSV)."""
import copy
import datetime
import hashlib
import json
import os

from helpers import *  # noqa: F401,F403
from helpers import U, step, scenario, setup, post, results, results_xml, tsv, r_, body, qid, cid_, xff, TAG, SKIP, NA, FIXTURES  # noqa: F401
from helpers import C1, C2, C3, C4, C5, C6, C7, IDLE, ADMIN, MODERATOR, PUBLISHER, BANNED, R, NOFCM1, NOFCM2, P, INVALID_PROFILE, FRESH_C1, ANY
from helpers import variants

SC = lambda name, steps, **kw: scenario(name, steps, TAG, dbdiff="step", setup=kw.pop("setup_", setup()), **kw)
SCN = lambda name, steps, **kw: scenario(name, steps, TAG, dbdiff="step", setup=kw.pop("setup_", {}), **kw)  # no write: no trigger needed


def u(n):
    """The n-th fake user of the data scenarios (not in agora_users: only the answers and profiles reference it)."""
    return "00000000-0000-4000-9100-%012x" % n


def uid(*parts):
    return str(__import__("uuid").UUID(hashlib.md5("/".join(str(p) for p in parts).encode()).hexdigest()))


def q_(s):
    return "NULL" if s is None else "'" + str(s).replace("'", "''") + "'"


def lit(c):
    """The literal id of a consultation template ({{seed.ConsultationN}}); the setup SQL is not expanded."""
    return "co%022d" % int(c.split("Consultation")[1].rstrip("}")) if c.startswith("{{") else c


def data_sql(cid, users, participants=True, now="now()"):
    """SQL creating participants. users: list of dict(n, profile={gender, year_of_birth, department, ...}|None, answers=[(question, choice|None, text)])."""
    cid = lit(cid)
    uac, rep, prof = [], [], []
    for us in users:
        n = us["n"]
        if participants:
            uac.append("('%s', '%s', %s, '%s')" % (uid("uac", cid, n), cid, now, u(n)))
        for k, (q, ch, text) in enumerate(us.get("answers", [])):
            rep.append("('%s', %s, '%s', %s, '%s', '%s', %s, '%s')" % (uid("rep", cid, n, k), q_(ch), cid, now, uid("part", cid, n), q, q_(text if text is not None else ""), u(n)))
        pr = us.get("profile")
        if pr is not None:
            cols = ["gender", "year_of_birth", "department", "city_type", "job_category", "vote_frequency", "public_meeting_frequency", "consultation_frequency"]
            prof.append("('%s', '%s', %s)" % (uid("prof", n), u(n), ", ".join(q_(pr.get(c)) if c != "year_of_birth" else ("NULL" if pr.get(c) is None else str(pr[c])) for c in cols)))
    sql = []
    if uac:
        sql.append("INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES " + ", ".join(uac))
    if rep:
        sql.append("INSERT INTO reponses_consultation (id, choice_id, consultation_id, participation_date, participation_id, question_id, response_text, user_id) VALUES " + ", ".join(rep))
    if prof:
        sql.append("INSERT INTO users_profile (id, user_id, gender, year_of_birth, department, city_type, job_category, vote_frequency, public_meeting_frequency, consultation_frequency) VALUES " + ", ".join(prof))
    return sql


def pr(**kw):
    return kw


YEAR = datetime.date.today().year


def seeded():
    out = []
    cons = [C1, C2, C3, C4, C5, C6, C7]
    steps = []
    for i, c in enumerate(cons, 1):
        steps += [results("c%d-anon" % i, c), results("c%d-user" % i, c, R[1]), results_xml("c%d-xml" % i, c), step("c%d-head" % i, "/v2/consultations/%s/responses" % c, **{"method": "HEAD"})]
    steps.append(results("c1-again", C1))
    out.append(SCN("S6-results-seeded", steps))

    steps = [
        results("unknown", "co9999999999999999999999"), results("unknown-again", "co9999999999999999999999"), results("slug", "consultation-1"),
        results("uppercase", C1.upper()), results("space", C1 + "%20"), results("encoded", "co000000000000000000000%31"), results("long", "c" * 400),
        results("unicode", "%C3%A9%F0%9F%98%80"), results("dots", ".."), results("empty-segment", ""), results("update-id", "cu0000000000000000000001"),
        results("qag-id", "{{seed.QagAcceptedTop}}"),
        step("trailing-slash", "/v2/consultations/%s/responses/" % C1), step("semicolon", "/v2/consultations/%s;x=1/responses" % C1),
        step("post", "/v2/consultations/%s/responses" % C1, **{"method": "POST", "as": R[1], "body": body([])}),
        step("put", "/v2/consultations/%s/responses" % C1, **{"method": "PUT", "as": R[1]}), step("delete", "/v2/consultations/%s/responses" % C1, **{"method": "DELETE", "as": R[1]}),
        step("options", "/v2/consultations/%s/responses" % C1, **{"method": "OPTIONS"}),
        step("cors-preflight", "/v2/consultations/%s/responses" % C1, **{"method": "OPTIONS", "headers": {"Origin": "https://www.agora.gouv.fr", "Access-Control-Request-Method": "GET"}}),
        step("cors-get", "/v2/consultations/%s/responses" % C1, **{"headers": {"Origin": "https://www.agora.gouv.fr"}}),
        step("accept-xml", "/v2/consultations/%s/responses" % C1, **{"headers": {"Accept": "application/xml"}}),
        step("accept-text", "/v2/consultations/%s/responses" % C1, **{"headers": {"Accept": "text/plain"}}),
        step("accept-json", "/v2/consultations/%s/responses" % C1, **{"headers": {"Accept": "application/json"}}),
        step("accept-star", "/v2/consultations/%s/responses" % C1, **{"headers": {"Accept": "*/*"}}),
        step("media-foo", "/v2/consultations/%s/responses?mediaType=foo" % C1), step("media-json", "/v2/consultations/%s/responses?mediaType=json" % C1),
        step("media-xml-upper", "/v2/consultations/%s/responses?mediaType=XML" % C1),
        step("bad-jwt", "/v2/consultations/%s/responses" % C1, **{"bearer": "abc.def.ghi"}),
        step("unknown-user", "/v2/consultations/%s/responses" % C1, **{"as": "00000000-0000-4000-9000-0000000000ff"}),
        step("banned", "/v2/consultations/%s/responses" % C1, **{"as": BANNED}),
        step("etag", "/v2/consultations/%s/responses" % C1, **{"headers": {"If-None-Match": '"0abc"'}}),
        step("old-route", "/consultations/%s/responses" % C1),
    ]
    out.append(SCN("S6-results-routes", steps))
    return out


def data_scenarios():
    out = []
    qs = {1: qid(2, 1), 2: qid(2, 2), 3: qid(2, 3), 4: qid(2, 4), 6: qid(2, 6)}
    ch = lambda q, k: cid_(2, q, k)

    def users_with(counts, extra=None):
        """counts: {question: {choice: n_users}} -> n users answer q1/q2 as described; returns users list."""
        pass

    def check(name, users, extra_steps=(), participants=True):
        steps = [results("results", C2), results_xml("results-xml", C2), results("again", C2)] + list(extra_steps)
        return SCN(name, steps, setup_=setup(sql=data_sql(C2, users, participants)))

    # 1. 8 participants: ratios on x.5 (Math.round, half up)
    users = []
    for n in range(1, 9):
        users.append({"n": n, "answers": [(qs[1], ch(1, 1 + (n % 3)), ""), (qs[2], ch(2, 1), ""), (qs[2], ch(2, 2), "") if n % 2 else (qs[2], ch(2, 3), "")]})
    out.append(check("S6-results-data-halves", users))

    # 2. 3 participants: 1/3 and 2/3 (33 / 67), one skipped, one not applicable
    users = [{"n": 1, "answers": [(qs[1], ch(1, 1), "")]}, {"n": 2, "answers": [(qs[1], ch(1, 1), "")]}, {"n": 3, "answers": [(qs[1], SKIP, ""), (qs[2], NA, "")]}]
    out.append(check("S6-results-data-thirds", users))

    # 3. no participant but answers: x / 0 is Infinity (Int.MAX_VALUE), 0 / 0 is 0
    users = [{"n": 1, "answers": [(qs[1], ch(1, 1), ""), (qs[2], ch(2, 1), "")]}, {"n": 2, "answers": [(qs[1], ch(1, 2), "")]}]
    out.append(check("S6-results-data-no-participant", users, participants=False))

    # 4. the "not applicable" answers remove their participants from the denominator
    users = [{"n": n, "answers": [(qs[1], NA, ""), (qs[2], NA, ""), (qs[4], ch(4, 1), "")]} for n in range(1, 5)]
    users += [{"n": 5, "answers": [(qs[1], ch(1, 1), ""), (qs[2], ch(2, 1), ""), (qs[4], NA, "")]}]
    out.append(check("S6-results-data-not-applicable", users))

    # 5. every participant answers "not applicable": the denominator is 0
    users = [{"n": n, "answers": [(qs[1], NA, ""), (qs[1], ch(1, 1), "")]} for n in range(1, 4)]
    out.append(check("S6-results-data-all-not-applicable", users))

    # 6. more answers than participants (ratio over 100 %), negative denominators
    users = [{"n": 1, "answers": [(qs[1], ch(1, 1), "")] * 5}, {"n": 2, "answers": [(qs[1], NA, "")] * 4}]
    out.append(check("S6-results-data-too-many-answers", users))

    # 7. rows for unknown questions and choices, NULL choices (open answers), texts: ignored
    users = [{"n": 1, "answers": [("999", "x", ""), (qs[1], "unknown-choice", ""), (qs[3], None, "texte"), (qs[1], None, ""), (qs[5] if 5 in qs else "205", ch(5, 1), "")]},
             {"n": 2, "answers": [(qs[1], ch(1, 3), "")]}]
    out.append(check("S6-results-data-ignored-rows", users))

    # 8. the aggregated table wins as soon as it has a row for the consultation; its rows are summed
    users = [{"n": n, "answers": [(qs[1], ch(1, 1 + n % 3), "")]} for n in range(1, 7)]
    agg = ["INSERT INTO consultation_results (id, choice_id, consultation_id, question_id, response_count) VALUES %s" % ", ".join(
        "('%s', %s, '%s', '%s', %d)" % (uid("agg", i), q_(c), lit(C2), q, n) for i, (q, c, n) in enumerate([
            (qs[1], ch(1, 1), 3), (qs[1], ch(1, 1), 2), (qs[1], ch(1, 2), 1), (qs[2], ch(2, 1), 6), (qs[1], NA, 1), (qs[1], SKIP, 2), ("999", "z", 4)]))]
    out.append(SCN("S6-results-data-aggregated", [results("results", C2), results_xml("results-xml", C2)],
                   setup_=setup(sql=data_sql(C2, users) + agg)))
    # aggregated rows of a consultation without participant
    out.append(SCN("S6-results-data-aggregated-no-participant", [results("results", C2)], setup_=setup(sql=agg)))
    # the seeded aggregated consultation, and rows added to the raw table of an aggregated consultation (ignored)
    extra = data_sql(C5, [{"n": 1, "answers": [(qid(5, 1), cid_(5, 1, 1), "")] * 3}])
    out.append(SCN("S6-results-data-aggregated-seeded", [results("c5", C5), results("c5-xml", C5)], setup_=setup(sql=extra)))
    # NULL ids in the aggregated table: the mapper throws (HTTP 500)
    for col in ("consultation_id", "question_id", "choice_id"):
        row = {"consultation_id": q_(lit(C2)), "question_id": q_(qs[1]), "choice_id": q_(ch(1, 1))}
        row[col] = "NULL"
        sql = ["INSERT INTO consultation_results (id, choice_id, consultation_id, question_id, response_count) VALUES ('%s', %s, %s, %s, 1)" % (uid("aggnull", col), row["choice_id"], row["consultation_id"], row["question_id"])]
        # consultation_id NULL never matches the WHERE clause: only a NULL question / choice id throws
        out.append(SCN("S6-results-data-aggregated-null-%s" % col.replace("_id", ""), [results("results", C2), results("again", C2)], setup_=setup(sql=sql)))
    # participants counted once however many rows (DISTINCT user), duplicate participation rows
    users = [{"n": 1, "answers": [(qs[1], ch(1, 1), "")]}]
    dup = ["INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES ('%s', '%s', now(), '%s')" % (uid("dup", 1), lit(C2), u(1)),
           "INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES ('%s', '%s', now(), '%s')" % (uid("dup", 2), lit(C2), u(2))]
    out.append(SCN("S6-results-data-duplicate-participants", [results("results", C2)], setup_=setup(sql=data_sql(C2, users) + dup)))
    return out


# ---------------------------------------------------------------------------------------------------------------------
# Strapi variants
# ---------------------------------------------------------------------------------------------------------------------

def vq(n, comp, **kw):
    d = {"id": 6000 + n, "__component": comp, "titre": "Question %d" % n, "numero": n, "popup_explication": None, "question_suivante": None}
    d.update(kw)
    return d


def vch(n, k, **kw):
    d = {"id": n * 10 + k, "label": "Choix %d.%d" % (n, k), "ouvert": False}
    d.update(kw)
    return d


UNIQUE = "question-de-consultation.question-a-choix-unique"
MULTI = "question-de-consultation.question-a-choix-multiples"
OPEN = "question-de-consultation.question-ouverte"
COND = "question-de-consultation.question-conditionnelle"
CHAPTER = "question-de-consultation.description"


def chapter(n):
    return vq(n, CHAPTER, description=variants.rich("Chapitre"), url_image=None, transcription_image=None, image=None)


def variant_consultations():
    now = lambda d: "{{now%s}}" % d
    mk = lambda n, qs, **kw: variants.consultation(n, start=kw.pop("start", now("-10d")), end=kw.pop("end", now("+60d")), secs_avant=[], secs_apres=[], qs=False, questions=qs, **kw)
    cs = []
    cs.append(mk(6001, []))
    cs.append(mk(6002, [chapter(1), chapter(2)]))
    cs.append(mk(6003, [vq(1, OPEN), vq(2, OPEN, titre="Question ouverte 2")]))
    cs.append(mk(6004, [vq(1, UNIQUE, choix=[]), vq(2, UNIQUE, choix=[vch(2, 1)]), vq(3, MULTI, nombre_maximum_de_choix=1, choix=[])]))
    cs.append(mk(6005, [vq(1, MULTI, nombre_maximum_de_choix=0, choix=[vch(1, 1), vch(1, 2)]), vq(2, MULTI, nombre_maximum_de_choix=1, choix=[vch(2, 1), vch(2, 2)]),
                        vq(3, MULTI, nombre_maximum_de_choix=5, choix=[vch(3, k) for k in range(1, 4)]), vq(4, MULTI, nombre_maximum_de_choix=-1, choix=[vch(4, 1)])]))
    cs.append(mk(6006, [vq(1, COND, choix=[vch(1, 1, numero_de_la_question_suivante=2), vch(1, 2, numero_de_la_question_suivante=2)]), vq(2, UNIQUE, choix=[vch(2, 1)])]))
    many = []
    for n in range(1, 31):
        kind = n % 5
        many.append(vq(n, [UNIQUE, MULTI, OPEN, COND, CHAPTER][kind], **({"choix": [vch(n, 1), vch(n, 2)]} if kind in (0, 1) else
                                                                    ({"choix": [vch(n, 1, numero_de_la_question_suivante=min(n + 1, 30))]} if kind == 3 else
                                                                     ({"description": variants.rich("C"), "url_image": None, "transcription_image": None, "image": None} if kind == 4 else {}))),
                      **({"nombre_maximum_de_choix": 2} if kind == 1 else {})))
    cs.append(mk(6007, many))
    cs.append(mk(6008, [vq(1, UNIQUE, choix=[vch(1, 1), vch(1, 2)])], start=now("-60d"), end=now("-40d")))
    cs.append(mk(6009, [vq(1, UNIQUE, choix=[vch(1, 1), vch(1, 2)]), vq(1, UNIQUE, titre="Doublon", choix=[vch(7, 1)])]))
    cs.append(mk(6010, [vq(1, UNIQUE, choix=[vch(1, 1), vch(1, 2)]), vq(2, OPEN)], publishedAt=None))
    # ids that are not numbers / texts with HTML in the titles and labels
    cs.append(mk(6011, [vq(1, UNIQUE, titre="<b>Titre</b> & \"q\" é😀", choix=[vch(1, 1, label="Libellé <i>HTML</i> & 'x'"), vch(1, 2, label="")]),
                        vq(2, OPEN, titre="")]))
    cs.append(mk(6012, [vq(1, UNIQUE, choix=[vch(1, 1), vch(1, 2)]), vq(2, MULTI, nombre_maximum_de_choix=3, choix=[vch(2, k) for k in range(1, 4)])], end=now("+60d"), titre_consultation="Titre <&> \"é\""))
    return cs


def write_variants():
    cs = variant_consultations()
    variants.write_fixture("s6-questions.json", cs)
    return cs


def variant_scenarios():
    out = []
    ov = {"strapiOverrides": [{"model": "consultations", "fixture": "variants/s6-questions.json"}]}
    for n in range(6001, 6013):
        c = "vc%04d" % n
        steps = [results("results-before", c), results_xml("results-xml-before", c), tsv("tsv-before", c)]
        steps.append(step("questions", "/consultations/%s/questions" % c))
        out.append((n, c, steps))
    scen = []
    # answers per variant
    ans = {
        6001: [("empty", [], IDLE), ("one", [r_("1", ["2"])], R[4])],
        6002: [("empty", [], IDLE), ("chapter", [r_("6001", ["x"], "texte")], R[4]), ("chapter-1", [r_("6002")], R[5])],
        6003: [("empty", [], IDLE), ("texts", [r_("6001", [], "a"), r_("6002", [], "b")], R[4]), ("one", [r_("6001", [], "seulement")], R[5])],
        6004: [("empty", [], IDLE), ("unique-no-choices", [r_("6001", ["x"])], R[4]), ("unique-one", [r_("6002", ["20"])], R[5]), ("multi-empty-choices", [r_("6003", [])], R[1])],
        6005: [("max0-none", [r_("6001", [])], IDLE), ("max0-one", [r_("6001", ["a"])], R[4]), ("max1-two", [r_("6002", ["a", "b"])], R[5]), ("max1-one", [r_("6002", ["a"])], R[1]),
               ("max5-three", [r_("6003", ["a", "b", "c"])], R[2]), ("max-1-none", [r_("6004", [])], R[3]), ("max-1-one", [r_("6004", ["a"])], R[6])],
        6006: [("cond-one", [r_("6001", ["10"])], IDLE), ("cond-two", [r_("6001", ["10", "11"])], R[4]), ("cond-other", [r_("6002", ["x"])], R[5])],
        6007: [("empty", [], IDLE), ("some", [r_("6001", ["a"]), r_("6002", ["a"]), r_("6003", [], "t"), r_("6030", [])], R[4])],
        6008: [("finished", [], IDLE), ("finished-answer", [r_("6001", ["a"])], R[4])],
        6009: [("duplicate-id", [r_("6001", ["a"])], IDLE), ("duplicate-id-other-choice", [r_("6001", ["70"])], R[4])],
        6010: [("unpublished", [r_("6001", ["a"]), r_("6002", [], "t")], IDLE)],
        6011: [("html", [r_("6001", ["x"], "<b>t</b>")], IDLE)],
        6012: [("a", [r_("6001", ["a"])], IDLE), ("b", [r_("6002", ["a", "b"])], R[4]), ("c", [r_("6001", ["a"]), r_("6002", ["a"])], R[5])],
    }
    for n, c, steps in out:
        wsteps = list(steps)
        for name, rs, user in ans.get(n, []):
            wsteps.append(post("answer-%s" % name, c, rs, user))
            wsteps.append(results("results-after-%s" % name, c))
        wsteps.append(tsv("tsv-after", c))
        wsteps.append(step("answered", "/consultations/answered/1", **{"as": IDLE}))
        scen.append(scenario("S6-variant-%d" % n, wsteps, TAG, dbdiff="step", setup=setup(**ov)))
    return scen


def fault_scenarios():
    out = []
    # a Strapi failure: the consultation is unknown (HTTP 400 for the results); an answer is valid only when empty (no question)
    for mode in ["500", "malformed", "nulldata", "missingfield"]:
        faults_ = {"strapiFaults": [{"model": "consultations", "mode": mode}]}
        # consultation ids no scenario read before (the reference keeps 5 minutes of Strapi data in its memory)
        c = "vcfault%s" % mode
        steps = [results("results", c), results("results-again", c), tsv("tsv", c),
                 post("answer-empty", c, [], IDLE), post("answer-one", c, [r_("1", ["2"])], R[4]), results("results-after", c),
                 step("answered", "/consultations/answered/1", **{"as": IDLE})]
        out.append(scenario("S6-strapi-fault-%s" % mode, steps, TAG, dbdiff="step", setup=setup(**faults_)))
    return out
