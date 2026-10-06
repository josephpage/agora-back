#!/usr/bin/env python3
"""Generates the fake-Strapi fixtures (parity/fixtures/strapi/*.json).

Usage:  python3 tools/gen_fixtures.py        (from parity/fixtures/strapi/)

The JSON files are stored fully populated (the fake ignores `populate`).
Strings of the form {{now+3d}} / {{date:now-2d}} are resolved by the fake at
load time (see README.md).
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.dirname(HERE)

CDN = "https://cdn.parity.example"
PUBLISHED = "2024-05-29T10:19:13.352Z"
CREATED = "2024-05-29T09:37:12.407Z"
UPDATED = "2024-08-23T13:04:14.174Z"


def dump(name, data, subdir=""):
    path = os.path.join(OUT, subdir, name)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")


def doc(num, doc_id, fields, published=True, tail=True):
    """Strapi v5 document envelope: id, documentId, fields, bookkeeping dates."""
    d = {"id": num, "documentId": doc_id}
    d.update(fields)
    if tail:
        d["createdAt"] = CREATED
        d["updatedAt"] = UPDATED
        d["publishedAt"] = PUBLISHED if published else None
        d["locale"] = None
    return d


def doc_id(prefix, n):
    return prefix + "%022d" % n


# ---------------------------------------------------------------------------
# rich text (Strapi "blocks")

def T(text, **marks):
    n = {"type": "text", "text": text}
    n.update(marks)
    return n


def P(*children):
    return {"type": "paragraph", "children": [T(c) if isinstance(c, str) else c for c in children]}


def H(level, *children):
    n = {"type": "heading", "children": [T(c) if isinstance(c, str) else c for c in children]}
    if level is not None:
        n["level"] = level
    return n


def LINK(url, *texts):
    return {"type": "link", "url": url, "children": [T(t) if isinstance(t, str) else t for t in texts]}


def LI(*children):
    return {"type": "list-item", "children": [T(c) if isinstance(c, str) else c for c in children]}


def LIST(fmt, *items):
    return {"type": "list", "format": fmt, "children": list(items)}


def QUOTE(*children):
    return {"type": "quote", "children": [T(c) if isinstance(c, str) else c for c in children]}


def kitchen_sink(title="Kitchen sink"):
    """Every node type / mark the Kotlin StrapiRichText understands (and some it does not)."""
    return [
        H(1, title),
        H(2, "Titre niveau 2"),
        H(3, "Titre niveau 3 avec ", LINK("https://www.agora.gouv.fr/titre", "un lien")),
        H(4, "Titre niveau 4"),
        H(5, "Titre niveau 5"),
        H(6, "Titre niveau 6"),
        H(7, "Titre de niveau invalide (7)"),
        H(None, "Titre sans niveau"),
        P(
            T("Texte simple, "),
            T("gras", bold=True), T(", "),
            T("italique", italic=True), T(", "),
            T("souligné", underline=True), T(", "),
            T("barré", strikethrough=True), T(", "),
            T("code", code=True), T(", "),
            T("tout à la fois", bold=True, italic=True, underline=True, strikethrough=True, code=True), T(", "),
            T("marques à false", bold=False, italic=False, underline=False, strikethrough=False, code=False),
            T("."),
        ),
        P(
            T("Un paragraphe avec "),
            LINK("https://www.agora.gouv.fr/page?a=1&b=2", "un lien ", T("en gras", bold=True), T(" et italique", italic=True)),
            T(" puis du texte accentué : éàüçœ — « guillemets » … 🙂 & Tom <3 \"quoted\" \\ backslash."),
        ),
        P(T("")),
        LIST("unordered", LI("Premier point"), LI("Deuxième point avec ", LINK("https://example.org", "lien")),
             LI(T("Troisième point en gras", bold=True))),
        LIST("ordered", LI("Étape un"), LI("Étape deux"), LIST("unordered", LI("Sous-point A"), LI("Sous-point B"))),
        QUOTE("« La participation citoyenne est un droit. »"),
        QUOTE(T("Citation en italique", italic=True), T(" avec "), LINK("https://example.org/q", "lien")),
        {"type": "code", "children": [T("fmt.Println(\"bloc inconnu\")")]},
        {"type": "image", "image": {"url": CDN + "/uploads/inline.png", "alternativeText": "image inline"},
         "children": [T("")]},
    ]


def simple(*paras):
    return [P(p) for p in paras]


def picture(num, name, medium=True, formats=True):
    pic = {
        "id": num,
        "documentId": doc_id("pc", num),
        "name": name + ".jpg",
        "alternativeText": None,
        "width": 1600,
        "height": 900,
        "url": "%s/uploads/%s.jpg" % (CDN, name),
        "mime": "image/jpeg",
        "ext": ".jpg",
        "size": 123.45,
        "createdAt": CREATED,
        "updatedAt": UPDATED,
        "publishedAt": PUBLISHED,
    }
    if formats and medium:
        pic["formats"] = {
            "thumbnail": {"name": "thumbnail_%s.jpg" % name, "url": "%s/uploads/thumbnail_%s.jpg" % (CDN, name), "width": 245, "height": 138},
            "medium": {"name": "medium_%s.jpg" % name, "url": "%s/uploads/medium_%s.jpg" % (CDN, name), "width": 750, "height": 422},
        }
    elif formats:
        pic["formats"] = {  # formats present but without a "medium" entry
            "thumbnail": {"name": "thumbnail_%s.jpg" % name, "url": "%s/uploads/thumbnail_%s.jpg" % (CDN, name), "width": 245, "height": 138},
        }
    else:
        pic["formats"] = None
    return pic


# ---------------------------------------------------------------------------
# thematiques

THEMATIQUES = [
    ("Environnement", "🌳"),
    ("Santé", "🏥"),
    ("Éducation", "📚"),
    ("Transports", "🚆"),
    ("Numérique", "💻"),
    ("Démocratie", "🗳️"),
]


def thematique_ref(n):
    """Populated relation as embedded in other models (documentId/label/pictogramme)."""
    label, picto = THEMATIQUES[n - 1]
    return {
        "id": n,
        "documentId": doc_id("th", n),
        "label": label,
        "pictogramme": picto,
        "createdAt": CREATED,
        "updatedAt": UPDATED,
        "publishedAt": PUBLISHED,
        "locale": None,
    }


def gen_thematiques():
    dump("thematiques.json", [
        doc(i + 1, doc_id("th", i + 1), {"label": l, "pictogramme": p}) for i, (l, p) in enumerate(THEMATIQUES)
    ])


# ---------------------------------------------------------------------------
# consultations

class Counter:
    def __init__(self, start):
        self.n = start

    def next(self):
        self.n += 1
        return self.n


def sec_titre(i, text):
    return {"id": i, "__component": "consultation-section.section-titre", "titre": text}


def sec_texte(i, rich):
    return {"id": i, "__component": "consultation-section.section-texte-riche", "description": rich}


def sec_citation(i, rich):
    return {"id": i, "__component": "consultation-section.section-citation", "description": rich}


def sec_image(i, url, descr, pic):
    return {"id": i, "__component": "consultation-section.section-image", "url": url,
            "description_accessible_de_l_image": descr, "image": pic}


def sec_video(i, url, w, h, auteur, poste, date, transcription, video):
    return {"id": i, "__component": "consultation-section.section-video", "url": url, "largeur": w, "hauteur": h,
            "nom_auteur": auteur, "poste_auteur": poste, "date_tournage": date, "transcription": transcription,
            "video": video}


def sec_chiffre(i, titre, rich):
    return {"id": i, "__component": "consultation-section.section-chiffre", "titre": titre, "description": rich}


def sec_accordeon(i, titre, rich):
    return {"id": i, "__component": "consultation-section.section-accordeon", "titre": titre, "description": rich}


def all_sections(c, count, with_kitchen_sink=False):
    """`count` sections cycling over the 7 section types (>= 8 enables bodyPreview)."""
    ids = Counter(c * 10000 + count * 7)
    builders = [
        lambda: sec_titre(ids.next(), "Un titre de section"),
        lambda: sec_texte(ids.next(), kitchen_sink("Texte riche") if with_kitchen_sink else simple("Un texte riche.", "Avec deux paragraphes.")),
        lambda: sec_citation(ids.next(), simple("« Une citation de section. »")),
        lambda: sec_image(ids.next(), CDN + "/sections/image-url.jpg", "Description accessible de l'image",
                          picture(c * 100 + 1, "section-image-%d" % c)),
        lambda: sec_chiffre(ids.next(), "42 %", simple("des participants se disent concernés.")),
        lambda: sec_accordeon(ids.next(), "Question fréquente ?", simple("Réponse de l'accordéon.", "Deuxième paragraphe.")),
        lambda: sec_video(ids.next(), CDN + "/sections/video-url.mp4", 1280, 720, "Camille Durand", "Directrice de projet",
                          "{{date:now-20d}}", "Transcription de la vidéo de la section.",
                          {"id": c * 100 + 2, "url": CDN + "/uploads/section-video-%d.mp4" % c, "mime": "video/mp4"}),
        # same types again, with their "fallback" variants (media relation absent)
        lambda: sec_image(ids.next(), CDN + "/sections/image-fallback.jpg", "Image sans média Strapi", None),
        lambda: sec_video(ids.next(), CDN + "/sections/video-fallback.mp4", 640, 360, "Alex Moreau", "Chargé de mission",
                          "2024-08-26", "Transcription (vidéo sans média Strapi).", None),
    ]
    return [builders[i % len(builders)]() for i in range(count)]


# per-consultation scenario ---------------------------------------------------
# start/end are templates; published False => only visible with status=draft
CONSULTATIONS = [
    dict(n=1, titre="Consultation 1 : mieux protéger l'environnement", start="{{now-10d}}", end="{{now+10d}}", published=True,
         territoire="France", th=1, pop=12000, qcount="5 questions", temps="8 minutes", cover="medium", content="medium"),
    dict(n=2, titre="Consultation 2 : l'accès aux soins dans le Nord", start="{{now-5d}}", end="{{now+25d}}", published=True,
         territoire="Nord", th=2, pop=8000, qcount="4 à 5 questions", temps="6 minutes", cover="nomedium", content="nomedium"),
    dict(n=3, titre="Consultation 3 : l'école de demain en Île-de-France", start="{{now-1d}}", end="{{now+3d}}", published=True,
         territoire="Ile-de-France", th=3, pop=5000, qcount="5 questions", temps="10 minutes", cover="none", content="none"),
    dict(n=4, titre="Consultation 4 : les mobilités du quotidien", start="{{now-40d}}", end="{{now-3d}}", published=True,
         territoire="France", th=4, pop=20000, qcount="5 questions", temps="7 minutes", cover="noformats", content="medium"),
    dict(n=5, titre="Consultation 5 : le numérique et la vie privée", start="{{now-60d}}", end="{{now-10d}}", published=True,
         territoire="Bretagne", th=5, pop=3000, qcount="5 questions", temps="9 minutes", cover="medium", content="none"),
    dict(n=6, titre="Consultation 6 : la démocratie locale (brouillon)", start="{{now-2d}}", end="{{now+20d}}", published=False,
         territoire="France", th=6, pop=1000, qcount="5 questions", temps="5 minutes", cover="medium", content="medium"),
    dict(n=7, titre="Consultation 7 : transition énergétique", start="{{now-90d}}", end="{{now-30d}}", published=True,
         territoire="France", th=1, pop=15000, qcount="5 questions", temps="12 minutes", cover="nomedium", content="medium"),
]


def cons_picture(c, kind, which):
    if kind == "none":
        return None
    name = "consultation-%d-%s" % (c, which)
    num = c * 100 + (10 if which == "cover" else 20)
    if kind == "medium":
        return picture(num, name)
    if kind == "nomedium":
        return picture(num, name, medium=False)
    return picture(num, name, formats=False)


def questions(c):
    base, cb = c * 100, c * 1000
    explication = [P("Cette question porte sur la consultation %d." % c), P(T("Précision importante.", bold=True))]
    choix = lambda q, labels, open_idx=(): [
        {"id": cb + q * 10 + k + 1, "label": lab, "ouvert": k in open_idx} for k, lab in enumerate(labels)]
    q1 = {"id": base + 1, "__component": "question-de-consultation.question-a-choix-unique",
          "titre": "Consultation %d - Q1 : quel est votre avis général ?" % c, "numero": 1,
          "popup_explication": explication, "question_suivante": None,
          "choix": choix(1, ["Plutôt favorable", "Plutôt défavorable", "Sans opinion"])}
    q2 = {"id": base + 2, "__component": "question-de-consultation.question-a-choix-multiples",
          "titre": "Consultation %d - Q2 : quelles priorités retenez-vous ?" % c, "numero": 2,
          "nombre_maximum_de_choix": 2, "popup_explication": None, "question_suivante": None,
          "choix": choix(2, ["Priorité A", "Priorité B", "Priorité C"])}
    q3 = {"id": base + 3, "__component": "question-de-consultation.question-ouverte",
          "titre": "Consultation %d - Q3 : que proposeriez-vous ?" % c, "numero": 3,
          "popup_explication": [P("Répondez librement, en quelques lignes.")] if c % 2 else None,
          "question_suivante": 4 if c % 2 == 0 else None}
    q4 = {"id": base + 4, "__component": "question-de-consultation.question-conditionnelle",
          "titre": "Consultation %d - Q4 : êtes-vous concerné(e) personnellement ?" % c, "numero": 4,
          "popup_explication": None,
          "choix": [
              {"id": cb + 41, "label": "Oui, directement", "ouvert": False, "numero_de_la_question_suivante": 5},
              {"id": cb + 42, "label": "Oui, indirectement", "ouvert": False, "numero_de_la_question_suivante": 6},
              {"id": cb + 43, "label": "Non", "ouvert": False, "numero_de_la_question_suivante": 6},
          ]}
    q5 = {"id": base + 5, "__component": "question-de-consultation.description",
          "titre": "Consultation %d - Chapitre : zoom sur le sujet" % c, "numero": 5,
          "url_image": CDN + "/questions/chapitre-%d.jpg" % c if c % 2 else None,
          "transcription_image": "Illustration du chapitre %d" % c if c % 2 else None,
          "description": kitchen_sink("Chapitre %d" % c) if c == 1 else simple("Description du chapitre de la consultation %d." % c, "Deuxième paragraphe."),
          "question_suivante": 6,
          "image": picture(base + 50, "question-chapitre-%d" % c, medium=(c % 3 != 0)) if c % 2 == 0 else None}
    q6 = {"id": base + 6, "__component": "question-de-consultation.question-a-choix-unique",
          "titre": "Consultation %d - Q6 : un dernier mot ?" % c, "numero": 6,
          "popup_explication": None, "question_suivante": 999,
          "choix": choix(6, ["Je suis satisfait(e)", "Je suis mitigé(e)", "Autre (précisez)"], open_idx=(2,))}
    return [q1, q2, q3, q4, q5, q6]


# The DB seed (parity/seed/ids.go) stores feedbacks for three consultation updates whose documentIds are
# cu0000000000000000000001..3: they are the "latest update" content of the three ongoing consultations.
SEEDED_UPDATE_IDS = {
    (1, 6): "cu0000000000000000000001",  # consultation 1, autre "actualite-2" (latest published autre)
    (2, 5): "cu0000000000000000000002",  # consultation 2, autre "actualite-1"
    (3, 2): "cu0000000000000000000003",  # consultation 3, apres reponse "fin-de-la-consultation"
}


def upd(c, kind):
    """documentId of a consultation update (kind: 1 avant, 2 apres, 3 analyse, 4 commanditaire, 5+ autres, 90 a venir)."""
    return SEEDED_UPDATE_IDS.get((c, kind)) or doc_id("up", c * 100 + kind)


def contenu_base(c, kind, slug, historique_titre, cta, extra, with_feedback=True):
    d = {"id": c * 100 + kind, "documentId": upd(c, kind), "slug": slug,
         "template_partage": "Comme moi, tu peux participer à la Consultation : {title} {url}",
         "historique_titre": historique_titre, "historique_call_to_action": cta,
         "nom_strapi": "Consultation %d - %s" % (c, slug)}
    d.update(extra)
    d["createdAt"] = CREATED
    d["updatedAt"] = UPDATED
    return d


def build_consultation(spec):
    c = spec["n"]
    many = 8 if c % 2 else 3  # >= 8 sections => bodyPreview is non-empty

    avant = contenu_base(c, 1, "lancement", "Lancement de la consultation", "Voir les objectifs", {
        "commanditaire": simple("Le Gouvernement"),
        "objectif": simple("Recueillir l'avis des citoyens sur le sujet n°%d." % c),
        "axe_gouvernemental": simple("Axe %d : une action publique plus proche des citoyens" % c),
        "presentation": (kitchen_sink("Pourquoi cette consultation ?") if c == 1 else
                         [P("Présentation de la consultation %d. Premier paragraphe." % c),
                          P("Deuxième paragraphe de présentation."), P("Troisième paragraphe.")]),
        "sections": all_sections(c, 8, with_kitchen_sink=(c == 1)),
    })

    apres = contenu_base(c, 2, "fin-de-la-consultation", "Fin de la consultation", "Consulter les résultats", {
        "feedback_message": "Êtes-vous satisfait(e) de cette consultation ?",
        "sections": all_sections(c, many),
    })

    # analyse des réponses ------------------------------------------------------
    analyse = None
    if c in (2, 4, 5, 7):
        pub = {2: "{{now+40d}}", 4: "{{now-2d}}", 5: "{{now-8d}}", 7: "{{now-25d}}"}[c]
        analyse = contenu_base(c, 3, "analyse-des-reponses", "Analyse des réponses", "Consulter la synthèse", {
            "lien_telechargement_analyse": CDN + "/syntheses/consultation-%d-synthese.pdf" % c,
            "datetime_publication": pub,
            "feedback_message": "Êtes-vous satisfait(e) de l'analyse de cette consultation ?",
            "flamme_label": "L'analyse est disponible" if c != 5 else None,
            "recap_emoji": "📊" if c in (4, 7) else None,
            "recap_label": "Synthèse des réponses" if c in (4, 7) else None,
            "sections": all_sections(c, many),
            "pdf_analyse": ({"id": c * 100 + 30, "url": CDN + "/uploads/consultation-%d-analyse.pdf" % c, "mime": "application/pdf"}
                            if c in (2, 7) else None),
        })

    # réponse du commanditaire ---------------------------------------------------
    commanditaire = None
    if c in (1, 4, 7):
        pub = {1: "{{now+30d}}", 4: "{{now-1d}}", 7: "{{now-20d}}"}[c]
        commanditaire = contenu_base(c, 4, "reponse-du-gouvernement", "Réponse du Gouvernement", "Actions mises en place", {
            "datetime_publication": pub,
            "feedback_message": "Êtes-vous satisfait(e) de la réponse du Gouvernement ?",
            "flamme_label": "Le Gouvernement a répondu",
            "recap_emoji": "🏛️" if c != 1 else None,
            "recap_label": "Réponse du commanditaire" if c != 1 else None,
            "sections": all_sections(c, many),
        })

    # autres contenus --------------------------------------------------------------
    autres_spec = {
        1: [("{{now-5d}}", "Point d'étape", "actualite-1", "Trop bien", "🚀", "Premier point d'étape"),
            ("{{now-2d}}", "Nouvelle actualité", "actualite-2", None, None, None),
            ("{{now+3d}}", "Actualité à venir", "actualite-3", "Bientôt", "⏳", "À venir")],
        2: [("{{now-1d}}", "Lancement dans le Nord", "actualite-1", "Venez répondre !", "📣", "Actualité")],
        4: [("{{now-10d}}", "Bilan intermédiaire", "actualite-1", "Merci !", "🙏", "Bilan")],
        6: [("{{now-1d}}", "Actualité du brouillon", "actualite-1", None, None, None)],
        7: [("{{now-60d}}", "Webinaire de présentation", "actualite-1", "Replay disponible", "🎥", "Webinaire"),
            ("{{now-40d}}", "Réunion publique", "actualite-2", None, "📅", "Réunion")],
    }.get(c, [])
    autres = []
    for i, (pub, titre, slug, flamme, emoji, label) in enumerate(autres_spec):
        autres.append(contenu_base(c, 5 + i, slug, titre, "En savoir plus", {
            "feedback_message": "Êtes-vous satisfait(e) de cette actualité ?",
            "datetime_publication": pub,
            "flamme_label": flamme,
            "recap_emoji": emoji,
            "recap_label": label,
            "sections": all_sections(c + i, 8 if i == 0 else 4),
        }))

    a_venir = None
    if c in (1, 2, 5):
        a_venir = {"id": c * 100 + 90, "documentId": upd(c, 90), "titre_historique": "Résultats à venir",
                   "createdAt": CREATED, "updatedAt": UPDATED}

    d = {
        "titre_consultation": spec["titre"],
        "slug": "consultation-%d" % c,
        "datetime_de_debut": spec["start"],
        "datetime_de_fin": spec["end"],
        "url_image_de_couverture": "%s/consultation_covers/consultation-%d.jpg" % (CDN, c),
        "url_image_page_de_contenu": "%s/consultation_covers/consultation-%d-contenu.jpg" % (CDN, c),
        "nombre_de_questions": 5,
        "estimation_nombre_de_questions": spec["qcount"],
        "estimation_temps": spec["temps"],
        "nombre_participants_cible": spec["pop"],
        "territoire": spec["territoire"],
        "titre_page_web": "Consultation %d - Grande consultation" % c,
        "sous_titre_page_web": "par le Gouvernement",
        "thematique": thematique_ref(spec["th"]),
        "questions": questions(c),
        "consultation_avant_reponse": avant,
        "consultation_apres_reponse_ou_terminee": apres,
        "consultation_contenu_analyse_des_reponse": analyse,
        "contenu_reponse_du_commanditaires": commanditaire,
        "consultation_contenu_autres": autres,
        "consultation_contenu_a_venir": a_venir,
        "image_de_couverture": cons_picture(c, spec["cover"], "cover"),
        "image_page_de_contenu": cons_picture(c, spec["content"], "contenu"),
    }
    return doc(c, doc_id("co", c), d, published=spec["published"])


def gen_consultations():
    dump("consultations.json", [build_consultation(s) for s in CONSULTATIONS])


# ---------------------------------------------------------------------------
# other collection types

def gen_theme_hebdos():
    def th(num, theme, debut, fin, periode=None, photo=None, nom=None, fonction=None, libre=False):
        return doc(num, doc_id("he", num), {
            "theme": theme, "periode": periode, "photo": photo, "nom_ministre": nom, "fonction": fonction,
            "date_debut": debut, "date_fin": fin, "est_theme_libre": libre})

    items = [
        # past
        th(1, "Justice et sécurité", "{{now-16d}}", "{{now-9d}}", periode="Semaine du 18 au 24 septembre",
           photo=picture(901, "ministre-justice"), nom="Claire Lambert", fonction="Ministre de la Justice"),
        th(2, "Semaine libre d'été", "{{now-30d}}", "{{now-23d}}", periode="Semaine libre", libre=True),
        # current week: no explicit periode => computed from the dates
        th(3, "Transition écologique", "{{now-2d}}", "{{now+5d}}", periode=None,
           photo=picture(902, "ministre-ecologie"), nom="Paul Renaud", fonction="Ministre de la Transition écologique"),
        # future (deliberately not in chronological order)
        th(5, "Semaine libre", "{{now+12d+5m}}", "{{now+19d}}", periode="Semaine libre", libre=True),
        th(4, "Santé et solidarités", "{{now+5d+5m}}", "{{now+12d}}", periode=None,
           photo=picture(903, "ministre-sante", medium=False), nom="Hélène Girard", fonction="Ministre de la Santé"),
        th(7, "Culture et patrimoine", "{{now+26d+5m}}", "{{now+33d}}", periode="Fin octobre",
           photo=None, nom="Marc Petit", fonction="Ministre de la Culture"),
        th(6, "Éducation nationale", "{{now+19d+5m}}", "{{now+26d}}", periode=None,
           photo=picture(904, "ministre-education", formats=False), nom="Sophie Leroy", fonction="Ministre de l'Éducation nationale"),
    ]
    dump("theme-hebdos.json", items)

    # variant: the CURRENT week is a "thème libre" (cluster filtering of trending QaGs applies)
    libre = [dict(i) for i in items]
    libre[2] = th(3, "Semaine libre", "{{now-2d}}", "{{now+5d}}", periode="Semaine libre", libre=True)
    dump("theme-hebdos.libre-current.json", libre, subdir="variants")


def gen_concertations():
    def ce(n, titre, th, pub, flamme, image, image_url=None):
        return doc(n, doc_id("ce", n), {
            "titre": titre,
            "url": "https://concertation.parity.example/%d" % n,
            "image_url": image_url or "%s/concertations/concertation-%d.jpg" % (CDN, n),
            "datetime_publication": pub,
            "flamme_label": flamme,
            "thematique": thematique_ref(th) if th else {
                "id": 99, "documentId": doc_id("th", 9999), "label": "Thématique inconnue", "pictogramme": "❓",
                "createdAt": CREATED, "updatedAt": UPDATED, "publishedAt": PUBLISHED},
            "image": image,
        })

    dump("concertations.json", [
        ce(1, "Concertation sur la rénovation énergétique", 1, "{{now-3d}}", "Nouveau", picture(801, "concertation-1")),
        ce(2, "Concertation sur l'hôpital public", 2, "{{now-10d}}", None, picture(802, "concertation-2", medium=False)),
        ce(3, "Concertation sur les rythmes scolaires", 3, "{{now-10d}}", "Dernière ligne droite", None),  # same date as #2 (stable sort)
        ce(4, "Concertation sur les transports ruraux", 4, "{{now-30d}}", None, picture(804, "concertation-4", formats=False)),
        ce(5, "Concertation dont la thématique est inconnue", None, "{{now-1d}}", None, None),  # dropped by the mapper
    ])


def gen_charte():
    dump("charte-participations.json", [
        doc(1, doc_id("ch", 1), {
            "charte": simple("Ancienne charte de participation.", "Version 1."),
            "charte_preview": simple("Ancienne charte (aperçu)."),
            "datetime_debut": "{{now-200d}}"}),
        doc(3, doc_id("ch", 3), {   # future version: must not be returned (datetime_debut < now)
            "charte": simple("Charte à venir."),
            "charte_preview": simple("Charte à venir (aperçu)."),
            "datetime_debut": "{{now+30d}}"}),
        doc(2, doc_id("ch", 2), {   # current version
            "charte": kitchen_sink("Charte de participation"),
            "charte_preview": [P("Charte de participation : ", T("aperçu", bold=True), T(" de la version en vigueur."))],
            "datetime_debut": "{{now-30d}}"}),
    ])


def gen_news():
    dump("welcome-page-news.json", [
        doc(1, doc_id("ne", 1), {
            "message": simple("Bienvenue sur Agora, première version."),
            "short_message": "Bienvenue", "call_to_action": "Découvrir",
            "date_de_debut": "{{now-60d}}", "page_route_mobile_enum": "qag", "page_route_argument_mobile": None}),
        doc(2, doc_id("ne", 2), {
            "message": [P("Participez à la consultation ", T("n°1", bold=True), T(" dès maintenant !")),
                        LIST("unordered", LI("Donnez votre avis"), LI("Suivez les résultats"))],
            "short_message": "Nouvelle consultation", "call_to_action": "Participer",
            "date_de_debut": "{{now-3d}}", "page_route_mobile_enum": "consultation",
            "page_route_argument_mobile": doc_id("co", 1)}),
        doc(3, doc_id("ne", 3), {
            "message": simple("Annonce programmée dans le futur (ne doit pas être renvoyée)."),
            "short_message": "À venir", "call_to_action": "Bientôt",
            "date_de_debut": "{{now+5d}}", "page_route_mobile_enum": "themeHebdo", "page_route_argument_mobile": None}),
    ])


def gen_fiches():
    def fiche(n, titre, th, etape, cond, modalite, annee, debut, fin, type_, pic):
        return doc(n, doc_id("fi", n), {
            "etape_1_lancement": kitchen_sink("Lancement") if n == 1 else simple("Lancement de « %s »." % titre, "Détails du lancement."),
            "etape_2_analyse": simple("Analyse de « %s »." % titre),
            "etape_3_suivi": [P("Suivi : ", LINK("https://suivi.parity.example/%d" % n, "tableau de bord")),
                              LIST("ordered", LI("Action 1"), LI("Action 2"))],
            "titre": titre,
            "debut": debut,
            "fin": fin,
            "porteur": "Ministère n°%d" % n,
            "lien_site": "https://fiche.parity.example/%d" % n,
            "condition_participation": cond,
            "modalite_participation": modalite,
            "thematique": thematique_ref(th),
            "illustration": pic,
            "etape": etape,
            "annee_de_lancement": annee,
            "type": type_,
        })

    dump("fiche-inventaires.json", [
        fiche(1, "Grande consultation sur l'environnement", 1, "Lancement", "Ouvert à tous", "En ligne", "2024",
              "2024-03-01", "2024-06-30", "Consultation", picture(701, "fiche-1")),
        fiche(2, "Convention citoyenne sur la santé", 2, "Analyse", "Sur inscription", "En présentiel", "2023",
              "2023-06-15", "2023-12-15", "Convention citoyenne", picture(702, "fiche-2", medium=False)),
        fiche(3, "Débat public sur les transports", 4, "Suivi", "Ouvert à tous", "Hybride", "2024",
              "2024-09-10", "{{date:now+30d}}", "Débat public", picture(703, "fiche-3", formats=False)),
        fiche(4, "Consultation sur l'école numérique", 3, "Lancement", "Tirage au sort", "En ligne", "2025",
              "2025-01-20", "2025-04-20", "Consultation", picture(704, "fiche-4")),
        fiche(5, "Débat national sur l'énergie", 1, "Analyse", "Sur inscription", "En ligne", "2022",
              "2022-02-01", "2022-05-01", "Débat public", picture(705, "fiche-5")),
    ])


def gen_reponses():
    qid = lambda n: "00000000-0000-4000-8000-0000000000a%d" % n
    dump("reponse-du-gouvernements.json", [
        doc(1, doc_id("re", 1), {
            "auteur": "Marie Dupont",
            "auteurPortraitUrl": CDN + "/portraits/marie-dupont.jpg",
            "auteurFonction": None,
            "reponseDate": "{{date:now-2d}}",
            "feedbackQuestion": "Cette réponse vous a-t-elle été utile ?",
            "questionId": qid(1),
            "reponseType": [{
                "id": 11, "__component": "reponse.reponse-video",
                "auteurDescription": "Ministre de la Transition écologique",
                "urlVideo": CDN + "/reponses/reponse-1.mp4",
                "videoWidth": 1920, "videoHeight": 1080,
                "transcription": "Transcription de la réponse vidéo n°1.",
                "informationAdditionnelleTitre": None,
                "page_title": "Réponse du ministre sur la transition écologique",
                "informationAdditionnelleDescription": None,
                "video": {"id": 1101, "url": CDN + "/uploads/reponse-1.mp4", "mime": "video/mp4"},
            }],
            "auteurPortrait": picture(601, "portrait-marie-dupont"),
        }),
        doc(2, doc_id("re", 2), {
            "auteur": "Jean Martin",
            "auteurPortraitUrl": CDN + "/portraits/jean-martin.jpg",
            "auteurFonction": "Ministre de la Santé",
            "reponseDate": "{{date:now-6d}}",
            "feedbackQuestion": "Êtes-vous satisfait(e) de cette réponse ?",
            "questionId": qid(2),
            "reponseType": [{
                "id": 12, "__component": "reponse.reponsetextuelle",
                "label": "Réponse du ministre de la Santé",
                "text": kitchen_sink("Réponse écrite"),
            }],
            "auteurPortrait": None,
        }),
        doc(3, doc_id("re", 3), {
            "auteur": "Sophie Leroy",
            "auteurPortraitUrl": CDN + "/portraits/sophie-leroy.jpg",
            "auteurFonction": "Ministre de l'Éducation nationale",
            "reponseDate": "{{date:now-12d}}",
            "feedbackQuestion": "Cette réponse répond-elle à votre question ?",
            "questionId": qid(3),
            "reponseType": [{
                "id": 13, "__component": "reponse.reponse-video",
                "auteurDescription": "Ministre de l'Éducation nationale (description vidéo)",
                "urlVideo": CDN + "/reponses/reponse-3.mp4",
                "videoWidth": 1280, "videoHeight": 720,
                "transcription": "Transcription de la réponse vidéo n°3.",
                "informationAdditionnelleTitre": "Pour aller plus loin",
                "page_title": "Réponse sur l'école de demain",
                "informationAdditionnelleDescription": [P("Plus d'informations sur le ", LINK("https://education.parity.example", "site du ministère"), T(".")),
                                                         LIST("unordered", LI("Dossier de presse"), LI("Foire aux questions"))],
                "video": None,
            }],
            "auteurPortrait": picture(603, "portrait-sophie-leroy", medium=False),
        }),
    ])


def gen_clusters():
    dump("cluster-semaine-libres.json", [
        doc(1, doc_id("cl", 1), {"titre": "cluster-ecologie", "keywords": "climat, écologie, environnement,  énergie "}),
        doc(2, doc_id("cl", 2), {"titre": "cluster-sante", "keywords": "santé,hôpital, médecin, soins"}),
        doc(3, doc_id("cl", 3), {"titre": "cluster-logement", "keywords": "logement, loyer, HLM"}),
        doc(4, doc_id("cl", 4), {"titre": "cluster-vide", "keywords": ""}),       # ignored (warning)
        doc(5, doc_id("cl", 5), {"titre": "cluster-sans-mots", "keywords": None}),  # ignored (warning)
    ])


def gen_headers():
    def hd(n, type_, titre, message, pub):
        return doc(n, doc_id("qh", n), {"titre": titre, "message": message, "type": type_, "datetime_publication": pub})

    items = []
    n = 0
    for type_, label in (("top", "Les plus soutenues"), ("latest", "Les plus récentes"),
                         ("supporting", "Mes soutiens"), ("trending", "Tendances")):
        n += 1
        items.append(hd(n, type_, "%s (ancien)" % label, "Ancien message pour l'onglet %s." % type_, "{{now-90d}}"))
        n += 1
        items.append(hd(n, type_, label, "Message courant pour l'onglet %s." % type_, "{{now-7d}}"))
        n += 1
        items.append(hd(n, type_, "%s (à venir)" % label, "Message futur pour l'onglet %s." % type_, "{{now+7d}}"))
    dump("qa-g-headers-onglets.json", items)


# ---------------------------------------------------------------------------
# single types

def single(num, prefix, fields):
    return doc(num, doc_id(prefix, num), fields)


def gen_singles():
    dump("page-reponse-aux-questions-au-gouvernement.json", single(1, "pg", {
        "information_reponse_a_venir_bottomsheet":
            "Les réponses du Gouvernement aux questions les plus soutenues arrivent bientôt."}))
    dump("page-poser-ma-question.json", single(2, "pg", {"texte_regles": kitchen_sink("Règles pour poser ma question")}))
    dump("page-questions-au-gouvernement.json", single(3, "pg", {
        "information_bottomsheet": "Les questions au Gouvernement vous permettent d'interpeller les ministres.",
        "nombre_de_questions": "{} questions posées par les citoyens",
        "programme_du_mois": [H(2, "Programme du mois"), LIST("unordered", LI("Semaine 1 : écologie"), LI("Semaine 2 : santé"))],
        "comment_ca_marche": "Posez votre question, soutenez celles des autres, le Gouvernement répond aux plus soutenues.",
    }))
    dump("site-vitrine-accueil.json", single(4, "pg", {
        "titre_header": "Agora, la plateforme de participation citoyenne",
        "sous_titre_header": "Donnez votre avis, posez vos questions",
        "titre_body": "Comment ça marche ?",
        "description_body": "Participez aux consultations et interpellez le Gouvernement.",
        "texte_image_1": simple("Répondez aux consultations."),
        "texte_image_2": [P("Posez vos ", T("questions", bold=True), T(" au Gouvernement."))],
        "texte_image_3": simple("Suivez les réponses des ministres."),
    }))
    dump("site-vitrine-conditions-generales-d-utilisation.json", single(5, "pg", {
        "conditions_generales_d_utilisation": kitchen_sink("Conditions générales d'utilisation")}))
    dump("site-vitrine-consultation.json", single(6, "pg", {
        "donnez_votre_avis": [H(2, "Donnez votre avis"), P("Participez aux consultations en cours.")]}))
    dump("site-vitrine-declaration-d-accessibilite.json", single(7, "pg", {
        "declaration": [H(1, "Déclaration d'accessibilité"), P("Ce site est partiellement conforme au RGAA."),
                        LIST("ordered", LI("Contenus non accessibles"), LI("Voies de recours"))]}))
    dump("site-vitrine-mentions-legale.json", single(8, "pg", {"mentions_legales": kitchen_sink("Mentions légales")}))
    dump("site-vitrine-politique-de-confidentialite.json", single(9, "pg", {
        "politique_de_confidentialite": [H(1, "Politique de confidentialité"),
                                         P("Nous collectons le minimum de données. ", LINK("https://www.cnil.fr", "CNIL"))]}))
    dump("site-vitrine-question-au-gouvernement.json", single(10, "pg", {
        "titre": "Questions au Gouvernement",
        "sous_titre": "Posez votre question au Gouvernement",
        "texte_soutien": [P("Soutenez les questions qui comptent pour vous. ", T("Chaque soutien compte.", bold=True))]}))


def main():
    gen_thematiques()
    gen_consultations()
    gen_theme_hebdos()
    gen_concertations()
    gen_charte()
    gen_news()
    gen_fiches()
    gen_reponses()
    gen_clusters()
    gen_headers()
    gen_singles()
    print("fixtures written to", OUT)


if __name__ == "__main__":
    sys.exit(main())
