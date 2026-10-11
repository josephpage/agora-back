"""Generates parity/scenarios/S6_*.yaml (slice S6, consultation responses and results) and the Strapi variants
parity/fixtures/strapi/variants/s6-*.json.

    python3 -I parity/scenarios/gen/s6/gen_all.py parity/scenarios
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import helpers  # noqa: E402
from helpers import common  # noqa: E402

import answers  # noqa: E402
import knowndiv  # noqa: E402
import more  # noqa: E402
import results  # noqa: E402
import tsvgen  # noqa: E402


def main(out_dir):
    def w(name, scenarios, comment=None):
        common.dump(scenarios, os.path.join(out_dir, name), comment)
        print("%s: %d scenarios, %d steps" % (name, len(scenarios), sum(len(s["steps"]) for s in scenarios)))

    w("S6_answer.yaml", answers.happy(), "Valid answers to consultations 1 and 2, their effects (answered lists, results, preview).")
    w("S6_answer_texts.yaml", answers.texts(), "Sanitized texts (200 / 400 UTF-16 units) and lengths.")
    w("S6_answer_invalid.yaml", answers.rejections(), "Rejected answers (HTTP 400): validation of every question type, consultations that cannot be answered.")
    w("S6_answer_bodies.yaml", more.bodies() + more.http_matrix(), "Request bodies, content types, null elements and the HTTP matrix of the submission.")
    w("S6_ratelimit.yaml", more.ratelimit(), "consultationResponseRateLimit: 20 requests per hour and per IP hash, counters shared with the Kotlin backend.")
    results.write_variants()
    w("S6_results.yaml", results.seeded() + results.data_scenarios(), "Results of a consultation (cache \"consultationResults\"): seeded consultations, routes, crafted data.")
    w("S6_variants.yaml", results.variant_scenarios() + results.fault_scenarios(), "Consultations of Strapi variants (parity/fixtures/strapi/variants/s6-questions.json) and Strapi failures.")
    w("S6_tsv.yaml", tsvgen.seeded() + tsvgen.data(), "Admin export of the answers with the demographic information (TSV).")
    w("S6_known_divergences.yaml", knowndiv.scenarios(), "Skipped: known divergences (see parity/divergences/S6.md).")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "..", ".."))
