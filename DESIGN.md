# DESIGN.md

English | [한국어](DESIGN-ko.md)

## Product archetype

`archetype: Developer Tool`

egovframe-launcher is a developer productivity launcher and runtime coordinator for electronic government framework (eGovFrame) workspaces.

## Product personality

- **Density:** High (compact launcher controls and status logs)
- **Visual weight:** Clean administrative developer UI with status badges
- **Accent:** Korea Gov blue (`#1d4ed8`) and operational status indicators

## Token mapping

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
