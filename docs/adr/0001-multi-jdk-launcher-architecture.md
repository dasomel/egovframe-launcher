# ADR-0001: Multi-JDK and Workspace Isolation Architecture

- Status: Accepted
- Date: 2026-08-28

## Context
Developers working with electronic government frameworks often need to switch between legacy (JDK 8/eGov 3.x) and modern (JDK 17+/eGov 4.x) environments without global environment pollution.

## Decision
Adopt a standalone Go daemon launcher that dynamically spawns JDK runtimes and IDE instances with process-isolated environment variables.

## Consequences
- Clean developer workstation state without requiring global JAVA_HOME overrides.
- Multi-version coexistence with deterministic process lifecycle management.
