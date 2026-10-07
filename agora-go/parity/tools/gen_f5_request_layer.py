import base64, json, sys
b64 = lambda b: base64.b64encode(b).decode()
Y = b'{"yearOfBirth":"1990"}'
def u16(s, enc): return s.encode(enc)
bodies = [
 ("utf8", "application/json", Y),
 ("bom", "application/json", b'\xef\xbb\xbf' + Y),
 ("bom-ws", "application/json", b'\xef\xbb\xbf  ' + Y),
 ("ws-bom", "application/json", b'  \xef\xbb\xbf' + Y),
 ("overlong-2-in-value", "application/json", b'{"yearOfBirth":"19\xc0\xb990"}'),
 ("overlong-3-in-value", "application/json", b'{"yearOfBirth":"19\xe0\x80\xb990"}'),
 ("overlong-in-name", "application/json", b'{"\xc1\xb9earOfBirth":"1987"}'),
 ("wtf8-surrogate", "application/json", b'{"x":"\xed\xa0\x80","yearOfBirth":"1990"}'),
 ("above-10ffff", "application/json", b'{"x":"\xf4\x90\x80\x80","yearOfBirth":"1990"}'),
 ("overlong-4", "application/json", b'{"x":"\xf0\x80\x80\x80","yearOfBirth":"1990"}'),
 ("f8-lead", "application/json", b'{"x":"\xf8\x80\x80\x80","yearOfBirth":"1990"}'),
 ("bad-continuation", "application/json", b'{"x":"\xc3\x28","yearOfBirth":"1990"}'),
 ("lone-continuation", "application/json", b'{"x":"\x80","yearOfBirth":"1990"}'),
 ("truncated-seq", "application/json", b'{"x":"\xe2\x82","yearOfBirth":"1990"}'),
 ("bad-utf8-name", "application/json", b'{"\xff":"a","yearOfBirth":"1990"}'),
 ("valid-nonascii-name", "application/json", b'{"\xc3\xa9":"a","yearOfBirth":"1990"}'),
 ("garbage-after-value", "application/json", Y + b'\xff\xfe'),
 ("second-value-after", "application/json", Y + b'{"yearOfBirth":"1991"}'),
 ("utf16le", "application/json", u16(Y.decode(), 'utf-16-le')),
 ("utf16be", "application/json", u16(Y.decode(), 'utf-16-be')),
 ("utf16le-bom", "application/json", b'\xff\xfe' + u16(Y.decode(), 'utf-16-le')),
 ("utf16be-bom", "application/json", b'\xfe\xff' + u16(Y.decode(), 'utf-16-be')),
 ("utf32le", "application/json", u16(Y.decode(), 'utf-32-le')),
 ("utf32be-bom", "application/json", b'\x00\x00\xfe\xff' + u16(Y.decode(), 'utf-32-be')),
 ("utf32-odd-length", "application/json", u16(Y.decode(), 'utf-32-le') + b'\x00'),
 ("utf16le-lone-high", "application/json", u16('{"x":"', 'utf-16-le') + b'\x00\xd8' + u16('","yearOfBirth":"1990"}', 'utf-16-le')),
 ("utf16le-lone-low", "application/json", u16('{"x":"', 'utf-16-le') + b'\x00\xdc' + u16('","yearOfBirth":"1990"}', 'utf-16-le')),
 ("utf16le-fffe", "application/json", u16('{"x":"', 'utf-16-le') + b'\xfe\xff' + u16('","yearOfBirth":"1990"}', 'utf-16-le')),
 ("utf16le-odd-length", "application/json", u16(Y.decode(), 'utf-16-le') + b'\x20'),
 ("utf16le-pair", "application/json", u16('{"x":"\U0001F600","yearOfBirth":"1992"}', 'utf-16-le')),
 ("latin1", "application/json;charset=ISO-8859-1", b'{"x":"\xe9","yearOfBirth":"1990"}'),
 ("latin1-alias", "application/json;charset=latin1", b'{"x":"\xe9","yearOfBirth":"1990"}'),
 ("latin1-capital-param", "application/json;Charset=ISO-8859-1", b'{"x":"\xe9","yearOfBirth":"1990"}'),
 ("win1252", "application/json;charset=windows-1252", b'{"x":"\x80\x81","yearOfBirth":"1990"}'),
 ("cp1252-alias", "application/json;charset=cp1252", b'{"x":"\x80","yearOfBirth":"1990"}'),
 ("cp037", "application/json;charset=IBM037", '{"yearOfBirth":"1990"}'.encode('cp037')),
 ("latin1-bom-bytes", "application/json;charset=ISO-8859-1", b'\xef\xbb\xbf' + Y),
 ("utf16-declared-utf8-bytes", "application/json;charset=UTF-16", Y),
 ("utf16le-declared-utf8-bom", "application/json;charset=UTF-16LE", b'\xef\xbb\xbf' + Y),
 ("us-ascii-declared", "application/json;charset=US-ASCII", b'{"x":"\xc3\xa9","yearOfBirth":"1990"}'),
 ("utf8-alias", "application/json;charset=utf8", Y),
 ("utf8-bad-alias", "application/json;charset=UTF_8", Y),
 ("unknown-charset", "application/json;charset=foo", Y),
 ("illegal-charset", "application/json;charset=@@", Y),
 ("empty-charset", "application/json;charset=", Y),
 ("quoted-charset", 'application/json;charset="utf-8"', Y),
 ("single-quoted-charset", "application/json;charset='utf-8'", Y),
 ("capital-charset-unknown", "application/json;Charset=foo", Y),
 ("lone-surrogate-escape", "application/json", b'{"gender":"\\ud800","yearOfBirth":"1990"}'),
 ("pair-escape", "application/json", b'{"gender":"\\ud83d\\ude00","yearOfBirth":"1990"}'),
 ("raw-nul", "application/json", b'{"x":"a\x00b","yearOfBirth":"1990"}'),
 ("raw-tab-in-string", "application/json", b'{"x":"a\tb","yearOfBirth":"1990"}'),
 ("empty-after-bom", "application/json", b'\xef\xbb\xbf'),
 ("only-ws", "application/json", b'   '),
 ("partial-bom", "application/json", b'\xef\xbb' + Y),
 ("dup-object-then-string", "application/json", b'{"yearOfBirth":{},"yearOfBirth":"1990"}'),
 ("dup-null-then-string", "application/json", b'{"yearOfBirth":null,"yearOfBirth":"1993"}'),
 ("dup-string-then-null", "application/json", b'{"yearOfBirth":"1994","yearOfBirth":null}'),
 ("dup-array-then-string", "application/json", b'{"gender":["M"],"gender":"F"}'),
 ("nested-dup-unknown", "application/json", b'{"x":{"a":1,"a":{}},"yearOfBirth":"1995"}'),
 ("deep-unknown-garbage", "application/json", b'{"x":[1,{"y":"\xc3\xa9"}],"yearOfBirth":"1996"}'),
]
cts = ["application/json","APPLICATION/JSON","Application/Json; Charset=UTF-8","application/json;","application/json; foo","application/json;foo=","application/json; charset=utf-8; charset=utf-8",
"application/json ; charset=utf-8","application/json;charset=\"\"","application/json;a=\"b","application/json;a=\"b;c\"","application/json;a=b c",
"application/*+json","application/*+json;charset=utf-8","application/vnd.foo+json","application/json-patch+json","text/json","*/*","*/*;charset=utf-8","application/*","*","json","application/","/json","application/json/x",
"text/plain","text/plain;charset=foo","multipart/form-data; boundary=x","application/xml","application/json, text/plain"," application/json","application/js on","application/json;charset=utf-8;","application/json;;charset=utf-8",
"application/json;=x","application/json;charset=\"utf-8","application/x-json","application/problem+json","application/json;q=abc","application/json;q=0.5","application/json;q=2","application/json;x=\"\xe9\"","application/json;x=\xe9",
"application/json;charset=x-user-defined","application/json;charset=iso_8859-1","application/json;charset=UTF-32","application/json;charset=utf-16be","application/x-www-form-urlencoded","application/octet-stream","application/xml;charset=foo","text/xml"]

