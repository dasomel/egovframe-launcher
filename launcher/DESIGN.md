---
name: eGovFrame Launcher
description: Developer control panel for running eGovFrame samples from VSCode
colors:
  primary: "#0B5FFF"
  ink: "#1A1C1E"
  muted: "#6C7278"
  surface: "#FFFFFF"
  canvas: "#F5F7FA"
  border: "#E2E6EC"
  success: "#1E9E6A"
  warning: "#C9821A"
  danger: "#C8402E"
  logbg: "#0E1116"
  logfg: "#D7DCE2"
typography:
  h1:
    fontFamily: "Pretendard, system-ui, sans-serif"
    fontSize: 1.5rem
    fontWeight: "700"
  body:
    fontFamily: "Pretendard, system-ui, sans-serif"
    fontSize: 0.95rem
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: 0.8rem
rounded:
  sm: 6px
  md: 10px
spacing:
  sm: 8px
  md: 16px
  lg: 24px
---

## Overview

기능적 미니멀리즘. 개발자 대시보드로서 정보 밀도와 상태 가독성을 우선한다.
카드 = 레포 한 개, 배지 = 상태, 우측 = 로그 콘솔. 장식 최소화.

## Colors

- **Primary (#0B5FFF):** 실행/주요 액션 버튼.
- **Ink (#1A1C1E):** 본문 텍스트.
- **Muted (#6C7278):** 보조 텍스트·메타.
- **Canvas (#F5F7FA) / Surface (#FFFFFF):** 배경/카드.
- **Success/Warning/Danger:** 상태 배지(running/prereq 부족/error).
- **logbg/logfg:** 로그 콘솔 명암.

## Typography

Pretendard 우선(없으면 system-ui). 로그는 모노스페이스.

## Components

- **button-primary:** background `{colors.primary}`, text `#fff`, radius `{rounded.sm}`.
- **badge:** radius 999px, 상태별 색.
- **card:** surface 배경, `{colors.border}` 1px, radius `{rounded.md}`, padding `{spacing.md}`.

## Do's and Don'ts

- Do: 상태를 색+텍스트로 동시에 전달(색맹 대응).
- Don't: 애니메이션/그라데이션 남용. 발표 가독성 저해.
