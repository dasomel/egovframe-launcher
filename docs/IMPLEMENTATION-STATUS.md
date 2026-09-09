# Implementation Status

Last verified: 2026-09-09 against `main`

This snapshot describes behavior implemented on the default branch.

## Implemented

- Local GUI launcher that drives eGovFrame example repositories from clone through build, run, stop, logs, and browser access.
- Go single-binary launcher with browser dashboard and configurable workspace/JDK/Tomcat settings.
- Spring Boot direct-run flows and isolated per-target Tomcat deployment for WAR applications with separate HTTP/shutdown ports.
- Dependency-service provisioning for targets that need MySQL, Redis, RabbitMQ, or Docker-based supporting services.
- JDK discovery/selection, SSE build/runtime log streaming, configurable target ports, and VS Code integration helpers.
- Target catalog covering Spring Boot, WAR, React/Vite, common-component/MSA examples, with macOS/Windows release artifacts and cross-build targets documented in the repository.

## Partial / environment-dependent

- Individual eGovFrame targets depend on external JDK/Maven/npm/Docker/Tomcat prerequisites and upstream repository behavior.
- Successful launcher unit tests/builds do not by themselves prove every upstream sample remains runnable after upstream changes; target-level verification is a separate evidence class.

## Not claimed

- The launcher is a developer convenience/runtime orchestrator, not an eGovFrame distribution or replacement for the upstream projects.
- Support for a target should not be inferred beyond the target catalog and executable paths present on `main`.

## Evidence

- `README.md`
- `launcher/`
- `scripts/`
- release/build workflows
- launcher tests and documented `make test` / `make cross` commands
