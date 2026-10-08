#!/bin/bash
# Parity harness environment manager.
#   parity/run.sh up          start PG, 2 Redis, 2 fake Strapi, Kotlin ref (:8081), Go (:8082)
#   parity/run.sh restart-go  rebuild and restart the Go backend
#   parity/run.sh restart-ref restart the Kotlin reference
#   parity/run.sh test [args] run the differential scenarios (go run ./parity/cmd/parity args)
#   parity/run.sh down        stop everything
set -euo pipefail
cd "$(dirname "$0")/.."
SLOT=${PARITY_SLOT:-0}
if [ "$SLOT" = "0" ]; then
  RUN=${PARITY_RUN_DIR:-/home/user/run}
  REF_PORT=8081; GO_PORT=8082; REF_REDIS=6380; GO_REDIS=6381; REF_STRAPI=1337; GO_STRAPI=1338
  REF_DB=agora_ref; GO_DB=agora_go
else
  RUN=${PARITY_RUN_DIR:-/home/user/run/slot$SLOT}
  REF_PORT=$((8100 + SLOT * 10 + 1)); GO_PORT=$((8100 + SLOT * 10 + 2))
  REF_REDIS=$((6400 + SLOT * 10)); GO_REDIS=$((6400 + SLOT * 10 + 1))
  REF_STRAPI=$((1400 + SLOT * 10)); GO_STRAPI=$((1400 + SLOT * 10 + 1))
  REF_DB=agora_ref_$SLOT; GO_DB=agora_go_$SLOT
fi
mkdir -p "$RUN"
export PARITY_REF_URL=http://localhost:$REF_PORT PARITY_GO_URL=http://localhost:$GO_PORT
export PARITY_REF_DB=postgres://backend:agora_password@localhost:5432/$REF_DB PARITY_GO_DB=postgres://backend:agora_password@localhost:5432/$GO_DB
export PARITY_REF_REDIS=localhost:$REF_REDIS PARITY_GO_REDIS=localhost:$GO_REDIS PARITY_REF_REDIS_PASS=refpass PARITY_GO_REDIS_PASS=gopass
export PARITY_REF_STRAPI=http://localhost:$REF_STRAPI PARITY_GO_STRAPI=http://localhost:$GO_STRAPI
export PARITY_REF_CMD="$RUN/start-ref.sh" PARITY_GO_CMD="$RUN/start-go.sh"
REFJAR=${PARITY_REF_JAR:-/home/user/agora-ref/build/libs/agora-back-0.0.1.jar}
JAVA17=${PARITY_JAVA:-/usr/lib/jvm/java-17-openjdk-amd64/bin/java}
# one Strapi clock for every slot of a machine (fixture dates are relative to it)
STRAPI_NOW_FILE=${PARITY_STRAPI_NOW_FILE:-${PARITY_RUN_DIR:-/home/user/run}/strapi_now}

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
  # PARITY_PG_EXTERNAL=1: PostgreSQL is provided (CI service), user backend/agora_password
  if [ "${PARITY_PG_EXTERNAL:-0}" != "1" ]; then
    pg_lsclusters 2>/dev/null | grep -q online || pg_ctlcluster 16 main start
  fi
  for db in $REF_DB $GO_DB; do
    if ! PGPASSWORD=agora_password psql -h localhost -U backend -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$db'" | grep -q 1; then
      PGPASSWORD=agora_password psql -q -h localhost -U backend -d postgres -c "CREATE DATABASE $db OWNER backend"
    fi
    PGPASSWORD=agora_password psql -q -h localhost -U backend -d $db -f internal/store/schema/baseline.sql >/dev/null 2>&1
    # The order of a SELECT DISTINCT without ORDER BY (it feeds Strapi URIs, see
    # parity/ledger/S4.md) must not depend on JDBC vs pgx switching to a generic
    # plan after 5 executions of a prepared statement: always plan with the values.
    PGPASSWORD=agora_password psql -q -h localhost -U backend -d postgres -c "ALTER DATABASE $db SET plan_cache_mode = force_custom_plan" >/dev/null
  done
}

start_redis() {
  redis-cli -p $REF_REDIS -a refpass ping >/dev/null 2>&1 || redis-server --port $REF_REDIS --requirepass refpass --daemonize yes --save "" --appendonly no --logfile "$RUN/redis-ref.log"
  redis-cli -p $GO_REDIS -a gopass ping >/dev/null 2>&1 || redis-server --port $GO_REDIS --requirepass gopass --daemonize yes --save "" --appendonly no --logfile "$RUN/redis-go.log"
}

start_strapi() {
  [ -f "$STRAPI_NOW_FILE" ] || date -u +%Y-%m-%dT%H:%M:%SZ > "$STRAPI_NOW_FILE"
  local now; now=$(cat "$STRAPI_NOW_FILE")
  go build -o "$RUN/fakestrapi" ./parity/fakestrapi/cmd/fakestrapi
  for port in $REF_STRAPI $GO_STRAPI; do
    if ! curl -s -o /dev/null "http://localhost:$port/__control/requests"; then
      nohup "$RUN/fakestrapi" -addr ":$port" -fixtures "$PWD/parity/fixtures/strapi" -now "$now" > "$RUN/fakestrapi-$port.log" 2>&1 &
    fi
  done
  wait_http http://localhost:$REF_STRAPI/__control/requests strapi-ref
  wait_http http://localhost:$GO_STRAPI/__control/requests strapi-go
}

