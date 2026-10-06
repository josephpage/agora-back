#!/usr/bin/env bash
# Compiles the JVM oracle (parity/oracle/java/*.java) against the extracted reference jar.
#
#   REFJAR_DIR         extracted reference jar (default /home/user/tools/refjar)
#   ORACLE_JAVA_HOME   JDK 17 (default /usr/lib/jvm/java-17-openjdk-amd64, then $JAVA_HOME, then PATH)
#   FORCE=1            rebuild even if up to date
#   build.sh --check   exit 0 if the build is up to date, 1 otherwise (builds nothing)
set -euo pipefail

ORACLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REFJAR_DIR="${REFJAR_DIR:-/home/user/tools/refjar}"
SRC_DIR="$ORACLE_DIR/java"
BUILD_DIR="$ORACLE_DIR/.build"
STAMP="$BUILD_DIR/.stamp"
WANT_STAMP="refjar=$REFJAR_DIR"

up_to_date() {
  [ -f "$BUILD_DIR/Oracle.class" ] && [ -f "$STAMP" ] || return 1
  [ "$(cat "$STAMP")" = "$WANT_STAMP" ] || return 1
  [ -z "$(find "$SRC_DIR" "$ORACLE_DIR/build.sh" -type f -newer "$STAMP" -print -quit)" ] || return 1
}

if [ "${1:-}" = "--check" ]; then
  up_to_date
  exit $?
fi

if [ "${FORCE:-0}" != "1" ] && up_to_date; then
  exit 0
fi

JAVA_DIR="${ORACLE_JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}"
if [ ! -x "$JAVA_DIR/bin/javac" ]; then
  JAVA_DIR="${JAVA_HOME:-}"
fi
if [ -n "$JAVA_DIR" ] && [ -x "$JAVA_DIR/bin/javac" ]; then
  JAVAC="$JAVA_DIR/bin/javac"
else
  JAVAC="$(command -v javac || true)"
fi
[ -n "$JAVAC" ] || { echo "build.sh: no javac found (set ORACLE_JAVA_HOME to a JDK 17)" >&2; exit 1; }
[ -d "$REFJAR_DIR/BOOT-INF/classes" ] || { echo "build.sh: $REFJAR_DIR/BOOT-INF/classes not found (set REFJAR_DIR)" >&2; exit 1; }

# Serialize concurrent builds (go test runs packages as parallel processes).
if command -v flock >/dev/null 2>&1; then
  exec 9>"$ORACLE_DIR/.build.lock"
  flock 9
  if [ "${FORCE:-0}" != "1" ] && up_to_date; then
    exit 0
  fi
fi

CP="$REFJAR_DIR/BOOT-INF/classes:$REFJAR_DIR/BOOT-INF/lib/*"
TMP="$ORACLE_DIR/.build-tmp.$$"
rm -rf "$TMP"
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT

unset JAVA_TOOL_OPTIONS JDK_JAVA_OPTIONS
"$JAVAC" -encoding UTF-8 --release 17 -proc:none -Xlint:-options -nowarn \
  -cp "$CP" -d "$TMP" "$SRC_DIR"/*.java

printf '%s' "$WANT_STAMP" > "$TMP/.stamp"

rm -rf "$ORACLE_DIR/.build.old"
if [ -d "$BUILD_DIR" ]; then
  mv "$BUILD_DIR" "$ORACLE_DIR/.build.old"
fi
mv "$TMP" "$BUILD_DIR"
rm -rf "$ORACLE_DIR/.build.old"
echo "build.sh: oracle built in $BUILD_DIR" >&2
