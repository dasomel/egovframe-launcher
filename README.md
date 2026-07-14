# 전자정부 표준프레임워크 VSCode 런처 및 스크립트 툴킷 (egovframe-launcher)

![License](https://img.shields.io/badge/License-Apache%202.0-lightgrey)

전자정부 표준프레임워크 소스 코드를 VSCode 환경에서 손쉽게 빌드, Tomcat/Docker 연동 기동 및 디버깅을 제어하기 위한 단일 바이너리 런처(GUI 대시보드) 및 보조 CLI 셸 스크립트 툴킷입니다.


## 구성 요소

*   `launcher/` — Go 기반 단일 바이너리 로컬 런처 (GUI 대시보드)
*   `scripts/` — 백엔드 연동용 CLI 헤드리스 스크립트 (sh 및 ps1 파일)
*   `.vscode/` — 실습 공간 내 추천 플러그인 및 디버깅 템플릿 환경 설정

## 빠른 시작

### 방법 A: 대시보드 런처
```bash
cd launcher
go run .      # 127.0.0.1:7070 대시보드 브라우저 구동
```

**런처 주요 기능**
- 각 타깃 카드: Clone / Build / ▶ Run(Boot·React) / ■ Stop / Open / 로그(SSE) / VSCode 버튼
- **WAR 타깃** — **▶ Tomcat 기동**: `mvn package` 후 타깃 전용 격리 Tomcat 인스턴스에 자동 배포(HTTP·shutdown 포트 자동 할당, 타깃 간 충돌 없음)
- **Tomcat 연동 안내** 모달: Community Server Connectors 확장 설치 버튼 내장(`redhat.vscode-community-server-connector`)
- 설정 패널(~/.egov-launcher.json): Tomcat 설치 경로 · VSCode `code` 경로(자동 감지) · 작업 디렉터리 · skip-tests · **JDK 자동 감지 및 선택**(기본 JDK 17)
- 포트 충돌 자동 회피 + 타깃별 커스텀 포트 설정
- darwin / windows 크로스 빌드(`make cross`)

### 방법 B: CLI 셸 스크립트
```bash
bash scripts/00-check-prereqs.sh
bash scripts/01-clone.sh
bash scripts/10-run-boot-sample.sh
```

## 실행 대상 예제 (Tier 분류)

| Tier | 레포 ID | 기동 성격 |
| :--- | :--- | :--- |
| **Tier 1 (즉시 기동)** | `boot-sample`, `simple-react` | 즉시 실행 가능 (추가 WAS 불필요) |
| **Tier 1 (WAR 배포)** | `web-sample`, `homepage`, `portal`, `enterprise-business`, `common-components` | 런처에서 ▶ Tomcat 기동(격리 인스턴스 자동배포) |
| **Tier 2 (라이브러리)** | `runtime` | mvn install 패키징 테스트 전용 |
| **Tier 3 (인프라)** | `msa`, `ai-rag` | Docker 컨테이너 / Ollama 로컬 기동 필요 |

## 참고 링크
*   [eGovFrame 공식 GitHub](https://github.com/eGovFramework)
*   [eGovFrame VSCode Initializr 확장 레포지토리](https://github.com/eGovFramework/egovframe-vscode-initializr)
