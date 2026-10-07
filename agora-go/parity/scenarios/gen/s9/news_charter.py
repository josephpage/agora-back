from common import *


def news(doc, short, date, route="qag", arg=None, message=None, **extra):
    d = {"documentId": doc, "message": message if message is not None else [para(text("Message "), text(short, bold=True))],
         "short_message": short, "call_to_action": "Go", "date_de_debut": date, "page_route_mobile_enum": route,
         "page_route_argument_mobile": arg, "publishedAt": "2024-05-29T10:19:13.352Z"}
    d.update(extra)
    return {k: v for k, v in d.items() if v is not ...}


def run_news():
    out = []
    path = "/welcome_page/last_news"
    model = "welcome-page-news"
    out.append(scenario("s9-news-basic", http_matrix(path)))
    for mode, delay in [("500", None), ("malformed", None), ("nulldata", None), ("missingfield", None), ("slow", 600)]:
        out.append(scenario("s9-news-fault-" + mode, fault_steps(path), setup=faults(model, mode, delay)))
    out.append(scenario("s9-news-parallel", [step("par", path, parallel=24), step("after", path)]))
    out.append(scenario("s9-news-other-model-fault", [step("json", path)], setup=faults("thematiques", "500")))
    out.append(scenario("s9-news-all-models-fault", [step("json", path), step("xml", path + "?mediaType=xml")], setup=faults("*", "500")))
    st = [step("json", path), step("xml", path + "?mediaType=xml"), step("user", path, **{"as": U1}), step("head", path, method="HEAD"),
          step("foo", path + "?mediaType=foo")]

    def sc(name, data, steps=st, tags=("S9",)):
        out.append(scenario("s9-news-" + name, steps, setup=override(model, data), tags=tags))

    sc("empty", [])
    sc("all-future", [news("n1", "Futur", "{{now+3d}}"), news("n2", "Futur 2", "{{now+9d}}")])
    sc("only-past", [news("n1", "Passé", "{{now-3d}}")])
    sc("one-past-one-future", [news("n1", "Futur", "{{now+3d}}"), news("n2", "Passé", "{{now-30d}}")])
    sc("same-date-first-wins", [news("n1", "Premier", "2020-01-01T10:00:00.000Z"), news("n2", "Second", "2020-01-01T10:00:00.000Z")])
    sc("sorted-desc-three", [news("n1", "Vieux", "2020-01-01T10:00:00.000Z"), news("n2", "Récent", "2021-06-01T10:00:00.000Z"),
                             news("n3", "Milieu", "2020-09-01T10:00:00.000Z")])
    sc("route-argument", [news("n1", "Arg", "{{now-1d}}", route="consultation", arg="co0000000000000000000001")])
    sc("route-argument-missing", [news("n1", "Arg", "{{now-1d}}", arg=...)])
    sc("route-argument-number", [news("n1", "Arg", "{{now-1d}}", arg=12)])
    sc("route-enum-number", [news("n1", "Arg", "{{now-1d}}", route=3)])
    sc("empty-message", [news("n1", "Vide", "{{now-1d}}", message=[])])
    sc("message-null", [news("n1", "Null", "{{now-1d}}", message=None)] if False else [dict(news("n1", "Null", "{{now-1d}}"), message=None)])
    sc("message-missing", [{k: v for k, v in news("n1", "Miss", "{{now-1d}}").items() if k != "message"}])
    sc("message-null-element", [news("n1", "NullEl", "{{now-1d}}", message=[None])])
    sc("message-rich", [news("n1", "Rich", "{{now-1d}}", message=[
        {"type": "heading", "level": 2, "children": [text("Titre")]},
        para(text("a & <b> \"c\"", italic=True), {"type": "link", "url": "https://x/?a=1&b=2", "children": [text("lien")]}),
        {"type": "list", "format": "ordered", "children": [{"type": "list-item", "children": [text("1")]}]},
        {"type": "quote", "children": [text("q")]}, {"type": "code", "children": [text("unknown")]}, ])])
    sc("unicode", [news("n1", "é😀   \"q\" \\ </p> ]]>", "{{now-1d}}", route="é", arg="😀")])
    # date forms (Jackson LocalDateTimeDeserializer)
    for i, d in enumerate([
        "2020-01-01T10:00:00.000Z", "2020-01-01T10:00:00Z", "2020-01-01T10:00Z", "2020-01-01T10:00:00", "2020-01-01T10:00",
        "2020-01-01T10:00:00.123456789", "2020-01-01T10:00:00.1234567891", "2020-01-01T10:00:00.", "2020-01-01t10:00:00.5z",
        "2020-01-01T10:00:00+02:00", "2020-01-01T10:00:00.000+0000", "2020-01-01", "2020-01-01 10:00:00", "20200101T100000",
        "2020-1-1T10:00:00", "2020-02-30T10:00:00", "2020-02-29T10:00:00", "2019-02-29T10:00:00", "2020-01-01T24:00:00",
        "2020-01-01T23:59:60", "2020-01-01T23:59:59.999999999Z", " 2020-01-01T10:00:00Z ", "", "   ", "garbage", "+12020-01-01T10:00:00",
        "12020-01-01T10:00:00", "-0001-01-01T10:00:00", "0000-01-01T10:00:00", "2020-01-01T10:00:00ZZ", "2020-01-01T10:00:00.5Z[UTC]",
        "2020-01-01T1:00:00", "2020-01-01T10:0", "2020-01-01T10:00:0", "2020-01-01T10:00:00,5",
        [2020, 1, 1, 10, 0], [2020, 1, 1, 10, 0, 5], [2020, 1, 1, 10, 0, 5, 7], [2020, 1, 1], [2020, 13, 1, 10, 0], [], 1577872800000,
        1577872800.5, True, {"a": 1}, None,
    ]):
        sc("date-form-%02d" % i, [news("n1", "Forme %02d" % i, d if d is not None else None, **{}) if d is not None else
                                  dict(news("n1", "Forme null", "x"), date_de_debut=None)],
           steps=[step("json", path), step("xml", path + "?mediaType=xml")])
    sc("date-missing", [{k: v for k, v in news("n1", "Sans date", "x").items() if k != "date_de_debut"}])
    # one news with a bad date makes the whole list undecodable -> 404
    sc("bad-date-among-good", [news("n1", "Bon", "{{now-1d}}"), news("n2", "Mauvais", "pas une date")])
    sc("missing-short-message", [{k: v for k, v in news("n1", "x", "{{now-1d}}").items() if k != "short_message"}])
    sc("extra-fields", [news("n1", "Extra", "{{now-1d}}", unknown={"a": [1]}, createdAt="x", updatedAt=1, locale="fr")])
    sc("many", [news("n%d" % i, "News %d" % i, "{{now-%dh}}" % (i + 1)) for i in range(40)])
    # a news that starts in 2 s: not started at the first step, started at the second
    out.append(scenario("s9-news-starts-soon", [step("before", path), step("after", path, sleepMs=2600)],
                        setup=override(model, [news("n1", "Ancien", "{{now-3d}}"), news("n2", "Bientôt", "{{now+2s}}")])))
    out.append(scenario("s9-news-starts-soon-only", [step("before", path), step("after", path, sleepMs=2600)],
                        setup=override(model, [news("n2", "Bientôt", "{{now+2s}}")])))
    return out


