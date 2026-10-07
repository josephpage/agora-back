#!/usr/bin/env bash
# Runs every query of hot_queries.txt N times on $DB (EXPLAIN ANALYZE) and
# prints the median execution time in ms and the top plan node.
#   DB=postgres://backend:agora_password@localhost:5432/agora_perf N=5 ./parity/perf/bench_queries.sh
set -euo pipefail
DB=${DB:-postgres://backend:agora_password@localhost:5432/agora_perf}
N=${N:-5}
dir="$(cd "$(dirname "$0")" && pwd)"
grep -v '^#' "$dir/hot_queries.txt" | while IFS='|' read -r id desc sql; do
  [ -z "$id" ] && continue
  times=()
  for _ in $(seq "$N"); do
    t=$(psql "$DB" -tA -c "EXPLAIN (ANALYZE, FORMAT JSON) $sql" | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["Execution Time"])')
    times+=("$t")
  done
  med=$(printf '%s\n' "${times[@]}" | sort -g | awk '{a[NR]=$1} END {print a[int((NR+1)/2)]}')
  scans=$(psql "$DB" -tA -c "EXPLAIN $sql" | grep -oE '(Seq Scan|Index Only Scan|Index Scan|Bitmap Index Scan) (using [a-z_0-9]+ )?on [a-z_]+' | sort | uniq -c | tr -s ' ' | tr '\n' ';')
  printf '%-4s %9.2f ms  %-45s %s\n' "$id" "$med" "$desc" "$scans"
done