out = []
out.append({"name": "F5-body-bytes", "tags": ["F5", "request"], "dbdiff": "step", "steps": [
  {"id": n, "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": ct, "bodyB64": b64(b)} for n, ct, b in bodies]})
steps = []
for i, ct in enumerate(cts):
    st = {"id": "ct-%d" % i, "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "bodyB64": b64(Y), "compare": {"headers": ["Accept"]}}
    if any(ord(c) > 127 for c in ct):
        st["contentType"] = "none"; st["headers"] = {"Content-Type": "b64:" + b64(ct.encode('latin-1'))}
    else:
        st["contentType"] = ct
    steps.append(st)
steps.append({"id": "no-ct-with-body", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "none", "bodyB64": b64(Y), "compare": {"headers": ["Accept"]}})
steps.append({"id": "no-ct-no-body", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "none", "compare": {"headers": ["Accept"]}})
steps.append({"id": "json-no-body", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "application/json", "compare": {"headers": ["Accept"]}})
steps.append({"id": "text-no-body", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "text/plain", "compare": {"headers": ["Accept"]}})
steps.append({"id": "two-ct-headers", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "none", "headers": {"Content-Type": "text/plain"}, "bodyB64": b64(Y), "compare": {"headers": ["Accept"]}})
out.append({"name": "F5-content-type", "tags": ["F5", "request"], "dbdiff": "step", "steps": steps})

O = "https://www.agora.gouv.fr"
tab = "b64:" + b64(b"a\tb")
utf8dash = "b64:" + b64("café – x".encode())
utf8e = "b64:" + b64("café".encode())
fw = [
 {"id": "auth-tab", "path": "/thematiques", "headers": {"Authorization": "b64:" + b64(b"Bearer a\tb")}},
 {"id": "auth-latin1", "path": "/thematiques", "headers": {"Authorization": "b64:" + b64("Bearer é".encode())}},
 {"id": "unread-header-tab", "path": "/thematiques", "headers": {"X-Foo": tab}},
 {"id": "cors-header-tab", "path": "/thematiques", "headers": {"Origin": O, "X-Foo": tab}},
 {"id": "cors-header-utf8-dash", "path": "/thematiques", "headers": {"Origin": O, "X-Foo": utf8dash}},
 {"id": "cors-header-utf8-e", "path": "/thematiques", "headers": {"Origin": O, "X-Foo": utf8e}},
 {"id": "cors-evil-header-tab", "path": "/thematiques", "headers": {"Origin": "https://evil.com", "X-Foo": tab}},
 {"id": "etag-inm-tab", "path": "/thematiques", "headers": {"If-None-Match": "b64:" + b64(b'"x\tb"')}},
 {"id": "no-etag-inm-tab", "path": "/theme_hebdo", "headers": {"If-None-Match": "b64:" + b64(b'"x\tb"')}},
 {"id": "body-other-header-tab", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "application/json", "bodyB64": b64(Y), "headers": {"X-Foo": tab}},
 {"id": "get-profile-header-tab", "path": "/profile", "as": "{{seed.UserRegular1}}", "headers": {"X-Foo": tab}},
 {"id": "unknown-anon-cors-tab", "path": "/nope", "headers": {"Origin": O, "X-Foo": tab}},
 {"id": "unknown-anon-cors-evil-tab", "path": "/nope", "headers": {"Origin": "https://evil.com", "X-Foo": tab}},
 {"id": "unknown-authed-cors-tab", "path": "/nope", "as": "{{seed.UserRegular1}}", "headers": {"Origin": O, "X-Foo": tab}},
 {"id": "protected-anon-cors-tab", "path": "/profile", "headers": {"Origin": O, "X-Foo": tab}},
 {"id": "accept-tab-inner", "path": "/thematiques", "headers": {"Accept": "b64:" + b64(b"application/json,\ttext/plain")}},
 {"id": "accept-c1", "path": "/thematiques", "headers": {"Accept": utf8dash}},
 {"id": "accept-c1-404", "path": "/nope", "as": "{{seed.UserRegular1}}", "headers": {"Accept": utf8dash}},
 {"id": "accept-c1-401", "path": "/nope", "headers": {"Accept": utf8dash}},
 {"id": "accept-c1-mediatype", "path": "/thematiques?mediaType=json", "headers": {"Accept": utf8dash}},
 {"id": "origin-c1", "path": "/thematiques", "headers": {"Origin": "b64:" + b64("https://www.agora.gouv.fr/–".encode())}},
 {"id": "origin-tab", "path": "/thematiques", "headers": {"Origin": "b64:" + b64(b"https://www.\tagora.gouv.fr")}},
 {"id": "header-name-only-read-later", "path": "/thematiques", "headers": {"Cookie": tab}},
]
for s in fw:
    s.setdefault("method", "GET")
    s["compare"] = {"headers": ["Access-Control-Allow-Origin", "Content-Language"]}
out.append({"name": "F5-header-firewall", "tags": ["F5", "request"], "steps": fw})

cors = [
 {"id": "known-cors-wild", "path": "/thematiques", "contentType": "*/*", "headers": {"Origin": O}},
 {"id": "known-cors-evil-wild", "path": "/thematiques", "contentType": "*/*", "headers": {"Origin": "https://evil.com"}},
 {"id": "known-no-cors-wild", "path": "/thematiques", "contentType": "*/*"},
 {"id": "known-cors-wild-suffix", "path": "/thematiques", "contentType": "application/*+json", "headers": {"Origin": O}},
 {"id": "known-cors-wild-suffix-charset", "path": "/thematiques", "contentType": "application/*+json;charset=utf-8", "headers": {"Origin": O}},
 {"id": "known-cors-invalid-ct", "path": "/thematiques", "contentType": "foo", "headers": {"Origin": O}},
 {"id": "known-cors-bad-charset", "path": "/thematiques", "contentType": "application/json;charset=foo", "headers": {"Origin": O}},
 {"id": "known-cors-app-wild", "path": "/thematiques", "contentType": "application/*", "headers": {"Origin": O}},
 {"id": "known-cors-star", "path": "/thematiques", "contentType": "*", "headers": {"Origin": O}},
 {"id": "preflight-wild", "method": "OPTIONS", "path": "/thematiques", "contentType": "*/*", "headers": {"Origin": O, "Access-Control-Request-Method": "GET"}},
 {"id": "preflight-unknown-wild", "method": "OPTIONS", "path": "/nope", "contentType": "*/*", "headers": {"Origin": O, "Access-Control-Request-Method": "GET"}},
 {"id": "unknown-anon-cors-wild", "path": "/nope", "contentType": "*/*", "headers": {"Origin": O}},
 {"id": "unknown-anon-cors-evil-wild", "path": "/nope", "contentType": "*/*", "headers": {"Origin": "https://evil.com"}},
 {"id": "unknown-authed-cors-wild", "path": "/nope", "as": "{{seed.UserRegular1}}", "contentType": "*/*", "headers": {"Origin": O}},
 {"id": "protected-anon-cors-wild", "path": "/profile", "contentType": "*/*", "headers": {"Origin": O}},
 {"id": "protected-authed-cors-wild", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "*/*", "headers": {"Origin": O}},
 {"id": "post-profile-cors-ok", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "application/json", "bodyB64": b64(Y), "headers": {"Origin": O}},
 {"id": "post-profile-cors-wild", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "*/*", "bodyB64": b64(Y), "headers": {"Origin": O}},
 {"id": "post-profile-cors-bad-charset", "method": "POST", "path": "/profile", "as": "{{seed.UserRegular1}}", "contentType": "application/json;charset=foo", "bodyB64": b64(Y), "headers": {"Origin": O}},
]
for s in cors:
    s.setdefault("method", "GET")
    s["compare"] = {"headers": ["Access-Control-Allow-Origin", "Content-Language", "Accept"]}
out.append({"name": "F5-cors-content-type", "tags": ["F5", "request"], "steps": cors})

import yaml
sys.stdout.write("# Generated by the lead (request layer: body bytes, Content-Type parsing, header firewall, CORS).\n")
sys.stdout.write("# Behaviours captured on the Kotlin reference; see DIVERGENCES.md for the residual differences.\n")
sys.stdout.write(yaml.safe_dump_all(out, sort_keys=False, allow_unicode=False, width=200))

misc = [
 {"id": "q-invalid-utf8", "path": "/thematiques?mediaType=xm%FF"},
 {"id": "q-truncated-utf8", "path": "/thematiques?mediaType=xml%C3"},
 {"id": "q-second-value-invalid", "path": "/thematiques?mediaType=xml&mediaType=%FF"},
 {"id": "q-invalid-name", "path": "/thematiques?media%FFType=xml"},
 {"id": "q-bad-escape-skipped", "path": "/thematiques?mediaType=x%zz"},
 {"id": "q-bad-escape-other", "path": "/thematiques?mediaType=xml&x=%zz"},
 {"id": "q-plus-space", "path": "/thematiques?mediaType=+xml"},
 {"id": "q-empty-name", "path": "/thematiques?=xml&mediaType=json"},
 {"id": "path-invalid-utf8", "path": "/qags/%FF", "as": "{{seed.UserRegular1}}"},
 {"id": "path-truncated-utf8", "path": "/nope/%E2%82"},
 {"id": "path-overlong", "path": "/%C0%AF"},
 {"id": "path-valid-utf8", "path": "/thematiques/%C3%A9"},
 {"id": "signup-ua-tab", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "User-Agent": tab}},
 {"id": "signup-ua-c1", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "User-Agent": utf8dash}},
 {"id": "signup-platform-tab", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "b64:" + b64(b"andr\toid")}},
 {"id": "signup-xra-tab-unread", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "X-Forwarded-For": "203.0.113.9", "X-Remote-Address": tab}},
 {"id": "signup-xra-tab-read", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "X-Remote-Address": tab}},
 {"id": "signup-xff-c1", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "X-Forwarded-For": utf8dash}},
 {"id": "signup-multi-fcm", "method": "POST", "path": "/signup", "headers": {"versionCode": "20", "platform": "android", "fcmToken": "a"}, "extraHeaders": [["fcmToken", "b"]]},
]
for s in misc:
    s.setdefault("method", "GET")
    s["compare"] = {"headers": ["Content-Language"]}
    s.pop("extraHeaders", None)
out2 = [{"name": "F5-params-path-headers", "tags": ["F5", "request"], "dbdiff": "step", "steps": misc}]
sys.stdout.write("---\n" + yaml.safe_dump_all(out2, sort_keys=False, allow_unicode=False, width=200))
