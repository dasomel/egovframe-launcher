# Security Policy

English | [한국어](SECURITY-ko.md)

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v0.x    | :white_check_mark: |

## Security Scope

`egovframe-launcher` executes local processes and configures JDK/IDE environment paths.
- Local HTTP endpoints are bound to `127.0.0.1` and not exposed to the public network.
- Executed commands and path inputs are sanitized to prevent command injection.

## Reporting a Vulnerability

Please report security issues responsibly via GitHub Private Vulnerability Reporting or by contacting maintainers directly.

Reference: [OpenForge Security Standard](https://github.com/dasomel/openforge/blob/main/docs/security.md)
