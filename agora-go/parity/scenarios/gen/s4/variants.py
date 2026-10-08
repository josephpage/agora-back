"""Strapi variants of the consultations: fixtures under parity/fixtures/strapi/variants/s4-*.json and the scenarios using them.

Every variant uses consultation ids that no other scenario uses: the reference keeps its consultation-by-id
cache (5 minutes, in the JVM) across scenarios.
"""
import copy
import json
import os

from common import *

VARIANTS_DIR = os.path.join(FIXTURES, "variants")
PUBLISHED = "2024-05-29T10:19:13.352Z"
CREATED = "2024-05-29T09:37:12.407Z"
THEMATIQUE = {"id": 3, "documentId": "th0000000000000000000003", "label": "Éducation", "pictogramme": "📚",
              "createdAt": CREATED, "updatedAt": CREATED, "publishedAt": PUBLISHED, "locale": None}


def T(text, **marks):
    d = {"type": "text", "text": text}
    d.update(marks)
    return d


def P(*children):
    return {"type": "paragraph", "children": list(children)}


def rich(*paras):
    return [P(T(p)) for p in paras]


def pic(name, medium=True, formats=True):
    d = {"id": 1, "documentId": "pic" + name, "name": name + ".jpg", "url": "https://cdn.parity.example/uploads/%s.jpg" % name}
    if formats:
        d["formats"] = {"thumbnail": {"url": "https://cdn.parity.example/uploads/thumbnail_%s.jpg" % name}}
        if medium:
            d["formats"]["medium"] = {"url": "https://cdn.parity.example/uploads/medium_%s.jpg" % name}
    else:
        d["formats"] = None
    return d


# --- sections -------------------------------------------------------------

def s_titre(i, t="Titre"):
    return {"id": i, "__component": "consultation-section.section-titre", "titre": t}


def s_texte(i, *paras):
    return {"id": i, "__component": "consultation-section.section-texte-riche", "description": rich(*(paras or ("Texte",)))}


def s_citation(i, *paras):
    return {"id": i, "__component": "consultation-section.section-citation", "description": rich(*(paras or ("Citation",)))}


def s_image(i, image=None, url="https://cdn.parity.example/sections/img-%d.jpg", descr="Image"):
    return {"id": i, "__component": "consultation-section.section-image", "url": url % i if "%d" in url else url,
            "description_accessible_de_l_image": descr, "image": image}


def s_video(i, video=None, date="2026-03-04", w=640, h=360):
    return {"id": i, "__component": "consultation-section.section-video", "url": "https://cdn.parity.example/sections/vid-%d.mp4" % i,
            "largeur": w, "hauteur": h, "nom_auteur": "Auteur %d" % i, "poste_auteur": "Poste %d" % i, "date_tournage": date,
            "transcription": "Transcription %d" % i, "video": video}


def s_chiffre(i, titre="42 %", *paras):
    return {"id": i, "__component": "consultation-section.section-chiffre", "titre": titre, "description": rich(*(paras or ("Chiffre",)))}


def s_accordeon(i, titre="Accordéon", *paras):
    return {"id": i, "__component": "consultation-section.section-accordeon", "titre": titre, "description": rich(*(paras or ("Accordéon",)))}


def sections(n, start=1):
    makers = [lambda i: s_titre(i, "Titre %d" % i), lambda i: s_texte(i, "Texte %d" % i), lambda i: s_citation(i, "Citation %d" % i),
              lambda i: s_image(i, pic("img%d" % i)), lambda i: s_video(i, {"id": i, "url": "https://cdn.parity.example/uploads/v%d.mp4" % i}),
              lambda i: s_chiffre(i, "%d %%" % i, "Chiffre %d" % i), lambda i: s_accordeon(i, "Accordéon %d" % i, "Réponse %d" % i)]
    return [makers[(start + k) % 7](start + k) for k in range(n)]


# --- contents -------------------------------------------------------------

