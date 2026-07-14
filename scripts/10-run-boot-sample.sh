#!/usr/bin/env bash
set -euo pipefail
WORK="${1:-./.work}"
cd "$WORK/egovframe-boot-sample-java-config"
mvn -B spring-boot:run
