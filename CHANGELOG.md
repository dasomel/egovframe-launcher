# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