def avant(cid, secs=None, **kw):
    d = {"id": 1, "documentId": "up%s01" % cid, "slug": "lancement", "template_partage": "Participez : {title} {url}",
         "historique_titre": "Lancement", "historique_call_to_action": "Voir les objectifs", "nom_strapi": "avant",
         "commanditaire": rich("Le Gouvernement"), "objectif": rich("Recueillir l'avis"), "axe_gouvernemental": rich("Axe 1"),
         "presentation": rich("Premier paragraphe.", "Deuxième paragraphe."), "sections": secs if secs is not None else sections(2),
         "createdAt": CREATED, "updatedAt": CREATED}
    d.update(kw)
    return d


def apres(cid, secs=None, **kw):
    d = {"id": 2, "documentId": "up%s02" % cid, "slug": "fin", "template_partage": "Résultats : {title} {url}",
         "historique_titre": "Fin", "historique_call_to_action": "Résultats", "nom_strapi": "apres", "feedback_message": "Satisfait(e) ?",
         "sections": secs if secs is not None else sections(2), "createdAt": CREATED, "updatedAt": CREATED}
    d.update(kw)
    return d


def autre(cid, n, when, secs=None, **kw):
    d = {"id": 10 + n, "documentId": "up%s%02d" % (cid, 10 + n), "slug": "autre-%d" % n, "template_partage": "Actu %d : {title} {url}" % n,
         "historique_titre": "Actualité %d" % n, "historique_call_to_action": "Lire %d" % n, "nom_strapi": "autre-%d" % n,
         "feedback_message": "Avis sur %d ?" % n, "datetime_publication": when, "flamme_label": "Flamme %d" % n,
         "recap_emoji": "🔥", "recap_label": "Récap %d" % n, "sections": secs if secs is not None else sections(1),
         "createdAt": CREATED, "updatedAt": CREATED}
    d.update(kw)
    return d


def analyse(cid, when, secs=None, **kw):
    d = {"id": 3, "documentId": "up%s03" % cid, "slug": "analyse", "template_partage": "Analyse : {title} {url}",
         "lien_telechargement_analyse": "https://cdn.parity.example/analyses/%s.pdf" % cid, "datetime_publication": when,
         "feedback_message": "Avis sur l'analyse ?", "historique_titre": "Analyse", "historique_call_to_action": "Synthèse",
         "nom_strapi": "analyse", "flamme_label": "Analyse dispo", "recap_emoji": "📊", "recap_label": "Analyse",
         "sections": secs if secs is not None else sections(1), "pdf_analyse": {"id": 3, "url": "https://cdn.parity.example/uploads/%s.pdf" % cid},
         "createdAt": CREATED, "updatedAt": CREATED}
    d.update(kw)
    return d


def commanditaire(cid, when, secs=None, **kw):
    d = {"id": 4, "documentId": "up%s04" % cid, "slug": "reponse", "template_partage": "Réponse : {title} {url}",
         "datetime_publication": when, "feedback_message": "Avis sur la réponse ?", "historique_titre": "Réponse du Gouvernement",
         "historique_call_to_action": "Actions", "nom_strapi": "cmd", "flamme_label": "Réponse !", "recap_emoji": "🏛", "recap_label": "Réponse",
         "sections": secs if secs is not None else sections(1), "createdAt": CREATED, "updatedAt": CREATED}
    d.update(kw)
    return d


def questions(cid):
    def q(n, component, **kw):
        d = {"id": int(cid[-3:]) * 10 + n if cid[-3:].isdigit() else n, "__component": component, "titre": "Question %d" % n, "numero": n,
             "popup_explication": None, "question_suivante": None}
        d.update(kw)
        return d

    def ch(n, k, **kw):
        d = {"id": n * 10 + k, "label": "Choix %d.%d" % (n, k), "ouvert": False}
        d.update(kw)
        return d
    return [
        q(1, "question-de-consultation.question-a-choix-unique", choix=[ch(1, 1), ch(1, 2, ouvert=True)]),
        q(2, "question-de-consultation.question-a-choix-multiples", nombre_maximum_de_choix=2, choix=[ch(2, 1), ch(2, 2), ch(2, 3)]),
        q(3, "question-de-consultation.question-ouverte"),
        q(4, "question-de-consultation.question-conditionnelle", choix=[ch(4, 1, numero_de_la_question_suivante=5), ch(4, 2, numero_de_la_question_suivante=6)]),
        q(5, "question-de-consultation.description", description=rich("Chapitre"), url_image=None, transcription_image=None, image=None),
        q(6, "question-de-consultation.question-a-choix-unique", choix=[ch(6, 1), ch(6, 2)], question_suivante=999),
    ]