def charter(doc, date, charte=None, preview=None, **extra):
    d = {"documentId": doc, "charte": charte if charte is not None else [para(text("Charte "), text(doc, bold=True))],
         "charte_preview": preview if preview is not None else [para(text("Aperçu " + doc))],
         "datetime_debut": date, "publishedAt": "2024-05-29T10:19:13.352Z"}
    d.update(extra)
    return {k: v for k, v in d.items() if v is not ...}


def run_charter():
    out = []
    path = "/participation_charter"
    model = "charte-participations"
    m = http_matrix(path)
    # ETag handling (ShallowEtagHeaderFilter on this exact path)
    m += [
        step("etag-wrong", path, headers={"If-None-Match": '"0000"'}),
        step("etag-star", path, headers={"If-None-Match": "*"}),
        step("etag-weak-wrong", path, headers={"If-None-Match": 'W/"0000"'}),
        step("etag-list", path, headers={"If-None-Match": '"0000", "1111"'}),
        step("etag-xml-wrong", path + "?mediaType=xml", headers={"If-None-Match": '"0000"'}),
        step("etag-head", path, method="HEAD", headers={"If-None-Match": '"0000"'}),
        step("if-modified-since", path, headers={"If-Modified-Since": "Wed, 21 Oct 2015 07:28:00 GMT"}),
        step("etag-sub-path", path + "/x"),
        step("etag-match", path, headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-match-user", path, **{"as": U1}, headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-match-weak", path, headers={"If-None-Match": 'W/"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-match-in-list", path, headers={"If-None-Match": '"nope", "0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-unquoted", path, headers={"If-None-Match": '0e17a04c03c30eb284cebf487759b69ab'}),
        step("etag-empty", path, headers={"If-None-Match": ''}),
        step("etag-xml-json-etag", path + "?mediaType=xml", headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-xml-match", path + "?mediaType=xml", headers={"If-None-Match": '"032b2725ecf6fd98288a1b039c61a4aee"'}),
        step("etag-foo", path + "?mediaType=foo", headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-head-match", path, method="HEAD", headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
        step("etag-post-match", path, method="POST", headers={"If-None-Match": '"0e17a04c03c30eb284cebf487759b69ab"'}),
    ]
    out.append(scenario("s9-charter-basic", m))
    # exact ETag match: the ETag value comes from the response and is bound by the runner capture? use a computed one
    for mode, delay in [("500", None), ("malformed", None), ("nulldata", None), ("missingfield", None), ("slow", 600)]:
        out.append(scenario("s9-charter-fault-" + mode, fault_steps(path), setup=faults(model, mode, delay)))
    out.append(scenario("s9-charter-parallel", [step("par", path, parallel=24), step("after", path)]))
    out.append(scenario("s9-charter-other-model-fault", [step("json", path)], setup=faults("thematiques", "500")))
    out.append(scenario("s9-charter-all-models-fault", [step("json", path), step("xml", path + "?mediaType=xml")], setup=faults("*", "500")))
    st = [step("json", path), step("xml", path + "?mediaType=xml"), step("user", path, **{"as": U1}), step("head", path, method="HEAD"),
          step("foo", path + "?mediaType=foo"), step("etag-star", path, headers={"If-None-Match": "*"})]

    def sc(name, data, steps=st):
        out.append(scenario("s9-charter-" + name, steps, setup=override(model, data)))

    sc("empty", [])
    sc("only-future", [charter("c1", "{{now+3d}}")])
    sc("only-past", [charter("c1", "{{now-3d}}")])
    sc("newest-wins", [charter("c1", "2020-01-01T00:00:00.000Z"), charter("c2", "2021-01-01T00:00:00.000Z"), charter("c3", "2019-01-01T00:00:00.000Z")])
    sc("future-ignored", [charter("c1", "{{now-3d}}"), charter("c2", "{{now+3d}}")])
    sc("same-date", [charter("c1", "2020-01-01T00:00:00.000Z"), charter("c2", "2020-01-01T00:00:00.000Z")])
    sc("empty-texts", [charter("c1", "{{now-3d}}", charte=[], preview=[])])
    sc("null-element-charte", [charter("c1", "{{now-3d}}", charte=[None])])
    sc("null-element-preview", [charter("c1", "{{now-3d}}", preview=[None])])
    sc("charte-missing", [{k: v for k, v in charter("c1", "{{now-3d}}").items() if k != "charte"}])
    sc("preview-null", [dict(charter("c1", "{{now-3d}}"), charte_preview=None)])
    sc("date-missing", [{k: v for k, v in charter("c1", "x").items() if k != "datetime_debut"}])
    sc("date-bad", [charter("c1", "pas une date")])
    sc("date-bad-second-element", [charter("c1", "{{now-3d}}"), charter("c2", "pas une date")])
    sc("date-offset", [charter("c1", "2020-01-01T10:00:00+02:00")])
    sc("date-no-z", [charter("c1", "2020-01-01T10:00:00")])
    sc("date-only", [charter("c1", "2020-01-01")])
    sc("rich", [charter("c1", "{{now-3d}}", charte=[
        {"type": "heading", "level": 1, "children": [text("Charte")]},
        para(text("a & <b> \"c\" \\ é😀", bold=True, italic=True, underline=True, strikethrough=True, code=True)),
        {"type": "list", "format": "unordered", "children": [{"type": "list-item", "children": [text("x"), {"type": "link", "url": "u\"", "children": [text("l")]}]}]},
        {"type": "quote", "children": [text("q")]}, {"type": "image", "children": []}, para(text("<br/>"))],
        preview=[para(text("p<br/>"))])])
    sc("unicode", [charter("c1", "{{now-3d}}", charte=[para(text("é😀 </body>]]>"))], preview=[para(text(" "))])])
    sc("extra-fields", [charter("c1", "{{now-3d}}", unknown=1, createdAt="x", locale="fr")])
    sc("long", [charter("c1", "{{now-3d}}", charte=[para(text("lorem ipsum " * 5000))])])
    return out


if __name__ == "__main__":
    import sys
    dump(run_news(), sys.argv[1])
    dump(run_charter(), sys.argv[2])
