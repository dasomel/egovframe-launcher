#!/usr/bin/env bash
set -euo pipefail
echo "== eGovFrame VSCode 핸즈온 사전 점검 =="
need() {
  if command -v "$1" >/dev/null 2>&1; then
    printf "  [O] %-8s %s\n" "$1" "$($1 --version 2>&1 | head -n1)"
  else
    printf "  [X] %-8s (미설치)\n" "$1"
  fi
}
need git
need mvn
need node
need npm
need go
need docker
echo "JDK: ${JAVA_HOME:-(JAVA_HOME 미설정)}; java -version:"
java -version 2>&1 | head -n1 || echo "  java 미설치"