def consultation(n, slug=None, start="{{now-5d}}", end="{{now+5d}}", secs_avant=None, secs_apres=None, autres=(), analyse_=None,
                 commanditaire_=None, a_venir=None, qs=True, **kw):
    cid = "vc%04d" % n
    c = {"id": 1000 + n, "documentId": cid, "titre_consultation": "Variante %d" % n, "slug": slug or "variante-%d" % n,
         "datetime_de_debut": start, "datetime_de_fin": end,
         "url_image_de_couverture": "https://cdn.parity.example/covers/%s.jpg" % cid, "url_image_page_de_contenu": "https://cdn.parity.example/covers/%s-c.jpg" % cid,
         "nombre_de_questions": 5, "estimation_nombre_de_questions": "5 questions", "estimation_temps": "10 minutes",
         "nombre_participants_cible": 1000, "territoire": "France", "titre_page_web": "Titre web %d" % n, "sous_titre_page_web": "Sous-titre %d" % n,
         "thematique": copy.deepcopy(THEMATIQUE), "questions": questions(cid) if qs else [],
         "consultation_avant_reponse": avant(cid, secs_avant), "consultation_apres_reponse_ou_terminee": apres(cid, secs_apres),
         "consultation_contenu_analyse_des_reponse": analyse_, "contenu_reponse_du_commanditaires": commanditaire_,
         "consultation_contenu_autres": list(autres), "consultation_contenu_a_venir": a_venir,
         "image_de_couverture": None, "image_page_de_contenu": None, "createdAt": CREATED, "updatedAt": CREATED, "publishedAt": PUBLISHED, "locale": None}
    c.update(kw)
    return c


def write_fixture(name, data):
    path = os.path.join(VARIANTS_DIR, name)
    with open(path, "w") as f:
        json.dump(data, f, ensure_ascii=False, indent=1)
        f.write("\n")


# --- scenarios --------------------------------------------------------------

def answered_sql(ids):
    rows = []
    n = 0
    for cid in ids:
        for user in ("00000000-0000-4000-9000-000000000001", "00000000-0000-4000-9000-000000000002"):
            n += 1
            rows.append("('cccccccc-0000-4000-a000-%012d', '%s', now(), '%s')" % (n, cid, user))
    # ANALYZE: the order of a DISTINCT without ORDER BY depends on the plan, which depends on the statistics (the two
    # databases would otherwise be analyzed by autovacuum at different times)
    return ["INSERT INTO user_answered_consultation (id, consultation_id, participation_date, user_id) VALUES " + ", ".join(rows),
            "ANALYZE user_answered_consultation"]


def cons_steps(c, with_writes=True):
    """Every route for one variant consultation."""
    cid, slug = c["documentId"], c["slug"]
    steps = [
        step("%s-details-anon" % cid, "/v2/consultations/" + cid),
        step("%s-details-slug" % cid, "/v2/consultations/" + slug),
        step("%s-details-idle" % cid, "/v2/consultations/" + cid, **{"as": IDLE}),
        step("%s-details-answered" % cid, "/v2/consultations/" + cid, **{"as": U1}),
        step("%s-questions" % cid, "/consultations/%s/questions" % cid),
        step("%s-questions-xml" % cid, "/consultations/%s/questions?mediaType=xml" % cid),
    ]
    # XML of the unanswered view only (the other views have goals: null, see S4-known-xml-null-list)
    if str(c.get("datetime_de_fin", "")).startswith("{{now+"):
        steps.append(step("%s-details-xml" % cid, "/v2/consultations/%s?mediaType=xml" % cid, **{"as": IDLE}))
    try:
        ups = updates_of(c)
    except Exception:
        ups = []
    for (uid, uslug, kind) in ups:
        steps.append(step("%s-update-%s" % (cid, uid[-2:]), "/v2/consultations/%s/updates/%s" % (cid, uid), **{"as": U1}))
        steps.append(step("%s-update-slug-%s" % (cid, uid[-2:]), "/v2/consultations/%s/updates/%s" % (cid, uslug)))
    if with_writes:
        for (uid, uslug, kind) in ups:
            steps.append(step("%s-feedback-%s" % (cid, uid[-2:]), "/consultations/%s/updates/%s/feedback" % (cid, uid), method="POST",
                              body={"isPositive": True}, **{"as": IDLE}))
        steps.append(step("%s-details-after-feedback" % cid, "/v2/consultations/" + cid, **{"as": U1}))
        steps.append(step("%s-details-after-feedback-idle" % cid, "/v2/consultations/" + cid, **{"as": IDLE}))
    return steps


