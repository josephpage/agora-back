import json
from common import *

P = "/feedback"
DEV = {"model": "Pixel 8", "osVersion": "Android 14", "appVersion": "1.4.2"}


def post(id, body=None, raw=None, ct=None, user=U1, path=P, headers=None, **kw):
    s = {"id": id, "method": "POST", "path": path}
    if user:
        s["as"] = user
    if raw is not None:
        s["bodyRaw"] = raw
    elif body is not None:
        s["body"] = body
    if ct:
        s["contentType"] = ct
    if headers:
        s["headers"] = headers
    s.update(kw)
    return s


def run():
    out = []

    def sc(name, steps, tags=("S9",), setup=None):
        out.append(scenario("s9-feedback-" + name, steps, tags=tags, dbdiff="step", setup=setup))

    # --- types
    sc("types", [
        post("bug", {"type": "bug", "description": "Un bug"}),
        post("feature", {"type": "feature", "description": "Une idée", "deviceInfo": DEV}),
        post("comment", {"type": "comment", "description": "Un commentaire", "deviceInfo": None}),
        post("upper-bug", {"type": "BUG", "description": "d"}),
        post("upper-feature", {"type": "FEATURE", "description": "d"}),
        post("upper-comment", {"type": "COMMENT", "description": "d"}),
        post("mixed", {"type": "BuG", "description": "d"}),
        post("mixed-feature", {"type": "FeAtUrE", "description": "d"}),
    ])
    types_bad = ["", " ", " bug", "bug ", "bug\n", "feature_request", "FEATURE_REQUEST", "features", "bugs", "other", "0", "null", "none", "BUG\u0000",
                 "bu g", "bug​", "ＢＵＧ", "Bug\t", "comments", "commentaire", "idée", "feat", "request", "İ", "bİg"]
    sc("types-unknown", [post("t%02d" % i, {"type": t, "description": "d"}) for i, t in enumerate(types_bad)])
    sc("types-wrong-json-types", [
        post("number", {"type": 1, "description": "d"}),
        post("float", {"type": 1.5, "description": "d"}),
        post("true", {"type": True, "description": "d"}),
        post("false", {"type": False, "description": "d"}),
        post("null", {"type": None, "description": "d"}),
        post("object", {"type": {"a": 1}, "description": "d"}),
        post("array", {"type": ["bug"], "description": "d"}),
        post("empty-array", {"type": [], "description": "d"}),
        post("missing", {"description": "d"}),
        post("type-only", {"type": "bug"}),
        post("missing-both", {"deviceInfo": DEV}),
    ])
    sc("description-wrong-json-types", [
        post("number", {"type": "bug", "description": 5}),
        post("float", {"type": "bug", "description": 1.50}),
        post("big-number", {"type": "bug", "description": 12345678901234567890}),
        post("true", {"type": "bug", "description": True}),
        post("null", {"type": "bug", "description": None}),
        post("object", {"type": "bug", "description": {"a": 1}}),
        post("array", {"type": "bug", "description": ["a"]}),
        post("empty-string", {"type": "bug", "description": ""}),
        post("blank", {"type": "bug", "description": "   \t\n"}),
        post("missing", {"type": "bug"}),
        post("unknown-type-and-bad-description", {"type": "zzz", "description": None}),
        post("unknown-type-and-missing-description", {"type": "zzz"}),
    ])
    # --- description content
    sc("description-content", [
        post("unicode", {"type": "bug", "description": "é à ü ç œ 😀 👨‍👩‍👧     ﻿ 日本語 ‮"}),
        post("crlf", {"type": "bug", "description": "a\r\nb\rc\nd\te"}),
        post("html", {"type": "bug", "description": "<script>alert(1)</script> & \"q\" 'a' \\ /"}),
        post("sql", {"type": "bug", "description": "'; DROP TABLE app_feedbacks; --"}),
        post("percent", {"type": "bug", "description": "100% _ \\x00 %s {{x}}"}),
        post("long", {"type": "bug", "description": "lorem ipsum dolor " * 2000}),
        post("spaces-kept", {"type": "bug", "description": "  lead and trail  "}),
        post("control", {"type": "bug", "description": "a\u0001b\u001fc\u007fd"}),
        post("nul", {"type": "bug", "description": "a\u0000b"}),
        post("after-nul", {"type": "bug", "description": "ok"}),
    ])
    # --- deviceInfo
    sc("device-info", [
        post("full", {"type": "bug", "description": "d", "deviceInfo": DEV}),
        post("absent", {"type": "bug", "description": "d"}),
        post("null", {"type": "bug", "description": "d", "deviceInfo": None}),
        post("empty-object", {"type": "bug", "description": "d", "deviceInfo": {}}),
        post("missing-model", {"type": "bug", "description": "d", "deviceInfo": {"osVersion": "1", "appVersion": "2"}}),
        post("missing-os", {"type": "bug", "description": "d", "deviceInfo": {"model": "m", "appVersion": "2"}}),
        post("missing-app", {"type": "bug", "description": "d", "deviceInfo": {"model": "m", "osVersion": "1"}}),
        post("null-model", {"type": "bug", "description": "d", "deviceInfo": {"model": None, "osVersion": "1", "appVersion": "2"}}),
        post("numbers", {"type": "bug", "description": "d", "deviceInfo": {"model": 1, "osVersion": 2.5, "appVersion": True}}),
        post("string", {"type": "bug", "description": "d", "deviceInfo": "iphone"}),
        post("array", {"type": "bug", "description": "d", "deviceInfo": [DEV]}),
        post("number", {"type": "bug", "description": "d", "deviceInfo": 5}),
        post("object-fields", {"type": "bug", "description": "d", "deviceInfo": {"model": {}, "osVersion": "1", "appVersion": "2"}}),
        post("extra", {"type": "bug", "description": "d", "deviceInfo": dict(DEV, extra="x", nested={"a": 1})}),
        post("empty-strings", {"type": "bug", "description": "d", "deviceInfo": {"model": "", "osVersion": "", "appVersion": ""}}),
        post("unicode", {"type": "feature", "description": "d", "deviceInfo": {"model": "é😀", "osVersion": "𐀀", "appVersion": " "}}),
        post("255", {"type": "bug", "description": "d", "deviceInfo": {"model": "m" * 255, "osVersion": "o" * 255, "appVersion": "a" * 255}}),
        post("256-model", {"type": "bug", "description": "d", "deviceInfo": {"model": "m" * 256, "osVersion": "o", "appVersion": "a"}}),
        post("256-os", {"type": "bug", "description": "d", "deviceInfo": {"model": "m", "osVersion": "o" * 256, "appVersion": "a"}}),
        post("256-app", {"type": "bug", "description": "d", "deviceInfo": {"model": "m", "osVersion": "o", "appVersion": "a" * 256}}),
        post("nul-model", {"type": "bug", "description": "d", "deviceInfo": {"model": "a\u0000b", "osVersion": "o", "appVersion": "a"}}),
        post("unknown-type-with-device", {"type": "zzz", "description": "d", "deviceInfo": DEV}),
        post("unknown-type-bad-device", {"type": "zzz", "description": "d", "deviceInfo": {}}),
    ])
    # --- JSON syntax / leniency
    raws = [
        ("empty", ""), ("spaces", "   "), ("null", "null"), ("array", "[]"), ("array-obj", '[{"type":"bug","description":"d"}]'),
        ("string", '"bug"'), ("number", "123"), ("true", "true"), ("empty-object", "{}"), ("malformed", '{"type":"bug"'),
        ("malformed2", "{type:bug}"), ("single-quotes", "{'type':'bug','description':'d'}"), ("trailing-comma", '{"type":"bug","description":"d",}'),
        ("comment", '{"type":"bug",/*c*/"description":"d"}'), ("line-comment", '{"type":"bug","description":"d"} // c'),
        ("trailing-garbage", '{"type":"bug","description":"d"} xyz'), ("trailing-object", '{"type":"bug","description":"d"}{"x":1}'),
        ("duplicate-keys", '{"type":"zzz","type":"bug","description":"d","description":"e"}'),
        ("case-keys", '{"Type":"bug","Description":"d"}'), ("unquoted-key", '{type:"bug","description":"d"}'),
        ("nan", '{"type":"bug","description":NaN}'), ("big-int-type", '{"type":123456789012345678901234567890,"description":"d"}'),
        ("deep", '{"type":"bug","description":"d","x":' + "[" * 500 + "]" * 500 + "}"),
        ("deeper", '{"type":"bug","description":"d","x":' + "[" * 2000 + "]" * 2000 + "}"),
        ("escapes", '{"type":"b\\u0075g","description":"\\n\\t\\"\\\\\\/\\b\\f\\r\\u00e9\\ud83d\\ude00"}'),
        ("bad-escape", '{"type":"bug","description":"\\x"}'), ("bad-unicode-escape", '{"type":"bug","description":"\\u12"}'),
        ("raw-newline-in-string", '{"type":"bug","description":"a\nb"}'), ("raw-tab-in-string", '{"type":"bug","description":"a\tb"}'),
        ("leading-zero", '{"type":"bug","description":"d","n":01}'), ("plus", '{"type":"bug","description":"d","n":+1}'),
        ("whitespace-around", ' \n\t{"type":"bug","description":"d"} \n'),
        ("nul-byte", '{"type":"bug","description":"d"}\u0000'),
    ]
    sc("json-syntax", [post(n, raw=r, ct="application/json") for n, r in raws])
    # --- content types
    ok = '{"type":"bug","description":"d"}'
    cts = [
        ("none", "none"), ("text-plain", "text/plain"), ("json", "application/json"), ("json-charset-utf8", "application/json;charset=UTF-8"),
        ("json-charset-utf8-space", "application/json; charset=utf-8"), ("json-upper", "APPLICATION/JSON"), ("json-vnd", "application/vnd.api+json"),
        ("json-problem", "application/problem+json"), ("json-q", "application/json;q=0.9"), ("json-garbage-param", "application/json;foo=bar"),
        ("form", "application/x-www-form-urlencoded"), ("multipart", "multipart/form-data; boundary=x"), ("octet", "application/octet-stream"),
        ("star", "*/*"), ("json-star", "application/*"), ("text-json", "text/json"), ("text-html", "text/html"), ("invalid", "not a media type"),
        ("empty", ""), ("json-comma", "application/json, text/plain"), ("json-charset-latin1", "application/json;charset=ISO-8859-1"),
        ("json-charset-bad", "application/json;charset=nope"), ("json-charset-utf16", "application/json;charset=UTF-16"),
    ]
    jsonish = {"json", "json-charset-utf8", "json-charset-utf8-space", "json-upper", "json-vnd", "json-problem", "json-q",
               "json-garbage-param", "json-charset-latin1", "json-charset-utf16"}
    sc("content-types", [post("ct-" + n, raw=ok, ct=c) for n, c in cts if n in jsonish])
    # 415 answers: the Accept header Spring adds lists the converters in another order and with charsets (httpx.acceptForBodies)
    # and a wildcard Content-Type is a 500 (IllegalArgumentException), not a 415
    sc("content-types-unsupported", [post("ct-" + n, raw=ok, ct=c) for n, c in cts if n not in jsonish] + [
        post("form", raw="type=bug&description=d", ct="application/x-www-form-urlencoded"),
        post("form-query", raw="", ct="application/x-www-form-urlencoded", path=P + "?type=bug&description=d"),
    ], tags=("S9-known",))
    sc("content-types-charset", [post("ct-" + n, raw=ok, ct=c) for n, c in cts if n == "json-charset-bad"],
       tags=("S9-known",))
    sc("content-types-query", [post("query-params", raw=ok, ct="application/json", path=P + "?type=feature&description=x")])
    # --- methods, routes, auth
    sc("methods", [
        {"id": "get", "path": P, "as": U1}, {"id": "head", "method": "HEAD", "path": P, "as": U1},
        {"id": "put", "method": "PUT", "path": P, "as": U1, "bodyRaw": ok, "contentType": "application/json"},
        {"id": "delete", "method": "DELETE", "path": P, "as": U1}, {"id": "patch", "method": "PATCH", "path": P, "as": U1},
        {"id": "options", "method": "OPTIONS", "path": P, "as": U1},
        {"id": "options-anon", "method": "OPTIONS", "path": P},
        {"id": "cors-preflight", "method": "OPTIONS", "path": P, "headers": {"Origin": "https://www.agora.gouv.fr", "Access-Control-Request-Method": "POST"}},
        {"id": "cors-post", "method": "POST", "path": P, "as": U1, "bodyRaw": ok, "contentType": "application/json", "headers": {"Origin": "https://www.agora.gouv.fr"}},
        {"id": "cors-post-other-origin", "method": "POST", "path": P, "as": U1, "bodyRaw": ok, "contentType": "application/json", "headers": {"Origin": "https://evil.example"}},
        post("trailing-slash", raw=ok, ct="application/json", path=P + "/"),
        post("sub-path", raw=ok, ct="application/json", path=P + "/x"),
        post("upper-path", raw=ok, ct="application/json", path="/Feedback"),
        post("ext", raw=ok, ct="application/json", path=P + ".json"),
        post("matrix", raw=ok, ct="application/json", path=P + ";x=1"),
        post("double-slash", raw=ok, ct="application/json", path="//feedback"),
        post("feedbacks", raw=ok, ct="application/json", path="/feedbacks"),
    ])
    users = [("anon", None), ("regular", U1), ("regular2", "{{seed.UserRegular2}}"), ("banned", BANNED), ("moderator", MODERATOR), ("admin", ADMIN),
             ("publisher", "{{seed.UserPublisher}}"), ("idle", "{{seed.UserIdle}}"),
             ("never-connected", "{{seed.UserNeverConnected}}"),
             ("unknown-user", "00000000-0000-4000-9000-0000000000ff"), ("non-uuid-sub", "not-a-uuid")]
    st = [post("user-" + n, raw=ok, ct="application/json", user=u) for n, u in users]
    st += [post("bad-jwt", raw=ok, ct="application/json", user=None, bearer="abc.def.ghi"),
           post("basic", raw=ok, ct="application/json", user=None, headers={"Authorization": "Basic abc"}),
           post("empty-bearer", raw=ok, ct="application/json", user=None, headers={"Authorization": "Bearer "}),
           post("cookie-garbage", raw=ok, ct="application/json", user=None, headers={"Cookie": "auth-jwt=garbage"}),
           post("anon-bad-body", raw="not json", ct="application/json", user=None),
           post("anon-bad-type", raw=ok, ct="text/plain", user=None),
           post("anon-xml", raw=ok, ct="application/json", user=None, path=P + "?mediaType=xml")]
    sc("auth", st)
    # --- response negotiation
    sc("media-type", [
        post("json", raw=ok, ct="application/json", path=P + "?mediaType=json"),
        post("xml", raw=ok, ct="application/json", path=P + "?mediaType=xml"),
        post("xml-upper", raw=ok, ct="application/json", path=P + "?mediaType=XML"),
        post("foo", raw=ok, ct="application/json", path=P + "?mediaType=foo"),
        post("empty", raw=ok, ct="application/json", path=P + "?mediaType="),
        post("xml-unknown-type", raw='{"type":"zzz","description":"d"}', ct="application/json", path=P + "?mediaType=xml"),
        post("foo-unknown-type", raw='{"type":"zzz","description":"d"}', ct="application/json", path=P + "?mediaType=foo"),
        post("xml-bad-body", raw="{", ct="application/json", path=P + "?mediaType=xml"),
        post("foo-bad-body", raw="{", ct="application/json", path=P + "?mediaType=foo"),
        post("accept-xml", raw=ok, ct="application/json", headers={"Accept": "application/xml"}),
        post("accept-text", raw=ok, ct="application/json", headers={"Accept": "text/plain"}),
        post("accept-json", raw=ok, ct="application/json", headers={"Accept": "application/json"}),
        post("accept-star", raw=ok, ct="application/json", headers={"Accept": "*/*"}),
    ])
    # --- repeated and volume
    sc("repeat", [post("same", raw=ok, ct="application/json", repeat=3),
                  post("same-other-user", raw=ok, ct="application/json", user="{{seed.UserRegular2}}")])
    sc("parallel", [post("par", raw=ok, ct="application/json", parallel=12), post("par-mixed-users", raw=ok, ct="application/json", user=BANNED, parallel=4)])
    # --- seed rows are untouched; extra keys, large
    sc("big", [post("300k", {"type": "feature", "description": "é" * 150000})])
    sc("extra-keys", [post("extra", {"type": "comment", "description": "d", "id": "00000000-0000-0000-0000-000000000000", "userId": "x",
                                      "createdDate": "2000-01-01", "deviceModel": "forged"}),
                      post("preset-id-twice", {"type": "comment", "description": "d", "id": "11111111-1111-1111-1111-111111111111"})])
    # --- XML request body (Kotlin reads it with the XmlMapper; httpx does not: divergence C-XML-BODY)
    xml = "<AppFeedbackJson><type>bug</type><description>d</description></AppFeedbackJson>"
    out.append(scenario("s9-known-feedback-xml-body", [
        post("xml", raw=xml, ct="application/xml"), post("xml-text", raw=xml, ct="text/xml"), post("xml-vnd", raw=xml, ct="application/vnd.x+xml"),
        post("xml-bad", raw="<a>", ct="application/xml"), post("xml-unknown-type", raw=xml.replace("bug", "zzz"), ct="application/xml"),
    ], tags=("S9-known",), dbdiff="step"))
    # --- body encoding (jsonjava request decoder: being rewritten by the lead; expected to pass afterwards)
    sc("encoding-bom", [
        post("bom", raw="﻿" + ok, ct="application/json"),
        post("bom-charset", raw="﻿" + ok, ct="application/json;charset=UTF-8"),
        post("latin1-in-utf8", raw='{"type":"bug","description":"é"}', ct="application/json;charset=ISO-8859-1"),
    ], tags=("S9-known",))
    sc("lone-surrogate", [
        post("high", raw='{"type":"bug","description":"a\\ud800b"}', ct="application/json"),
        post("low", raw='{"type":"bug","description":"a\\udc00b"}', ct="application/json"),
        post("pair", raw='{"type":"bug","description":"a\\ud83d\\ude00b"}', ct="application/json"),
        post("type-lone", raw='{"type":"bu\\ud800g","description":"d"}', ct="application/json"),
    ], tags=("S9-known",))
    return out


if __name__ == "__main__":
    import sys
    dump(run(), sys.argv[1])
