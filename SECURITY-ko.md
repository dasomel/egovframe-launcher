# 보안 정책 (Security Policy)

[English](SECURITY.md) | 한국어

## 지원 대상 버전

| 버전 | 지원 여부 |
| ---- | -------- |
| v0.x | :white_check_mark: |

## 보안 범위

`egovframe-launcher`는 로컬 개발자 환경에서 프로세스를 실행하고 JDK/IDE 환경 경로를 구성합니다.
- 내장 HTTP 제어 서버는 로컬 루프백(`127.0.0.1`)에만 바인딩되며 외부 공용 네트워크에 노출되지 않습니다.
- 실행되는 명령어 및 파일 경로 입력값은 명령어 삽입(command injection) 방지를 위해 철저히 살균 및 검증됩니다.

## 취약점 보고 절차 (Reporting a Vulnerability)

보안 취약점은 공개 이슈로 등록하지 마시고, GitHub Private Vulnerability Reporting을 통해 비공개로 보고해 주십시오. 48시간 이내에 접수 확인 및 조치 계획을 안내합니다.

참조: [OpenForge Security Standard](https://github.com/dasomel/openforge/blob/main/docs/security.md)
