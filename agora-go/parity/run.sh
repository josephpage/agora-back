#!/bin/bash
# Parity harness environment manager.
#   parity/run.sh up          start PG, 2 Redis, 2 fake Strapi, Kotlin ref (:8081), Go (:8082)
#   parity/run.sh restart-go  rebuild and restart the Go backend
#   parity/run.sh restart-ref restart the Kotlin reference
#   parity/run.sh test [args] run the differential scenarios (go run ./parity/cmd/parity args)
#   parity/run.sh down        stop everything
set -euo pipefail
cd "$(dirname "$0")/.."
RUN=${PARITY_RUN_DIR:-/home/user/run}
mkdir -p "$RUN"
REFJAR=${PARITY_REF_JAR:-/home/user/agora-ref/build/libs/agora-back-0.0.1.jar}
JAVA17=${PARITY_JAVA:-/usr/lib/jvm/java-17-openjdk-amd64/bin/java}
STRAPI_NOW_FILE="$RUN/strapi_now"

load_env() { # file
  while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    export "${line?}"
  done < "$1"
}

wait_http() { # url name
  for _ in $(seq 1 120); do
    if curl -s -o /dev/null "$1"; then return 0; fi
    sleep 1
  done
  echo "timeout waiting for $2" >&2; return 1
}

start_pg() {
  pg_lsclusters 2>/dev/null | grep -q online || pg_ctlcluster 16 main start
  for db in agora_ref agora_go; do
    su postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='$db'\"" | grep -q 1 || su postgres -c "psql -q -c 'CREATE DATABASE $db OWNER backend'"
    PGPASSWORD=agora_password psql -q -h localhost -U backend -d $db -f internal/store/schema/baseline.sql >/dev/null 2>&1
  done
}

start_redis() {
  redis-cli -p 6380 -a refpass ping >/dev/null 2>&1 || redis-server --port 6380 --requirepass refpass --daemonize yes --save "" --appendonly no --logfile "$RUN/redis-ref.log"
  redis-cli -p 6381 -a gopass ping >/dev/null 2>&1 || redis-server --port 6381 --requirepass gopass --daemonize yes --save "" --appendonly no --logfile "$RUN/redis-go.log"
}

start_strapi() {
  [ -f "$STRAPI_NOW_FILE" ] || date -u +%Y-%m-%dT%H:%M:%SZ > "$STRAPI_NOW_FILE"
  local now; now=$(cat "$STRAPI_NOW_FILE")
  go build -o "$RUN/fakestrapi" ./parity/fakestrapi/cmd/fakestrapi
  for port in 1337 1338; do
    if ! curl -s -o /dev/null "http://localhost:$port/__control/requests"; then
      nohup "$RUN/fakestrapi" -addr ":$port" -fixtures "$PWD/parity/fixtures/strapi" -now "$now" > "$RUN/fakestrapi-$port.log" 2>&1 &
    fi
  done
  wait_http http://localhost:1337/__control/requests strapi1337
  wait_http http://localhost:1338/__control/requests strapi1338
}

start_ref() {
  if curl -s -o /dev/null http://localhost:8081/thematiques; then return 0; fi
  ( set -a; load_env parity/env/common.env; load_env parity/env/ref.env; set +a
    nohup "$JAVA17" -Duser.timezone="${TZ:-UTC}" -Xmx1g -jar "$REFJAR" > "$RUN/ref.log" 2>&1 & )
  wait_http http://localhost:8081/thematiques ref
}

start_go() {
  go build -o "$RUN/agora" ./cmd/agora
  ( set -a; load_env parity/env/common.env; load_env parity/env/go.env; set +a
    nohup "$RUN/agora" > "$RUN/go.log" 2>&1 & )
  wait_http http://localhost:8082/thematiques go
}

stop_go() { pkill -f "$RUN/agora\$" 2>/dev/null || pkill -x agora 2>/dev/null || true; sleep 1; }
stop_ref() { pkill -f "agora-back-0.0.1.jar" 2>/dev/null || true; sleep 2; }

case "${1:-up}" in
  up) start_pg; start_redis; start_strapi; start_ref; start_go; echo "parity environment up" ;;
  restart-go) stop_go; start_go; echo "go restarted" ;;
  restart-ref) stop_ref; start_ref; echo "ref restarted" ;;
  test) shift; exec go run ./parity/cmd/parity "$@" ;;
  down) stop_go; stop_ref; pkill -f "$RUN/fakestrapi" || true; redis-cli -p 6380 -a refpass shutdown nosave 2>/dev/null || true; redis-cli -p 6381 -a gopass shutdown nosave 2>/dev/null || true ;;
  *) echo "usage: $0 up|restart-go|restart-ref|test|down" >&2; exit 2 ;;
esac
