#!/usr/bin/env bash
set -euo pipefail
WORK="${1:-./.work}"
mkdir -p "$WORK"
BASE="https://github.com/eGovFramework"
REPOS=(
  egovframe-boot-sample-java-config
  egovframe-template-simple-react
  egovframe-template-simple-backend
  egovframe-web-sample
  egovframe-simple-homepage-template
  egovframe-portal-site-template
)
for r in "${REPOS[@]}"; do
  if [ -d "$WORK/$r/.git" ]; then
    echo "skip (exists): $r"
  else
    git clone --depth 1 "$BASE/$r.git" "$WORK/$r"
  fi
done
echo "done → $WORK"
