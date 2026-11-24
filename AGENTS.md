# AGENTS.md

eGovFrame Launcher follows the OpenForge agent engineering model: https://github.com/dasomel/openforge/blob/main/docs/agent-engineering.md

- Preserve the boundary between UI, launcher/domain workflow, process execution, filesystem access, and environment/toolchain detection.
- Treat process execution, filesystem mutation, credential/config handling, installer behavior, public CLI/API changes, and destructive cleanup as design changes.
- Keep long-running setup tasks observable with progress, logs, retry/cancel, and actionable recovery.
- `make verify` at the repository root is the canonical local baseline (gofumpt check against the explicit legacy-debt baseline + `go vet` + short Go tests + current-platform build); run `make cross` when platform-specific compilation can be affected.
- `launcher/.gofumpt-baseline` may contain only known pre-existing formatter debt. New formatter drift fails, and a resolved baseline entry also fails until the obsolete exception is removed.
- `make test` uses Go short mode so Docker-backed integration tests do not make the baseline environment-dependent; exercise integration/runtime targets explicitly when they matter.
- The Go baseline does not prove Maven/JDK/Tomcat/Docker/npm or browser/runtime behavior; exercise the real target lifecycle when those properties matter.
- Shared/production/destructive/release/credential/permission/external mutations require explicit authorization.
