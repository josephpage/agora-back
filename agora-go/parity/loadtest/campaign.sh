#!/bin/bash
# Load campaign (PERF.md §2.3) on the slot-0 parity environment: both databases
# seeded at the same scale with the hot-path indexes, then each backend alone in
# a cgroup of the size of a Scalingo container, every traffic profile in turn.
#   parity/loadtest/campaign.sh
# Environment: SCALE (5000) C virtual users (200) D measured (60s) W warmup (20s)
#   CPUS (1) MEM in MB (1024) USERS distinct users (5000) RESEED (1)
#   PROFILES ("app-open consultation support web") SIDES ("ref go") OUT (report dir)
# The parity scenarios must not run meanwhile (the slot-0 lock is taken). At the
# end the indexes are dropped again (the CI scenarios run without them) and both
# backends restart outside the cgroups; the next scenario reseeds at scale 1.
set -euo pipefail
cd "$(dirname "$0")/../.."
RUN=${PARITY_RUN_DIR:-/home/user/run}
SCALE=${SCALE:-5000}; C=${C:-200}; D=${D:-60s}; W=${W:-20s}; CPUS=${CPUS:-1}; MEM=${MEM:-1024}
USERS=${USERS:-5000}; RESEED=${RESEED:-1}
PROFILES=${PROFILES:-"app-open consultation support web"}; SIDES=${SIDES:-"ref go"}
OUT=${OUT:-$RUN/load}
mkdir -p "$OUT"
exec 9>"$RUN/test.lock"
flock -n 9 || { echo "a parity run holds slot 0" >&2; exit 3; }

declare -A PORT=([ref]=8081 [go]=8082) REDIS=([ref]=6380 [go]=6381) PASS=([ref]=refpass [go]=gopass)
declare -A DB=([ref]=postgres://backend:agora_password@localhost:5432/agora_ref [go]=postgres://backend:agora_password@localhost:5432/agora_go)
JWT_SECRET=$(grep '^JWT_SECRET=' parity/env/common.env | cut -d= -f2-)

# 9>&-: the long-lived processes started below must not inherit (and keep) the lock
./parity/run.sh up >/dev/null 9>&-
go build -o "$RUN/agora" ./cmd/agora
go build -o "$RUN/loadtest" ./parity/loadtest
go build -o "$RUN/seed" ./parity/seed/cmd/seed

stop_backends() { fuser -k 8081/tcp 8082/tcp >/dev/null 2>&1 || true; sleep 3; }
stop_backends
if [ "$RESEED" = 1 ]; then
  NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  echo "seed -scale $SCALE (now $NOW)"
  "$RUN/seed" -db "${DB[ref]}" -scale "$SCALE" -now "$NOW" -q & p1=$!
  "$RUN/seed" -db "${DB[go]}" -scale "$SCALE" -now "$NOW" -q & p2=$!
  wait $p1 $p2
fi
for s in ref go; do DATABASE_URL=${DB[$s]} "$RUN/agora" --migrate=up >/dev/null; done

cg() { # side → cgroup name
  echo "agora-load-$1"
}
setup_cgroup() {
  local name; name=$(cg "$1")
  for c in cpu memory; do mkdir -p /sys/fs/cgroup/$c/$name; done
  echo 100000 > /sys/fs/cgroup/cpu/$name/cpu.cfs_period_us
  local quota; quota=$(python3 -c "print(int(float('$CPUS')*100000))")
  echo "$quota" > /sys/fs/cgroup/cpu/$name/cpu.cfs_quota_us
  echo $((MEM * 1024 * 1024)) > /sys/fs/cgroup/memory/$name/memory.limit_in_bytes
  echo 0 > /sys/fs/cgroup/memory/$name/memory.max_usage_in_bytes 2>/dev/null || true
}

for side in $SIDES; do
  stop_backends
  for s in ref go; do redis-cli -p ${REDIS[$s]} -a ${PASS[$s]} --no-auth-warning flushall >/dev/null; done
  setup_cgroup "$side"
  name=$(cg "$side")
  # started from a shell already inside the cgroup: the JVM / Go runtime see the limits from the start
  ( for c in cpu memory; do echo $BASHPID > /sys/fs/cgroup/$c/$name/cgroup.procs; done
    exec ./parity/run.sh restart-$side ) >/dev/null 9>&-
  for p in $PROFILES; do
    echo "== $side $p ($CPUS CPU, $MEM MB, -c $C, $W + $D)"
    "$RUN/loadtest" -target "http://localhost:${PORT[$side]}" -profile "$p" -c "$C" -d "$D" -warmup "$W" \
      -users "$USERS" -users-db "${DB[$side]}" -jwt-secret "$JWT_SECRET" -report "$OUT/$side-$p.json" | tee "$OUT/$side-$p.txt"
    echo "peak memory $(( $(cat /sys/fs/cgroup/memory/$name/memory.max_usage_in_bytes) / 1048576 )) MB, throttled $(grep throttled_time /sys/fs/cgroup/cpu/$name/cpu.stat | awk '{print int($2/1e9)}') s" | tee -a "$OUT/$side-$p.txt"
  done
done

stop_backends
for s in ref go; do DATABASE_URL=${DB[$s]} "$RUN/agora" --migrate=down >/dev/null; done
./parity/run.sh restart-ref >/dev/null 9>&-
./parity/run.sh restart-go >/dev/null 9>&-
echo "reports in $OUT"
