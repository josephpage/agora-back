"""Generates parity/scenarios/S9_*.yaml (slice S9, CMS content & misc).

    python3 parity/scenarios/gen/s9/gen_all.py parity/scenarios

Scenarios tagged S9-known are written to S9_known_foundation_diffs.yaml: they
document differences caused by foundation packages (see parity/ledger/S9.md) and
are not part of the `-tags S9` run.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import common
import fiches
import news_charter
import pages
import feedback

outdir = sys.argv[1]


def split(scs):
    main = [s for s in scs if "S9-known" not in s["tags"]]
    known = [s for s in scs if "S9-known" in s["tags"]]
    return main, known


known = []
for fname, scs in [
    ("S9_content_pages.yaml", pages.run()),
    ("S9_news.yaml", news_charter.run_news()),
    ("S9_charter.yaml", news_charter.run_charter()),
    ("S9_fiches.yaml", fiches.run()),
    ("S9_feedback.yaml", feedback.run()),
]:
    main, k = split(scs)
    common.dump(main, os.path.join(outdir, fname))
    known += k
known += fiches.run_known()
common.dump(known, os.path.join(outdir, "S9_known_foundation_diffs.yaml"),
            comment="Differences caused by the foundation packages (httpx, xmljava, javacompat, jsonjava): see the\n"
                    "\"Foundation findings\" of parity/ledger/S9.md. Tag S9-known, not part of -tags S9.")
