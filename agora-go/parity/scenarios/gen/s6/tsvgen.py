"""GET /admin/consultations/{id}/responses (TSV export with the demographic information)."""
import os
import re

from helpers import *  # noqa: F401,F403
from helpers import step, scenario, setup, tsv, TAG, qid, cid_  # noqa: F401
from helpers import C1, C2, C3, C4, C5, C6, C7, IDLE, ADMIN, MODERATOR, PUBLISHER, BANNED, R, P
from results import SCN, u, uid, q_, data_sql, pr, YEAR


def department_codes():
    src = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "..", "..", "internal", "domain", "territoire_gen.go"), encoding="utf-8").read()
    names = re.findall(r'\{Name: "([A-Z0-9_]+)", Ordinal: \d+\}', src)
    return [n.rsplit("_", 1)[1] for n in names]


def seeded():
    out = []
    cons = [C1, C2, C3, C4, C5, C6, C7]
    steps = [tsv("c%d" % i, c) for i, c in enumerate(cons, 1)]
    steps += [step("c1-xml", "/admin/consultations/%s/responses?mediaType=xml" % C1, **{"as": ADMIN}),
              step("c1-head", "/admin/consultations/%s/responses" % C1, **{"method": "HEAD", "as": ADMIN})]
    out.append(SCN("S6-tsv-seeded", steps))

    adm = lambda id, path, **kw: step(id, path, **dict({"as": ADMIN}, **kw))
    steps = [
        tsv("anonymous", C1, user=None), tsv("regular", C1, user=R[1]), tsv("moderator", C1, user=MODERATOR), tsv("publisher", C1, user=PUBLISHER),
        tsv("banned", C1, user=BANNED), tsv("idle", C1, user=IDLE), tsv("admin", C1),
        tsv("bad-jwt", C1, user=None, bearer="abc.def.ghi"), tsv("unknown-user", C1, user="00000000-0000-4000-9000-0000000000ff"),
        tsv("unknown-consultation", "co9999999999999999999999"), tsv("unknown-consultation-again", "co9999999999999999999999"), tsv("slug", "consultation-1"),
        tsv("unpublished", C6), tsv("uppercase", C1.upper()), tsv("space", C1 + "%20"), tsv("empty-id", ""), tsv("update-id", "cu0000000000000000000001"),
        adm("accept-json", "/admin/consultations/%s/responses" % C1, headers={"Accept": "application/json"}),
        adm("accept-xml", "/admin/consultations/%s/responses" % C1, headers={"Accept": "application/xml"}),
        adm("accept-text", "/admin/consultations/%s/responses" % C1, headers={"Accept": "text/plain"}),
        adm("accept-image", "/admin/consultations/%s/responses" % C1, headers={"Accept": "image/png"}),
        adm("media-foo", "/admin/consultations/%s/responses?mediaType=foo" % C1),
        adm("post", "/admin/consultations/%s/responses" % C1, method="POST", body={}),
        adm("delete", "/admin/consultations/%s/responses" % C1, method="DELETE"),
        adm("options", "/admin/consultations/%s/responses" % C1, method="OPTIONS"),
        adm("trailing-slash", "/admin/consultations/%s/responses/" % C1),
        adm("etag", "/admin/consultations/%s/responses" % C1, headers={"If-None-Match": '"0abc"'}),
        adm("cors", "/admin/consultations/%s/responses" % C1, headers={"Origin": "https://www.agora.gouv.fr"}),
    ]
    out.append(SCN("S6-tsv-routes", steps))
    return out


