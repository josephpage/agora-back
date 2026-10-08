"""Scenarios that document a difference caused by a foundation package or by a Kotlin race (tag S4-known, skipped)."""
from common import *


def fb_path():
    return "/consultations/%s/updates/cu0000000000000000000001/feedback" % CONS[0]


def run():
    out = []
    steps = [step("details-answered-user-xml", "/v2/consultations/%s?mediaType=xml" % CONS[0], **{"as": U1})]
    steps += [step("details-xml-c%d" % i, "/v2/consultations/%s?mediaType=xml" % CONS[i - 1], **{"as": U1}) for i in (4, 5, 7)]
    steps += [step("update-xml-cu1", "/v2/consultations/%s/updates/cu0000000000000000000001?mediaType=xml" % CONS[0], **{"as": U1})]
    # was a foundation diff (xmljava wrote <goals/> for a null Kotlin List?): fixed, now an S4 scenario
    out.append(scenario("S4-xml-null-list", steps, tags=("S4",)))
    steps = [step("xml-body", fb_path(), method="POST", bodyRaw="<x><isPositive>true</isPositive></x>", contentType="application/xml", **{"as": IDLE})]
    out.append(scenario(
        "S4-known-xml-request-body", steps, tags=("S4-known",), dbdiff="step",
        skip="C-XML-BODY (DIVERGENCES.md): the reference decodes an application/xml request body (200), Go answers 400."))
    steps = [step("parallel", fb_path(), method="POST", body={"isPositive": True}, parallel=8, **{"as": U("UserProfile9")})]
    out.append(scenario(
        "S4-known-agora-queue-race", steps, tags=("S4-known",), dbdiff="step",
        skip="B-AGORAQUEUE: Kotlin checks canAddTask and adds the task in two steps, so two of 8 simultaneous requests of one user can both "
             "run (ref: [200 200 400x6], go: [200 400x7]); the Go queue is atomic."))
    return out
