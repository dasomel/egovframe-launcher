---
name: egovframe-launcher-target-workflow
description: Add or change an eGovFrame Launcher target across clone/build/run/stop/open/log behavior, JDK/Tomcat/Docker prerequisites, port isolation, process/filesystem boundaries, and verification. Use for Spring Boot, WAR/Tomcat, React, MSA, AI, infrastructure-provisioning, or launcher target lifecycle changes.
license: Apache-2.0
compatibility: Requires the eGovFrame Launcher checkout, Go toolchain, and the target-specific JDK/Maven/npm/Docker/Tomcat prerequisites documented by the repository.
metadata:
  openforge-scope: project
  openforge-owner: dasomel/egovframe-launcher
  openforge-maturity: verified
  openforge-version: "1"
---

# eGovFrame Launcher Target Workflow

## Use When

- Adding or changing a launcher target/card or its clone/build/run/stop/open/log lifecycle.
- Changing target-specific JDK, Maven/Gradle, Tomcat/WAR, npm, Docker infrastructure, schema-loading, port, or workspace behavior.
- Fixing launcher behavior that crosses process execution, filesystem state, toolchain detection, or browser-accessible services.

## Do Not Use When

- Pure documentation changes with no launcher behavior impact.
- A change belongs to an upstream eGovFrame sample repository rather than the launcher integration.

## Inputs

- Target type: Spring Boot, WAR/Tomcat, React, MSA, AI, or another explicitly supported category.
- Required toolchain and infrastructure dependencies.
- Expected lifecycle, default port, readiness condition, and user-visible URL.

## Workflow

1. Read `AGENTS.md`, `README.md`, the relevant launcher workflow/architecture docs, and the issue/spec.
2. Find the nearest existing target of the same lifecycle type in `launcher/internal/catalog/catalog.go`'s `Targets()` registry (match by `DeployType`: `boot`, `war`, `react`, or `lib`) and extend that `Target` entry plus the matching `DeployType` branch in `launcher/internal/runner` instead of adding a parallel execution path. There is no separate `target` package/module; `Target` is a struct defined in `internal/catalog` and consumed by `internal/runner` (process lifecycle) and `internal/server` (HTTP/UI wiring).
3. Keep process execution, filesystem mutation, toolchain detection, and UI boundaries separated. Treat public CLI/API changes, installer behavior, credentials/config, and destructive cleanup as design changes.
4. Make target setup idempotent where it provisions Docker services, schema/data, workspaces, or isolated Tomcat instances.
5. Preserve target isolation: allocate/configure ports so one target does not silently collide with another; WAR targets use their own runtime instance unless the design explicitly says otherwise.
6. Detect required JDK/tools through the existing launcher mechanism rather than hardcoding a local machine path.
   When replaying or testing against a disposable workspace, note that a persisted `WorkspacePath` in `~/.egov-launcher.json` overrides the `-workspace` flag (#17); confirm the effective workspace before cloning.
7. Keep long-running build/run/setup operations observable with logs/progress and actionable failure state; do not print success before readiness is measured.
8. Add/update tests for pure Go/domain behavior and use a real toolchain/process path when the change depends on Maven, Java, Tomcat, Docker, npm, browser reachability, or external process lifecycle.
9. Run `make verify` from the repository root. Run `make cross` when the change can affect platform-specific compilation or release binaries.
10. Verify the changed target through the relevant end-to-end lifecycle (clone/build/run/readiness/open/stop as applicable) when claiming runtime correctness.

## Verification

`make verify` is the canonical deterministic local baseline: gofumpt check against the explicit `.gofumpt-baseline`, `go vet`, short-mode Go tests, and current-platform build. The formatter baseline is limited to known pre-existing debt: any new drift fails, and a resolved baseline entry also fails until removed. `make test` intentionally excludes Docker-backed integration paths so the baseline remains reproducible; use the target-specific integration/runtime path when that behavior matters. Separate baseline evidence from target runtime/toolchain/browser evidence. A successful Go build does not prove Tomcat deployment, Docker infrastructure, JDK compatibility, or service readiness.

Maturity `verified` (2026-09-24): a fresh-session replay recorded in dasomel/egovframe-launcher#9 exercised `make verify`, a real `boot-sample` clone/build/run/HTTP-readiness/stop lifecycle, and a Docker-down `simple-backend` failure that surfaced an actionable prerequisite error. Re-run a replay before relying on this after lifecycle, prerequisite, or port-allocation changes.

## Stop / Escalate When

- The target requires credential embedding, unsafe filesystem mutation, destructive cleanup outside its owned workspace, or a new installer/process authority not covered by design.
- Correctness depends on an external toolchain that cannot be exercised and no deterministic substitute proves the behavior.
- A change would couple one target's runtime/ports/state to another target instead of preserving isolation.

## References

- `AGENTS.md`
- `README.md`
- root `Makefile`
- `launcher/Makefile`
- `launcher/internal/catalog/catalog.go` (`Target` struct, `Targets()` registry, `DeployType`)
- `launcher/internal/runner/` (process lifecycle branching on `DeployType`)
- `launcher/internal/server/` (HTTP/UI wiring over `catalog.Target`)
- `scripts/`
