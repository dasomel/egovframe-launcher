# DESIGN-ko.md

[English](DESIGN.md) | 한국어

## 제품 아키타입 (Product archetype)

`archetype: Developer Tool`

egovframe-launcher는 전자정부 표준프레임워크(eGovFrame) 워크스페이스를 위한 개발자 생산성 런처 및 런타임 코디네이터입니다.

## 제품 성격 (Personality)

- **밀도 (Density):** 높음 (High — 런처 제어 버튼 및 상태 로그 레이아웃)
- **시각적 비중:** 상태 배지 중심의 깔끔한 관리자/개발자 UI
- **강조 색상:** 코리아 거브 블루 (`#1d4ed8`) 및 런타임 상태 지표

## 시맨틱 토큰 매핑 (Token mapping)

```yaml
tokens:
  bgCanvas: var(--of-color-bg-canvas, #0f172a)
  bgSurface: var(--of-color-bg-surface, #1e293b)
  bgSurfaceRaised: var(--of-color-bg-surface-raised, #334155)
  textPrimary: var(--of-color-text-primary, #f8fafc)
  textSecondary: var(--of-color-text-secondary, #94a3b8)
  textMuted: var(--of-color-text-muted, #64748b)
  borderDefault: var(--of-color-border-default, #334155)
  accentPrimary: var(--of-color-accent-primary, #3b82f6)
  danger: var(--of-color-status-danger, #ef4444)
  success: var(--of-color-status-success, #22c55e)
```
