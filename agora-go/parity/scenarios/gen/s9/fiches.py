from common import *

L = "/fiches_inventaire"
MODEL = "fiche-inventaires"
TH = lambda n: "th00000000000000000000%02d" % n
FI = lambda n: "fi00000000000000000000%02d" % n


def fiche(doc="fi0000000000000000000099", **kw):
    d = {
        "id": 99, "documentId": doc,
        "etape_1_lancement": [para(text("Lancement "), text(doc, bold=True))],
        "etape_2_analyse": [para(text("Analyse"))],
        "etape_3_suivi": [para(text("Suivi"))],
        "titre": "Titre " + doc, "debut": "2024-03-01", "fin": "2024-06-30", "porteur": "Porteur", "lien_site": "https://x.example/f",
        "condition_participation": "Ouvert à tous", "modalite_participation": "En ligne",
        "thematique": {"id": 1, "documentId": TH(1), "label": "Environnement", "pictogramme": "🌳",
                       "createdAt": "2024-05-29T09:37:12.407Z", "publishedAt": "2024-05-29T10:19:13.352Z"},
        "illustration": {"id": 1, "url": "https://cdn.example/i.jpg", "formats": {"medium": {"url": "https://cdn.example/m.jpg"}}},
        "etape": "Lancement", "annee_de_lancement": "2024", "type": "Consultation",
        "publishedAt": "2024-05-29T10:19:13.352Z",
    }
    for k, v in kw.items():
        if v is ...:
            d.pop(k, None)
        else:
            d[k] = v
    return d


