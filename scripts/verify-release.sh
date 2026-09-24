#!/usr/bin/env bash
set -euo pipefail

# Verifies a directory of downloaded eGovFrame Launcher release assets.
#
# Offline (default): checks SHA256SUMS against every file in the directory.
# --online: additionally runs `gh attestation verify` against each release
#           asset using GitHub's Sigstore-backed attestation API. Requires
#           the GitHub CLI (gh) on PATH and authenticated.
#
# Usage: scripts/verify-release.sh <assets-dir> [--online]
#
# Exit codes: 0 verified, 1 verification failed, 2 usage error.

REPO="dasomel/egovframe-launcher"

usage() {
  echo "Usage: $0 <assets-dir> [--online]" >&2
  exit 2
}

if [[ $# -lt 1 ]]; then
  usage
fi

ASSETS_DIR="$1"
shift

ONLINE=false
if [[ "${1:-}" == "--online" ]]; then
  ONLINE=true
elif [[ $# -gt 0 ]]; then
  usage
fi

if [[ ! -d "$ASSETS_DIR" ]]; then
  echo "error: not a directory: $ASSETS_DIR" >&2
  exit 2
fi

cd "$ASSETS_DIR"

if [[ ! -f SHA256SUMS ]]; then
  echo "error: SHA256SUMS not found in $ASSETS_DIR" >&2
  exit 1
fi

echo "==> Verifying checksums (offline)"
if ! sha256sum -c SHA256SUMS; then
  echo "error: checksum verification failed" >&2
  exit 1
fi
echo "Checksums OK."

if [[ "$ONLINE" != true ]]; then
  echo "Offline verification complete. Pass --online to also verify build/SBOM attestations."
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "error: --online requires the GitHub CLI (gh), which was not found on PATH" >&2
  exit 1
fi

echo "==> Verifying attestations (online, repo: $REPO)"
FAILED=0
while IFS= read -r -d '' file; do
  name="$(basename "$file")"
  [[ "$name" == "SHA256SUMS" ]] && continue
  [[ "$name" == *.spdx.json ]] && continue
  echo "--- $name ---"
  if ! gh attestation verify "$file" --repo "$REPO" --source-ref refs/heads/main; then
    echo "error: attestation verification failed for $name" >&2
    FAILED=1
  fi
done < <(find . -maxdepth 1 -type f -print0)

if [[ "$FAILED" -ne 0 ]]; then
  echo "One or more attestation verifications failed." >&2
  exit 1
fi

echo "All attestation verifications passed."
