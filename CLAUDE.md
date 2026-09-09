@AGENTS.md

# eGovFrame Launcher Claude adapter

For launcher target lifecycle changes across clone/build/run/stop/open/log, JDK/Tomcat/Docker/npm prerequisites, isolated ports, or process/filesystem behavior, load `.agents/skills/egovframe-launcher-target-workflow/SKILL.md`.

Use root `make verify` as the deterministic baseline and add target-specific real toolchain/runtime evidence when the property cannot be proven by Go tests/builds.

Keep Claude-specific orchestration outside the portable project skill; repository boundaries remain in `AGENTS.md`.