def run():
    out = []
    # --- HTTP matrix of both routes
    out.append(scenario("s9-fiches-list-basic", http_matrix(L)))
    out.append(scenario("s9-fiches-detail-basic", http_matrix(L + "/" + FI(1))))
    out.append(scenario("s9-fiches-detail-unknown-basic", http_matrix(L + "/unknown")))

    # --- filters
    queries = [
        "", "?", "?titre=consultation", "?titre=CONSULTATION", "?titre=%C3%A9cole", "?titre=%C3%89COLE", "?titre=D%C3%A9bat",
        "?titre=", "?titre=%20", "?titre=%20%20", "?titre=%09", "?titre=%C2%A0", "?titre=%20consultation", "?titre=consultation%20",
        "?titre=a&titre=b", "?titre=d%C3%A9bat&titre=public", "?titre=null", "?titre=%ZZ", "?titre=%", "?titre=a%00b",
        "?titre=a+b", "?titre=a%2Bb", "?titre=%F0%9F%98%80", "?titre=%27", "?titre=%22", "?titre=%26%3D%3F%23",
        "?titre=100%25", "?titre=_%25", "?titre=n%C2%B0", "?titre=unknown-title",
        "?thematique=" + TH(1), "?thematique=" + TH(2), "?thematique=" + TH(3), "?thematique=" + TH(1) + "," + TH(2),
        "?thematique=" + TH(1) + "&thematique=" + TH(2), "?thematique=unknown", "?thematique=", "?thematique=%20", "?thematique=" + TH(1) + "%20",
        "?thematique=null", "?thematique=%20" + TH(1),
        "?etape=Lancement", "?etape=Analyse", "?etape=Suivi", "?etape=Lancement,Analyse", "?etape=Lancement&etape=Analyse",
        "?etape=Lancement,Analyse&etape=Suivi", "?etape=Suivi,Analyse", "?etape=Analyse,Suivi", "?etape=Analyse&etape=Suivi",
        "?etape=Suivi&etape=Analyse", "?etape=Lancement,&etape=", "?etape=%20Lancement%20", "?etape=a,,b", "?etape=", "?etape=,", "?etape=,,,",
        "?etape=%20", "?etape=%20,%20", "?etape=%C2%A0Lancement", "?etape=%C2%85Lancement", "?etape=Lancement%2CAnalyse", "?etape=Lancement%20,%20Analyse",
        "?etape=&etape=Lancement", "?etape=Lancement&etape=", "?etape=unknown", "?etape=null", "?etape=lancement", "?etape=a%00",
        "?etape=" + ",".join("e%d" % i for i in range(120)),
        "?etape=%F0%9F%98%80,%EF%BD%9E", "?etape=%EF%BD%9E,%F0%9F%98%80", "?etape=%C3%A9,e,z,%C3%89,E",
        "?conditionParticipation=Ouvert%20%C3%A0%20tous", "?conditionParticipation=Sur%20inscription", "?conditionParticipation=Tirage%20au%20sort",
        "?conditionParticipation=Ouvert%20%C3%A0%20tous,Sur%20inscription", "?conditionParticipation=Ouvert%20%C3%A0%20tous&conditionParticipation=Tirage%20au%20sort",
        "?conditionParticipation=", "?conditionParticipation=,", "?conditionParticipation=unknown", "?conditionParticipation=%20x%20",
        "?modaliteParticipation=En%20ligne", "?modaliteParticipation=En%20pr%C3%A9sentiel", "?modaliteParticipation=Hybride",
        "?modaliteParticipation=En%20ligne,Hybride", "?modaliteParticipation=En%20ligne&modaliteParticipation=En%20pr%C3%A9sentiel",
        "?modaliteParticipation=", "?modaliteParticipation=unknown",
        "?anneeDeLancement=2024", "?anneeDeLancement=2023", "?anneeDeLancement=2025", "?anneeDeLancement=2022",
        "?anneeDeLancement=2024,2023", "?anneeDeLancement=2024&anneeDeLancement=2023", "?anneeDeLancement=", "?anneeDeLancement=%20",
        "?anneeDeLancement=abc", "?anneeDeLancement=%202024%20", "?anneeDeLancement=null",
        # combinations
        "?titre=consultation&etape=Lancement", "?titre=d%C3%A9bat&anneeDeLancement=2024", "?thematique=" + TH(1) + "&etape=Analyse",
        "?thematique=" + TH(1) + "&anneeDeLancement=2022&etape=Analyse&modaliteParticipation=En%20ligne&conditionParticipation=Sur%20inscription&titre=national",
        "?etape=Lancement,Analyse&conditionParticipation=Ouvert%20%C3%A0%20tous,Sur%20inscription&modaliteParticipation=En%20ligne",
        "?etape=Lancement&modaliteParticipation=En%20ligne&anneeDeLancement=2025",
        "?anneeDeLancement=2024&etape=Lancement&etape=Suivi",
        "?modaliteParticipation=Hybride&conditionParticipation=Ouvert%20%C3%A0%20tous",
        "?titre=%20&thematique=%20&etape=%20&anneeDeLancement=%20",
        "?titre=zzz&etape=Lancement",
        # unknown / differently spelled parameters
        "?unknown=1", "?TITRE=consultation", "?Titre=consultation", "?etape[]=Lancement", "?etape%5B%5D=Lancement", "?etapes=Lancement",
        "?condition=Sur%20inscription", "?annee=2024", "?mediaType=xml&etape=Lancement", "?etape=Lancement&mediaType=foo",
        "?etape=Lancement;x=1", "?etape=Lancement&&etape=Analyse", "?&etape=Lancement", "?etape", "?titre", "?etape&titre", "?=x", "?%=x", "?etape=Lancement#frag",
    ]
    chunk = 12
    for i in range(0, len(queries), chunk):
        steps = []
        for j, q in enumerate(queries[i:i + chunk]):
            steps.append(step("q%03d" % (i + j), L + q))
        out.append(scenario("s9-fiches-list-filters-%02d" % (i // chunk), steps))
    # the same filters, authenticated, xml and HEAD on a selection
    sel = ["?etape=Lancement,Analyse", "?titre=d%C3%A9bat", "?anneeDeLancement=2024", "?thematique=" + TH(1), "?etape=unknown"]
    steps = []
    for j, q in enumerate(sel):
        steps += [step("user-%d" % j, L + q, **{"as": U1}), step("xml-%d" % j, L + q + ("&" if "?" in q else "?") + "mediaType=xml"),
                  step("head-%d" % j, L + q, method="HEAD")]
    out.append(scenario("s9-fiches-list-filters-variants", steps))

    # --- cache semantics (key = filters joined with "null" for absent filters; empty answers cached too)
    cs = [
        ("null-collision-a", ["", "?titre=null", "?thematique=null", "?anneeDeLancement=null", "?etape=null"]),
        ("null-collision-b", ["?titre=null", "", "?titre=consultation", "?titre=null"]),
        ("null-collision-c", ["?etape=null", "", "?etape=Lancement"]),
        ("etape-order", ["?etape=Suivi,Analyse", "?etape=Analyse,Suivi", "?etape=Analyse&etape=Suivi", "?etape=Suivi&etape=Analyse,", "?etape=Analyse"]),
        ("etape-duplicates", ["?etape=Analyse,Analyse", "?etape=Analyse", "?etape=Analyse&etape=Analyse"]),
        ("comma-in-element", ["?etape=a,b&etape=c", "?etape=a&etape=b,c", "?etape=a,b,c", "?etape=c,b,a"]),
        ("trim-same-key", ["?etape=Lancement", "?etape=%20Lancement%20", "?etape=Lancement,%20", "?etape=Lancement,,"]),
        ("blank-same-key", ["?titre=consultation", "?titre=%20consultation", "?titre=consultation%20"]),
        ("blank-same-key-b", ["", "?titre=%20", "?thematique=", "?etape=", "?etape=,", "?anneeDeLancement=%09"]),
        ("separator-collision", ["?titre=a|thematique=b", "?titre=a&thematique=b"]),
        ("separator-collision-b", ["?titre=x|thematique=null|etape=null|condition=null|modalite=null|annee=null", "?titre=x"]),
        ("condition-modalite-keys", ["?conditionParticipation=Sur%20inscription", "?modaliteParticipation=Sur%20inscription"]),
        ("utf16-sort", ["?etape=%F0%9F%98%80,%EF%BD%9E", "?etape=%EF%BD%9E,%F0%9F%98%80", "?etape=%EF%BD%9E&etape=%F0%9F%98%80"]),
        ("repeat-same", ["?etape=Lancement", "?etape=Lancement", "?etape=Lancement"]),
    ]
    for name, qs in cs:
        out.append(scenario("s9-fiches-cache-" + name, [step("q%d" % i, L + q) for i, q in enumerate(qs)]))
    # cache keys are shared across users (the response never depends on the user)
    out.append(scenario("s9-fiches-cache-users", [
        step("anon", L + "?etape=Analyse"), step("user", L + "?etape=Analyse", **{"as": U1}),
        step("xml", L + "?etape=Analyse&mediaType=xml"), step("foo", L + "?etape=Analyse&mediaType=foo")]))
    # concurrent misses (singleflight): same statuses
    out.append(scenario("s9-fiches-parallel", [
        step("par-list", L, parallel=24), step("par-filtered", L + "?etape=Lancement,Analyse&titre=d", parallel=24),
        step("par-detail", L + "/" + FI(1), parallel=24), step("par-unknown", L + "/zzz", parallel=24),
        step("par-xml", L + "?mediaType=xml", parallel=24), step("after", L)]))
    # a Strapi failure is cached as an empty list
    for mode, delay in [("500", None), ("malformed", None), ("nulldata", None), ("missingfield", None), ("slow", 600)]:
        out.append(scenario("s9-fiches-fault-list-" + mode, fault_steps(L) + [step("filtered", L + "?etape=Analyse"),
                                                                              step("filtered-again", L + "?etape=Analyse")],
                            setup=faults(MODEL, mode, delay)))
        out.append(scenario("s9-fiches-fault-detail-" + mode, fault_steps(L + "/" + FI(1)), setup=faults(MODEL, mode, delay)))
    out.append(scenario("s9-fiches-other-model-fault", [step("list", L), step("detail", L + "/" + FI(1))], setup=faults("thematiques", "500")))
    out.append(scenario("s9-fiches-all-models-fault", [step("list", L), step("detail", L + "/" + FI(1))], setup=faults("*", "500")))

    # --- detail ids
    ids = [FI(1), FI(2), FI(3), FI(4), FI(5), "fi0000000000000000000006", "unknown", "FI0000000000000000000001", FI(1) + "%20", "%20" + FI(1),
           "a%20b", "%C3%A9", "%C3%89", "a,b", FI(1) + "," + FI(2), "a+b", "a%2Bb", "a%3Fb", "a%23b", "a%26b", "a%3Db", "a%5B1%5D", "a%25",
           "x" * 300, "%F0%9F%98%80", FI(1) + ".json", FI(1) + ".xml", FI(1) + ".html", FI(1) + ";x=1", FI(1) + "/", FI(1) + "/x",
           "null", "undefined", "0", "-1", "true", "%00", "..%2F", "*", "'", "%22", "%27%20OR%201=1", "%24in", "%5B%24in%5D"]
    for i in range(0, len(ids), 8):
        out.append(scenario("s9-fiches-detail-ids-%02d" % (i // 8),
                            [step("id-%02d" % (i + j), L + "/" + v) for j, v in enumerate(ids[i:i + 8])]))
    out.append(scenario("s9-fiches-detail-variants", [
        step("json", L + "/" + FI(1)), step("xml", L + "/" + FI(1) + "?mediaType=xml"), step("foo", L + "/" + FI(1) + "?mediaType=foo"),
        step("unknown-xml", L + "/zzz?mediaType=xml"), step("unknown-foo", L + "/zzz?mediaType=foo"), step("user", L + "/" + FI(2), **{"as": U1}),
        step("unknown-user", L + "/zzz", **{"as": U1}), step("head", L + "/" + FI(1), method="HEAD"), step("head-unknown", L + "/zzz", method="HEAD"),
        step("query-ignored", L + "/" + FI(1) + "?etape=Analyse&titre=x"), step("post", L + "/" + FI(1), method="POST"),
        step("accept-xml", L + "/zzz", headers={"Accept": "application/xml"}),
    ]))

    # --- document variants (override), checked on the list and on the detail
    def sc(name, docs, detail_id=None, extra=None, tags=("S9",)):
        did = detail_id or ((docs[0].get("documentId") or FI(99)) if docs and isinstance(docs[0], dict) else FI(1))
        steps = [step("list", L), step("list-xml", L + "?mediaType=xml"), step("detail", L + "/" + did),
                 step("detail-xml", L + "/" + did + "?mediaType=xml")]
        if extra:
            steps += extra
        out.append(scenario("s9-fiches-doc-" + name, steps, setup=override(MODEL, docs), tags=tags))

    sc("minimal", [fiche()])
    sc("empty", [], detail_id=FI(1))
    sc("extra-fields", [fiche(unknown={"a": 1}, createdAt="x", updatedAt=1, locale="fr", etape_4=[1])])
    sc("duplicates", [fiche(FI(7), titre="A"), fiche(FI(7), titre="B"), fiche(FI(8), titre="C")], detail_id=FI(7))
    sc("same-debut-order", [fiche(FI(7), titre="A", debut="2024-01-01"), fiche(FI(8), titre="B", debut="2024-01-01"),
                            fiche(FI(9), titre="C", debut="2024-01-01")])
    sc("many-120", [fiche("fi%022d" % i, titre="Fiche %d" % i, debut="2020-01-%02d" % (1 + i % 28)) for i in range(120)])
    sc("unicode", [fiche(titre="é😀 \"q\" \\ </p> ]]>", porteur="𐀀", lien_site="https://x/é?a=1&b=2", etape="Étape",
                         type="Débat\tpublic\n", condition_participation="n°1 ≥ 2", modalite_participation=" ")])
    sc("long-text", [fiche(titre="lorem " * 20000)])
    for field in ["documentId", "etape_1_lancement", "etape_2_analyse", "etape_3_suivi", "titre", "debut", "fin", "porteur", "lien_site",
                  "condition_participation", "modalite_participation", "thematique", "illustration", "etape", "annee_de_lancement", "type"]:
        sc("missing-" + field.replace("_", "-"), [fiche(**{field: ...})], detail_id=FI(99) if field != "documentId" else FI(1))
        sc("null-" + field.replace("_", "-"), [fiche(**{field: None})])
    # a bad document among good ones makes the whole list undecodable
    sc("bad-among-good", [fiche(FI(7)), fiche(FI(8), titre=...), fiche(FI(9))], detail_id=FI(7))
    sc("bad-among-good-detail-8", [fiche(FI(7)), fiche(FI(8), titre=...)], detail_id=FI(8))
    # scalar coercions
    sc("number-strings", [fiche(titre=5, porteur=1.5, annee_de_lancement=2024, etape=True, type=False, lien_site=0)])
    sc("object-string", [fiche(titre={"a": 1})])
    sc("array-string", [fiche(titre=["a"])])
    # illustration
    ill = lambda **k: {"id": 1, **k}
    sc("ill-medium", [fiche(illustration=ill(url="https://u", formats={"medium": {"url": "https://m"}}))])
    sc("ill-no-medium", [fiche(illustration=ill(url="https://u", formats={"small": {"url": "https://s"}}))])
    sc("ill-medium-null", [fiche(illustration=ill(url="https://u", formats={"medium": None}))])
    sc("ill-formats-null", [fiche(illustration=ill(url="https://u", formats=None))])
    sc("ill-formats-missing", [fiche(illustration=ill(url="https://u"))])
    sc("ill-formats-empty", [fiche(illustration=ill(url="https://u", formats={}))])
    sc("ill-url-missing", [fiche(illustration=ill(formats={"medium": {"url": "https://m"}}))])
    sc("ill-url-null", [fiche(illustration=ill(url=None, formats=None))])
    sc("ill-url-number", [fiche(illustration=ill(url=5, formats=None))])
    sc("ill-medium-url-missing", [fiche(illustration=ill(url="https://u", formats={"medium": {}}))])
    sc("ill-medium-url-null", [fiche(illustration=ill(url="https://u", formats={"medium": {"url": None}}))])
    sc("ill-string", [fiche(illustration="https://u")])
    sc("ill-empty-object", [fiche(illustration={})])
    sc("ill-unicode-url", [fiche(illustration=ill(url="https://é/😀?a=1&b=2", formats=None))])
    # thematique
    th = lambda **k: dict({"id": 1, "documentId": TH(1), "label": "L", "pictogramme": "P"}, **k)
    sc("th-minimal", [fiche(thematique={"documentId": "t", "label": "l", "pictogramme": "p"})])
    for f in ["documentId", "label", "pictogramme"]:
        sc("th-missing-" + f, [fiche(thematique={k: v for k, v in th().items() if k != f})])
        sc("th-null-" + f, [fiche(thematique=th(**{f: None}))])
    sc("th-numbers", [fiche(thematique=th(documentId=5, label=6, pictogramme=7))])
    sc("th-unknown-id", [fiche(thematique=th(documentId="inconnu"))])
    sc("th-string", [fiche(thematique="th")])
    sc("th-list", [fiche(thematique=[th()])])
    sc("th-unicode", [fiche(thematique=th(label="É😀\"<&", pictogramme=" \\"))])
    # dates (Jackson LocalDateDeserializer)
    dates = ["2024-03-01", "2024-03-01T10:00:00.000Z", "2024-03-01T10:00:00", "2024-03-01T10:00", "2024-03-01T10:00:00+02:00",
             "2024-03-01T10:00:00.5", "2024-03-01Txxz", "2024-03-01TZ", "2024-03-01T", "2024-03-01Z", "2024-03-01 10:00:00",
             "2024-3-1", "20240301", "2024-02-30", "2024-02-29", "2023-02-29", "2024-13-01", "2024-00-10", "2024-01-00", "2024-01-32",
             "+10000-01-01", "10000-01-01", "-0001-01-01", "0000-01-01", "-0000-01-01", "+999999999-12-31", "+1000000000-01-01", "+02024-01-01",
             "0001-01-01", "9999-12-31", " 2024-03-01 ", "\t2024-03-01\n", "", "  ", "x", "2024-03-01T10:00:00ZZ", "2024-03-01t10:00:00z",
             "2024-03-01T25:00:00", "2024-03-01T10:61:00", "2024-03-01T10:00:00.1234567890Z", "2024-03-01T10:00:00.Z", "2024-03-01 Z",
             [2024, 3, 1], [2024, 3, 1, 10], [2024, 3], [2024, 13, 1], [2024.0, 3, 1], ["2024", 3, 1], [], [None], 19800, 19800.5, True, {"a": 1}, None]
    for i, d in enumerate(dates):
        sc("date-debut-%02d" % i, [fiche(debut=d)])
    for i, d in enumerate(dates[:6] + ["2024-02-30"]):
        sc("date-fin-%02d" % i, [fiche(fin=d)])
    # rich text on the three fields
    rich = [
        ("empty", []), ("null-element", [None]), ("string", "texte"), ("object", {"type": "paragraph"}),
        ("rich", [{"type": "heading", "level": 3, "children": [text("T")]}, para(text("a & <b> \"c\"", bold=True, code=True),
                                                                                 {"type": "link", "url": "https://x?a=1&b=2", "children": [text("l")]}),
                  {"type": "list", "format": "ordered", "children": [{"type": "list-item", "children": [text("1")]}]},
                  {"type": "quote", "children": [text("q")]}, {"type": "image", "children": []}, para(text("<br/>"))]),
        ("text-no-text", [{"type": "text"}]), ("link-no-children", [{"type": "link", "url": "u"}]),
        ("unknown-no-children", [{"type": "zzz"}]),
    ]
    for f in ["etape_1_lancement", "etape_2_analyse", "etape_3_suivi"]:
        for n, v in rich:
            sc("rich-%s-%s" % (f[6:], n), [fiche(**{f: v})])
    # first document unusable for the detail, later ones fine
    sc("detail-null-sibling", [fiche(FI(7)), fiche(FI(8), etape_1_lancement=[None])], detail_id=FI(7))
    return out


def run_known():
    """Known foundation diffs (tag S9-known): invalid UTF-8 / control characters in the path."""
    out = []
    for name, v in [("invalid-utf8", "%FF"), ("surrogate", "%ED%A0%80"), ("control-char", "a%0Ab")]:
        out.append(scenario("s9-known-fiches-detail-" + name, [step("id", L + "/" + v)], tags=("S9-known",)))
    # U+FFFD (genuine or decoded from invalid bytes) is written "?" by javacompat.URLEncode
    for name, q in [("invalid-byte", "?titre=%E9"), ("invalid-bytes", "?titre=a%FFb%FE"), ("surrogate", "?titre=%ED%A0%80"),
                    ("genuine", "?titre=%EF%BF%BD"), ("etape", "?etape=%EF%BF%BD,a"), ("thematique", "?thematique=%EF%BF%BD")]:
        out.append(scenario("s9-known-fiches-list-fffd-" + name, [step("q", L + q)], tags=("S9-known",)))
    return out


if __name__ == "__main__":
    import sys
    dump(run(), sys.argv[1])
