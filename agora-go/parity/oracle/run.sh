#!/usr/bin/env bash
# Starts the JVM oracle (JSON lines over stdin/stdout, see java/Oracle.java).
# Environment variables (LOGIN_TOKEN_*, JWT_SECRET, REMOTE_ADDRESS_*...) are read by the Kotlin
# code through System.getenv: export them (e.g. from parity/env/common.env) before calling.
#
#   REFJAR_DIR         extracted reference jar (default /home/user/tools/refjar)
#   ORACLE_JAVA_HOME   JDK 17 (default /usr/lib/jvm/java-17-openjdk-amd64, then $JAVA_HOME, then PATH)
#   ORACLE_JAVA_OPTS   replaces the default JVM options
set -euo pipefail

ORACLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REFJAR_DIR="${REFJAR_DIR:-/home/user/tools/refjar}"
BUILD_DIR="$ORACLE_DIR/.build"

[ -f "$BUILD_DIR/Oracle.class" ] || { echo "run.sh: oracle not built, run build.sh first" >&2; exit 1; }

JAVA_DIR="${ORACLE_JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}"
if [ ! -x "$JAVA_DIR/bin/java" ]; then
  JAVA_DIR="${JAVA_HOME:-}"
fi
if [ -n "$JAVA_DIR" ] && [ -x "$JAVA_DIR/bin/java" ]; then
  JAVA="$JAVA_DIR/bin/java"
else
  JAVA="$(command -v java || true)"
fi
[ -n "$JAVA" ] || { echo "run.sh: no java found (set ORACLE_JAVA_HOME to a JDK 17)" >&2; exit 1; }

CP="$BUILD_DIR:$REFJAR_DIR/BOOT-INF/classes:$REFJAR_DIR/BOOT-INF/lib/*"

DEFAULT_OPTS="-XX:+UseSerialGC -Xmx1g -Xss8m -Dfile.encoding=UTF-8 -Djava.awt.headless=true"
# env overrides per call need System.getenv() to be patched through reflection
DEFAULT_OPTS="$DEFAULT_OPTS --add-opens java.base/java.util=ALL-UNNAMED --add-opens java.base/java.lang=ALL-UNNAMED"
OPTS="${ORACLE_JAVA_OPTS:-$DEFAULT_OPTS}"

unset JAVA_TOOL_OPTIONS JDK_JAVA_OPTIONS
# exec: the JVM replaces this shell, so killing the pid kills the JVM.
# shellcheck disable=SC2086
exec "$JAVA" $OPTS \
  -Dlogback.configurationFile="$ORACLE_DIR/java/logback-oracle.xml" \
  -cp "$CP" Oracle
