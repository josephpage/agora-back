"""Generates parity/scenarios/S4_*.yaml (slice S4, consultation details).

    python3 parity/scenarios/gen/s4/gen_all.py parity/scenarios
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import common
import details
import feedback
import known
import variants

outdir = sys.argv[1]

for fname, scs in [
    ("S4_details.yaml", details.run_details()),
    ("S4_updates.yaml", details.run_updates()),
    ("S4_preview.yaml", details.run_preview()),
    ("S4_questions.yaml", details.run_questions()),
    ("S4_feedback.yaml", feedback.run()),
    ("S4_variants.yaml", variants.run()),
    ("S4_known_foundation_diffs.yaml", known.run()),
]:
    common.dump(scs, os.path.join(outdir, fname))
    print(fname, len(scs), "scenarios,", sum(len(s["steps"]) for s in scs), "steps")
