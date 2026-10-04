# Issue #54 replay: egovframe-launcher-target-workflow (2026-10-04)

Fresh session (claude-sonnet-5-5, Claude Code subagent). Worktree `egovframe-launcher-wt-54`, HEAD `ba58dbd` (catalog port-uniqueness test). Go toolchain: go1.27.1 darwin/arm64.

## Plan

1. Activate from the SKILL.md description, then follow the workflow on a scratch-only target change.
2. Edge case: break port isolation (the skill's step 5 / Stop rule) and check the repo's own test fails.
3. Run `make verify`, confirm `git status` shows only the two deliverables, then write the evidence.

## Step 1: Activation

Only one skill exists: `.agents/skills/egovframe-launcher-target-workflow/SKILL.md`. The description (frontmatter) starts "Add or change an eGovFrame Launcher target across clone/build/run/stop/open/log behavior, JDK/Tomcat/Docker prerequisites, port isolation...". Version string: `openforge-version: "1"`.

Representative task chosen from the description: "Add a new Spring Boot target card with its own port to the launcher." It matches the description (target lifecycle change, Spring Boot, port isolation), so the skill activates from the description alone. The one-skill listing means there is no competing-skill ambiguity, so this does not test discrimination between skills.

I then read the skill and followed its workflow. The skill pointed at `launcher/internal/catalog/catalog.go` `Targets()`, which is accurate: `Target` struct, `DeployType`, `Port` and `Run[].Port` fields exist as described.

## Step 2: Happy path

Scratch change: insert a `scratch-boot` Target (`DeployType: "boot"`, `Port: 8097`, mirrors `boot-sample`) at the top of `Targets()`. `grep -c 8097` on catalog.go gave 0 beforehand, so the port was unused.

```
cd launcher && go test -count=1 ./internal/catalog/ ./internal/runner/ ./internal/server/
EXIT=0
ok  egovframe-launcher/internal/catalog 0.092s
ok  egovframe-launcher/internal/runner  5.179s
ok  egovframe-launcher/internal/server  0.556s
```

Revert: `git checkout -- launcher/internal/catalog/catalog.go`. `git diff --stat` empty afterwards.

Scope of this evidence: a new boot target with a unique port passes catalog, runner and server tests. It does NOT show the target clones, builds, runs, or becomes ready (see Unverified).

## Step 3: Edge case (port isolation)

Hazard: skill workflow step 5 and Stop rule ("couple one target's runtime/ports/state to another"). Guard: `TestPortsUniqueAcrossCatalog` in `launcher/internal/catalog/catalog_test.go`, added in HEAD `ba58dbd`.

Baseline: `go test ./internal/catalog/` EXIT=0 (`ok ... (cached)`).

Mutation A, target port collision (`web-sample` Port 8091 -> 8090, catalog.go line 103):

```
EXIT=1
--- FAIL: TestPortsUniqueAcrossCatalog (0.00s)
    catalog_test.go:62: port 8090 collides: boot-sample (target port) and web-sample (target port)
FAIL	egovframe-launcher/internal/catalog	0.091s
```

Mutation B, Run-step dependency collision (msa ConfigServer Port 8888 -> 8094, line 181, which equals `enterprise-business`'s target port):

```
EXIT=1
--- FAIL: TestPortsUniqueAcrossCatalog (0.00s)
    catalog_test.go:62: port 8094 collides: enterprise-business (target port) and msa (run[0] java)
FAIL	egovframe-launcher/internal/catalog	0.083s
```

Each mutation was reverted with `git checkout -- launcher/internal/catalog/catalog.go`. After both reverts `go test ./internal/catalog/` gave EXIT=0 (`ok ... (cached)`; the cache hit is expected because the file content equals the baseline).

Scope: the guard catches static duplicate ports declared in the catalog. It does not detect a port already bound by a non-launcher process on the host, nor WAR targets sharing a Tomcat instance (the skill's second isolation clause); I did not test either.

## Step 4: Repository verification entrypoint

`make verify` (repo root), EXIT=0. Stages seen in output: `fmt-check`, `go vet ./...`, `go test -short ./...` (catalog, logbuf, runner, server `ok`, cached; packages without tests reported as such), `go build` producing `launcher/egov-launcher` (ignored by `.gitignore:6`, so not tracked).

`make cross` was not run (no platform-specific compilation changed; scratch changes were reverted).

## Final working-tree state

`git status --short` after all reverts and `make verify` showed only the build artifact as ignored (no tracked or untracked changes before these two deliverables were written).

## Unverified

- No real target lifecycle (git clone, `mvn`/JDK build, run, HTTP readiness, open, stop) was exercised in this replay. The skill's maturity note cites an earlier replay (egovframe-launcher#9) for that; I did not re-verify it.
- No Docker-backed infrastructure target, Tomcat/WAR runtime instance isolation, or live host port-in-use behavior was tested.
- Skill workflow steps 4 (idempotent setup), 6 (JDK detection) and 7 (readiness-before-success logging) were not exercised.
- `make cross` not run.
- Activation was assessed against a single-skill directory only.
