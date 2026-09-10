# OpenForge adoption

Follow the canonical OpenForge standards:
- https://github.com/dasomel/openforge/blob/main/docs/model-agnostic-agent-instructions.md
- https://github.com/dasomel/openforge/blob/main/docs/agent-engineering.md
- https://github.com/dasomel/openforge/blob/main/docs/user-centric-validation.md

Keep launcher workflow, process/filesystem, toolchain detection, installer, and platform invariants local. Model/tool files are thin adapters. For install/configuration, process execution, toolchain lifecycle, browser/UI, cross-platform, and upgrade changes, use risk-proportional validation against the real target lifecycle where mocks or short Go tests cannot prove behavior. Confirmed user-visible defects become regression evidence.

Safe local/disposable work within scope may proceed autonomously. Shared/production mutation, destructive external actions, release/publish, credential/permission widening, or unrelated external mutation requires explicit authorization unless already granted.
