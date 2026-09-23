#!/bin/sh
# Запуск MCP без лишнего вывода Gradle в stdout.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
JAR="$ROOT/build/libs/open-meteo-0.1.0-all.jar"

if [ ! -f "$JAR" ]; then
  echo "Нет $JAR. Сначала: ./gradlew shadowJar" >&2
  exit 1
fi

pick_java() {
  if [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/java" ]; then
    echo "$JAVA_HOME/bin/java"
    return
  fi
  if command -v java >/dev/null 2>&1; then
    echo java
    return
  fi
  find "$HOME/.gradle/jdks" -type f -path '*/bin/java' 2>/dev/null | while read -r candidate; do
    if "$candidate" -version 2>&1 | grep -Eq 'version "(1[7-9]|[2-9][0-9])'; then
      echo "$candidate"
      break
    fi
  done
}

JAVA=$(pick_java)
if [ -z "$JAVA" ]; then
  echo "Нужен JDK 17+. Поставьте его или укажите JAVA_HOME." >&2
  exit 1
fi

exec "$JAVA" -Dkotlin-logging.logStartupMessage=false -jar "$JAR"
