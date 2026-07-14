# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.2] - 2026-07-15

### Fixed
- **Embedded-DB Seed Data Corruption (Windows)**: Korean seed data in embedded-database WAR templates (e.g. simple-homepage's HSQLDB `shtdb.sql`) rendered as mojibake when Tomcat ran under the RSP backend, because Spring's `<jdbc:script>` reads seed SQL with the JVM default charset (MS949 on Korean Windows). The launcher now injects `encoding="UTF-8"` into `<jdbc:script>` elements across cloned WAR projects before build/deploy. MySQL-backed templates (portal, enterprise) were unaffected since their schemas are imported via the container's `utf8mb4` client.

## [1.0.1] - 2026-07-15

### Fixed
- **Windows RSP Auto-Start**: Hardened RSP backend discovery on Windows — filter out processes with a null `CommandLine` in the WMI query (which previously made the whole PowerShell pipeline fail) and broadened RSP process matching.
- **Korean Log Corruption (VSCode)**: Tomcat console log encoding is now aligned with the RSP backend JVM's default charset (MS949 on Korean Windows for JDK ≤ 17), so server logs streamed into VSCode's output view render Korean correctly.
- **Korean Log Corruption (Launcher UI)**: Child JVMs (Maven, Spring Boot) on Windows are forced to UTF-8 output via `JAVA_TOOL_OPTIONS`; isolated `CATALINA_BASE` copies pin their console handler to UTF-8 to match the launcher's UTF-8 web UI.
- **Docker Container Recovery**: Containers stuck in `Created` state (e.g. after a failed port bind on `docker run`) are now removed and recreated instead of endlessly retrying `docker start`; host-port conflicts (such as a locally installed MySQL service on 3306) fail fast with a clear message before pulling images, and `docker start` failures include the daemon's error output.
- **VSCode Extension Detection**: Extension status/install now uses the `bin\code.cmd` CLI shim when the configured VSCode path points at the GUI binary (`Code.exe`), which silently ignores `--list-extensions`/`--install-extension` — previously the launcher kept reporting an already-installed Server Connector extension as missing and could not install it.
- **RSP Fallback Diagnostics**: When the RSP backend is not detected, the file-based fallback now logs why instead of proceeding silently.

## [1.0.0] - 2026-07-14

### Added
- **Developer Control Panel (egov-launcher)**: Go-based lightweight GUI dashboard to run and monitor eGovFrame targets.
- **Isolate Tomcat Instance Run**: Automatically package WAR targets using `mvn package` and deploy them to target-specific isolated Tomcat instances with auto-assigned HTTP and shutdown ports to prevent port conflicts.
- **Docker Integration**: Idempotent provisioning of shared database/messaging middleware:
  - MySQL 8.0 (auto-importing schema scripts in execution order, preventing race conditions via authenticated query ping checks)
  - Redis 7-alpine (configured with credentials where required)
  - RabbitMQ 3 (configured with a pre-created valid `.erlang.cookie` file, owned by `rabbitmq:rabbitmq` with `600` permissions to avoid start failures)
- **Automatic JDK Discovery**: Scan system JDKs and select JDK 17 as the default target, falling back to the highest major version.
- **Live Logging**: Log streaming via Server-Sent Events (SSE) directly to the dashboard console.
- **VSCode Workspace Integration**: Provide one-click button to open targets inside VSCode (`code`) and check required extensions (e.g. RedHat Server Connector).
- **Multi-Platform Native Builds**: Cross-compilation support in `Makefile` for macOS (`darwin-amd64`, `darwin-arm64`) and Windows (`windows-amd64`, `windows-arm64`).
- **CI/CD Pipeline**: GitHub Actions workflow for automatic unit tests, packaging, and draft release generation for tag triggers.
