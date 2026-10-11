"""Scenarios skipped on purpose: the known divergences of S6."""
from helpers import step, scenario, setup, TAG, qid, cid_, body, r_, C2, IDLE, R, xff


def scenarios():
    ans = [r_(qid(2, 1), [cid_(2, 1, 1)]), r_(qid(2, 2), [cid_(2, 2, 1)])]
    out = []
    # B-AGORAQUEUE: six simultaneous answers of ONE user. The Kotlin queue checks, then adds: on a slow machine several requests
    # pass (several 200, duplicated rows); Go lets exactly one in (one 200, five 400).
    out.append(scenario("S6-known-agora-queue-race", [
        step("parallel-same-user", "/consultations/%s/responses" % C2, **{"method": "POST", "body": body(ans), "as": IDLE, "headers": xff(1), "parallel": 6}),
    ], TAG, dbdiff="end", setup=setup(), skip="B-AGORAQUEUE: simultaneous requests of one user are not deterministic on the Kotlin side (see parity/divergences/S6.md)"))
    # C-XML-BODY: Kotlin reads an XML body, Go refuses it (400)
    out.append(scenario("S6-known-xml-request-body", [
        step("xml-body", "/consultations/%s/responses" % C2, **{"method": "POST", "bodyRaw": "<ReponsesConsultationJson><consultationId>x</consultationId><responses><responses><questionId>%s</questionId><choiceIds><choiceIds>%s</choiceIds></choiceIds></responses></responses></ReponsesConsultationJson>" % (qid(2, 1), cid_(2, 1, 1)),
                                                                   "contentType": "application/xml", "as": R[4], "headers": xff(2)}),
    ], TAG, dbdiff="end", setup=setup(), skip="C-XML-BODY (DIVERGENCES.md): an XML request body is refused by Go"))
    return out