write_envs() {
  cat > "$RUN/ref.env" <<EOT
AGORA_PORT=$REF_PORT
PORT=$REF_PORT
DATABASE_URL=postgresql://backend:agora_password@localhost:5432/$REF_DB
REDIS_URL=redis://default:refpass@localhost:$REF_REDIS
CMS_API_URL=http://localhost:$REF_STRAPI/api/
EOT
  cat > "$RUN/go.env" <<EOT
AGORA_PORT=$GO_PORT
PORT=$GO_PORT
DATABASE_URL=postgresql://backend:agora_password@localhost:5432/$GO_DB
REDIS_URL=redis://default:gopass@localhost:$GO_REDIS
CMS_API_URL=http://localhost:$GO_STRAPI/api/
AGORA_BOOTSTRAP_SCHEMA=true
EOT
  # optional extra variables for both sides (e.g. ACME stub mode)
  if [ -n "${PARITY_EXTRA_ENV:-}" ] && [ -f "$PARITY_EXTRA_ENV" ]; then
    cat "$PARITY_EXTRA_ENV" >> "$RUN/ref.env"
    cat "$PARITY_EXTRA_ENV" >> "$RUN/go.env"
  fi
  # side-specific extras ({REF_PORT}/{GO_PORT} placeholders are substituted)
  if [ -n "${PARITY_EXTRA_ENV_REF:-}" ] && [ -f "$PARITY_EXTRA_ENV_REF" ]; then
    sed -e "s/{REF_PORT}/$REF_PORT/g" -e "s/{GO_PORT}/$GO_PORT/g" "$PARITY_EXTRA_ENV_REF" >> "$RUN/ref.env"
  fi
  if [ -n "${PARITY_EXTRA_ENV_GO:-}" ] && [ -f "$PARITY_EXTRA_ENV_GO" ]; then
    sed -e "s/{REF_PORT}/$REF_PORT/g" -e "s/{GO_PORT}/$GO_PORT/g" "$PARITY_EXTRA_ENV_GO" >> "$RUN/go.env"
  fi
  local here="$PWD"
  cat > "$RUN/start-ref.sh" <<EOT
#!/bin/bash
set -a
while IFS= read -r line; do [[ -z "\$line" || "\$line" == \#* ]] && continue; export "\$line"; done < "$here/parity/env/common.env"
while IFS= read -r line; do [[ -z "\$line" || "\$line" == \#* ]] && continue; export "\$line"; done < "$RUN/ref.env"
set +a
exec "$JAVA17" -Duser.timezone=\${TZ:-UTC} -Xmx768m -jar "$REFJAR" "\$@"
EOT
  cat > "$RUN/start-go.sh" <<EOT
#!/bin/bash
set -a
while IFS= read -r line; do [[ -z "\$line" || "\$line" == \#* ]] && continue; export "\$line"; done < "$here/parity/env/common.env"
while IFS= read -r line; do [[ -z "\$line" || "\$line" == \#* ]] && continue; export "\$line"; done < "$RUN/go.env"
set +a
exec "$RUN/agora" "\$@"
EOT
  chmod +x "$RUN/start-ref.sh" "$RUN/start-go.sh"
}

start_ref() {
  write_envs
  if curl -s -o /dev/null http://localhost:$REF_PORT/thematiques; then return 0; fi
  nohup "$RUN/start-ref.sh" > "$RUN/ref.log" 2>&1 &
  wait_http http://localhost:$REF_PORT/thematiques ref
}

start_go() {
  write_envs
  go build -o "$RUN/agora" ./cmd/agora
  nohup "$RUN/start-go.sh" > "$RUN/go.log" 2>&1 &
  wait_http http://localhost:$GO_PORT/thematiques go
}

stop_go() { pkill -f "^$RUN/agora\$" 2>/dev/null || true; fuser -k $GO_PORT/tcp 2>/dev/null || true; sleep 1; }
# a restarted reference JVM has no stuck AgoraQueue slot (see parity/runner/kotlinlocks.go)
stop_ref() { fuser -k $REF_PORT/tcp 2>/dev/null || true; rm -f "$RUN/kotlin-queue-locks"; sleep 3; }

case "${1:-up}" in
  up) start_pg; start_redis; start_strapi; start_ref; start_go; echo "parity environment up" ;;
  restart-go) stop_go; start_go; echo "go restarted" ;;
  restart-ref) stop_ref; start_ref; echo "ref restarted" ;;
  test) shift; PARITY_KOTLIN_LOCKS="$RUN/kotlin-queue-locks" exec go run ./parity/cmd/parity "$@" ;;
  down) stop_go; stop_ref; fuser -k $REF_STRAPI/tcp $GO_STRAPI/tcp 2>/dev/null || true; redis-cli -p $REF_REDIS -a refpass shutdown nosave 2>/dev/null || true; redis-cli -p $GO_REDIS -a gopass shutdown nosave 2>/dev/null || true ;;
  *) echo "usage: $0 up|restart-go|restart-ref|test|down" >&2; exit 2 ;;
esac
