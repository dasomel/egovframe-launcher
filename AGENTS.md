# AGENTS.md

eGovFrame Launcher follows the OpenForge context-efficient agent engineering model.

Read `README.md`, architecture/design docs, launcher workflow docs, and the relevant issue/spec before editing.

- Make the smallest coherent change that solves the requested problem.
- Do not auto-fix unrelated findings; report them separately.
- Preserve the boundary between UI, launcher/domain workflow, process execution, filesystem access, and environment/toolchain detection.
- Treat process execution, filesystem mutation, credential/config handling, installer behavior, public CLI/API changes, and destructive cleanup as design changes.
- Keep long-running setup tasks observable with progress, logs, retry/cancel, and actionable recovery.
- Let formatter/linter rules own deterministic style. Comments explain why, invariants, hazards, or compatibility constraints.
- For bugs, prefer: reproduce -> failing test/evidence -> minimal fix -> same test passes -> relevant regression suite.
- Use real toolchain/process verification when mocked tests cannot prove installation or launcher behavior.
- Do not claim completion without stating which checks actually ran and their scope.
- End substantive work as A) complete/verified, B) meaningful verified progress with the next blocker isolated, or C) stop with evidence when further work requires unjustified scope, fragile patches, unsupported assumptions, or unacceptable risk.

Reference: https://github.com/dasomel/openforge/blob/main/docs/agent-engineering.md
