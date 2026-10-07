from common import *

# (route suffix, strapi model, [(field, kind)]) kind: s = String, r = List<StrapiRichText>, sn = String?, rn = List<StrapiRichText>?
PAGES = [
    ("page-questions-au-gouvernement", "page-questions-au-gouvernement",
     [("information_bottomsheet", "s"), ("nombre_de_questions", "s"), ("programme_du_mois", "rn"), ("comment_ca_marche", "sn")]),
    ("page-reponses-aux-qags", "page-reponse-aux-questions-au-gouvernement",
     [("information_reponse_a_venir_bottomsheet", "s")]),
    ("page-poser-ma-question", "page-poser-ma-question", [("texte_regles", "r")]),
    ("page-site-vitrine-accueil", "site-vitrine-accueil",
     [("titre_header", "s"), ("sous_titre_header", "s"), ("titre_body", "s"), ("description_body", "s"),
      ("texte_image_1", "r"), ("texte_image_2", "r"), ("texte_image_3", "r")]),
    ("page-site-vitrine-conditions-generales", "site-vitrine-conditions-generales-d-utilisation",
     [("conditions_generales_d_utilisation", "r")]),
    ("page-site-vitrine-consultation", "site-vitrine-consultation", [("donnez_votre_avis", "r")]),
    ("page-site-vitrine-declaration-accessibilite", "site-vitrine-declaration-d-accessibilite", [("declaration", "r")]),
    ("page-site-vitrine-mentions-legales", "site-vitrine-mentions-legale", [("mentions_legales", "r")]),
    ("page-site-vitrine-politique-confidentialite", "site-vitrine-politique-de-confidentialite",
     [("politique_de_confidentialite", "r")]),
    ("page-site-vitrine-question-au-gouvernement", "site-vitrine-question-au-gouvernement",
     [("titre", "s"), ("sous_titre", "s"), ("texte_soutien", "r")]),
]

MISSING = object()

FULL = {("page-poser-ma-question", "texte_regles"), ("page-questions-au-gouvernement", "programme_du_mois"),
        ("page-site-vitrine-accueil", "texte_image_1"), ("page-site-vitrine-question-au-gouvernement", "titre"),
        ("page-site-vitrine-accueil", "titre_header"), ("page-questions-au-gouvernement", "comment_ca_marche")}

FULL = {("page-poser-ma-question", "texte_regles"), ("page-questions-au-gouvernement", "programme_du_mois"),
        ("page-site-vitrine-accueil", "texte_image_1"), ("page-site-vitrine-question-au-gouvernement", "titre"),
        ("page-site-vitrine-accueil", "titre_header"), ("page-questions-au-gouvernement", "comment_ca_marche")}


def valid(kind):
    if kind in ("s", "sn"):
        return "Texte valide é & <b> \"q\""
    return [para(text("Un "), text("gras", bold=True), text(" & <i>"))]


def page_doc(fields, **over):
    d = {"documentId": "pg0000000000000000000099"}
    for f, k in fields:
        d[f] = valid(k)
    for k, v in over.items():
        if v is MISSING:
            d.pop(k, None)
        else:
            d[k] = v
    return d


