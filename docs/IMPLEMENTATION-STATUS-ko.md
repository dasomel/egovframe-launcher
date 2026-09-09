# 구현 상태

Last verified: 2026-09-09 against `main`

이 snapshot은 default branch에 실제 구현된 동작을 설명합니다.

## 구현됨

- eGovFrame example repository를 clone → build → run → stop → log → browser access까지 연결하는 local GUI launcher.
- Browser dashboard와 workspace/JDK/Tomcat 설정을 제공하는 Go single-binary launcher.
- Spring Boot direct-run 및 WAR application별 HTTP/shutdown port를 분리한 isolated Tomcat deployment.
- MySQL, Redis, RabbitMQ, Docker supporting service가 필요한 target의 dependency provisioning.
- JDK discovery/selection, SSE build/runtime log streaming, configurable target port, VS Code integration helper.
- Spring Boot, WAR, React/Vite, common-component/MSA example target catalog 및 repository에 문서화된 macOS/Windows release/cross-build target.

## 부분적 / 환경 의존

- 개별 eGovFrame target은 외부 JDK/Maven/npm/Docker/Tomcat prerequisite와 upstream repository 동작에 의존합니다.
- Launcher unit test/build 성공만으로 upstream 변경 이후 모든 sample이 계속 실행됨을 증명하지 않습니다. Target-level verification은 별도 evidence class입니다.

## 주장하지 않음

- eGovFrame distribution 또는 upstream project 대체물이 아니라 developer convenience/runtime orchestrator입니다.
- `main`에 존재하는 target catalog/executable path를 넘어서는 지원을 주장하지 않습니다.

## Evidence

- `README.md`
- `launcher/`
- `scripts/`
- release/build workflows
- launcher tests 및 `make test` / `make cross` 명령
