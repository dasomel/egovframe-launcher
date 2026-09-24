# Supply Chain

Last updated: 2026-09-24. Tracks GitHub issue #1 (Narwhal #165 applied to this repo).

## Inputs inventory

| Input | Pin | Notes |
| :--- | :--- | :--- |
| Go toolchain | `1.26.5` (exact) | `.github/workflows/build.yml`, `actions/setup-go` |
| gofumpt | `v0.12.0` (exact) | installed via `go install mvdan.cc/gofumpt@v0.12.0` |
| GitHub Actions | full commit SHA + `# vX.Y.Z` comment | every `uses:` in `.github/workflows/*.yml` |
| Runner images | `macos-15` (build/test), `ubuntu-24.04` (release, dependency review) | fixed image labels, no `-latest` |
| Apache Tomcat | `10.1.25` zip, SHA-512 pinned | offline pre-placed at `~/.egov-launcher/apache-tomcat-10.1.25.zip`; hardened in a parallel change |
| Docker images (MySQL, RabbitMQ) | digest-pinned | hardened in a parallel change |
| eGovFramework sample repositories | upstream HEAD (no pin) | **accepted, documented exception** — these are user-facing sample sources the launcher clones for the developer to run, not launcher build inputs; pinning them would defeat their purpose as up-to-date examples |

`launcher/go.mod` currently declares no third-party Go dependencies, so there is no Go module dependency graph to pin beyond the toolchain itself.

## Cooling policy

`.github/dependabot.yml` applies a 14-day cooldown to both the `gomod` and `github-actions` ecosystems: a routine dependency update PR is not opened until the new version has been publicly available for 14 days. This is the window in which a compromised release is typically detected and yanked. The same 14-day-minimum-age rule is applied manually whenever an action pin in this document or in a workflow is bumped by hand — check `gh release list -R <owner>/<repo>` and pick a release published at least 14 days before the pin date.

## Release integrity pipeline

On every push to `main`/`master` that reaches the `release` job in `.github/workflows/build.yml`. Same-repository pull requests run the same job as a dry run (steps 1–5, no tag or publish), so egress, SBOM, attestation, and the verify gate are proven before merge:

1. **Harden Runner** (`step-security/harden-runner`, `egress-policy: block`) restricts the job to an explicit allow-list of endpoints (GitHub API/upload/artifact hosts and the Sigstore services used by attestation). macOS runners are not supported by harden-runner, so only the `ubuntu-24.04` release job is egress-restricted; the `macos-15` build job is not.
2. **SBOM**: `anchore/sbom-action` (Syft) scans `launcher/` and produces `sbom.spdx.json` (SPDX JSON). Chosen over a `go install`-based generator so SBOM generation needs no `proxy.golang.org` egress on the hardened runner.
3. **Checksums**: `SHA256SUMS` is generated over the packaged release binaries (`dist/*.tar.gz` / `dist/*.zip`).
4. **Attestations**: `actions/attest-build-provenance` attests the packaged binaries' build provenance (which workflow run, commit, and source built them); `actions/attest-sbom` attests that `sbom.spdx.json` applies to those same binaries. Both are signed via GitHub's Sigstore-backed attestation API (Fulcio/Rekor) and published to the repository's attestation store.
5. **Gate**: before anything is published, the job re-verifies `SHA256SUMS` and runs `gh attestation verify` on every packaged binary against this repository. Any failure fails the job, so a tampered or unattested artifact never reaches the GitHub Release.
6. **Publish**: `softprops/action-gh-release` uploads the binaries, `SHA256SUMS`, and `sbom.spdx.json` as release assets.

Pull requests additionally run `.github/workflows/dependency-review.yml` (`actions/dependency-review-action`), which fails on `high`+ severity vulnerabilities and posts a diff summary comment on failure, so dependency changes are visible in review before merge.

## Verifying a release

### Online (recommended)

Requires the [GitHub CLI](https://cli.github.com/) (`gh`), authenticated.

```bash
mkdir release-assets && cd release-assets
gh release download <tag> -R dasomel/egovframe-launcher
cd ..
scripts/verify-release.sh release-assets --online
```

This checks `SHA256SUMS` and runs `gh attestation verify` on each binary against `dasomel/egovframe-launcher`, confirming both that the file wasn't tampered with and that it was actually built by this repository's release workflow. It passes `--source-ref refs/heads/main`, because same-repo PR dry runs also publish attestations for their (unreleased) builds; only artifacts built from `main` are accepted.

### Offline

No network access required beyond the initial download:

```bash
scripts/verify-release.sh release-assets
```

This checks only `SHA256SUMS`. A matching `scripts/verify-release.ps1` is available for Windows/PowerShell with the same `-Online` switch; it has not been exercised in CI (no Windows runner in this pipeline) and should be treated as best-effort until run in a real PowerShell session.

## Last-known-good and rollback

The last-known-good state is the previous published GitHub Release, together with the `SHA256SUMS`, `sbom.spdx.json`, and attestations attached to it — all retrievable via `gh release download <previous-tag>` and verifiable offline/online as above.

If a release is later found to be compromised (bad dependency, tampered pin, failed attestation):

1. Revert the commit(s) that introduced the bad pin/dependency on `main`.
2. Bump `CHANGELOG.md` with a new version entry describing the rollback; the release job tags and publishes it automatically on merge.
3. Users on the compromised version re-download the new (or previous) release and re-run `scripts/verify-release.sh --online` before deploying it.
4. The compromised release itself is not silently deleted — mark it as **not recommended** in its release notes so the attestation history stays intact for forensics.

## Related documentation

- [`.github/dependabot.yml`](../.github/dependabot.yml) — cooldown configuration
- [`.github/workflows/build.yml`](../.github/workflows/build.yml) — build/release pipeline
- [`.github/workflows/dependency-review.yml`](../.github/workflows/dependency-review.yml) — PR dependency diff gate
- [`scripts/verify-release.sh`](../scripts/verify-release.sh) / [`scripts/verify-release.ps1`](../scripts/verify-release.ps1) — release verification