def run():
    out = []
    for route, model, fields in PAGES:
        path = "/content/" + route
        tag = route.replace("page-", "")
        # 1. HTTP matrix
        out.append(scenario("s9-content-%s-basic" % tag, http_matrix(path)))
        # 2. Strapi faults
        for mode, delay in [("500", None), ("malformed", None), ("nulldata", None), ("missingfield", None), ("slow", 600)]:
            out.append(scenario("s9-content-%s-fault-%s" % (tag, mode), fault_steps(path), setup=faults(model, mode, delay)))
        # 3. field variants
        steps_basic = [step("json", path), step("xml", path + "?mediaType=xml"), step("head", path, method="HEAD")]
        for f, k in fields:
            variants = [("missing", MISSING), ("null", None)]
            full = (route, f) in FULL
            if k in ("s", "sn"):
                variants += [("number", 5), ("empty", ""), ("array", [])]
                if full:
                    variants += [("float", 1.5), ("true", True), ("object", {}),
                                 ("long", "x" * 3000), ("unicode", "é😀\u2028</p>&amp;\\\"")]
            elif not full:
                variants += [("string", "texte"), ("empty-list", []), ("null-element", [None])]
            else:
                variants += [("string", "texte"), ("number", 5), ("object", {}), ("empty-list", []),
                             ("null-element", [None]),
                             ("paragraph-no-children", [{"type": "paragraph"}]),
                             ("text-no-text", [{"type": "text"}]),
                             ("text-number", [{"type": "text", "text": 5, "bold": "yes"}]),
                             ("unknown-type", [{"type": "image", "children": [text("in image")]}]),
                             ("no-type", [{"children": [text("sans type")]}]),
                             ("trailing-br", [para(text("fin")), {"type": "paragraph", "children": [text("<br/>")]}]),
                             ("link", [para({"type": "link", "url": "https://x?a=1&b=2\"", "children": [text("l", italic=True)]})]),
                             ("heading-7", [{"type": "heading", "level": 7, "children": [text("h7")]}]),
                             ("heading-null-level", [{"type": "heading", "level": None, "children": [text("hn")]}]),
                             ("lists", [{"type": "list", "format": "ordered", "children": [{"type": "list-item", "children": [text("a")]}]},
                                        {"type": "list", "format": "unordered", "children": []},
                                        {"type": "list", "children": [{"type": "list-item", "children": [text("b")]}]}]),
                             ("quote", [{"type": "quote", "children": [text("q", underline=True, strikethrough=True, code=True)]}])]
            for vname, v in variants:
                kw = {f: v}
                out.append(scenario("s9-content-%s-%s-%s" % (tag, f.replace("_", "-"), vname),
                                    steps_basic, setup=override(model, page_doc(fields, **kw))))
        # 4. empty object / extra fields / created dates
        out.append(scenario("s9-content-%s-empty-object" % tag, steps_basic,
                            setup=override(model, {"documentId": "x"})))
        extra = page_doc(fields)
        extra.update({"createdAt": "2024-01-01T00:00:00.000Z", "updatedAt": "x", "publishedAt": "y", "unknown": {"a": 1}, "locale": "fr"})
        out.append(scenario("s9-content-%s-extra-fields" % tag, steps_basic, setup=override(model, extra)))
        # other model fault changes nothing
        out.append(scenario("s9-content-%s-other-model-fault" % tag, [step("json", path)],
                            setup=faults("thematiques", "500")))
    # all-model fault
    for route, model, fields in PAGES:
        pass
    out.append(scenario("s9-content-parallel", [step(r, "/content/" + r, parallel=16) for r, _, _ in PAGES] +
                        [step(r + "-after", "/content/" + r) for r, _, _ in PAGES]))
    out.append(scenario("s9-content-all-models-fault", [step("json", "/content/" + r) for r, _, _ in PAGES],
                        setup=faults("*", "500")))
    out.append(scenario("s9-content-all-pages-sequence",
                        [step(r, "/content/" + r) for r, _, _ in PAGES] +
                        [step(r + "-again", "/content/" + r) for r, _, _ in PAGES]))
    # unknown content pages
    out.append(scenario("s9-content-unknown", [
        step("root", "/content"), step("root-slash", "/content/"), step("unknown", "/content/unknown"),
        step("unknown-xml", "/content/unknown?mediaType=xml", **{"as": U1}),
        step("old-name", "/content/page-questions-au-gouvernements"),
        step("case", "/content/Page-Poser-Ma-Question"),
        step("ext", "/content/page-poser-ma-question.json"),
        step("ext-xml", "/content/page-poser-ma-question.xml"),
        step("matrix", "/content/page-poser-ma-question;a=b"),
        step("encoded", "/content/page%2Dposer-ma-question"),
        step("double-slash", "/content//page-poser-ma-question"),
        step("options-unknown", "/content/unknown", method="OPTIONS"),
        step("post-unknown", "/content/unknown", method="POST"),
    ]))
    # page-questions-au-gouvernement: placeholder and counts
    p = "/content/page-questions-au-gouvernement"
    f = PAGES[0][2]
    st = [step("json", p), step("xml", p + "?mediaType=xml"), step("user", p, **{"as": U1})]
    for name, v in [("three", "a {} b {} c {}"), ("none", "pas de marqueur"), ("only", "{}"), ("braces-space", "{ } {{}} {x}"),
                    ("regex-like", "$1 \\1 {} (.*) $"), ("empty", ""), ("unicode", "é😀 {} 😀")]:
        out.append(scenario("s9-content-qag-placeholder-%s" % name, st,
                            setup=override("page-questions-au-gouvernement", page_doc(f, nombre_de_questions=v))))
    out.append(scenario("s9-content-qag-count-zero", st,
                        setup={**override("page-questions-au-gouvernement", page_doc(f, nombre_de_questions="{} questions")),
                               "sql": ["UPDATE qags SET status = 0 WHERE status = 1"]}))
    out.append(scenario("s9-content-qag-count-many", st,
                        setup={"sql": ["UPDATE qags SET status = 1 WHERE status = 0"]}))
    out.append(scenario("s9-content-qag-count-no-qags", st,
                        setup={"sql": ["DELETE FROM qag_updates", "DELETE FROM supports_qag", "DELETE FROM feedbacks_qag",
                                       "DELETE FROM moderatus_locked_qags", "DELETE FROM low_priority_qags", "DELETE FROM qags"]}))
    # known foundation diff: control character in an XML response (see ledger "Foundation findings")
    out.append(scenario("s9-known-content-xml-control-char", [
        step("json", "/content/page-site-vitrine-question-au-gouvernement"),
        step("xml", "/content/page-site-vitrine-question-au-gouvernement?mediaType=xml"),
    ], tags=("S9-known",), setup=override("site-vitrine-question-au-gouvernement",
                                           {"documentId": "x", "titre": "a\u0000b", "sous_titre": "s", "texte_soutien": []})))
    return out


if __name__ == "__main__":
    import sys
    dump(run(), sys.argv[1])
