"""Strapi payloads the Kotlin DTOs cannot read (the whole list is then empty), payloads with null elements
(NullPointerException later), faults of the fake Strapi, and slug / id resolution."""
from common import *
from variants import *


def now(d):
    return "{{now%s}}" % d


def mutated(n, mutate, **kw):
    c = consultation(n, start=now("-3d"), end=now("+8d"), autres=[autre("vc%04d" % n, 1, now("-1d"))], **kw)
    mutate(c)
    return c


def short_steps(c, writes=True):
    """The routes of one consultation (the ids are fixed by consultation())."""
    cid, slug = c["documentId"], c["slug"] or "none"
    s = [
        step("details-anon", "/v2/consultations/" + cid), step("details-slug", "/v2/consultations/" + slug),
        step("details-answered", "/v2/consultations/" + cid, **{"as": U1}), step("details-idle", "/v2/consultations/" + cid, **{"as": IDLE}),
        step("update-avant", "/v2/consultations/%s/updates/lancement" % cid), step("update-fin", "/v2/consultations/%s/updates/fin" % cid, **{"as": U1}),
        step("update-autre", "/v2/consultations/%s/updates/autre-1" % cid), step("questions", "/consultations/%s/questions" % cid),
        step("preview", "/consultations"), step("preview-user", "/consultations", **{"as": U1}),
    ]
    if writes:
        s += [step("feedback-fin", "/consultations/%s/updates/up%s02/feedback" % (cid, cid), method="POST", body={"isPositive": True}, **{"as": IDLE}),
              step("feedback-autre", "/consultations/%s/updates/up%s11/feedback" % (cid, cid), method="POST", body={"isPositive": False}, **{"as": IDLE})]
    return s


def sc(name, n, mutate, writes=True, data=None, **kw):
    c = mutated(n, mutate, **kw)
    fixture = "s4-invalid-%s.json" % name
    write_fixture(fixture, data(c) if data else [c])
    setup = {"sql": answered_sql([c["documentId"]])}
    setup.update(override_fixture("consultations", "variants/" + fixture))
    return scenario("S4-invalid-" + name, short_steps(c, writes), dbdiff="step" if writes else "end", setup=setup)