def fixture_scenario(name, fixture, consultations_, extra=None, dbdiff=None, with_writes=True):
    dbdiff = dbdiff or ("step" if with_writes else "end")
    steps = []
    for c in consultations_:
        steps += cons_steps(c, with_writes)
    steps += [step("preview-anon", "/consultations"), step("preview-user", "/consultations", **{"as": U1}),
              step("preview-moderator", "/consultations", **{"as": MODERATOR}), step("preview-xml", "/consultations?mediaType=xml", **{"as": U1})]
    steps += extra or []
    setup = {"sql": answered_sql([c["documentId"] for c in consultations_])}
    setup.update(override_fixture("consultations", "variants/" + fixture))
    return scenario(name, steps, dbdiff=dbdiff, setup=setup)


def run():
    out = []
    now = lambda d: "{{now%s}}" % d

    # 1. sections of every kind, with and without media, previews (8 sections or more)
    media_secs = [
        s_image(1, pic("a")), s_image(2, pic("b", medium=False)), s_image(3, pic("c", formats=False)), s_image(4, None),
        s_video(5, {"id": 5, "url": "https://cdn.parity.example/uploads/v5.mp4"}), s_video(6, None), s_titre(7, ""), s_texte(8, "", "<b>brut</b> & \"quotes\" é"),
        s_citation(9), s_chiffre(10, "", ""), s_accordeon(11, "", ""),
    ]
    c1 = consultation(1, secs_avant=sections(8), secs_apres=sections(7), autres=[autre("vc0001", 1, now("-3d"), sections(9))],
                      analyse_=analyse("vc0001", now("-1d"), sections(8)), commanditaire_=commanditaire("vc0001", now("-2d"), media_secs))
    c2 = consultation(2, secs_avant=media_secs, secs_apres=[], autres=[autre("vc0002", 1, now("-3d"), media_secs[:8])], start=now("-20d"), end=now("-10d"))
    c3 = consultation(3, secs_avant=[], secs_apres=sections(14), start=now("-3d"), end=now("+8d"))
    cs = [c1, c2, c3]
    write_fixture("s4-sections.json", cs)
    out.append(fixture_scenario("S4-variant-sections", "s4-sections.json", cs))

    # 2. which update is the latest, history statuses and dates
    def combo(n, autres=(), a=None, r=None, start=now("-30d"), end=now("-20d"), venir=False):
        cid = "vc%04d" % n
        return consultation(n, start=start, end=end, secs_avant=sections(1), secs_apres=sections(1),
                            autres=[autre(cid, k + 1, w) for k, w in enumerate(autres)],
                            analyse_=analyse(cid, a) if a else None, commanditaire_=commanditaire(cid, r) if r else None,
                            a_venir={"id": 90 + n, "documentId": "up%s90" % cid, "titre_historique": "À venir !"} if venir else None)
    latest = [
        combo(10), combo(11, autres=[now("-5d")]), combo(12, autres=[now("-5d"), now("-3d"), now("-8d")]),
        combo(13, autres=[now("+12d")]), combo(14, a=now("-4d")), combo(15, r=now("-4d")), combo(16, a=now("-4d"), r=now("-6d")),
        combo(17, a=now("+13d"), r=now("+14d"), venir=True), combo(18, autres=[now("-5d")], a=now("-4d"), r=now("-3d"), venir=True),
        combo(19, autres=["2026-01-01T10:00:00.000Z", "2026-01-01T10:00:00.000Z"]),  # tie: the first one wins
        combo(20, start=now("-3d"), end=now("+8d"), autres=[now("-1d")], venir=True),  # ongoing, answered view = latest
        combo(21, start=now("+10d"), end=now("+19d"), venir=True),  # not started yet
        combo(22, start=now("-3d"), end=now("+8d"), a=now("-1d"), r=now("+12d")),
        combo(23, autres=[now("-1d")], a=now("+12d")),
    ]
    write_fixture("s4-latest.json", latest)
    out.append(fixture_scenario("S4-variant-latest-update", "s4-latest.json", latest, with_writes=False))
    out.append(fixture_scenario("S4-variant-latest-update-feedback", "s4-latest.json", latest[:6], with_writes=True))

    # 3. questions of every kind
    def q(n, comp, **kw):
        d = {"id": 5000 + n, "__component": comp, "titre": "Q%d <b> & é" % n, "numero": n, "popup_explication": None, "question_suivante": None}
        d.update(kw)
        return d

    def ch(i, **kw):
        d = {"id": 7000 + i, "label": "Choix %d" % i, "ouvert": False}
        d.update(kw)
        return d
    UNI, MUL, OUV, DES, CON = ("question-de-consultation.question-a-choix-unique", "question-de-consultation.question-a-choix-multiples",
                               "question-de-consultation.question-ouverte", "question-de-consultation.description", "question-de-consultation.question-conditionnelle")
    popup = [P(T("Popup"), T(" gras", bold=True))]
    qsets = {
        30: [q(1, UNI, choix=[ch(1), ch(2, ouvert=True)], popup_explication=popup), q(2, MUL, nombre_maximum_de_choix=3, choix=[ch(3), ch(4), ch(5)]), q(3, OUV, popup_explication=[]),
             q(4, DES, description=rich("Chapitre un"), url_image="https://cdn.parity.example/q/4.jpg", transcription_image="Transcription", image=pic("q4")),
             q(5, CON, choix=[{"id": 7100, "label": "Si oui", "ouvert": False, "numero_de_la_question_suivante": 6}, {"id": 7101, "label": "Si non", "ouvert": True, "numero_de_la_question_suivante": 7}]),
             q(6, UNI, choix=[ch(6)], question_suivante=7), q(7, OUV, question_suivante=999)],
        31: [q(3, OUV), q(1, UNI, choix=[ch(1)]), q(2, DES, description=rich("Chapitre"), url_image=None, image=None)],  # unordered numbers
        32: [q(1, UNI, choix=[ch(1)], question_suivante=3), q(2, OUV), q(3, OUV), q(1, OUV, id=5099)],  # duplicate number, explicit next
        33: [q(1, UNI, choix=[ch(1)], question_suivante=42), q(2, MUL, nombre_maximum_de_choix=0, choix=[])],  # next question that does not exist
        34: [q(1, DES, description=[]), q(2, DES, description=rich("A", "B"), image=pic("m", medium=False)), q(3, DES, description=rich("C"), image=pic("n", formats=False), url_image="u")],
        35: [],  # no question at all
        36: [q(1, UNI, choix=[]), q(2, MUL, choix=[], nombre_maximum_de_choix=1), q(3, CON, choix=[])],  # no choice
        37: [q(2147483647, OUV), q(-1, OUV), q(0, UNI, choix=[ch(1)])],  # extreme numbers (numero + 1 overflows)
    }
    qcs = []
    for n, qs in sorted(qsets.items()):
        c = consultation(n, qs=False, start=now("-3d"), end=now("+8d"))
        c["questions"] = qs
        c["nombre_de_questions"] = len([x for x in qs if x["__component"] != DES])
        qcs.append(c)
    write_fixture("s4-questions.json", qcs)
    out.append(fixture_scenario("S4-variant-questions", "s4-questions.json", qcs, with_writes=False))

    # a conditional question that points to a missing question: `first` throws (HTTP 500 on the questions only)
    bad = consultation(40, qs=False)
    bad["questions"] = [q(1, UNI, choix=[ch(1)]), q(2, CON, choix=[{"id": 7200, "label": "x", "ouvert": False, "numero_de_la_question_suivante": 99}])]
    write_fixture("s4-conditional-missing.json", [bad])
    out.append(fixture_scenario("S4-variant-conditional-question-missing-next", "s4-conditional-missing.json", [bad], with_writes=False))

    # 4. nulls, optional and minimal content
    mini = consultation(50, qs=False, secs_avant=[], secs_apres=[])
    mini["questions"] = []
    del mini["consultation_avant_reponse"]["sections"]
    del mini["consultation_apres_reponse_ou_terminee"]["sections"]
    nolabels = consultation(51, autres=[autre("vc0051", 1, now("-1d"), flamme_label=None, recap_emoji=None, recap_label=None)],
                            analyse_=analyse("vc0051", now("-2d"), flamme_label=None, recap_emoji="🔥", recap_label=None, pdf_analyse=None),
                            commanditaire_=commanditaire("vc0051", now("-3d"), flamme_label=None, recap_emoji=None, recap_label="Seulement le label"),
                            start=now("-10d"), end=now("-5d"))
    nolabels2 = consultation(52, autres=[autre("vc0052", 1, now("+12d"), flamme_label="Futur")],
                             analyse_=analyse("vc0052", now("-2d"), flamme_label="Label analyse"),
                             commanditaire_=commanditaire("vc0052", now("-3d"), flamme_label=None), start=now("-10d"), end=now("-5d"))
    nolabels3 = consultation(53, autres=[autre("vc0053", 1, now("-1d"), flamme_label=None)], commanditaire_=commanditaire("vc0053", now("-3d"), flamme_label="Label cmd"),
                             analyse_=analyse("vc0053", now("-2d"), flamme_label="Label analyse"), start=now("-10d"), end=now("-5d"))
    images = consultation(54, image_de_couverture=pic("cover"), image_page_de_contenu=pic("page", medium=False), start=now("-3d"), end=now("+8d"))
    images2 = consultation(55, image_de_couverture=pic("cover2", formats=False), image_page_de_contenu=None, start=now("-3d"), end=now("+8d"))
    optional = [mini, nolabels, nolabels2, nolabels3, images, images2]
    write_fixture("s4-optional.json", optional)
    out.append(fixture_scenario("S4-variant-optional-content", "s4-optional.json", optional, with_writes=False))

    # 5. rich text in the unanswered view: presentation / goals
    def rt(*nodes):
        return list(nodes)
    li = lambda *c: {"type": "list-item", "children": list(c)}
    lst = lambda fmt, *items: {"type": "list", "format": fmt, "children": list(items)}
    heading = lambda lvl, *c: {"type": "heading", "level": lvl, "children": list(c)}
    link = lambda url, *c: {"type": "link", "url": url, "children": list(c)}
    pres = {
        60: [],
        61: [P(T("Un seul paragraphe sans fin"))],
        62: [heading(2, T("Titre")), P(T("Après le titre"))],
        63: [{"type": "quote", "children": [T("Citation")]}, P(T("p"))],
        64: [lst("unordered", li(T("a")), li(T("b")))],
        65: [P(T("a </p> dans le texte")), P(T("b"))],
        66: [P(T("<p>déjà</p>"))],
        67: [P(link("https://example.org?a=1&b=2", T("lien")))],
        68: [{"type": "code", "children": [T("x")]}, {"children": [T("sans type")]}],
        69: [P()],
    }
    rts = []
    for n, presentation in sorted(pres.items()):
        cid = "vc%04d" % n
        c = consultation(n, start=now("-3d"), end=now("+8d"))
        c["consultation_avant_reponse"]["presentation"] = presentation
        c["consultation_avant_reponse"]["commanditaire"] = presentation
        c["consultation_avant_reponse"]["objectif"] = [P(T("<p>")), P(T("deux"))]
        c["consultation_avant_reponse"]["axe_gouvernemental"] = []
        rts.append(c)
    write_fixture("s4-richtext.json", rts)
    out.append(fixture_scenario("S4-variant-richtext-goals", "s4-richtext.json", rts, with_writes=False))

    # 6. accepted date formats
    dates = {
        70: ("2026-10-01T10:00:00", "2026-10-30T10:00:00"), 71: ("2026-10-01T10:00:00.123456789", "2026-10-30T10:00:00.1"),
        72: ("2026-10-01T10:00", "2026-10-30t10:00:00Z"), 73: ("  2026-10-01T10:00:00Z  ", "2026-10-30T10:00:00.000Z"),
        74: ([2026, 10, 1, 10, 0], [2026, 10, 30, 10, 0, 5]), 75: ([2026, 10, 1, 10, 0, 5, 123], [2026, 10, 30, 10, 0, 5, 123456789]),
        76: ("+12026-10-01T10:00:00", "+12026-12-01T10:00:00"), 77: ("2026-10-01T10:00:00.", "2026-10-30T10:00:00"),
        78: ("2026-10-01T24:00:00", "2026-10-30T10:00:00"), 79: ("2026-02-30T10:00:00", "2026-10-30T10:00:00"),
    }
    out_ok = []
    for n, (d1, d2) in sorted(dates.items()):
        c = consultation(n, start=d1, end=d2)
        out_ok.append(c)
        write_fixture("s4-date-%d.json" % n, [c])
        out.append(fixture_scenario("S4-variant-dates-%d" % n, "s4-date-%d.json" % n, [c], with_writes=False))

    # 7. date_tournage of a video section: every form
    tournage = {
        80: "2026-03-04", 81: "2026-03-04T10:00:00", 82: "2026-03-04T10:00:00Z", 83: [2026, 3, 4], 84: 20000, 85: "2026-3-4", 86: "", 87: 1.5,
        88: "2026-03-04T10:00", 89: "20260304",
    }
    for n, d in sorted(tournage.items()):
        c = consultation(n, secs_avant=[s_video(1, None, date=d)], start=now("-3d"), end=now("+8d"))
        write_fixture("s4-video-date-%d.json" % n, [c])
        out.append(fixture_scenario("S4-variant-video-date-%d" % n, "s4-video-date-%d.json" % n, [c], with_writes=False))

    # 8. share text templates
    tpl = consultation(90, start=now("-3d"), end=now("+8d"))
    tpl["consultation_avant_reponse"]["template_partage"] = "{url} {url} {title} {title} {Title} {{url}} \\{url} 100% é\n"
    tpl["consultation_apres_reponse_ou_terminee"]["template_partage"] = ""
    tpl["titre_consultation"] = "Titre avec {url} dedans"
    tpl["slug"] = "slug-{title}"
    write_fixture("s4-share-text.json", [tpl])
    out.append(fixture_scenario("S4-variant-share-text", "s4-share-text.json", [tpl], with_writes=False))

    # 9. coercions of the Strapi values
    co = consultation(91, start=now("-3d"), end=now("+8d"))
    co["nombre_de_questions"] = "5"
    co["nombre_participants_cible"] = 12.9
    co["estimation_temps"] = 10
    co["titre_page_web"] = True
    co["questions"][0]["numero"] = "1"
    co["questions"][0]["id"] = "q-one"
    co["consultation_avant_reponse"]["sections"] = [s_video(1, None, w="640", h=360.9)]
    write_fixture("s4-coercions.json", [co])
    out.append(fixture_scenario("S4-variant-coercions", "s4-coercions.json", [co], with_writes=False))

    # 10. territories, unicode, long texts
    ter = consultation(92, start=now("-3d"), end=now("+8d"), territoire="Français de l'étranger")
    ter["titre_consultation"] = "Titre " + "é" * 500 + " 😀   \"guillemets\" \\ / <b>"
    write_fixture("s4-text.json", [ter])
    out.append(fixture_scenario("S4-variant-text", "s4-text.json", [ter], with_writes=False))

    # 11. published and draft: a draft is returned with status=draft (every detail query asks for it)
    dr = consultation(93, start=now("-3d"), end=now("+8d"), publishedAt=None)
    pb = consultation(94, start=now("-3d"), end=now("+8d"))
    write_fixture("s4-draft.json", [dr, pb])
    out.append(fixture_scenario("S4-variant-draft", "s4-draft.json", [dr, pb], with_writes=False))

    # 12. many consultations: the preview lists (sort by end date / last update, answered ones removed from the ongoing list)
    many = []
    for k in range(12):
        many.append(consultation(100 + k, start=now("-%dd" % (20 + k)), end=now("%+dd" % (k - 5)), territoire="France" if k % 2 else "Nord",
                                 autres=[autre("vc%04d" % (100 + k), 1, now("-%dd" % (1 + k % 5)))] if k % 3 == 0 else []))
    write_fixture("s4-many.json", many)
    out.append(fixture_scenario("S4-variant-many-consultations", "s4-many.json", many[:3], with_writes=False))
    return out