def data():
    out = []
    codes = department_codes()
    qs1, qs2, qs3, qs4, qs6 = qid(2, 1), qid(2, 2), qid(2, 3), qid(2, 4), qid(2, 6)
    ch = lambda q, k: cid_(2, q, k)

    # 1. every value of every attribute (and invalid ones), ages around the limits
    users = []

    def add(profile, answers=None):
        n = len(users) + 1
        users.append({"n": n, "profile": profile, "answers": answers or [(qs1, ch(1, 1 + n % 3), ""), (qs2, ch(2, 1 + n % 3), "")]})
    for g in ["M", "F", "A", "X", "", None]:
        add(pr(gender=g))
    for age in [-3, -1, 0, 1, 17, 18, 19, 25, 26, 35, 36, 45, 46, 55, 56, 65, 66, 100, 2000, None]:
        add(pr(year_of_birth=None if age is None else YEAR - age))
    for y in [0, 1990, 5, 99999]:
        add(pr(year_of_birth=y))
    for c in ["R", "U", "A", "Z", "r", None]:
        add(pr(city_type=c))
    for c in ["AG", "AR", "CA", "PI", "EM", "OU", "ET", "RE", "AU", "UN", "ZZ", "ag", None]:
        add(pr(job_category=c))
    for f in ["S", "P", "J", "X", None]:
        add(pr(vote_frequency=f, public_meeting_frequency=f, consultation_frequency=f))
    for c in codes + ["1", "01", "999", "", "Paris", "2a", None]:
        add(pr(department=c))
    add(None)
    add(None)
    add(pr())
    out.append(SCN("S6-tsv-data-values", [tsv("tsv", C2), tsv("tsv-again", C2)], setup_=setup(sql=data_sql(C2, users))))

    # 2. 128 participants: ties at the 5th decimal (HALF_EVEN) in the percentages (1/128 of 100 is 0.78125)
    users = []
    for i in range(1, 129):
        g = "M" if i == 1 else ("F" if i <= 4 else ("A" if i <= 9 else None))
        a = [(qs1, ch(1, 1 if i == 1 else (2 if i <= 4 else 3)), ""), (qs2, ch(2, 1), "") if i % 3 else (qs2, ch(2, 2), "")]
        users.append({"n": i, "profile": pr(gender=g, city_type="R" if i % 7 == 0 else None), "answers": a})
    out.append(SCN("S6-tsv-data-ties", [tsv("tsv", C2)], setup_=setup(sql=data_sql(C2, users))))

    # 3. participant counts that give recurring decimals
    for count in (3, 7, 9, 11, 13, 6, 1):
        users = [{"n": i, "profile": pr(gender="M" if i % 2 else "F", department=codes[i % len(codes)], year_of_birth=YEAR - 20 - i, job_category="CA" if i % 3 == 0 else None),
                  "answers": [(qs1, ch(1, 1 + i % 3), ""), (qs2, ch(2, 1), ""), (qs2, ch(2, 2 + i % 2), ""), (qs3, None, "texte"), (qs4, ch(4, 1 + i % 3), ""), (qs6, ch(6, 3), "x")]}
                 for i in range(1, count + 1)]
        out.append(SCN("S6-tsv-data-count-%d" % count, [tsv("tsv", C2)], setup_=setup(sql=data_sql(C2, users))))

    # 4. answers of another consultation to the questions of consultation 2: ratios over 1 (grouping of the digits) and Infinity (a
    # value that no participant of the consultation has)
    users = [{"n": 1, "profile": pr(gender="M", city_type="U"), "answers": [(qs1, ch(1, 1), "")]}]
    other = [{"n": i, "profile": pr(gender="F" if i % 2 else "M", department="75", year_of_birth=YEAR - 30, job_category="AG", vote_frequency="J"), "answers": []} for i in range(10, 40)]
    sql = data_sql(C2, users) + data_sql(C2, other, participants=False)
    rows = ["('%s', '%s', '%s', now(), '%s', '%s', '', '%s')" % (uid("foreign", i), ch(1, 1), C1_ID, uid("fp", i), qs1, u(i)) for i in range(10, 40)]
    sql.append("INSERT INTO reponses_consultation (id, choice_id, consultation_id, participation_date, participation_id, question_id, response_text, user_id) VALUES " + ", ".join(rows))
    out.append(SCN("S6-tsv-data-inconsistent", [tsv("tsv", C2)], setup_=setup(sql=sql)))

    # 5. null keys: users without a profile, profiles with NULL columns and unknown codes collide on the null key
    users = [{"n": 1, "profile": None, "answers": [(qs1, ch(1, 1), "")]}, {"n": 2, "profile": pr(), "answers": [(qs1, ch(1, 1), "")]},
             {"n": 3, "profile": pr(gender="Q", city_type="Q", job_category="Q", vote_frequency="Q", year_of_birth=None, department="Q"), "answers": [(qs1, ch(1, 2), "")]},
             {"n": 4, "profile": pr(gender="M"), "answers": [(qs1, ch(1, 2), "")]}]
    out.append(SCN("S6-tsv-data-null-keys", [tsv("tsv", C2)], setup_=setup(sql=data_sql(C2, users))))

    # 6. no participant, participants without answers, answers without participants
    out.append(SCN("S6-tsv-data-empty", [tsv("tsv", C2), tsv("tsv-c3", C3)], setup_=setup()))
    out.append(SCN("S6-tsv-data-participants-only", [tsv("tsv", C2)], setup_=setup(sql=data_sql(C2, [{"n": i, "profile": pr(gender="M"), "answers": []} for i in range(1, 4)]))))
    out.append(SCN("S6-tsv-data-answers-only", [tsv("tsv", C2)], setup_=setup(sql=data_sql(C2, [{"n": i, "profile": pr(gender="M"), "answers": [(qs1, ch(1, 1), "")]} for i in range(1, 4)], participants=False))))
    return out


C1_ID = "co0000000000000000000001"