def run():
    out = []

    def setkey(k, v):
        def f(c):
            c[k] = v
        return f

    def delkey(k):
        def f(c):
            del c[k]
        return f

    def nested(path, v, delete=False):
        def f(c):
            o = c
            for p in path[:-1]:
                o = o[p]
            if delete:
                del o[path[-1]]
            else:
                o[path[-1]] = v
        return f

    cases = [
        ("date-offset", setkey("datetime_de_fin", "2026-10-30T10:00:00+02:00")),
        ("date-number", setkey("datetime_de_fin", 1790000000)),
        ("date-empty", setkey("datetime_de_debut", "")),
        ("date-date-only", setkey("datetime_de_debut", "2026-10-01")),
        ("date-null", setkey("datetime_de_fin", None)),
        ("missing-titre", delkey("titre_consultation")),
        ("null-slug", setkey("slug", None)),
        ("null-thematique", setkey("thematique", None)),
        ("thematique-without-label", nested(["thematique", "label"], None, True)),
        ("null-avant", setkey("consultation_avant_reponse", None)),
        ("missing-apres", delkey("consultation_apres_reponse_ou_terminee")),
        ("apres-without-feedback-message", nested(["consultation_apres_reponse_ou_terminee", "feedback_message"], None, True)),
        ("null-questions", setkey("questions", None)),
        ("missing-questions", delkey("questions")),
        ("missing-autres", delkey("consultation_contenu_autres")),
        ("null-autres", setkey("consultation_contenu_autres", None)),
        ("autre-null-date", nested(["consultation_contenu_autres", 0, "datetime_publication"], None)),
        ("section-unknown-component", nested(["consultation_avant_reponse", "sections"], [{"id": 1, "__component": "consultation-section.nope", "titre": "x"}])),
        ("section-missing-component", nested(["consultation_avant_reponse", "sections"], [{"id": 1, "titre": "x"}])),
        ("section-null-component", nested(["consultation_avant_reponse", "sections"], [{"id": 1, "__component": None, "titre": "x"}])),
        ("section-numeric-component", nested(["consultation_avant_reponse", "sections"], [{"id": 1, "__component": 5, "titre": "x"}])),
        ("section-not-an-object", nested(["consultation_avant_reponse", "sections"], ["x"])),
        ("section-missing-field", nested(["consultation_avant_reponse", "sections"], [{"id": 1, "__component": "consultation-section.section-titre"}])),
        ("section-video-missing-author", nested(["consultation_avant_reponse", "sections"],
                                                  [{k: v for k, v in s_video(1, None).items() if k != "nom_auteur"}])),
        ("sections-null", nested(["consultation_avant_reponse", "sections"], None)),
        ("sections-not-a-list", nested(["consultation_avant_reponse", "sections"], {"a": 1})),
        ("question-unknown-component", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.nope", "titre": "x", "numero": 1}])),
        ("question-missing-component", nested(["questions"], [{"id": 1, "titre": "x", "numero": 1}])),
        ("question-missing-titre", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-ouverte", "numero": 1}])),
        ("question-choix-null", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-a-choix-unique", "titre": "x", "numero": 1, "choix": None}])),
        ("question-choix-missing-label", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-a-choix-unique", "titre": "x", "numero": 1, "choix": [{"id": 1, "ouvert": False}]}])),
        ("question-numero-missing", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-ouverte", "titre": "x"}])),
        ("richtext-bad-children", nested(["consultation_avant_reponse", "presentation"], [{"type": "paragraph"}])),
        ("richtext-not-a-list", nested(["consultation_avant_reponse", "presentation"], {"type": "paragraph"})),
        ("richtext-null", nested(["consultation_avant_reponse", "presentation"], None)),
        ("richtext-link-child-not-text", nested(["consultation_avant_reponse", "presentation"], [{"type": "paragraph", "children": [{"type": "link", "url": "u", "children": [{"type": "paragraph", "children": []}]}]}])),
        ("analyse-without-link", nested(["consultation_contenu_analyse_des_reponse"], {k: v for k, v in analyse("vc0000", now("-2d")).items() if k != "lien_telechargement_analyse"})),
        ("string-number", setkey("nombre_participants_cible", "abc")),
        ("float-number", setkey("nombre_participants_cible", 1e30)),
        ("bool-for-string", setkey("territoire", {"a": 1})),
    ]
    n = 200
    for name, mutate in cases:
        n += 1
        c = None
        if name in ("analyse-without-link",):
            out.append(sc(name, n, lambda c, m=mutate: (m(c)), writes=False))
        else:
            out.append(sc(name, n, mutate, writes=False))

    # decodable, but the mappers fail on a null element (NullPointerException / NoWhenBranchMatchedException: HTTP 500)
    nulls = [
        ("autres-null-element", nested(["consultation_contenu_autres"], [None])),
        ("autres-null-element-after", lambda c: c["consultation_contenu_autres"].append(None)),
        ("avant-sections-null-element", nested(["consultation_avant_reponse", "sections"], [None])),
        ("apres-sections-null-element", nested(["consultation_apres_reponse_ou_terminee", "sections"], [s_titre(1), None])),
        ("autre-sections-null-element", nested(["consultation_contenu_autres", 0, "sections"], [None])),
        ("questions-null-element", nested(["questions"], [None])),
        ("questions-null-element-last", lambda c: c["questions"].append(None)),
        ("choix-null-element", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-a-choix-unique", "titre": "x", "numero": 1, "choix": [None]}])),
        ("popup-null-element", nested(["questions"], [{"id": 1, "__component": "question-de-consultation.question-ouverte", "titre": "x", "numero": 1, "popup_explication": [None]}])),
        ("richtext-null-element", nested(["consultation_avant_reponse", "presentation"], [None])),
        ("richtext-null-child", nested(["consultation_avant_reponse", "presentation"], [{"type": "paragraph", "children": [None]}])),
        ("sections-body-null-element-of-list-of-list", nested(["consultation_apres_reponse_ou_terminee", "sections"], [{"id": 1, "__component": "consultation-section.section-texte-riche", "description": [None]}])),
    ]
    for name, mutate in nulls:
        n += 1
        out.append(sc(name, n, mutate, writes=False))

    # an exception inside the queued action leaves the user locked in the reference (B-AGORAQUEUE): one user per throwing feedback
    n += 1
    c = mutated(n, nested(["consultation_contenu_autres"], [None]))
    write_fixture("s4-invalid-null-element-feedback.json", [c])
    cid = c["documentId"]
    steps = [step("feedback-throws", "/consultations/%s/updates/up%s02/feedback" % (cid, cid), method="POST", body={"isPositive": True}, **{"as": U("UserProfile10")}),
             step("feedback-throws-2", "/consultations/%s/updates/up%s11/feedback" % (cid, cid), method="POST", body={"isPositive": True}, **{"as": U("UserProfile11")}),
             step("feedback-unknown-update", "/consultations/%s/updates/nope/feedback" % cid, method="POST", body={"isPositive": True}, **{"as": U("UserProfile12")})]
    out.append(scenario("S4-invalid-null-element-feedback", steps, dbdiff="step",
                        setup=override_fixture("consultations", "variants/s4-invalid-null-element-feedback.json")))

    # `data` shapes
    def only(data):
        return lambda c: data
    out.append(scenario("S4-invalid-data-empty", short_steps(mutated(301, lambda c: None), False), dbdiff="end", setup=override_data("consultations", [])))
    out.append(scenario("S4-invalid-data-object", short_steps(mutated(302, lambda c: None), False), dbdiff="end", setup=override_data("consultations", {"a": 1})))

    # two consultations: the second one cannot be read, so none can
    good = mutated(310, lambda c: None)
    bad = mutated(311, setkey("datetime_de_fin", "x"))
    write_fixture("s4-invalid-second-unreadable.json", [good, bad])
    out.append(scenario("S4-invalid-second-unreadable", short_steps(good, False), dbdiff="end",
                        setup=dict(override_fixture("consultations", "variants/s4-invalid-second-unreadable.json"), sql=answered_sql([good["documentId"]]))))

    # resolution of ids and slugs
    a = mutated(320, lambda c: None)  # slug variante-320
    b = mutated(321, lambda c: c.update({"slug": a["documentId"]}))  # the slug of b is the id of a: asked by that string, the slug wins
    c3 = mutated(322, lambda c: c.update({"slug": "Slug With Spaces & Ünïcode/é"}))
    c4 = mutated(323, lambda c: c.update({"documentId": a["documentId"], "slug": "double"}))  # same document id as a: the first one wins
    c5 = mutated(324, lambda c: c.update({"slug": "variante-320"}))  # same slug as a
    c6 = mutated(325, lambda c: c.update({"slug": ""}))
    c7 = mutated(326, lambda c: c.update({"slug": "vc0326"}))  # slug == id
    write_fixture("s4-resolution.json", [a, b, c3, c4, c5, c6, c7])
    steps = []
    for p in (a["documentId"], a["slug"], b["documentId"], "Slug%20With%20Spaces%20%26%20%C3%9Cn%C3%AFcode%2F%C3%A9", "double", "variante-320", "vc0326", "%20"):
        steps.append(step("details-" + p[:20], "/v2/consultations/" + p))
        steps.append(step("update-" + p[:20], "/v2/consultations/%s/updates/lancement" % p))
        steps.append(step("questions-" + p[:20], "/consultations/%s/questions" % p))
    steps.append(step("preview", "/consultations"))
    out.append(scenario("S4-resolution-ids-and-slugs", steps, setup=override_fixture("consultations", "variants/s4-resolution.json")))

    # Strapi faults, on ids that were never read (the reference keeps its by-id cache across scenarios)
    ids = [("fault-" + str(i), "/v2/consultations/" + "x%d" % (900 + i)) for i in range(1)]
    for mode, delay in (("500", None), ("malformed", None), ("nulldata", None), ("missingfield", None), ("slow", 300)):
        steps = [step("details", "/v2/consultations/zz-%s-1" % mode), step("details-again", "/v2/consultations/zz-%s-1" % mode, **{"as": U1}),
                 step("update", "/v2/consultations/zz-%s-2/updates/lancement" % mode), step("questions", "/consultations/zz-%s-3/questions" % mode),
                 step("questions-again", "/consultations/zz-%s-3/questions" % mode), step("preview", "/consultations"), step("preview-user", "/consultations", **{"as": U1}),
                 step("preview-moderator", "/consultations", **{"as": MODERATOR}),
                 step("feedback", "/consultations/zz-%s-4/updates/up1/feedback" % mode, method="POST", body={"isPositive": True}, **{"as": IDLE})]
        out.append(scenario("S4-faults-" + mode, steps, dbdiff="step", setup=faults("consultations", mode, delay)))
    # a fault on the thematiques only: the thematique of the consultation is part of the consultation payload
    steps = [step("details", "/v2/consultations/" + CONS[0]), step("preview", "/consultations")]
    out.append(scenario("S4-faults-thematiques-do-not-matter", steps, setup=faults("thematiques", "500")))
    # Strapi suspended is a configuration (not covered here); a slow Strapi answers normally
    steps = [step("details-slow", "/v2/consultations/" + CONS[1]), step("preview-slow", "/consultations", **{"as": U1}), step("questions-slow", "/consultations/%s/questions" % CONS[1])]
    out.append(scenario("S4-faults-slow-known-ids", steps, setup=faults("consultations", "slow", 400)))
    return out
