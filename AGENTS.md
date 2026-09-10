# AGENTS.md

eGovFrame Launcher follows the OpenForge model-agnostic agent engineering model.

Inspect repository guidance, architecture/design context, launcher workflow docs, project skills, and the issue/spec relevant to the current task before editing. Load only the project skill that matches the task.

- Make the smallest coherent change that solves the requested problem.
- Do not auto-fix unrelated findings; report them separately.
- Preserve the boundary between UI, launcher/domain workflow, process execution, filesystem access, and environment/toolchain detection.
- Treat process execution, filesystem mutation, credential/config handling, installer behavior, public CLI/API changes, and destructive cleanup as design changes.
- Keep long-running setup tasks observable with progress, logs, retry/cancel, and actionable recovery.
- Let formatter/linter rules own deterministic style. Comments explain why, invariants, hazards, or compatibility constraints.
- For bugs, prefer: reproduce -> failing test/evidence -> minimal fix -> same test passes -> relevant regression suite.
- Use real toolchain/process verification when mocked tests cannot prove installation or launcher behavior.
- `make verify` at the repository root is the canonical local baseline (gofumpt check against the explicit legacy-debt baseline + `go vet` + short Go tests + current-platform build); run `make cross` when platform-specific compilation can be affected.
- `.gofumpt-baseline` may contain only known pre-existing formatter debt. New formatter drift fails, and a resolved baseline entry also fails until the obsolete exception is removed.
- `make test` intentionally uses Go short mode so Docker-backed integration tests do not make the deterministic baseline environment-dependent; exercise integration/runtime targets explicitly when they matter.
- Do not imply that the Go baseline proves Maven/JDK/Tomcat/Docker/npm or browser/runtime behavior; exercise the relevant real target lifecycle when those properties matter.
- Choose verification proportional to task risk and user impact. Safe local/disposable inspect-edit-build-test-fix-retest work may proceed within scope; shared/production/destructive/release/credential/permission/external mutations require explicit authorization unless already granted.
- Do not claim completion without stating which checks actually ran and their scope.
- End substantive work as A) complete/verified, B) meaningful verified progress with the next blocker isolated, or C) stop with evidence when further work requires unjustified scope, fragile patches, unsupported assumptions, or unacceptable risk.

References:
- https://github.com/dasomel/openforge/blob/main/docs/agent-engineering.md
- https://github.com/dasomel/openforge/blob/main/docs/model-agnostic-agent-instructions.md
- https://github.com/dasomel/openforge/blob/main/docs/user-centric-validation.md
- https://github.com/dasomel/openforge/blob/main/docs/agent-skills.md
